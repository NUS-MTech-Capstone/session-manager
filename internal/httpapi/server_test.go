package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/service"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

func newHTTPTestServer() *Server {
	return New(service.New(store.NewMemoryStore(), 15*time.Second, time.Minute))
}

func TestCreateSessionHTTPValidationAndNoCapacity(t *testing.T) {
	server := newHTTPTestServer()
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "missing fields", body: `{"userId":"user-1","hostId":"host-1"}`, status: http.StatusBadRequest},
		{name: "unsupported connection", body: `{"userId":"user-1","hostId":"host-1","crId":"cr-1","connectionType":"agent"}`, status: http.StatusBadRequest},
		{name: "no pod", body: `{"userId":"user-1","hostId":"host-1","crId":"cr-1"}`, status: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(apiVersionHeader, supportedAPIVersion)
			server.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tt.status, rec.Body.String())
			}
		})
	}
}

func TestSessionHTTPCreateHeartbeatListAndClose(t *testing.T) {
	server := newHTTPTestServer()
	doJSON(t, server, http.MethodPost, "/proxy-pods/register", `{
		"podId":"pod-1","instanceId":"instance-1","websocketUrl":"wss://proxy","operationsUrl":"http://proxy:8083","maxSessions":1
	}`, http.StatusCreated)

	create := doJSON(t, server, http.MethodPost, "/sessions", `{"userId":"user-1","hostId":"host-1","crId":"cr-1"}`, http.StatusCreated)
	var session model.Session
	if err := json.Unmarshal(create.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.SessionID == "" || session.ConnectionType != "ssh" {
		t.Fatalf("session = %#v", session)
	}

	doJSON(t, server, http.MethodPost, "/sessions/"+session.SessionID+"/heartbeat", `{"ownerPodId":"pod-1","ownerInstanceId":"wrong"}`, http.StatusBadRequest)
	doJSON(t, server, http.MethodPost, "/sessions/"+session.SessionID+"/heartbeat", `{"ownerPodId":"pod-1","ownerInstanceId":"instance-1"}`, http.StatusOK)

	list := doJSON(t, server, http.MethodGet, "/sessions?userId=user-1&hostId=host-1&crId=cr-1&status=ACTIVE", ``, http.StatusOK)
	if !strings.Contains(list.Body.String(), session.SessionID) {
		t.Fatalf("list body = %s, want session %s", list.Body.String(), session.SessionID)
	}

	doJSON(t, server, http.MethodPost, "/sessions/"+session.SessionID+"/close", `{}`, http.StatusBadRequest)
	doJSON(t, server, http.MethodPost, "/sessions/"+session.SessionID+"/close", `{"ownerPodId":"pod-1","ownerInstanceId":"instance-1"}`, http.StatusOK)
	doJSON(t, server, http.MethodPost, "/sessions/"+session.SessionID+"/heartbeat", `{"ownerPodId":"pod-1","ownerInstanceId":"instance-1"}`, http.StatusConflict)
}

func TestPodHTTPValidationAndAvailablePods(t *testing.T) {
	server := newHTTPTestServer()
	doJSON(t, server, http.MethodPost, "/proxy-pods/register", `{"podId":"pod-1"}`, http.StatusBadRequest)
	doJSON(t, server, http.MethodPost, "/proxy-pods/register", `{
		"podId":"pod-1","instanceId":"instance-1","websocketUrl":"wss://proxy","operationsUrl":"http://proxy:8083","maxSessions":2
	}`, http.StatusCreated)
	doJSON(t, server, http.MethodPost, "/proxy-pods/heartbeat", `{
		"podId":"pod-1","instanceId":"instance-1","websocketUrl":"wss://proxy","operationsUrl":"http://proxy:8083","maxSessions":2
	}`, http.StatusOK)
	rec := doJSON(t, server, http.MethodGet, "/proxy-pods/available", ``, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "pod-1") {
		t.Fatalf("available body = %s", rec.Body.String())
	}
}

func TestSessionHTTPGetAndUnknownActions(t *testing.T) {
	server := newHTTPTestServer()
	doJSON(t, server, http.MethodGet, "/sessions/missing", ``, http.StatusNotFound)
	doJSON(t, server, http.MethodPost, "/sessions/sess-1/unknown", `{}`, http.StatusNotFound)
}

func TestAutomationRunHTTPLifecycle(t *testing.T) {
	server := newHTTPTestServer()
	doJSON(t, server, http.MethodPost, "/proxy-pods/register", `{
		"podId":"pod-1","instanceId":"instance-1","websocketUrl":"wss://proxy","operationsUrl":"http://proxy:8083","maxSessions":2
	}`, http.StatusCreated)

	doJSON(t, server, http.MethodPost, "/automation/runs", `{}`, http.StatusBadRequest)
	create := doJSON(t, server, http.MethodPost, "/automation/runs", `{"incidentId":"inc-1","hostId":"host-1"}`, http.StatusCreated)
	var run model.AutomationRun
	if err := json.Unmarshal(create.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.RunID == "" || run.OwnerInstanceID != "instance-1" {
		t.Fatalf("run = %#v", run)
	}

	doJSON(t, server, http.MethodGet, "/automation/runs/"+run.RunID, ``, http.StatusOK)
	doJSON(t, server, http.MethodPost, "/automation/runs/"+run.RunID+"/close", `{}`, http.StatusOK)
	doJSON(t, server, http.MethodGet, "/automation/runs/missing", ``, http.StatusNotFound)
	doJSON(t, server, http.MethodPost, "/automation/runs/missing/close", `{}`, http.StatusNotFound)
	doJSON(t, server, http.MethodPost, "/automation/runs/"+run.RunID+"/other", `{}`, http.StatusNotFound)
}

func doJSON(t *testing.T, server *Server, method, path, body string, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(apiVersionHeader, supportedAPIVersion)
	server.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec
}
