package service

import (
	"testing"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func TestServiceForwardsSimpleStoreMethods(t *testing.T) {
	store := &recordingStore{}
	svc := New(store, 0, 0)

	if pod, err := svc.RegisterPod(t.Context(), model.ProxyPod{PodID: "pod-1"}); err != nil || pod.PodID != "pod-1" {
		t.Fatalf("RegisterPod() = %#v, %v", pod, err)
	}
	if pod, err := svc.HeartbeatPod(t.Context(), model.ProxyPod{PodID: "pod-2"}); err != nil || pod.PodID != "pod-2" {
		t.Fatalf("HeartbeatPod() = %#v, %v", pod, err)
	}
	if session, err := svc.GetSession(t.Context(), "sess-1"); err != nil || session.SessionID != "sess-1" {
		t.Fatalf("GetSession() = %#v, %v", session, err)
	}
	if sessions, err := svc.ListSessions(t.Context(), model.ListSessionsFilter{}); err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions() = %#v, %v", sessions, err)
	}
	if session, err := svc.CloseSession(t.Context(), "sess-1", model.CloseSessionRequest{}); err != nil || session.Status != model.SessionStatusClosed {
		t.Fatalf("CloseSession() = %#v, %v", session, err)
	}
	if run, err := svc.GetAutomationRun(t.Context(), "run-1"); err != nil || run.RunID != "run-1" {
		t.Fatalf("GetAutomationRun() = %#v, %v", run, err)
	}
	if run, err := svc.CloseAutomationRun(t.Context(), "run-1"); err != nil || run.Status != model.AutomationRunClosed {
		t.Fatalf("CloseAutomationRun() = %#v, %v", run, err)
	}

	if store.registerPod.PodID != "pod-1" || store.heartbeatPod.PodID != "pod-2" {
		t.Fatalf("store calls = %#v %#v", store.registerPod, store.heartbeatPod)
	}
}
