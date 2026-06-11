package service

import (
	"context"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func (s *Service) RegisterPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	return s.store.RegisterPod(ctx, pod)
}

func (s *Service) HeartbeatPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	return s.store.HeartbeatPod(ctx, pod)
}

func (s *Service) AvailablePods(ctx context.Context) ([]model.ProxyPod, error) {
	return s.store.AvailablePods(ctx, s.heartbeatMaxAge)
}
