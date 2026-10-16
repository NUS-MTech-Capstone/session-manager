package store

import (
	"context"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func TestMemoryStoreAutomationRunExpiresAndCloseIsIdempotent(t *testing.T) {
	st := NewMemoryStore()
	now := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	st.now = func() time.Time { return now }
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-1", InstanceID: "inst-1", OperationsURL: "http://proxy:8083",
		Status: model.PodStatusReady, MaxSessions: 1,
	})

	run, err := st.CreateAutomationRun(ctx, model.CreateAutomationRunRequest{IncidentID: "inc-1", HostID: "host-1"}, 15*time.Second, time.Minute)
	if err != nil {
		t.Fatalf("CreateAutomationRun() error = %v", err)
	}
	if run.Status != model.AutomationRunActive || run.OwnerInstanceID != "inst-1" || run.OperationsURL != "http://proxy:8083" {
		t.Fatalf("run = %#v", run)
	}

	now = now.Add(time.Minute)
	expired, err := st.GetAutomationRun(ctx, run.RunID)
	if err != nil {
		t.Fatalf("GetAutomationRun() error = %v", err)
	}
	if expired.Status != model.AutomationRunClosed {
		t.Fatalf("expired status = %q, want %q", expired.Status, model.AutomationRunClosed)
	}

	for range 2 {
		closed, err := st.CloseAutomationRun(ctx, run.RunID)
		if err != nil {
			t.Fatalf("CloseAutomationRun() error = %v", err)
		}
		if closed.Status != model.AutomationRunClosed {
			t.Fatalf("closed status = %q", closed.Status)
		}
	}
}
