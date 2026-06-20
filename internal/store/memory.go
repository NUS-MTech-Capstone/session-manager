package store

import (
	"context"
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
	pods           map[string]model.ProxyPod
	now            func() time.Time
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:       make(map[string]model.Session),
		automationRuns: make(map[string]model.AutomationRun),
		pods:           make(map[string]model.ProxyPod),
		now:            time.Now,
	}
}

func (s *MemoryStore) CreateAutomationRun(_ context.Context, req model.CreateAutomationRunRequest, heartbeatMaxAge, ttl time.Duration) (model.AutomationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	cutoff := now.Add(-heartbeatMaxAge)
	var chosen *model.ProxyPod
	for _, pod := range s.pods {
		pod := pod
		if pod.Status != model.PodStatusReady || pod.OperationsURL == "" || !pod.LastHeartbeatAt.After(cutoff) {
			continue
		}
		if chosen == nil || pod.ActiveSessions < chosen.ActiveSessions || pod.ActiveSessions == chosen.ActiveSessions && pod.PodID < chosen.PodID {
			chosen = &pod
		}
	}
	if chosen == nil {
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
	cutoff := s.now().Add(-maxAge)
	var pods []model.ProxyPod
	for _, pod := range s.pods {
		if pod.Status == model.PodStatusReady && pod.LastHeartbeatAt.After(cutoff) && pod.ActiveSessions < pod.MaxSessions {
			pods = append(pods, pod)
		}
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].ActiveSessions == pods[j].ActiveSessions {
			return pods[i].PodID < pods[j].PodID
		}
		return pods[i].ActiveSessions < pods[j].ActiveSessions
	})
	return pods, nil
}

func (s *MemoryStore) CreateSession(_ context.Context, req model.CreateSessionRequest, heartbeatMaxAge time.Duration, lease time.Duration) (model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireSessionsLocked()
	now := s.now().UTC()
	cutoff := now.Add(-heartbeatMaxAge)
	var chosen *model.ProxyPod
	for _, pod := range s.pods {
		pod := pod
		if pod.Status != model.PodStatusReady || !pod.LastHeartbeatAt.After(cutoff) || pod.ActiveSessions >= pod.MaxSessions {
			continue
		}
		if chosen == nil || pod.ActiveSessions < chosen.ActiveSessions || pod.ActiveSessions == chosen.ActiveSessions && pod.PodID < chosen.PodID {
			chosen = &pod
		}
	}
	if chosen == nil {
		return model.Session{}, ErrNoAvailablePod
	}
	chosen.ActiveSessions++
	s.pods[chosen.PodID] = *chosen

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

func (s *MemoryStore) expireSessionsLocked() {
	now := s.now().UTC()
	for _, session := range s.sessions {
		if session.Status == model.SessionStatusActive && !session.LeaseExpiresAt.After(now) {
			s.closeSessionLocked(session)
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
