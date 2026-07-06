package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

var (
	ErrNoAvailablePod   = errors.New("no available proxy dataplane pod")
	ErrNotFound         = errors.New("not found")
	ErrSessionNotActive = errors.New("session is not active")
)

type MemoryStore struct {
	mu             sync.RWMutex
	sessions       map[string]model.Session
	automationRuns map[string]model.AutomationRun
	auditRecords   map[string][]model.SessionAuditRecord
	pods           map[string]model.ProxyPod
	now            func() time.Time
	podSelection   PodSelectionStrategy
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:       make(map[string]model.Session),
		automationRuns: make(map[string]model.AutomationRun),
		auditRecords:   make(map[string][]model.SessionAuditRecord),
		pods:           make(map[string]model.ProxyPod),
		now:            time.Now,
		podSelection:   LeastActivePodSelectionStrategy{},
	}
}

func (s *MemoryStore) CreateAutomationRun(_ context.Context, req model.CreateAutomationRunRequest, heartbeatMaxAge, ttl time.Duration) (model.AutomationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	chosen, ok := s.podSelection.ChooseAutomationPod(s.pods, now, heartbeatMaxAge)
	if !ok {
		return model.AutomationRun{}, ErrNoAvailablePod
	}
	run := model.AutomationRun{
		RunID: fmt.Sprintf("run-%d", now.UnixNano()), IncidentID: req.IncidentID, HostID: req.HostID,
		OwnerPodID: chosen.PodID, OwnerInstanceID: chosen.InstanceID,
		OperationsURL: chosen.OperationsURL, Status: model.AutomationRunActive,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}
	s.automationRuns[run.RunID] = run
	return run, nil
}

func (s *MemoryStore) GetAutomationRun(_ context.Context, id string) (model.AutomationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.automationRuns[id]
	if !ok {
		return model.AutomationRun{}, ErrNotFound
	}
	if run.Status == model.AutomationRunActive && !run.ExpiresAt.After(s.now().UTC()) {
		run.Status = model.AutomationRunClosed
		s.automationRuns[id] = run
	}
	return run, nil
}

func (s *MemoryStore) CloseAutomationRun(_ context.Context, id string) (model.AutomationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.automationRuns[id]
	if !ok {
		return model.AutomationRun{}, ErrNotFound
	}
	run.Status = model.AutomationRunClosed
	s.automationRuns[id] = run
	return run, nil
}

func (s *MemoryStore) RegisterPod(_ context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pod.LastHeartbeatAt = s.now().UTC()
	if pod.Status == "" {
		pod.Status = model.PodStatusReady
	}
	if pod.MaxSessions == 0 {
		pod.MaxSessions = 50
	}
	if existing, ok := s.pods[pod.PodID]; ok &&
		existing.InstanceID == pod.InstanceID &&
		existing.ActiveSessions > pod.ActiveSessions {
		pod.ActiveSessions = existing.ActiveSessions
	}
	s.pods[pod.PodID] = pod
	return pod, nil
}

func (s *MemoryStore) HeartbeatPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	return s.RegisterPod(ctx, pod)
}

func (s *MemoryStore) AvailablePods(_ context.Context, maxAge time.Duration) ([]model.ProxyPod, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSessionsLocked()
	return s.podSelection.AvailablePods(s.pods, s.now().UTC(), maxAge), nil
}

func (s *MemoryStore) CreateSession(_ context.Context, req model.CreateSessionRequest, heartbeatMaxAge time.Duration, lease time.Duration) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSessionsLocked()
	now := s.now().UTC()
	chosen, ok := s.podSelection.ChooseInteractivePod(s.pods, now, heartbeatMaxAge)
	if !ok {
		return model.Session{}, ErrNoAvailablePod
	}
	chosen.ActiveSessions++
	s.pods[chosen.PodID] = chosen

	id := fmt.Sprintf("sess-%d", now.UnixNano())
	if req.ConnectionType == "" {
		req.ConnectionType = "ssh"
	}
	session := model.Session{
		SessionID:       id,
		UserID:          req.UserID,
		HostID:          req.HostID,
		CRID:            req.CRID,
		ConnectionType:  req.ConnectionType,
		OwnerPodID:      chosen.PodID,
		OwnerInstanceID: chosen.InstanceID,
		WebsocketURL:    fmt.Sprintf("%s/sessions/%s/ws", chosen.WebsocketURL, id),
		Status:          model.SessionStatusActive,
		CreatedAt:       now,
		LastHeartbeatAt: now,
		LeaseExpiresAt:  now.Add(lease),
	}
	s.sessions[id] = session
	return session, nil
}

func (s *MemoryStore) GetSession(_ context.Context, id string) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSessionsLocked()
	session, ok := s.sessions[id]
	if !ok {
		return model.Session{}, ErrNotFound
	}
	return session, nil
}

func (s *MemoryStore) ListSessions(_ context.Context, filter model.ListSessionsFilter) ([]model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSessionsLocked()
	sessions := make([]model.Session, 0)
	for _, session := range s.sessions {
		if filter.UserID != "" && session.UserID != filter.UserID {
			continue
		}
		if filter.HostID != "" && session.HostID != filter.HostID {
			continue
		}
		if filter.CRID != "" && session.CRID != filter.CRID {
			continue
		}
		if filter.Status != "" && session.Status != filter.Status {
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})
	return sessions, nil
}

func (s *MemoryStore) HeartbeatSession(_ context.Context, id string, req model.SessionHeartbeatRequest, lease time.Duration) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return model.Session{}, ErrNotFound
	}
	if session.OwnerPodID != req.OwnerPodID || session.OwnerInstanceID != req.OwnerInstanceID {
		return model.Session{}, errors.New("session owner mismatch")
	}
	if session.Status != model.SessionStatusActive {
		return model.Session{}, ErrSessionNotActive
	}
	now := s.now().UTC()
	if !session.LeaseExpiresAt.After(now) {
		s.closeSessionLocked(session)
		return model.Session{}, ErrSessionNotActive
	}
	session.LastHeartbeatAt = now
	session.LeaseExpiresAt = now.Add(lease)
	s.sessions[id] = session
	return session, nil
}

func (s *MemoryStore) CloseSession(_ context.Context, id string, req model.CloseSessionRequest) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSessionsLocked()
	session, ok := s.sessions[id]
	if !ok {
		return model.Session{}, ErrNotFound
	}
	if session.OwnerPodID != req.OwnerPodID || session.OwnerInstanceID != req.OwnerInstanceID {
		return model.Session{}, errors.New("session owner mismatch")
	}
	return s.closeSessionLocked(session), nil
}

func (s *MemoryStore) AppendSessionAuditRecords(
	_ context.Context,
	sessionID string,
	req model.AppendSessionAuditRecordsRequest,
	retention time.Duration,
	maxBytes int,
) ([]model.SessionAuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneAuditLocked(retention)

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrNotFound
	}
	if session.OwnerPodID != req.OwnerPodID || session.OwnerInstanceID != req.OwnerInstanceID {
		return nil, errors.New("session owner mismatch")
	}
	if len(req.Records) == 0 {
		return []model.SessionAuditRecord{}, nil
	}

	existing := s.auditRecords[sessionID]
	sequence := int64(0)
	if len(existing) > 0 {
		sequence = existing[len(existing)-1].Sequence
	}
	usedBytes := auditByteTotal(existing)
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
		existing = append(existing, record)
		appended = append(appended, record)
		usedBytes += addBytes
		if record.Truncated {
			break
		}
	}
	s.auditRecords[sessionID] = existing
	return appended, nil
}

func (s *MemoryStore) ListSessionAuditRecords(
	_ context.Context,
	sessionID string,
	filter model.ListSessionAuditRecordsFilter,
	retention time.Duration,
) ([]model.SessionAuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneAuditLocked(retention)
	if _, ok := s.sessions[sessionID]; !ok {
		return nil, ErrNotFound
	}
	records := make([]model.SessionAuditRecord, 0)
	for _, record := range s.auditRecords[sessionID] {
		if record.Sequence <= filter.AfterSequence {
			continue
		}
		records = append(records, record)
		if filter.Limit > 0 && len(records) >= filter.Limit {
			break
		}
	}
	return records, nil
}

func (s *MemoryStore) ListAuditSessions(
	_ context.Context,
	filter model.ListAuditSessionsFilter,
	retention time.Duration,
) ([]model.AuditSessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneAuditLocked(retention)
	summaries := make([]model.AuditSessionSummary, 0)
	for sessionID, records := range s.auditRecords {
		if len(records) == 0 {
			continue
		}
		session, ok := s.sessions[sessionID]
		if !ok {
			continue
		}
		if filter.SessionID != "" && session.SessionID != filter.SessionID {
			continue
		}
		if filter.UserID != "" && session.UserID != filter.UserID {
			continue
		}
		if filter.HostID != "" && session.HostID != filter.HostID {
			continue
		}
		if filter.CRID != "" && session.CRID != filter.CRID {
			continue
		}
		lastAuditAt := records[len(records)-1].CreatedAt
		if !filter.From.IsZero() && lastAuditAt.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && lastAuditAt.After(filter.To) {
			continue
		}
		truncated := false
		for _, record := range records {
			if record.Truncated {
				truncated = true
				break
			}
		}
		summaries = append(summaries, model.AuditSessionSummary{
			SessionID:      session.SessionID,
			UserID:         session.UserID,
			HostID:         session.HostID,
			CRID:           session.CRID,
			ConnectionType: session.ConnectionType,
			Status:         session.Status,
			CreatedAt:      session.CreatedAt,
			LastAuditAt:    lastAuditAt,
			RecordCount:    len(records),
			Truncated:      truncated,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].LastAuditAt.Equal(summaries[j].LastAuditAt) {
			return summaries[i].SessionID < summaries[j].SessionID
		}
		return summaries[i].LastAuditAt.After(summaries[j].LastAuditAt)
	})
	if filter.Offset > len(summaries) {
		return []model.AuditSessionSummary{}, nil
	}
	end := filter.Offset + filter.Limit
	if filter.Limit <= 0 || end > len(summaries) {
		end = len(summaries)
	}
	return append([]model.AuditSessionSummary(nil), summaries[filter.Offset:end]...), nil
}

func (s *MemoryStore) pruneAuditLocked(retention time.Duration) {
	if retention <= 0 {
		return
	}
	cutoff := s.now().UTC().Add(-retention)
	for sessionID, records := range s.auditRecords {
		kept := records[:0]
		for _, record := range records {
			if !record.CreatedAt.Before(cutoff) {
				kept = append(kept, record)
			}
		}
		if len(kept) == 0 {
			delete(s.auditRecords, sessionID)
			continue
		}
		s.auditRecords[sessionID] = append([]model.SessionAuditRecord(nil), kept...)
	}
}

func (s *MemoryStore) expireSessionsLocked() {
	now := s.now().UTC()
	for _, session := range s.sessions {
		if session.Status == model.SessionStatusActive && !session.LeaseExpiresAt.After(now) {
			s.closeSessionLocked(session)
		}
	}
}

func buildAuditRecord(session model.Session, incoming model.AppendSessionAuditRecord, fallback time.Time) (model.SessionAuditRecord, int, error) {
	createdAt := fallback
	if incoming.CreatedAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, incoming.CreatedAt)
		if err != nil {
			return model.SessionAuditRecord{}, 0, fmt.Errorf("invalid audit createdAt: %w", err)
		}
		createdAt = parsed.UTC()
	}
	record := model.SessionAuditRecord{
		SessionID:   session.SessionID,
		UserID:      session.UserID,
		HostID:      session.HostID,
		CRID:        session.CRID,
		RecordType:  incoming.RecordType,
		CommandID:   incoming.CommandID,
		CommandText: incoming.CommandText,
		CreatedAt:   createdAt,
		Truncated:   incoming.Truncated,
	}
	var bodyBytes int
	switch incoming.RecordType {
	case model.AuditRecordCommand:
		record.ByteCount = len([]byte(incoming.CommandText))
		bodyBytes = record.ByteCount
	case model.AuditRecordOutput:
		data, err := base64.StdEncoding.DecodeString(incoming.OutputDataBase64)
		if err != nil {
			return model.SessionAuditRecord{}, 0, fmt.Errorf("invalid outputDataBase64: %w", err)
		}
		record.OutputDataBase64 = base64.StdEncoding.EncodeToString(data)
		if incoming.ByteCount > 0 {
			record.ByteCount = incoming.ByteCount
		} else {
			record.ByteCount = len(data)
		}
		bodyBytes = len(data)
	case model.AuditRecordControl, model.AuditRecordSessionEvent:
		record.ByteCount = incoming.ByteCount
	default:
		return model.SessionAuditRecord{}, 0, errors.New("unsupported audit recordType")
	}
	return record, bodyBytes, nil
}

func auditByteTotal(records []model.SessionAuditRecord) int {
	total := 0
	for _, record := range records {
		switch record.RecordType {
		case model.AuditRecordCommand:
			total += len([]byte(record.CommandText))
		case model.AuditRecordOutput:
			data, err := base64.StdEncoding.DecodeString(record.OutputDataBase64)
			if err == nil {
				total += len(data)
			}
		}
	}
	return total
}

func truncateAuditRecord(record *model.SessionAuditRecord, remaining int) {
	record.Truncated = true
	switch record.RecordType {
	case model.AuditRecordCommand:
		data := []byte(record.CommandText)
		if remaining < len(data) {
			record.CommandText = string(data[:remaining])
		}
	case model.AuditRecordOutput:
		data, err := base64.StdEncoding.DecodeString(record.OutputDataBase64)
		if err == nil && remaining < len(data) {
			record.OutputDataBase64 = base64.StdEncoding.EncodeToString(data[:remaining])
		}
	}
}

func (s *MemoryStore) closeSessionLocked(session model.Session) model.Session {
	if session.Status != model.SessionStatusClosed {
		session.Status = model.SessionStatusClosed
		if pod, ok := s.pods[session.OwnerPodID]; ok && pod.ActiveSessions > 0 {
			pod.ActiveSessions--
			s.pods[pod.PodID] = pod
		}
		s.sessions[session.SessionID] = session
	}
	return session
}
