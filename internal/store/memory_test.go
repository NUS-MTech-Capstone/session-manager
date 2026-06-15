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
	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "b", InstanceID: "i-b", WebsocketURL: "ws://b", Status: model.PodStatusReady, ActiveSessions: 2, MaxSessions: 50})
	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "a", InstanceID: "i-a", WebsocketURL: "ws://a", Status: model.PodStatusReady, ActiveSessions: 1, MaxSessions: 50})

	session, err := st.CreateSession(ctx, model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"}, 15*time.Second, time.Minute)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.OwnerPodID != "a" || session.OwnerInstanceID != "i-a" {
		t.Fatalf("owner = %s/%s, want a/i-a", session.OwnerPodID, session.OwnerInstanceID)
	}
	if session.ConnectionType != "ssh" {
		t.Fatalf("ConnectionType = %q, want ssh default", session.ConnectionType)
	}
}

func TestAvailablePodsReturnsHealthyPodsByLoad(t *testing.T) {
	st := NewMemoryStore()
	now := time.Now()
	st.now = func() time.Time { return now }
	ctx := context.Background()

	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "loaded", InstanceID: "i-1", WebsocketURL: "ws://loaded", Status: model.PodStatusReady, ActiveSessions: 3, MaxSessions: 5})
	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "least", InstanceID: "i-2", WebsocketURL: "ws://least", Status: model.PodStatusReady, ActiveSessions: 1, MaxSessions: 5})
	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "not-ready", InstanceID: "i-3", WebsocketURL: "ws://not-ready", Status: "DRAINING", MaxSessions: 5})
	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "full", InstanceID: "i-4", WebsocketURL: "ws://full", Status: model.PodStatusReady, ActiveSessions: 5, MaxSessions: 5})

	pods, err := st.AvailablePods(ctx, 15*time.Second)
	if err != nil {
		t.Fatalf("AvailablePods() error = %v", err)
	}
	if len(pods) != 2 || pods[0].PodID != "least" || pods[1].PodID != "loaded" {
		t.Fatalf("AvailablePods() = %#v, want least then loaded", pods)
	}
}

func TestCreateSessionRejectsStalePods(t *testing.T) {
	st := NewMemoryStore()
	now := time.Now()
	st.now = func() time.Time { return now }
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{PodID: "stale", InstanceID: "i", WebsocketURL: "ws://stale", Status: model.PodStatusReady, MaxSessions: 50})
	now = now.Add(20 * time.Second)

	if _, err := st.CreateSession(ctx, model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"}, 15*time.Second, time.Minute); err != ErrNoAvailablePod {
		t.Fatalf("CreateSession() error = %v, want ErrNoAvailablePod", err)
	}
}

func TestHeartbeatPreservesReservationsForSameInstance(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod",
		ActiveSessions: 2, MaxSessions: 50,
	})

	pod, err := st.HeartbeatPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod",
		ActiveSessions: 1, MaxSessions: 50,
	})
	if err != nil {
		t.Fatalf("HeartbeatPod() error = %v", err)
	}
	if pod.ActiveSessions != 2 {
		t.Fatalf("ActiveSessions = %d, want 2", pod.ActiveSessions)
	}
}

func TestRegistrationResetsReservationsForNewInstance(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod",
		ActiveSessions: 2, MaxSessions: 50,
	})

	pod, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-2", WebsocketURL: "ws://pod",
		ActiveSessions: 0, MaxSessions: 50,
	})
	if err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
	if pod.ActiveSessions != 0 {
		t.Fatalf("ActiveSessions = %d, want 0", pod.ActiveSessions)
	}
}

func TestSessionHeartbeatExtendsLeaseForOwner(t *testing.T) {
	st := NewMemoryStore()
	now := time.Now()
	st.now = func() time.Time { return now }
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod", MaxSessions: 50,
	})
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	now = now.Add(10 * time.Second)
	heartbeat, err := st.HeartbeatSession(ctx, session.SessionID, model.SessionHeartbeatRequest{
		OwnerPodID: "pod", OwnerInstanceID: "instance-1",
	}, time.Minute)
	if err != nil {
		t.Fatalf("HeartbeatSession() error = %v", err)
	}
	if !heartbeat.LastHeartbeatAt.Equal(now.UTC()) || !heartbeat.LeaseExpiresAt.Equal(now.UTC().Add(time.Minute)) {
		t.Fatalf("heartbeat timestamps = %s/%s, want %s/%s", heartbeat.LastHeartbeatAt, heartbeat.LeaseExpiresAt, now.UTC(), now.UTC().Add(time.Minute))
	}
}

func TestCloseSessionUpdatesStatusAndReleasesPodOnce(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod", MaxSessions: 1,
	})
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	for range 2 {
		closed, err := st.CloseSession(ctx, session.SessionID, model.CloseSessionRequest{
			OwnerPodID: "pod", OwnerInstanceID: "instance-1",
		})
		if err != nil {
			t.Fatalf("CloseSession() error = %v", err)
		}
		if closed.Status != model.SessionStatusClosed {
			t.Fatalf("status = %q, want %q", closed.Status, model.SessionStatusClosed)
		}
	}
	pods, err := st.AvailablePods(ctx, 15*time.Second)
	if err != nil {
		t.Fatalf("AvailablePods() error = %v", err)
	}
	if len(pods) != 1 || pods[0].ActiveSessions != 0 {
		t.Fatalf("available pods = %#v, want released pod", pods)
	}
}

func TestSessionHeartbeatRejectsClosedSession(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod", MaxSessions: 1,
	})
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := st.CloseSession(ctx, session.SessionID, model.CloseSessionRequest{
		OwnerPodID: "pod", OwnerInstanceID: "instance-1",
	}); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}

	_, err = st.HeartbeatSession(ctx, session.SessionID, model.SessionHeartbeatRequest{
		OwnerPodID: "pod", OwnerInstanceID: "instance-1",
	}, time.Minute)
	if err != ErrSessionNotActive {
		t.Fatalf("HeartbeatSession() error = %v, want ErrSessionNotActive", err)
	}
}

func TestExpiredSessionReleasesPodCapacity(t *testing.T) {
	st := NewMemoryStore()
	now := time.Now()
	st.now = func() time.Time { return now }
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod", MaxSessions: 1,
	})
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	now = now.Add(time.Minute)
	expired, err := st.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if expired.Status != model.SessionStatusClosed {
		t.Fatalf("status = %q, want %q", expired.Status, model.SessionStatusClosed)
	}
	if _, err := st.HeartbeatPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod", MaxSessions: 1,
	}); err != nil {
		t.Fatalf("HeartbeatPod() error = %v", err)
	}
	pods, err := st.AvailablePods(ctx, 15*time.Second)
	if err != nil {
		t.Fatalf("AvailablePods() error = %v", err)
	}
	if len(pods) != 1 || pods[0].ActiveSessions != 0 {
		t.Fatalf("available pods = %#v, want released pod", pods)
	}
}

func TestCloseSessionRejectsOwnerMismatch(t *testing.T) {
	st := NewMemoryStore()
	ctx := context.Background()
	_, _ = st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://pod", MaxSessions: 1,
	})
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "u", HostID: "h", CRID: "cr"},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = st.CloseSession(ctx, session.SessionID, model.CloseSessionRequest{
		OwnerPodID: "other", OwnerInstanceID: "instance-1",
	})
	if err == nil {
		t.Fatal("CloseSession() error = nil, want owner mismatch")
	}
	stored, err := st.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if stored.Status != model.SessionStatusActive {
		t.Fatalf("status = %q, want %q", stored.Status, model.SessionStatusActive)
	}
}

func TestAutomationRunUsesOperationsURLAndOwnerInstance(t *testing.T) {
	st := NewMemoryStore()
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	_, _ = st.RegisterPod(t.Context(), model.ProxyPod{
		PodID: "pod", InstanceID: "instance-1", WebsocketURL: "ws://proxy",
		OperationsURL: "http://10.0.0.10:8083", MaxSessions: 50,
	})
	run, err := st.CreateAutomationRun(t.Context(), model.CreateAutomationRunRequest{
		IncidentID: "inc-1", HostID: "host-1",
	}, 15*time.Second, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if run.OwnerPodID != "pod" || run.OwnerInstanceID != "instance-1" ||
		run.OperationsURL != "http://10.0.0.10:8083" {
		t.Fatalf("run = %#v", run)
	}
	now = now.Add(time.Hour)
	expired, err := st.GetAutomationRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if expired.Status != model.AutomationRunClosed {
		t.Fatalf("status = %s", expired.Status)
	}
}
