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
		model.CreateSessionRequest{UserID: "user-1", HostID: "host-1", CRID: "cr-1"},
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

	closed, err := reopened.CloseSession(ctx, session.SessionID)
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

func newTestPostgresStore(t *testing.T, ctx context.Context, databaseURL string) *PostgresStore {
	t.Helper()
	st, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("NewPostgresStore() error = %v", err)
	}
	if _, err := st.db.ExecContext(ctx, `TRUNCATE sessions, proxy_pods`); err != nil {
		_ = st.Close()
		t.Fatalf("truncate test tables: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
