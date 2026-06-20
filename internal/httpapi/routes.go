package httpapi

import (
	"net/http"

	"github.com/NUS-MTech-Capstone/session-manager/api"
)

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /openapi.yaml", serveYAML(api.OpenAPI))
	s.mux.HandleFunc("POST /sessions", s.createSession)
	s.mux.HandleFunc("GET /sessions", s.listSessions)
	s.mux.HandleFunc("GET /sessions/", s.getSession)
	s.mux.HandleFunc("POST /sessions/", s.sessionAction)
	s.mux.HandleFunc("POST /proxy-pods/register", s.registerPod)
	s.mux.HandleFunc("POST /proxy-pods/heartbeat", s.heartbeatPod)
	s.mux.HandleFunc("GET /proxy-pods/available", s.availablePods)
	s.mux.HandleFunc("POST /automation/runs", s.createAutomationRun)
	s.mux.HandleFunc("GET /automation/runs/", s.getAutomationRun)
	s.mux.HandleFunc("POST /automation/runs/", s.automationRunAction)
}
