package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

type scannerFunc func(...any) error

func (f scannerFunc) Scan(dest ...any) error {
	return f(dest...)
}

func TestPostgresScanHelpers(t *testing.T) {
	now := time.Date(2026, time.June, 24, 12, 0, 0, 0, time.UTC)
	var pod model.ProxyPod
	if err := scanPod(scannerFunc(func(dest ...any) error {
		*(dest[0].(*string)) = "pod-1"
		*(dest[1].(*string)) = "instance-1"
		*(dest[2].(*string)) = "10.0.0.10"
		*(dest[3].(*string)) = "ws://proxy"
		*(dest[4].(*string)) = "http://ops"
		*(dest[5].(*string)) = model.PodStatusReady
		*(dest[6].(*int)) = 1
		*(dest[7].(*int)) = 2
		*(dest[8].(*time.Time)) = now
		return nil
	}), &pod); err != nil {
		t.Fatal(err)
	}
	if pod.PodID != "pod-1" || pod.OperationsURL != "http://ops" || pod.ActiveSessions != 1 {
		t.Fatalf("pod = %#v", pod)
	}

	var run model.AutomationRun
	if err := scanAutomationRun(scannerFunc(func(dest ...any) error {
		*(dest[0].(*string)) = "run-1"
		*(dest[1].(*string)) = "inc-1"
		*(dest[2].(*string)) = "host-1"
		*(dest[3].(*string)) = "pod-1"
		*(dest[4].(*string)) = "instance-1"
		*(dest[5].(*string)) = "http://ops"
		*(dest[6].(*string)) = model.AutomationRunActive
		*(dest[7].(*time.Time)) = now
		*(dest[8].(*time.Time)) = now.Add(time.Hour)
		return nil
	}), &run); err != nil {
		t.Fatal(err)
	}
	if run.RunID != "run-1" || run.OwnerInstanceID != "instance-1" {
		t.Fatalf("run = %#v", run)
	}

	var session model.Session
	if err := scanSession(scannerFunc(func(dest ...any) error {
		*(dest[0].(*string)) = "sess-1"
		*(dest[1].(*string)) = "user-1"
		*(dest[2].(*string)) = "host-1"
		*(dest[3].(*string)) = "cr-1"
		*(dest[4].(*string)) = "ssh"
		*(dest[5].(*string)) = "pod-1"
		*(dest[6].(*string)) = "instance-1"
		*(dest[7].(*string)) = "ws://proxy/sessions/sess-1/ws"
		*(dest[8].(*string)) = model.SessionStatusActive
		*(dest[9].(*time.Time)) = now
		*(dest[10].(*time.Time)) = now
		*(dest[11].(*time.Time)) = now.Add(time.Minute)
		return nil
	}), &session); err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "sess-1" || session.ConnectionType != "ssh" {
		t.Fatalf("session = %#v", session)
	}
}

func TestPostgresScanHelpersReturnErrors(t *testing.T) {
	errScan := errors.New("scan failed")
	if err := scanPod(scannerFunc(func(...any) error { return errScan }), &model.ProxyPod{}); !errors.Is(err, errScan) {
		t.Fatalf("scanPod() error = %v", err)
	}
	if err := scanAutomationRun(scannerFunc(func(...any) error { return errScan }), &model.AutomationRun{}); !errors.Is(err, errScan) {
		t.Fatalf("scanAutomationRun() error = %v", err)
	}
	if err := scanSession(scannerFunc(func(...any) error { return errScan }), &model.Session{}); !errors.Is(err, errScan) {
		t.Fatalf("scanSession() error = %v", err)
	}
}

type execFunc func(context.Context, string, ...any) (sql.Result, error)

func (f execFunc) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return f(ctx, query, args...)
}

func TestExpireSessionsWrapsExecutorError(t *testing.T) {
	st := &PostgresStore{}
	errExec := errors.New("db down")
	err := st.expireSessions(t.Context(), execFunc(func(context.Context, string, ...any) (sql.Result, error) {
		return nil, errExec
	}), time.Now())
	if err == nil || !errors.Is(err, errExec) {
		t.Fatalf("expireSessions() error = %v, want wrapped db error", err)
	}
}

func TestPostgresIDHelpers(t *testing.T) {
	sessionID, err := sessionID()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionID) != len("sess-")+32 || sessionID[:5] != "sess-" {
		t.Fatalf("sessionID() = %q", sessionID)
	}
	runID, err := automationRunID()
	if err != nil {
		t.Fatal(err)
	}
	if len(runID) != len("run-")+32 || runID[:4] != "run-" {
		t.Fatalf("automationRunID() = %q", runID)
	}
}
