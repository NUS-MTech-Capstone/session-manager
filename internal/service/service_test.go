package service

import (
	"context"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

type recordingStore struct {
	registerPod      model.ProxyPod
	heartbeatPod     model.ProxyPod
	availableMaxAge  time.Duration
	createMaxAge     time.Duration
	createLease      time.Duration
	heartbeatLease   time.Duration
	automationMaxAge time.Duration
	automationTTL    time.Duration
	auditRetention   time.Duration
	auditMaxBytes    int
}

func (s *recordingStore) RegisterPod(_ context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	s.registerPod = pod
	return pod, nil
}

func (s *recordingStore) HeartbeatPod(_ context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	s.heartbeatPod = pod
	return pod, nil
}

func (s *recordingStore) AvailablePods(_ context.Context, maxAge time.Duration) ([]model.ProxyPod, error) {
	s.availableMaxAge = maxAge
	return []model.ProxyPod{{PodID: "pod-1"}}, nil
}

func (s *recordingStore) CreateSession(_ context.Context, req model.CreateSessionRequest, maxAge, lease time.Duration) (model.Session, error) {
	s.createMaxAge = maxAge
	s.createLease = lease
	return model.Session{UserID: req.UserID}, nil
}

func (s *recordingStore) GetSession(context.Context, string) (model.Session, error) {
	return model.Session{SessionID: "sess-1"}, nil
}

func (s *recordingStore) ListSessions(context.Context, model.ListSessionsFilter) ([]model.Session, error) {
	return []model.Session{{SessionID: "sess-1"}}, nil
}

func (s *recordingStore) HeartbeatSession(_ context.Context, id string, _ model.SessionHeartbeatRequest, lease time.Duration) (model.Session, error) {
	s.heartbeatLease = lease
	return model.Session{SessionID: id}, nil
}

func (s *recordingStore) CloseSession(_ context.Context, id string, _ model.CloseSessionRequest) (model.Session, error) {
	return model.Session{SessionID: id, Status: model.SessionStatusClosed}, nil
}

func (s *recordingStore) AppendSessionAuditRecords(_ context.Context, id string, _ model.AppendSessionAuditRecordsRequest, retention time.Duration, maxBytes int) ([]model.SessionAuditRecord, error) {
	s.auditRetention = retention
	s.auditMaxBytes = maxBytes
	return []model.SessionAuditRecord{{SessionID: id, Sequence: 1}}, nil
}

func (s *recordingStore) ListSessionAuditRecords(_ context.Context, id string, _ model.ListSessionAuditRecordsFilter, retention time.Duration) ([]model.SessionAuditRecord, error) {
	s.auditRetention = retention
	return []model.SessionAuditRecord{{SessionID: id, Sequence: 1}}, nil
}

func (s *recordingStore) ListAuditSessions(_ context.Context, _ model.ListAuditSessionsFilter, retention time.Duration) ([]model.AuditSessionSummary, error) {
	s.auditRetention = retention
	return []model.AuditSessionSummary{{SessionID: "sess-1"}}, nil
}

func (s *recordingStore) CreateAutomationRun(_ context.Context, req model.CreateAutomationRunRequest, maxAge, ttl time.Duration) (model.AutomationRun, error) {
	s.automationMaxAge = maxAge
	s.automationTTL = ttl
	return model.AutomationRun{IncidentID: req.IncidentID}, nil
}

func (s *recordingStore) GetAutomationRun(_ context.Context, id string) (model.AutomationRun, error) {
	return model.AutomationRun{RunID: id}, nil
}

func (s *recordingStore) CloseAutomationRun(_ context.Context, id string) (model.AutomationRun, error) {
	return model.AutomationRun{RunID: id, Status: model.AutomationRunClosed}, nil
}

func TestServicePassesSessionPolicyDurationsToStore(t *testing.T) {
	store := &recordingStore{}
	heartbeatMaxAge := 15 * time.Second
	leaseDuration := 2 * time.Minute
	svc := New(store, heartbeatMaxAge, leaseDuration)

	if _, err := svc.AvailablePods(t.Context()); err != nil {
		t.Fatalf("AvailablePods() error = %v", err)
	}
	if _, err := svc.CreateSession(t.Context(), model.CreateSessionRequest{UserID: "user-1"}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := svc.HeartbeatSession(t.Context(), "sess-1", model.SessionHeartbeatRequest{}); err != nil {
		t.Fatalf("HeartbeatSession() error = %v", err)
	}

	if store.availableMaxAge != heartbeatMaxAge {
		t.Fatalf("AvailablePods maxAge = %s, want %s", store.availableMaxAge, heartbeatMaxAge)
	}
	if store.createMaxAge != heartbeatMaxAge {
		t.Fatalf("CreateSession maxAge = %s, want %s", store.createMaxAge, heartbeatMaxAge)
	}
	if store.createLease != leaseDuration {
		t.Fatalf("CreateSession lease = %s, want %s", store.createLease, leaseDuration)
	}
	if store.heartbeatLease != leaseDuration {
		t.Fatalf("HeartbeatSession lease = %s, want %s", store.heartbeatLease, leaseDuration)
	}
}

func TestServicePassesConfiguredAutomationTTLToStore(t *testing.T) {
	store := &recordingStore{}
	heartbeatMaxAge := 20 * time.Second
	automationTTL := 30 * time.Minute
	svc := New(store, heartbeatMaxAge, time.Minute, automationTTL)

	if _, err := svc.CreateAutomationRun(t.Context(), model.CreateAutomationRunRequest{IncidentID: "inc-1"}); err != nil {
		t.Fatalf("CreateAutomationRun() error = %v", err)
	}

	if store.automationMaxAge != heartbeatMaxAge {
		t.Fatalf("CreateAutomationRun maxAge = %s, want %s", store.automationMaxAge, heartbeatMaxAge)
	}
	if store.automationTTL != automationTTL {
		t.Fatalf("CreateAutomationRun ttl = %s, want %s", store.automationTTL, automationTTL)
	}
}

func TestServiceDefaultsAutomationTTL(t *testing.T) {
	store := &recordingStore{}
	svc := New(store, 10*time.Second, time.Minute)

	if _, err := svc.CreateAutomationRun(t.Context(), model.CreateAutomationRunRequest{IncidentID: "inc-1"}); err != nil {
		t.Fatalf("CreateAutomationRun() error = %v", err)
	}

	if store.automationTTL != time.Hour {
		t.Fatalf("CreateAutomationRun ttl = %s, want %s", store.automationTTL, time.Hour)
	}
}

func TestServicePassesAuditPolicyToStore(t *testing.T) {
	store := &recordingStore{}
	retention := 48 * time.Hour
	maxBytes := 4096
	svc := New(store, 10*time.Second, time.Minute, time.Hour, retention, maxBytes)

	if _, err := svc.AppendSessionAuditRecords(t.Context(), "sess-1", model.AppendSessionAuditRecordsRequest{}); err != nil {
		t.Fatalf("AppendSessionAuditRecords() error = %v", err)
	}
	if store.auditRetention != retention {
		t.Fatalf("Append audit retention = %s, want %s", store.auditRetention, retention)
	}
	if store.auditMaxBytes != maxBytes {
		t.Fatalf("Append audit maxBytes = %d, want %d", store.auditMaxBytes, maxBytes)
	}

	if _, err := svc.ListSessionAuditRecords(t.Context(), "sess-1", model.ListSessionAuditRecordsFilter{}); err != nil {
		t.Fatalf("ListSessionAuditRecords() error = %v", err)
	}
	if store.auditRetention != retention {
		t.Fatalf("List audit retention = %s, want %s", store.auditRetention, retention)
	}
}
