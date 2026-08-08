package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

type integrationConnector struct{ conn *integrationSQLConn }

func (c integrationConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (integrationConnector) Driver() driver.Driver                          { return integrationSQLDriver{} }

type integrationSQLDriver struct{}

func (integrationSQLDriver) Open(string) (driver.Conn, error) { return &integrationSQLConn{}, nil }

type integrationSQLConn struct{}

func (*integrationSQLConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*integrationSQLConn) Close() error                        { return nil }
func (*integrationSQLConn) Begin() (driver.Tx, error)           { return integrationTx{}, nil }
func (*integrationSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return integrationTx{}, nil
}
func (*integrationSQLConn) Ping(context.Context) error { return nil }
func (*integrationSQLConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (*integrationSQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	now := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	pod := []driver.Value{"pod-1", "instance-1", "10.0.0.1", "wss://proxy", "http://proxy/operations", model.PodStatusReady, int64(0), int64(10), now}
	run := []driver.Value{"run-1", "inc-1", "host-1", "pod-1", "instance-1", "http://proxy/operations", model.AutomationRunActive, now, now.Add(time.Hour)}
	session := []driver.Value{"sess-1", "user-1", "host-1", "cr-1", "ssh", "pod-1", "instance-1", "wss://proxy/sessions/sess-1/ws", model.SessionStatusActive, now, now, now.Add(time.Hour)}
	switch {
	case strings.Contains(query, "automation_runs"):
		return &integrationRows{columns: integrationColumns(9), values: [][]driver.Value{run}}, nil
	case strings.Contains(query, "proxy_pods"):
		return &integrationRows{columns: integrationColumns(9), values: [][]driver.Value{pod}}, nil
	default:
		return &integrationRows{columns: integrationColumns(12), values: [][]driver.Value{session}}, nil
	}
}

type integrationTx struct{}

func (integrationTx) Commit() error   { return nil }
func (integrationTx) Rollback() error { return nil }

type integrationRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *integrationRows) Columns() []string { return r.columns }
func (*integrationRows) Close() error        { return nil }
func (r *integrationRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func integrationColumns(count int) []string {
	columns := make([]string, count)
	for i := range columns {
		columns[i] = "column"
	}
	return columns
}

func TestPostgresStoreLifecycleWithInProcessSQLDriver(t *testing.T) {
	db := sql.OpenDB(integrationConnector{conn: &integrationSQLConn{}})
	now := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	store := &PostgresStore{db: db, now: func() time.Time { return now }}
	ctx := context.Background()
	if err := store.migrate(ctx); err != nil {
		t.Fatalf("migrate() error = %v", err)
	}
	pod, err := store.RegisterPod(ctx, model.ProxyPod{PodID: "pod-1", InstanceID: "instance-1", WebsocketURL: "wss://proxy"})
	if err != nil || pod.PodID != "pod-1" {
		t.Fatalf("RegisterPod()=%#v error=%v", pod, err)
	}
	if _, err := store.HeartbeatPod(ctx, pod); err != nil {
		t.Fatalf("HeartbeatPod() error = %v", err)
	}
	pods, err := store.AvailablePods(ctx, time.Minute)
	if err != nil || len(pods) != 1 {
		t.Fatalf("AvailablePods()=%#v error=%v", pods, err)
	}
	session, err := store.CreateSession(ctx, model.CreateSessionRequest{UserID: "user-1", HostID: "host-1", CRID: "cr-1"}, time.Minute, time.Hour)
	if err != nil || session.OwnerPodID != "pod-1" || session.ConnectionType != "ssh" {
		t.Fatalf("CreateSession()=%#v error=%v", session, err)
	}
	run, err := store.CreateAutomationRun(ctx, model.CreateAutomationRunRequest{IncidentID: "inc-1", HostID: "host-1"}, time.Minute, time.Hour)
	if err != nil || run.OwnerInstanceID != "instance-1" {
		t.Fatalf("CreateAutomationRun()=%#v error=%v", run, err)
	}
	if _, err := store.GetAutomationRun(ctx, "run-1"); err != nil {
		t.Fatalf("GetAutomationRun() error = %v", err)
	}
	if _, err := store.CloseAutomationRun(ctx, "run-1"); err != nil {
		t.Fatalf("CloseAutomationRun() error = %v", err)
	}
	if _, err := store.GetSession(ctx, "sess-1"); err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	sessions, err := store.ListSessions(ctx, model.ListSessionsFilter{UserID: "user-1"})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions()=%#v error=%v", sessions, err)
	}
	if _, err := store.HeartbeatSession(ctx, "sess-1", model.SessionHeartbeatRequest{OwnerPodID: "pod-1", OwnerInstanceID: "instance-1"}, time.Hour); err != nil {
		t.Fatalf("HeartbeatSession() error = %v", err)
	}
	closed, err := store.CloseSession(ctx, "sess-1", model.CloseSessionRequest{OwnerPodID: "pod-1", OwnerInstanceID: "instance-1"})
	if err != nil || closed.Status != model.SessionStatusClosed {
		t.Fatalf("CloseSession()=%#v error=%v", closed, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
