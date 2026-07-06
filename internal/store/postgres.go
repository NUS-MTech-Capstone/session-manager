package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresStore struct {
	db  *sql.DB
	now func() time.Time
}

var _ Store = (*PostgresStore)(nil)

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store := &PostgresStore{db: db, now: time.Now}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS proxy_pods (
	pod_id TEXT PRIMARY KEY,
	instance_id TEXT NOT NULL,
	pod_ip TEXT NOT NULL DEFAULT '',
	websocket_url TEXT NOT NULL,
	operations_url TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	active_sessions INTEGER NOT NULL,
	max_sessions INTEGER NOT NULL,
	last_heartbeat_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS automation_runs (
	run_id TEXT PRIMARY KEY,
	incident_id TEXT NOT NULL,
	host_id TEXT NOT NULL,
	owner_pod_id TEXT NOT NULL,
	owner_instance_id TEXT NOT NULL,
	operations_url TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	session_id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	host_id TEXT NOT NULL,
	cr_id TEXT NOT NULL,
	connection_type TEXT NOT NULL DEFAULT 'ssh',
	owner_pod_id TEXT NOT NULL,
	owner_instance_id TEXT NOT NULL,
	websocket_url TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	last_heartbeat_at TIMESTAMPTZ NOT NULL,
	lease_expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS session_audit_records (
	record_id BIGSERIAL PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
	user_id TEXT NOT NULL,
	host_id TEXT NOT NULL,
	cr_id TEXT NOT NULL,
	record_type TEXT NOT NULL,
	sequence BIGINT NOT NULL,
	command_id TEXT NOT NULL DEFAULT '',
	command_text TEXT NOT NULL DEFAULT '',
	output_data BYTEA NOT NULL DEFAULT ''::bytea,
	created_at TIMESTAMPTZ NOT NULL,
	truncated BOOLEAN NOT NULL DEFAULT FALSE,
	byte_count INTEGER NOT NULL DEFAULT 0,
	UNIQUE (session_id, sequence)
);

CREATE INDEX IF NOT EXISTS proxy_pods_assignment_idx
	ON proxy_pods (status, active_sessions, pod_id);

ALTER TABLE sessions
	ADD COLUMN IF NOT EXISTS connection_type TEXT NOT NULL DEFAULT 'ssh';

ALTER TABLE proxy_pods
	ADD COLUMN IF NOT EXISTS operations_url TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS automation_runs_incident_idx
	ON automation_runs (incident_id, created_at DESC);

CREATE INDEX IF NOT EXISTS sessions_rejoin_lookup_idx
	ON sessions (user_id, host_id, cr_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS session_audit_records_session_sequence_idx
	ON session_audit_records (session_id, sequence);

CREATE INDEX IF NOT EXISTS session_audit_records_lookup_idx
	ON session_audit_records (user_id, host_id, cr_id, created_at DESC);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate postgres store: %w", err)
	}
	return nil
}

func (s *PostgresStore) RegisterPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	now := s.now().UTC()
	if pod.Status == "" {
		pod.Status = model.PodStatusReady
	}
	if pod.MaxSessions == 0 {
		pod.MaxSessions = 50
	}
	pod.LastHeartbeatAt = now

	const query = `
INSERT INTO proxy_pods (
	pod_id, instance_id, pod_ip, websocket_url, operations_url, status,
	active_sessions, max_sessions, last_heartbeat_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (pod_id) DO UPDATE SET
	instance_id = EXCLUDED.instance_id,
	pod_ip = EXCLUDED.pod_ip,
	websocket_url = EXCLUDED.websocket_url,
	operations_url = EXCLUDED.operations_url,
	status = EXCLUDED.status,
	active_sessions = CASE
		WHEN proxy_pods.instance_id = EXCLUDED.instance_id
			THEN GREATEST(proxy_pods.active_sessions, EXCLUDED.active_sessions)
		ELSE EXCLUDED.active_sessions
	END,
	max_sessions = EXCLUDED.max_sessions,
	last_heartbeat_at = EXCLUDED.last_heartbeat_at
RETURNING pod_id, instance_id, pod_ip, websocket_url, operations_url, status,
	active_sessions, max_sessions, last_heartbeat_at
`
	var stored model.ProxyPod
	err := scanPod(s.db.QueryRowContext(ctx, query,
		pod.PodID, pod.InstanceID, pod.PodIP, pod.WebsocketURL, pod.OperationsURL,
		pod.Status, pod.ActiveSessions, pod.MaxSessions, pod.LastHeartbeatAt,
	), &stored)
	if err != nil {
		return model.ProxyPod{}, fmt.Errorf("register proxy pod: %w", err)
	}
	return stored, nil
}

func (s *PostgresStore) HeartbeatPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	return s.RegisterPod(ctx, pod)
}

func (s *PostgresStore) AvailablePods(ctx context.Context, maxAge time.Duration) ([]model.ProxyPod, error) {
	if err := s.expireSessions(ctx, s.db, s.now().UTC()); err != nil {
		return nil, err
	}
	const query = `
SELECT pod_id, instance_id, pod_ip, websocket_url, operations_url, status,
	active_sessions, max_sessions, last_heartbeat_at
FROM proxy_pods
WHERE status = $1
	AND last_heartbeat_at > $2
	AND active_sessions < max_sessions
ORDER BY active_sessions, pod_id
`
	rows, err := s.db.QueryContext(ctx, query, model.PodStatusReady, s.now().UTC().Add(-maxAge))
	if err != nil {
		return nil, fmt.Errorf("list available proxy pods: %w", err)
	}
	defer rows.Close()

	pods := make([]model.ProxyPod, 0)
	for rows.Next() {
		var pod model.ProxyPod
		if err := scanPod(rows, &pod); err != nil {
			return nil, err
		}
		pods = append(pods, pod)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list available proxy pods: %w", err)
	}
	return pods, nil
}

func (s *PostgresStore) CreateSession(
	ctx context.Context,
	req model.CreateSessionRequest,
	heartbeatMaxAge time.Duration,
	lease time.Duration,
) (model.Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, fmt.Errorf("begin session assignment: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if err := s.expireSessions(ctx, tx, now); err != nil {
		return model.Session{}, err
	}

	const selectPod = `
SELECT pod_id, instance_id, pod_ip, websocket_url, operations_url, status,
	active_sessions, max_sessions, last_heartbeat_at
FROM proxy_pods
WHERE status = $1
	AND last_heartbeat_at > $2
	AND active_sessions < max_sessions
ORDER BY active_sessions, pod_id
FOR UPDATE SKIP LOCKED
LIMIT 1
`
	var pod model.ProxyPod
	err = scanPod(tx.QueryRowContext(
		ctx,
		selectPod,
		model.PodStatusReady,
		now.Add(-heartbeatMaxAge),
	), &pod)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, ErrNoAvailablePod
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("select proxy pod: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE proxy_pods SET active_sessions = active_sessions + 1 WHERE pod_id = $1`,
		pod.PodID,
	); err != nil {
		return model.Session{}, fmt.Errorf("reserve proxy pod: %w", err)
	}

	id, err := sessionID()
	if err != nil {
		return model.Session{}, err
	}
	if req.ConnectionType == "" {
		req.ConnectionType = "ssh"
	}
	session := model.Session{
		SessionID:       id,
		UserID:          req.UserID,
		HostID:          req.HostID,
		CRID:            req.CRID,
		ConnectionType:  req.ConnectionType,
		OwnerPodID:      pod.PodID,
		OwnerInstanceID: pod.InstanceID,
		WebsocketURL:    fmt.Sprintf("%s/sessions/%s/ws", pod.WebsocketURL, id),
		Status:          model.SessionStatusActive,
		CreatedAt:       now,
		LastHeartbeatAt: now,
		LeaseExpiresAt:  now.Add(lease),
	}

	const insertSession = `
INSERT INTO sessions (
	session_id, user_id, host_id, cr_id, connection_type, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`
	if _, err := tx.ExecContext(ctx, insertSession,
		session.SessionID, session.UserID, session.HostID, session.CRID, session.ConnectionType,
		session.OwnerPodID, session.OwnerInstanceID, session.WebsocketURL,
		session.Status, session.CreatedAt, session.LastHeartbeatAt, session.LeaseExpiresAt,
	); err != nil {
		return model.Session{}, fmt.Errorf("insert session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, fmt.Errorf("commit session assignment: %w", err)
	}
	return session, nil
}

func (s *PostgresStore) CreateAutomationRun(
	ctx context.Context,
	req model.CreateAutomationRunRequest,
	heartbeatMaxAge time.Duration,
	ttl time.Duration,
) (model.AutomationRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AutomationRun{}, fmt.Errorf("begin automation assignment: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC()
	const selectPod = `
SELECT pod_id, instance_id, pod_ip, websocket_url, operations_url, status,
	active_sessions, max_sessions, last_heartbeat_at
FROM proxy_pods
WHERE status = $1
	AND operations_url <> ''
	AND last_heartbeat_at > $2
ORDER BY active_sessions, pod_id
FOR UPDATE SKIP LOCKED
LIMIT 1
`
	var pod model.ProxyPod
	err = scanPod(tx.QueryRowContext(ctx, selectPod, model.PodStatusReady, now.Add(-heartbeatMaxAge)), &pod)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationRun{}, ErrNoAvailablePod
	}
	if err != nil {
		return model.AutomationRun{}, fmt.Errorf("select automation proxy pod: %w", err)
	}
	id, err := automationRunID()
	if err != nil {
		return model.AutomationRun{}, err
	}
	run := model.AutomationRun{
		RunID: id, IncidentID: req.IncidentID, HostID: req.HostID,
		OwnerPodID: pod.PodID, OwnerInstanceID: pod.InstanceID,
		OperationsURL: pod.OperationsURL, Status: model.AutomationRunActive,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
	const insertRun = `
INSERT INTO automation_runs (
	run_id, incident_id, host_id, owner_pod_id, owner_instance_id,
	operations_url, status, created_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`
	if _, err := tx.ExecContext(ctx, insertRun,
		run.RunID, run.IncidentID, run.HostID, run.OwnerPodID, run.OwnerInstanceID,
		run.OperationsURL, run.Status, run.CreatedAt, run.ExpiresAt,
	); err != nil {
		return model.AutomationRun{}, fmt.Errorf("insert automation run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.AutomationRun{}, fmt.Errorf("commit automation assignment: %w", err)
	}
	return run, nil
}

func (s *PostgresStore) GetAutomationRun(ctx context.Context, id string) (model.AutomationRun, error) {
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `
UPDATE automation_runs SET status = $1
WHERE run_id = $2 AND status = $3 AND expires_at <= $4
`, model.AutomationRunClosed, id, model.AutomationRunActive, now); err != nil {
		return model.AutomationRun{}, fmt.Errorf("expire automation run: %w", err)
	}
	const query = `
SELECT run_id, incident_id, host_id, owner_pod_id, owner_instance_id,
	operations_url, status, created_at, expires_at
FROM automation_runs
WHERE run_id = $1
`
	var run model.AutomationRun
	err := scanAutomationRun(s.db.QueryRowContext(ctx, query, id), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationRun{}, ErrNotFound
	}
	if err != nil {
		return model.AutomationRun{}, fmt.Errorf("get automation run: %w", err)
	}
	return run, nil
}

func (s *PostgresStore) CloseAutomationRun(ctx context.Context, id string) (model.AutomationRun, error) {
	const query = `
UPDATE automation_runs
SET status = $1
WHERE run_id = $2
RETURNING run_id, incident_id, host_id, owner_pod_id, owner_instance_id,
	operations_url, status, created_at, expires_at
`
	var run model.AutomationRun
	err := scanAutomationRun(s.db.QueryRowContext(ctx, query, model.AutomationRunClosed, id), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationRun{}, ErrNotFound
	}
	if err != nil {
		return model.AutomationRun{}, fmt.Errorf("close automation run: %w", err)
	}
	return run, nil
}

func (s *PostgresStore) GetSession(ctx context.Context, id string) (model.Session, error) {
	if err := s.expireSessions(ctx, s.db, s.now().UTC()); err != nil {
		return model.Session{}, err
	}
	const query = `
SELECT session_id, user_id, host_id, cr_id, connection_type, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
FROM sessions
WHERE session_id = $1
`
	var session model.Session
	err := scanSession(s.db.QueryRowContext(ctx, query, id), &session)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, ErrNotFound
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

func (s *PostgresStore) ListSessions(ctx context.Context, filter model.ListSessionsFilter) ([]model.Session, error) {
	if err := s.expireSessions(ctx, s.db, s.now().UTC()); err != nil {
		return nil, err
	}
	const query = `
SELECT session_id, user_id, host_id, cr_id, connection_type, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
FROM sessions
WHERE ($1 = '' OR user_id = $1)
	AND ($2 = '' OR host_id = $2)
	AND ($3 = '' OR cr_id = $3)
	AND ($4 = '' OR status = $4)
ORDER BY created_at DESC, session_id
`
	rows, err := s.db.QueryContext(ctx, query, filter.UserID, filter.HostID, filter.CRID, filter.Status)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]model.Session, 0)
	for rows.Next() {
		var session model.Session
		if err := scanSession(rows, &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

func (s *PostgresStore) HeartbeatSession(
	ctx context.Context,
	id string,
	req model.SessionHeartbeatRequest,
	lease time.Duration,
) (model.Session, error) {
	now := s.now().UTC()
	if err := s.expireSessions(ctx, s.db, now); err != nil {
		return model.Session{}, err
	}
	const query = `
UPDATE sessions
SET last_heartbeat_at = $1, lease_expires_at = $2
WHERE session_id = $3
	AND owner_pod_id = $4
	AND owner_instance_id = $5
	AND status = $6
RETURNING session_id, user_id, host_id, cr_id, connection_type, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
`
	var session model.Session
	err := scanSession(s.db.QueryRowContext(
		ctx, query, now, now.Add(lease), id, req.OwnerPodID, req.OwnerInstanceID, model.SessionStatusActive,
	), &session)
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := s.GetSession(ctx, id)
		if errors.Is(getErr, ErrNotFound) {
			return model.Session{}, ErrNotFound
		}
		if getErr != nil {
			return model.Session{}, getErr
		}
		if existing.Status != model.SessionStatusActive {
			return model.Session{}, ErrSessionNotActive
		}
		return model.Session{}, errors.New("session owner mismatch")
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("heartbeat session: %w", err)
	}
	return session, nil
}

func (s *PostgresStore) CloseSession(ctx context.Context, id string, req model.CloseSessionRequest) (model.Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, fmt.Errorf("begin close session: %w", err)
	}
	defer tx.Rollback()
	if err := s.expireSessions(ctx, tx, s.now().UTC()); err != nil {
		return model.Session{}, err
	}

	const selectSession = `
SELECT session_id, user_id, host_id, cr_id, connection_type, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
FROM sessions
WHERE session_id = $1
FOR UPDATE
`
	var session model.Session
	err = scanSession(tx.QueryRowContext(ctx, selectSession, id), &session)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, ErrNotFound
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("select session for close: %w", err)
	}
	if session.OwnerPodID != req.OwnerPodID || session.OwnerInstanceID != req.OwnerInstanceID {
		return model.Session{}, errors.New("session owner mismatch")
	}

	if session.Status != model.SessionStatusClosed {
		session.Status = model.SessionStatusClosed
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE sessions SET status = $1 WHERE session_id = $2`,
			session.Status,
			session.SessionID,
		); err != nil {
			return model.Session{}, fmt.Errorf("close session: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE proxy_pods SET active_sessions = GREATEST(active_sessions - 1, 0) WHERE pod_id = $1`,
			session.OwnerPodID,
		); err != nil {
			return model.Session{}, fmt.Errorf("release proxy pod: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, fmt.Errorf("commit close session: %w", err)
	}
	return session, nil
}

func (s *PostgresStore) AppendSessionAuditRecords(
	ctx context.Context,
	sessionID string,
	req model.AppendSessionAuditRecordsRequest,
	retention time.Duration,
	maxBytes int,
) ([]model.SessionAuditRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin append session audit: %w", err)
	}
	defer tx.Rollback()
	if err := s.pruneAudit(ctx, tx, retention); err != nil {
		return nil, err
	}

	const selectSession = `
SELECT session_id, user_id, host_id, cr_id, connection_type, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
FROM sessions
WHERE session_id = $1
FOR UPDATE
`
	var session model.Session
	err = scanSession(tx.QueryRowContext(ctx, selectSession, sessionID), &session)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select session for audit append: %w", err)
	}
	if session.OwnerPodID != req.OwnerPodID || session.OwnerInstanceID != req.OwnerInstanceID {
		return nil, errors.New("session owner mismatch")
	}
	if len(req.Records) == 0 {
		return []model.SessionAuditRecord{}, nil
	}

	var sequence int64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(sequence), 0)
FROM session_audit_records
WHERE session_id = $1
`, sessionID).Scan(&sequence); err != nil {
		return nil, fmt.Errorf("read session audit sequence: %w", err)
	}
	var usedBytes int
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(
	CASE
		WHEN record_type = $2 THEN octet_length(command_text::bytea)
		WHEN record_type = $3 THEN octet_length(output_data)
		ELSE 0
	END
), 0)::INTEGER
FROM session_audit_records
WHERE session_id = $1
`, sessionID, model.AuditRecordCommand, model.AuditRecordOutput).Scan(&usedBytes); err != nil {
		return nil, fmt.Errorf("read session audit byte total: %w", err)
	}

	appended := make([]model.SessionAuditRecord, 0, len(req.Records))
	for _, incoming := range req.Records {
		record, addBytes, err := buildAuditRecord(session, incoming, s.now().UTC())
		if err != nil {
			return nil, err
		}
		if maxBytes > 0 && usedBytes+addBytes > maxBytes {
			remaining := maxBytes - usedBytes
			if remaining < 0 {
				remaining = 0
			}
			truncateAuditRecord(&record, remaining)
			addBytes = remaining
		}
		sequence++
		record.Sequence = sequence
		outputData, err := base64.StdEncoding.DecodeString(record.OutputDataBase64)
		if record.OutputDataBase64 == "" {
			outputData = nil
			err = nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode audit output: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO session_audit_records (
	session_id, user_id, host_id, cr_id, record_type, sequence, command_id,
	command_text, output_data, created_at, truncated, byte_count
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, record.SessionID, record.UserID, record.HostID, record.CRID, record.RecordType, record.Sequence,
			record.CommandID, record.CommandText, outputData, record.CreatedAt, record.Truncated, record.ByteCount,
		); err != nil {
			return nil, fmt.Errorf("insert session audit record: %w", err)
		}
		appended = append(appended, record)
		usedBytes += addBytes
		if record.Truncated {
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit session audit append: %w", err)
	}
	return appended, nil
}

func (s *PostgresStore) ListSessionAuditRecords(
	ctx context.Context,
	sessionID string,
	filter model.ListSessionAuditRecordsFilter,
	retention time.Duration,
) ([]model.SessionAuditRecord, error) {
	if err := s.pruneAudit(ctx, s.db, retention); err != nil {
		return nil, err
	}
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT session_id, user_id, host_id, cr_id, record_type, sequence, command_id,
	command_text, output_data, created_at, truncated, byte_count
FROM session_audit_records
WHERE session_id = $1 AND sequence > $2
ORDER BY sequence
LIMIT $3
`, sessionID, filter.AfterSequence, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("list session audit records: %w", err)
	}
	defer rows.Close()
	records := make([]model.SessionAuditRecord, 0)
	for rows.Next() {
		var record model.SessionAuditRecord
		if err := scanAuditRecord(rows, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list session audit records: %w", err)
	}
	return records, nil
}

func (s *PostgresStore) ListAuditSessions(
	ctx context.Context,
	filter model.ListAuditSessionsFilter,
	retention time.Duration,
) ([]model.AuditSessionSummary, error) {
	if err := s.pruneAudit(ctx, s.db, retention); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT s.session_id, s.user_id, s.host_id, s.cr_id, s.connection_type, s.status,
	s.created_at, MAX(a.created_at) AS last_audit_at, COUNT(*)::INTEGER AS record_count,
	BOOL_OR(a.truncated) AS truncated
FROM sessions s
JOIN session_audit_records a ON a.session_id = s.session_id
WHERE ($1 = '' OR s.session_id = $1)
	AND ($2 = '' OR s.user_id = $2)
	AND ($3 = '' OR s.host_id = $3)
	AND ($4 = '' OR s.cr_id = $4)
	AND ($5::timestamptz IS NULL OR a.created_at >= $5)
	AND ($6::timestamptz IS NULL OR a.created_at <= $6)
GROUP BY s.session_id, s.user_id, s.host_id, s.cr_id, s.connection_type, s.status, s.created_at
ORDER BY last_audit_at DESC, s.session_id
LIMIT $7 OFFSET $8
`, filter.SessionID, filter.UserID, filter.HostID, filter.CRID,
		nullableTime(filter.From), nullableTime(filter.To), filter.Limit, filter.Offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit sessions: %w", err)
	}
	defer rows.Close()
	summaries := make([]model.AuditSessionSummary, 0)
	for rows.Next() {
		var summary model.AuditSessionSummary
		if err := rows.Scan(
			&summary.SessionID,
			&summary.UserID,
			&summary.HostID,
			&summary.CRID,
			&summary.ConnectionType,
			&summary.Status,
			&summary.CreatedAt,
			&summary.LastAuditAt,
			&summary.RecordCount,
			&summary.Truncated,
		); err != nil {
			return nil, fmt.Errorf("scan audit session: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit sessions: %w", err)
	}
	return summaries, nil
}

func (s *PostgresStore) pruneAudit(ctx context.Context, executor queryExecutor, retention time.Duration) error {
	if retention <= 0 {
		return nil
	}
	if _, err := executor.ExecContext(ctx, `
DELETE FROM session_audit_records
WHERE created_at < $1
`, s.now().UTC().Add(-retention)); err != nil {
		return fmt.Errorf("prune session audit records: %w", err)
	}
	return nil
}

type queryExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *PostgresStore) expireSessions(ctx context.Context, executor queryExecutor, now time.Time) error {
	const query = `
	WITH expired AS (
		UPDATE sessions
		SET status = $1
		WHERE status = $2
			AND lease_expires_at <= $3
		RETURNING owner_pod_id
	),
	released AS (
		SELECT owner_pod_id, COUNT(*)::INTEGER AS session_count
		FROM expired
		GROUP BY owner_pod_id
	)
	UPDATE proxy_pods AS pod
	SET active_sessions = GREATEST(pod.active_sessions - released.session_count, 0)
	FROM released
	WHERE pod.pod_id = released.owner_pod_id
	`
	if _, err := executor.ExecContext(ctx, query, model.SessionStatusClosed, model.SessionStatusActive, now); err != nil {
		return fmt.Errorf("expire sessions: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanPod(row rowScanner, pod *model.ProxyPod) error {
	return row.Scan(
		&pod.PodID,
		&pod.InstanceID,
		&pod.PodIP,
		&pod.WebsocketURL,
		&pod.OperationsURL,
		&pod.Status,
		&pod.ActiveSessions,
		&pod.MaxSessions,
		&pod.LastHeartbeatAt,
	)
}

func scanAutomationRun(row rowScanner, run *model.AutomationRun) error {
	return row.Scan(
		&run.RunID,
		&run.IncidentID,
		&run.HostID,
		&run.OwnerPodID,
		&run.OwnerInstanceID,
		&run.OperationsURL,
		&run.Status,
		&run.CreatedAt,
		&run.ExpiresAt,
	)
}

func scanSession(row rowScanner, session *model.Session) error {
	return row.Scan(
		&session.SessionID,
		&session.UserID,
		&session.HostID,
		&session.CRID,
		&session.ConnectionType,
		&session.OwnerPodID,
		&session.OwnerInstanceID,
		&session.WebsocketURL,
		&session.Status,
		&session.CreatedAt,
		&session.LastHeartbeatAt,
		&session.LeaseExpiresAt,
	)
}

func scanAuditRecord(row rowScanner, record *model.SessionAuditRecord) error {
	var outputData []byte
	if err := row.Scan(
		&record.SessionID,
		&record.UserID,
		&record.HostID,
		&record.CRID,
		&record.RecordType,
		&record.Sequence,
		&record.CommandID,
		&record.CommandText,
		&outputData,
		&record.CreatedAt,
		&record.Truncated,
		&record.ByteCount,
	); err != nil {
		return err
	}
	if len(outputData) > 0 {
		record.OutputDataBase64 = base64.StdEncoding.EncodeToString(outputData)
	}
	return nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func sessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return "sess-" + hex.EncodeToString(value[:]), nil
}

func automationRunID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate automation run ID: %w", err)
	}
	return "run-" + hex.EncodeToString(value[:]), nil
}
