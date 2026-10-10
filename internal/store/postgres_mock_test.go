package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

var fixedTime = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
var podCols = []string{"pod_id", "instance_id", "pod_ip", "websocket_url", "operations_url", "status", "active_sessions", "max_sessions", "last_heartbeat_at"}
var sessionCols = []string{"session_id", "user_id", "host_id", "cr_id", "connection_type", "owner_pod_id", "owner_instance_id", "websocket_url", "status", "created_at", "last_heartbeat_at", "lease_expires_at"}
var runCols = []string{"run_id", "incident_id", "host_id", "owner_pod_id", "owner_instance_id", "operations_url", "status", "created_at", "expires_at"}

func mockRegistry(t *testing.T) (*PostgresStore, sqlmock.Sqlmock) {
	t.Helper()
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		_ = db.Close()
	})
	return &PostgresStore{db: db, now: func() time.Time { return fixedTime }}, m
}
func podResult() *sqlmock.Rows {
	return sqlmock.NewRows(podCols).AddRow("pod", "instance", "10.0.0.1", "wss://proxy", "http://proxy", "READY", 2, 50, fixedTime)
}
func sessionResult(status string) *sqlmock.Rows {
	return sqlmock.NewRows(sessionCols).AddRow("sess", "user", "host", "cr", "ssh", "pod", "instance", "wss://proxy/sessions/sess/ws", status, fixedTime, fixedTime, fixedTime.Add(time.Minute))
}
func runResult(status string) *sqlmock.Rows {
	return sqlmock.NewRows(runCols).AddRow("run", "inc", "host", "pod", "instance", "http://proxy", status, fixedTime, fixedTime.Add(time.Minute))
}
func expectExpiry(m sqlmock.Sqlmock) {
	m.ExpectExec("WITH expired AS.*UPDATE proxy_pods").WithArgs(model.SessionStatusClosed, model.SessionStatusActive, fixedTime).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestSQLRegistryRegistersAndListsHealthyPods(t *testing.T) {
	s, m := mockRegistry(t)
	m.ExpectQuery("INSERT INTO proxy_pods.*WHEN proxy_pods.instance_id = EXCLUDED.instance_id.*GREATEST").WithArgs("pod", "instance", "10.0.0.1", "wss://proxy", "http://proxy", "READY", 0, 50, fixedTime).WillReturnRows(podResult())
	pod, err := s.HeartbeatPod(t.Context(), model.ProxyPod{PodID: "pod", InstanceID: "instance", PodIP: "10.0.0.1", WebsocketURL: "wss://proxy", OperationsURL: "http://proxy"})
	if err != nil || pod.ActiveSessions != 2 || pod.InstanceID != "instance" {
		t.Fatalf("pod=%+v err=%v", pod, err)
	}
	expectExpiry(m)
	m.ExpectQuery("SELECT pod_id.*last_heartbeat_at >.*active_sessions < max_sessions.*ORDER BY active_sessions, pod_id").WithArgs(model.PodStatusReady, fixedTime.Add(-time.Minute)).WillReturnRows(podResult()).RowsWillBeClosed()
	pods, err := s.AvailablePods(t.Context(), time.Minute)
	if err != nil || len(pods) != 1 || pods[0].PodID != "pod" {
		t.Fatalf("pods=%+v err=%v", pods, err)
	}
}

func TestSQLSessionAssignmentPersistsOwnerAndLease(t *testing.T) {
	s, m := mockRegistry(t)
	m.ExpectBegin()
	expectExpiry(m)
	m.ExpectQuery("SELECT pod_id.*active_sessions < max_sessions.*FOR UPDATE SKIP LOCKED LIMIT 1").WithArgs(model.PodStatusReady, fixedTime.Add(-time.Minute)).WillReturnRows(podResult())
	m.ExpectExec("UPDATE proxy_pods SET active_sessions = active_sessions \\+ 1").WithArgs("pod").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO sessions").WithArgs(sqlmock.AnyArg(), "user", "host", "cr", "ssh", "pod", "instance", sqlmock.AnyArg(), model.SessionStatusActive, fixedTime, fixedTime, fixedTime.Add(2*time.Minute)).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	got, err := s.CreateSession(t.Context(), model.CreateSessionRequest{UserID: "user", HostID: "host", CRID: "cr"}, time.Minute, 2*time.Minute)
	if err != nil || got.SessionID == "" || got.OwnerPodID != "pod" || got.OwnerInstanceID != "instance" || got.WebsocketURL != "wss://proxy/sessions/"+got.SessionID+"/ws" || !got.LeaseExpiresAt.Equal(fixedTime.Add(2*time.Minute)) {
		t.Fatalf("assignment=%+v err=%v", got, err)
	}
}

func TestSQLAssignmentFailuresRollback(t *testing.T) {
	for _, stage := range []string{"begin", "expiry", "no pod", "select", "reserve", "insert", "commit"} {
		t.Run(stage, func(t *testing.T) {
			s, m := mockRegistry(t)
			failure := errors.New("database unavailable")
			begin := m.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				expiry := m.ExpectExec("WITH expired AS")
				if stage == "expiry" {
					expiry.WillReturnError(failure)
				} else {
					expiry.WillReturnResult(sqlmock.NewResult(0, 0))
					q := m.ExpectQuery("SELECT pod_id")
					if stage == "no pod" {
						q.WillReturnRows(sqlmock.NewRows(podCols))
					} else if stage == "select" {
						q.WillReturnError(failure)
					} else {
						q.WillReturnRows(podResult())
						reserve := m.ExpectExec("UPDATE proxy_pods SET active_sessions")
						if stage == "reserve" {
							reserve.WillReturnError(failure)
						} else {
							reserve.WillReturnResult(sqlmock.NewResult(0, 1))
							insert := m.ExpectExec("INSERT INTO sessions")
							if stage == "insert" {
								insert.WillReturnError(failure)
							} else {
								insert.WillReturnResult(sqlmock.NewResult(1, 1))
								m.ExpectCommit().WillReturnError(failure)
							}
						}
					}
				}
				if stage != "commit" {
					m.ExpectRollback()
				}
			}
			got, err := s.CreateSession(t.Context(), model.CreateSessionRequest{}, time.Minute, time.Minute)
			if err == nil || got.SessionID != "" {
				t.Fatalf("assignment accepted: %+v %v", got, err)
			}
			if stage == "no pod" {
				if !errors.Is(err, ErrNoAvailablePod) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("lost cause: %v", err)
			}
		})
	}
}

func TestSQLSessionReadsAndHeartbeat(t *testing.T) {
	s, m := mockRegistry(t)
	expectExpiry(m)
	m.ExpectQuery("SELECT session_id.*FROM sessions WHERE session_id").WithArgs("sess").WillReturnRows(sessionResult(model.SessionStatusActive))
	got, err := s.GetSession(t.Context(), "sess")
	if err != nil || got.OwnerInstanceID != "instance" {
		t.Fatalf("session=%+v err=%v", got, err)
	}
	expectExpiry(m)
	m.ExpectQuery("SELECT session_id.*ORDER BY created_at DESC, session_id").WithArgs("user", "host", "cr", model.SessionStatusActive).WillReturnRows(sessionResult(model.SessionStatusActive)).RowsWillBeClosed()
	list, err := s.ListSessions(t.Context(), model.ListSessionsFilter{UserID: "user", HostID: "host", CRID: "cr", Status: model.SessionStatusActive})
	if err != nil || len(list) != 1 || list[0].SessionID != "sess" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	expectExpiry(m)
	m.ExpectQuery("UPDATE sessions SET last_heartbeat_at.*owner_pod_id = \\$4 AND owner_instance_id = \\$5 AND status = \\$6").WithArgs(fixedTime, fixedTime.Add(time.Minute), "sess", "pod", "instance", model.SessionStatusActive).WillReturnRows(sessionResult(model.SessionStatusActive))
	got, err = s.HeartbeatSession(t.Context(), "sess", model.SessionHeartbeatRequest{OwnerPodID: "pod", OwnerInstanceID: "instance"}, time.Minute)
	if err != nil || !got.LeaseExpiresAt.Equal(fixedTime.Add(time.Minute)) {
		t.Fatalf("heartbeat=%+v err=%v", got, err)
	}
}

func TestSQLHeartbeatClassifiesDeniedOwnership(t *testing.T) {
	for _, state := range []string{"missing", model.SessionStatusClosed, model.SessionStatusActive} {
		t.Run(state, func(t *testing.T) {
			s, m := mockRegistry(t)
			expectExpiry(m)
			m.ExpectQuery("UPDATE sessions SET last_heartbeat_at").WithArgs(fixedTime, fixedTime.Add(time.Minute), "sess", "pod", "stale", model.SessionStatusActive).WillReturnRows(sqlmock.NewRows(sessionCols))
			expectExpiry(m)
			q := m.ExpectQuery("SELECT session_id.*FROM sessions").WithArgs("sess")
			if state == "missing" {
				q.WillReturnError(sql.ErrNoRows)
			} else {
				q.WillReturnRows(sessionResult(state))
			}
			_, err := s.HeartbeatSession(t.Context(), "sess", model.SessionHeartbeatRequest{OwnerPodID: "pod", OwnerInstanceID: "stale"}, time.Minute)
			if state == "missing" && !errors.Is(err, ErrNotFound) {
				t.Fatal(err)
			}
			if state == model.SessionStatusClosed && !errors.Is(err, ErrSessionNotActive) {
				t.Fatal(err)
			}
			if state == model.SessionStatusActive && (err == nil || !strings.Contains(err.Error(), "owner mismatch")) {
				t.Fatal(err)
			}
		})
	}
}

func TestSQLCloseChecksBothOwnersAndReleasesOnce(t *testing.T) {
	for _, tc := range []struct {
		name, status, pod, instance string
		denied                      bool
	}{
		{"active", model.SessionStatusActive, "pod", "instance", false}, {"already closed", model.SessionStatusClosed, "pod", "instance", false}, {"wrong pod", model.SessionStatusActive, "other", "instance", true}, {"stale process", model.SessionStatusActive, "pod", "stale", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, m := mockRegistry(t)
			m.ExpectBegin()
			expectExpiry(m)
			m.ExpectQuery("SELECT session_id.*FOR UPDATE").WithArgs("sess").WillReturnRows(sessionResult(tc.status))
			if tc.denied {
				m.ExpectRollback()
			} else {
				if tc.status == model.SessionStatusActive {
					m.ExpectExec("UPDATE sessions SET status").WithArgs(model.SessionStatusClosed, "sess").WillReturnResult(sqlmock.NewResult(0, 1))
					m.ExpectExec("UPDATE proxy_pods SET active_sessions = GREATEST").WithArgs("pod").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				m.ExpectCommit()
			}
			got, err := s.CloseSession(t.Context(), "sess", model.CloseSessionRequest{OwnerPodID: tc.pod, OwnerInstanceID: tc.instance})
			if tc.denied {
				if err == nil || !strings.Contains(err.Error(), "owner mismatch") {
					t.Fatal(err)
				}
			} else if err != nil || got.Status != model.SessionStatusClosed {
				t.Fatalf("close=%+v err=%v", got, err)
			}
		})
	}
}

func TestSQLAutomationLifecycle(t *testing.T) {
	s, m := mockRegistry(t)
	m.ExpectBegin()
	m.ExpectQuery("SELECT pod_id.*operations_url <> ''.*FOR UPDATE SKIP LOCKED").WithArgs(model.PodStatusReady, fixedTime.Add(-time.Minute)).WillReturnRows(podResult())
	m.ExpectExec("INSERT INTO automation_runs").WithArgs(sqlmock.AnyArg(), "inc", "host", "pod", "instance", "http://proxy", model.AutomationRunActive, fixedTime, fixedTime.Add(time.Minute)).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	got, err := s.CreateAutomationRun(t.Context(), model.CreateAutomationRunRequest{IncidentID: "inc", HostID: "host"}, time.Minute, time.Minute)
	if err != nil || got.RunID == "" || got.OwnerInstanceID != "instance" || got.OperationsURL != "http://proxy" || !got.ExpiresAt.Equal(fixedTime.Add(time.Minute)) {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	m.ExpectExec("UPDATE automation_runs SET status").WithArgs(model.AutomationRunClosed, "run", model.AutomationRunActive, fixedTime).WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT run_id.*FROM automation_runs").WithArgs("run").WillReturnRows(runResult(model.AutomationRunActive))
	got, err = s.GetAutomationRun(t.Context(), "run")
	if err != nil || got.Status != model.AutomationRunActive {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	m.ExpectQuery("UPDATE automation_runs SET status").WithArgs(model.AutomationRunClosed, "run").WillReturnRows(runResult(model.AutomationRunClosed))
	got, err = s.CloseAutomationRun(t.Context(), "run")
	if err != nil || got.Status != model.AutomationRunClosed {
		t.Fatalf("closed=%+v err=%v", got, err)
	}
}

func TestSQLAutomationAssignmentFailure(t *testing.T) {
	for _, stage := range []string{"begin", "no pod", "select", "insert", "commit"} {
		t.Run(stage, func(t *testing.T) {
			s, m := mockRegistry(t)
			failure := errors.New("database unavailable")
			begin := m.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				q := m.ExpectQuery("SELECT pod_id")
				switch stage {
				case "no pod":
					q.WillReturnRows(sqlmock.NewRows(podCols))
				case "select":
					q.WillReturnError(failure)
				default:
					q.WillReturnRows(podResult())
					insert := m.ExpectExec("INSERT INTO automation_runs")
					if stage == "insert" {
						insert.WillReturnError(failure)
					} else {
						insert.WillReturnResult(sqlmock.NewResult(0, 1))
						m.ExpectCommit().WillReturnError(failure)
					}
				}
				if stage != "commit" {
					m.ExpectRollback()
				}
			}
			got, err := s.CreateAutomationRun(t.Context(), model.CreateAutomationRunRequest{}, time.Minute, time.Minute)
			if err == nil || got.RunID != "" {
				t.Fatalf("assignment=%+v err=%v", got, err)
			}
			if stage == "no pod" {
				if !errors.Is(err, ErrNoAvailablePod) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatal(err)
			}
		})
	}
}
