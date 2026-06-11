package service

import (
	"context"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func (s *Service) CreateSession(ctx context.Context, req model.CreateSessionRequest) (model.Session, error) {
	return s.store.CreateSession(ctx, req, s.heartbeatMaxAge, s.leaseDuration)
}

func (s *Service) GetSession(ctx context.Context, id string) (model.Session, error) {
	return s.store.GetSession(ctx, id)
}

func (s *Service) HeartbeatSession(ctx context.Context, id string, req model.SessionHeartbeatRequest) (model.Session, error) {
	return s.store.HeartbeatSession(ctx, id, req, s.leaseDuration)
}

func (s *Service) CloseSession(ctx context.Context, id string) (model.Session, error) {
	return s.store.CloseSession(ctx, id)
}
