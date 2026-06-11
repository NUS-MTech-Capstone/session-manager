package store

import (
	"context"
	"crypto/rand"
	"database/sql"
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
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store := &PostgresStore{db: db, now: time.Now}
	if err := store.migrate(ctx); err != nil {
		db.Close()
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
	status TEXT NOT NULL,
	active_sessions INTEGER NOT NULL,
	max_sessions INTEGER NOT NULL,
	last_heartbeat_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	session_id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	host_id TEXT NOT NULL,
	cr_id TEXT NOT NULL,
	owner_pod_id TEXT NOT NULL,
	owner_instance_id TEXT NOT NULL,
	websocket_url TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	last_heartbeat_at TIMESTAMPTZ NOT NULL,
	lease_expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS proxy_pods_assignment_idx
	ON proxy_pods (status, active_sessions, pod_id);
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
	pod_id, instance_id, pod_ip, websocket_url, status,
	active_sessions, max_sessions, last_heartbeat_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (pod_id) DO UPDATE SET
	instance_id = EXCLUDED.instance_id,
	pod_ip = EXCLUDED.pod_ip,
	websocket_url = EXCLUDED.websocket_url,
	status = EXCLUDED.status,
	active_sessions = CASE
		WHEN proxy_pods.instance_id = EXCLUDED.instance_id
			THEN GREATEST(proxy_pods.active_sessions, EXCLUDED.active_sessions)
		ELSE EXCLUDED.active_sessions
	END,
	max_sessions = EXCLUDED.max_sessions,
	last_heartbeat_at = EXCLUDED.last_heartbeat_at
RETURNING pod_id, instance_id, pod_ip, websocket_url, status,
	active_sessions, max_sessions, last_heartbeat_at
`
	var stored model.ProxyPod
	err := scanPod(s.db.QueryRowContext(ctx, query,
		pod.PodID, pod.InstanceID, pod.PodIP, pod.WebsocketURL, pod.Status,
		pod.ActiveSessions, pod.MaxSessions, pod.LastHeartbeatAt,
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
	const query = `
SELECT pod_id, instance_id, pod_ip, websocket_url, status,
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

	const selectPod = `
SELECT pod_id, instance_id, pod_ip, websocket_url, status,
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
		s.now().UTC().Add(-heartbeatMaxAge),
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

	now := s.now().UTC()
	id, err := sessionID()
	if err != nil {
		return model.Session{}, err
	}
	session := model.Session{
		SessionID:       id,
		UserID:          req.UserID,
		HostID:          req.HostID,
		CRID:            req.CRID,
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
	session_id, user_id, host_id, cr_id, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`
	if _, err := tx.ExecContext(ctx, insertSession,
		session.SessionID, session.UserID, session.HostID, session.CRID,
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

func (s *PostgresStore) GetSession(ctx context.Context, id string) (model.Session, error) {
	const query = `
SELECT session_id, user_id, host_id, cr_id, owner_pod_id, owner_instance_id,
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

func (s *PostgresStore) HeartbeatSession(
	ctx context.Context,
	id string,
	req model.SessionHeartbeatRequest,
	lease time.Duration,
) (model.Session, error) {
	now := s.now().UTC()
	const query = `
UPDATE sessions
SET last_heartbeat_at = $1, lease_expires_at = $2
WHERE session_id = $3
	AND owner_pod_id = $4
	AND owner_instance_id = $5
RETURNING session_id, user_id, host_id, cr_id, owner_pod_id, owner_instance_id,
	websocket_url, status, created_at, last_heartbeat_at, lease_expires_at
`
	var session model.Session
	err := scanSession(s.db.QueryRowContext(
		ctx, query, now, now.Add(lease), id, req.OwnerPodID, req.OwnerInstanceID,
	), &session)
	if errors.Is(err, sql.ErrNoRows) {
		_, getErr := s.GetSession(ctx, id)
		if errors.Is(getErr, ErrNotFound) {
			return model.Session{}, ErrNotFound
		}
		if getErr != nil {
			return model.Session{}, getErr
		}
		return model.Session{}, errors.New("session owner mismatch")
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("heartbeat session: %w", err)
	}
	return session, nil
}

func (s *PostgresStore) CloseSession(ctx context.Context, id string) (model.Session, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, fmt.Errorf("begin close session: %w", err)
	}
	defer tx.Rollback()

	const selectSession = `
SELECT session_id, user_id, host_id, cr_id, owner_pod_id, owner_instance_id,
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

type rowScanner interface {
	Scan(...any) error
}

func scanPod(row rowScanner, pod *model.ProxyPod) error {
	return row.Scan(
		&pod.PodID,
		&pod.InstanceID,
		&pod.PodIP,
		&pod.WebsocketURL,
		&pod.Status,
		&pod.ActiveSessions,
		&pod.MaxSessions,
		&pod.LastHeartbeatAt,
	)
}

func scanSession(row rowScanner, session *model.Session) error {
	return row.Scan(
		&session.SessionID,
		&session.UserID,
		&session.HostID,
		&session.CRID,
		&session.OwnerPodID,
		&session.OwnerInstanceID,
		&session.WebsocketURL,
		&session.Status,
		&session.CreatedAt,
		&session.LastHeartbeatAt,
		&session.LeaseExpiresAt,
	)
}

func sessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return "sess-" + hex.EncodeToString(value[:]), nil
}
