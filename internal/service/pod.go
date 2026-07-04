package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/observability"
)

func (s *Service) RegisterPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	registered, err := s.store.RegisterPod(ctx, pod)
	if err == nil {
		emitPodCapacity("register", registered)
		s.emitFleetGauges(ctx)
	}
	return registered, err
}

func (s *Service) HeartbeatPod(ctx context.Context, pod model.ProxyPod) (model.ProxyPod, error) {
	heartbeat, err := s.store.HeartbeatPod(ctx, pod)
	if err == nil {
		emitPodCapacity("heartbeat", heartbeat)
		s.emitFleetGauges(ctx)
	}
	return heartbeat, err
}

func (s *Service) AvailablePods(ctx context.Context) ([]model.ProxyPod, error) {
	pods, err := s.store.AvailablePods(ctx, s.heartbeatMaxAge)
	if err == nil {
		emitAvailablePodGauges(pods)
	}
	return pods, err
}

func emitPodCapacity(component string, pod model.ProxyPod) {
	available := pod.MaxSessions - pod.ActiveSessions
	if available < 0 {
		available = 0
	}
	observability.EmitMetric("session-manager", []observability.Metric{
		{Name: "ActiveSessions", Unit: "Count", Value: float64(pod.ActiveSessions)},
		{Name: "ProxyPodCapacity", Unit: "Count", Value: float64(available)},
	}, map[string]string{"Component": component})
}

func emitAvailablePodGauges(pods []model.ProxyPod) {
	totalCapacity := 0
	active := 0
	for _, pod := range pods {
		active += pod.ActiveSessions
		if available := pod.MaxSessions - pod.ActiveSessions; available > 0 {
			totalCapacity += available
		}
	}
	observability.EmitMetric("session-manager", []observability.Metric{
		{Name: "AvailableProxyPods", Unit: "Count", Value: float64(len(pods))},
		{Name: "ProxyPodCapacity", Unit: "Count", Value: float64(totalCapacity)},
		{Name: "ActiveSessions", Unit: "Count", Value: float64(active)},
	}, map[string]string{"Component": "available_pods"})
}

func (s *Service) emitFleetGauges(ctx context.Context) {
	pods, err := s.store.AvailablePods(ctx, s.heartbeatMaxAge)
	if err != nil {
		return
	}
	emitAvailablePodGauges(pods)
	now := time.Now().UTC()
	for _, pod := range pods {
		if now.Sub(pod.LastHeartbeatAt) > s.heartbeatMaxAge {
			observability.EmitCount("session-manager", "StaleProxyPods", map[string]string{"Component": "proxy_pod"})
			slog.WarnContext(ctx, "ProxyPodUnhealthy",
				"requestId", observability.RequestID(ctx),
				"podId", pod.PodID,
				"instanceId", pod.InstanceID,
				"lastHeartbeatAt", pod.LastHeartbeatAt,
				"staleForMs", now.Sub(pod.LastHeartbeatAt).Milliseconds(),
				"activeSessions", pod.ActiveSessions,
				"capacity", pod.MaxSessions,
			)
		}
	}
}
