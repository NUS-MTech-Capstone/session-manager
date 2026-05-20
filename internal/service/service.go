package service

import (
	"context"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

type Service struct {
	store           *store.MemoryStore
	heartbeatMaxAge time.Duration
	leaseDuration   time.Duration
}

func New(store *store.MemoryStore, heartbeatMaxAge, leaseDuration time.Duration) *Service {
	return &Service{store: store, heartbeatMaxAge: heartbeatMaxAge, leaseDuration: leaseDuration}
}

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

func (s *Service) RegisterPod(ctx context.Context, pod model.ProxyPod) model.ProxyPod {
	return s.store.RegisterPod(ctx, pod)
}

func (s *Service) HeartbeatPod(ctx context.Context, pod model.ProxyPod) model.ProxyPod {
	return s.store.HeartbeatPod(ctx, pod)
}

func (s *Service) AvailablePods(ctx context.Context) []model.ProxyPod {
	return s.store.AvailablePods(ctx, s.heartbeatMaxAge)
}
