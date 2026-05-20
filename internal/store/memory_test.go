package store

import (
	"context"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func TestCreateSessionChoosesReadyPodWithLowestActiveSessions(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	st.RegisterPod(ctx, model.ProxyPod{PodID: "b", InstanceID: "i-b", WebsocketURL: "ws://b", Status: model.PodStatusReady, ActiveSessions: 2, MaxSessions: 50})
	st.RegisterPod(ctx, model.ProxyPod{PodID: "a", InstanceID: "i-a", WebsocketURL: "ws://a", Status: model.PodStatusReady, ActiveSessions: 1, MaxSessions: 50})

	session, err := st.CreateSession(ctx, model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"}, 15*time.Second, time.Minute)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.OwnerPodID != "a" || session.OwnerInstanceID != "i-a" {
		t.Fatalf("owner = %s/%s, want a/i-a", session.OwnerPodID, session.OwnerInstanceID)
	}
}

func TestCreateSessionRejectsStalePods(t *testing.T) {
	st := NewMemoryStore()
	now := time.Now()
	st.now = func() time.Time { return now }
	ctx := context.Background()
	st.RegisterPod(ctx, model.ProxyPod{PodID: "stale", InstanceID: "i", WebsocketURL: "ws://stale", Status: model.PodStatusReady, MaxSessions: 50})
	now = now.Add(20 * time.Second)

	if _, err := st.CreateSession(ctx, model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"}, 15*time.Second, time.Minute); err != ErrNoAvailablePod {
		t.Fatalf("CreateSession() error = %v, want ErrNoAvailablePod", err)
	}
}
