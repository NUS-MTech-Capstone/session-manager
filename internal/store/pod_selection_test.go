package store

import (
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func TestLeastActivePodSelectionStrategyChoosesLowestActiveSessions(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	strategy := LeastActivePodSelectionStrategy{}
	pods := map[string]model.ProxyPod{
		"pod-b": readyPod("pod-b", 1, now),
		"pod-a": readyPod("pod-a", 1, now),
		"pod-c": readyPod("pod-c", 0, now.Add(-time.Hour)),
		"pod-d": readyPod("pod-d", 2, now),
	}

	chosen, ok := strategy.ChooseInteractivePod(pods, now, time.Minute)
	if !ok {
		t.Fatal("ChooseInteractivePod() ok = false")
	}
	if chosen.PodID != "pod-a" {
		t.Fatalf("ChooseInteractivePod() pod = %q, want pod-a", chosen.PodID)
	}
}

func TestLeastActivePodSelectionStrategyFiltersAutomationPodsWithoutOperationsURL(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	strategy := LeastActivePodSelectionStrategy{}
	podWithoutOperations := readyPod("pod-a", 0, now)
	podWithoutOperations.OperationsURL = ""
	podWithOperations := readyPod("pod-b", 1, now)
	podWithOperations.OperationsURL = "http://pod-b"

	chosen, ok := strategy.ChooseAutomationPod(map[string]model.ProxyPod{
		"pod-a": podWithoutOperations,
		"pod-b": podWithOperations,
	}, now, time.Minute)
	if !ok {
		t.Fatal("ChooseAutomationPod() ok = false")
	}
	if chosen.PodID != "pod-b" {
		t.Fatalf("ChooseAutomationPod() pod = %q, want pod-b", chosen.PodID)
	}
}

func readyPod(id string, active int, heartbeat time.Time) model.ProxyPod {
	return model.ProxyPod{
		PodID:           id,
		InstanceID:      "instance-" + id,
		WebsocketURL:    "ws://" + id,
		OperationsURL:   "http://" + id,
		Status:          model.PodStatusReady,
		ActiveSessions:  active,
		MaxSessions:     10,
		LastHeartbeatAt: heartbeat,
	}
}
