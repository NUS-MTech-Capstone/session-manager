package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/NUS-MTech-Capstone/session-manager/internal/events"
	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/observability"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

func (s *Service) CreateSession(ctx context.Context, req model.CreateSessionRequest) (model.Session, error) {
	session, err := s.store.CreateSession(ctx, req, s.heartbeatMaxAge, s.leaseDuration)
	if err != nil {
		if errors.Is(err, store.ErrNoAvailablePod) {
			observability.EmitCount("session-manager", "AssignmentFailures", map[string]string{"Component": "session_assignment", "Reason": "no_available_pod"})
			observability.EmitCount("session-manager", "StaleProxyPods", map[string]string{"Component": "session_assignment", "Reason": "no_available_pod"})
			slog.WarnContext(ctx, "ProxyPodUnhealthy",
				"requestId", observability.RequestID(ctx),
				"hostId", req.HostID,
				"crId", req.CRID,
				"errorKind", observability.ErrorKind(err),
				"error", err,
			)
		}
		return model.Session{}, err
	}
	s.emitFleetGauges(ctx)
	s.events.Emit(ctx, events.Event{
		EventType:  "SessionCreated",
		RequestID:  observability.RequestID(ctx),
		SessionID:  session.SessionID,
		UserID:     session.UserID,
		HostID:     session.HostID,
		CRID:       session.CRID,
		PodID:      session.OwnerPodID,
		InstanceID: session.OwnerInstanceID,
		Action:     "start",
		Mode:       "owner",
		Outcome:    "created",
	})
	return session, nil
}

func (s *Service) GetSession(ctx context.Context, id string) (model.Session, error) {
	return s.store.GetSession(ctx, id)
}

func (s *Service) ListSessions(ctx context.Context, filter model.ListSessionsFilter) ([]model.Session, error) {
	return s.store.ListSessions(ctx, filter)
}

func (s *Service) HeartbeatSession(ctx context.Context, id string, req model.SessionHeartbeatRequest) (model.Session, error) {
	session, err := s.store.HeartbeatSession(ctx, id, req, s.leaseDuration)
	if err != nil {
		observability.EmitCount("session-manager", "HeartbeatMisses", map[string]string{"Component": "session", "Reason": observability.ErrorKind(err)})
		return model.Session{}, err
	}
	return session, nil
}

func (s *Service) CloseSession(ctx context.Context, id string, req model.CloseSessionRequest) (model.Session, error) {
	session, err := s.store.CloseSession(ctx, id, req)
	if err == nil {
		s.emitFleetGauges(ctx)
		s.events.Emit(ctx, events.Event{
			EventType:  "SessionClosed",
			RequestID:  observability.RequestID(ctx),
			SessionID:  session.SessionID,
			UserID:     session.UserID,
			HostID:     session.HostID,
			CRID:       session.CRID,
			PodID:      session.OwnerPodID,
			InstanceID: session.OwnerInstanceID,
			Action:     "close",
			Outcome:    "closed",
		})
	}
	return session, err
}

func (s *Service) CreateAutomationRun(ctx context.Context, req model.CreateAutomationRunRequest) (model.AutomationRun, error) {
	run, err := s.store.CreateAutomationRun(ctx, req, s.heartbeatMaxAge, s.automationRunTTL)
	if err != nil && errors.Is(err, store.ErrNoAvailablePod) {
		observability.EmitCount("session-manager", "AssignmentFailures", map[string]string{"Component": "automation_assignment", "Reason": "no_available_pod"})
	}
	return run, err
}

func (s *Service) GetAutomationRun(ctx context.Context, id string) (model.AutomationRun, error) {
	return s.store.GetAutomationRun(ctx, id)
}

func (s *Service) CloseAutomationRun(ctx context.Context, id string) (model.AutomationRun, error) {
	return s.store.CloseAutomationRun(ctx, id)
}
