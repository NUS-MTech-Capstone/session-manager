package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func TestPostgresStorePersistsSessionAcrossConnections(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	pod, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID:        "pod-persistence",
		InstanceID:   "instance-1",
		WebsocketURL: "ws://proxy",
		MaxSessions:  1,
	})
	if err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}

	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{
			UserID: "user-1", HostID: "host-1", CRID: "cr-1", ConnectionType: "ssh",
		},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.OwnerPodID != pod.PodID || session.OwnerInstanceID != pod.InstanceID {
		t.Fatalf("owner = %s/%s, want %s/%s", session.OwnerPodID, session.OwnerInstanceID, pod.PodID, pod.InstanceID)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("NewPostgresStore() after close error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	stored, err := reopened.GetSession(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("GetSession() after reconnect error = %v", err)
	}
	if stored.SessionID != session.SessionID || stored.WebsocketURL != session.WebsocketURL {
		t.Fatalf("stored session = %#v, want sessionId=%q websocketUrl=%q", stored, session.SessionID, session.WebsocketURL)
	}
	if stored.ConnectionType != "ssh" {
		t.Fatalf("ConnectionType = %q, want ssh", stored.ConnectionType)
	}

	closed, err := reopened.CloseSession(ctx, session.SessionID, model.CloseSessionRequest{
		OwnerPodID: pod.PodID, OwnerInstanceID: pod.InstanceID,
	})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if closed.Status != model.SessionStatusClosed {
		t.Fatalf("status = %q, want %q", closed.Status, model.SessionStatusClosed)
	}

	available, err := reopened.AvailablePods(ctx, 15*time.Second)
	if err != nil {
		t.Fatalf("AvailablePods() error = %v", err)
	}
	if len(available) != 1 || available[0].ActiveSessions != 0 {
		t.Fatalf("available pods = %#v, want one pod with zero active sessions", available)
	}
}

func TestPostgresStoreRejectsStalePod(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	if _, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID:        "pod-stale",
		InstanceID:   "instance-1",
		WebsocketURL: "ws://proxy",
		MaxSessions:  1,
	}); err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
	st.now = func() time.Time { return now.Add(16 * time.Second) }

	_, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "user-1", HostID: "host-1", CRID: "cr-1"},
		15*time.Second,
		time.Minute,
	)
	if err != ErrNoAvailablePod {
		t.Fatalf("CreateSession() error = %v, want ErrNoAvailablePod", err)
	}
}

func TestPostgresStoreExpiresSessionAndReleasesCapacity(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	pod, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-expiry", InstanceID: "instance-1", WebsocketURL: "ws://proxy", MaxSessions: 1,
	})
	if err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "user-1", HostID: "host-1", CRID: "cr-1"},
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
		PodID: pod.PodID, InstanceID: pod.InstanceID, WebsocketURL: pod.WebsocketURL, MaxSessions: 1,
	}); err != nil {
		t.Fatalf("HeartbeatPod() error = %v", err)
	}
	available, err := st.AvailablePods(ctx, 15*time.Second)
	if err != nil {
		t.Fatalf("AvailablePods() error = %v", err)
	}
	if len(available) != 1 || available[0].ActiveSessions != 0 {
		t.Fatalf("available pods = %#v, want released pod", available)
	}
}

func TestPostgresStoreListSessionsFiltersAndOrders(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	if _, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-list", InstanceID: "instance-1", WebsocketURL: "ws://proxy", MaxSessions: 10,
	}); err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
	first, err := st.CreateSession(ctx, model.CreateSessionRequest{
		UserID: "user-1", HostID: "host-1", CRID: "cr-1",
	}, 15*time.Second, time.Minute)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	now = now.Add(time.Second)
	second, err := st.CreateSession(ctx, model.CreateSessionRequest{
		UserID: "user-1", HostID: "host-1", CRID: "cr-1",
	}, 15*time.Second, time.Minute)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	now = now.Add(time.Second)
	if _, err := st.CreateSession(ctx, model.CreateSessionRequest{
		UserID: "user-2", HostID: "host-1", CRID: "cr-1",
	}, 15*time.Second, time.Minute); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	sessions, err := st.ListSessions(ctx, model.ListSessionsFilter{
		UserID: "user-1", HostID: "host-1", CRID: "cr-1", Status: model.SessionStatusActive,
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 2 || sessions[0].SessionID != second.SessionID || sessions[1].SessionID != first.SessionID {
		t.Fatalf("ListSessions() = %#v, want second then first", sessions)
	}

	sessions, err = st.ListSessions(ctx, model.ListSessionsFilter{
		UserID: "user-1", HostID: "host-2", CRID: "cr-1", Status: model.SessionStatusActive,
	})
	if err != nil {
		t.Fatalf("ListSessions() no match error = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("ListSessions() no match = %#v, want empty result", sessions)
	}
}

func TestPostgresStoreListSessionsExcludesExpiredActiveSessions(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	now := time.Now().UTC()
	st.now = func() time.Time { return now }
	if _, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-list-expiry", InstanceID: "instance-1", WebsocketURL: "ws://proxy", MaxSessions: 1,
	}); err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
	if _, err := st.CreateSession(ctx, model.CreateSessionRequest{
		UserID: "user-1", HostID: "host-1", CRID: "cr-1",
	}, 15*time.Second, time.Minute); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	now = now.Add(time.Minute)
	sessions, err := st.ListSessions(ctx, model.ListSessionsFilter{
		UserID: "user-1", HostID: "host-1", CRID: "cr-1", Status: model.SessionStatusActive,
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("ListSessions() = %#v, want expired session excluded", sessions)
	}
}

func TestPostgresStoreRejectsCloseOwnerMismatch(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	_, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-close-owner", InstanceID: "instance-1", WebsocketURL: "ws://proxy", MaxSessions: 1,
	})
	if err != nil {
		t.Fatalf("RegisterPod() error = %v", err)
	}
	session, err := st.CreateSession(
		ctx,
		model.CreateSessionRequest{UserID: "user-1", HostID: "host-1", CRID: "cr-1"},
		15*time.Second,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = st.CloseSession(ctx, session.SessionID, model.CloseSessionRequest{
		OwnerPodID: "other-pod", OwnerInstanceID: "instance-1",
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

func TestPostgresStorePersistsAutomationRunOwnership(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st := newTestPostgresStore(t, ctx, databaseURL)
	pod, err := st.RegisterPod(ctx, model.ProxyPod{
		PodID: "pod-automation", InstanceID: "instance-automation",
		WebsocketURL: "ws://proxy", OperationsURL: "http://proxy:8083", MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateAutomationRun(ctx, model.CreateAutomationRunRequest{
		IncidentID: "incident-1", HostID: "host-1",
	}, 15*time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if run.OwnerPodID != pod.PodID || run.OwnerInstanceID != pod.InstanceID ||
		run.OperationsURL != pod.OperationsURL {
		t.Fatalf("automation run = %#v", run)
	}
	stored, err := st.GetAutomationRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnerInstanceID != "instance-automation" {
		t.Fatalf("stored automation run = %#v", stored)
	}
}

func newTestPostgresStore(t *testing.T, ctx context.Context, databaseURL string) *PostgresStore {
	t.Helper()
	st, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	if _, err := st.db.ExecContext(ctx, `TRUNCATE automation_runs, sessions, proxy_pods`); err != nil {
		_ = st.Close()
		t.Fatalf("truncate test tables: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
