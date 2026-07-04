package store

import (
	"sort"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

type PodSelectionStrategy interface {
	AvailablePods(map[string]model.ProxyPod, time.Time, time.Duration) []model.ProxyPod
	ChooseInteractivePod(map[string]model.ProxyPod, time.Time, time.Duration) (model.ProxyPod, bool)
	ChooseAutomationPod(map[string]model.ProxyPod, time.Time, time.Duration) (model.ProxyPod, bool)
}

type LeastActivePodSelectionStrategy struct{}

func (LeastActivePodSelectionStrategy) AvailablePods(pods map[string]model.ProxyPod, now time.Time, maxAge time.Duration) []model.ProxyPod {
	cutoff := now.Add(-maxAge)
	available := make([]model.ProxyPod, 0)
	for _, pod := range pods {
		if pod.Status == model.PodStatusReady && pod.LastHeartbeatAt.After(cutoff) && pod.ActiveSessions < pod.MaxSessions {
			available = append(available, pod)
		}
	}
	sortPods(available)
	return available
}

func (s LeastActivePodSelectionStrategy) ChooseInteractivePod(pods map[string]model.ProxyPod, now time.Time, maxAge time.Duration) (model.ProxyPod, bool) {
	available := s.AvailablePods(pods, now, maxAge)
	if len(available) == 0 {
		return model.ProxyPod{}, false
	}
	return available[0], true
}

func (LeastActivePodSelectionStrategy) ChooseAutomationPod(pods map[string]model.ProxyPod, now time.Time, maxAge time.Duration) (model.ProxyPod, bool) {
	cutoff := now.Add(-maxAge)
	available := make([]model.ProxyPod, 0)
	for _, pod := range pods {
		if pod.Status == model.PodStatusReady && pod.OperationsURL != "" && pod.LastHeartbeatAt.After(cutoff) {
			available = append(available, pod)
		}
	}
	sortPods(available)
	if len(available) == 0 {
		return model.ProxyPod{}, false
	}
	return available[0], true
}

func sortPods(pods []model.ProxyPod) {
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].ActiveSessions == pods[j].ActiveSessions {
			return pods[i].PodID < pods[j].PodID
		}
		return pods[i].ActiveSessions < pods[j].ActiveSessions
	})
}
