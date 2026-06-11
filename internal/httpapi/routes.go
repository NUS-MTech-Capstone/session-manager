package httpapi

import "net/http"

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("POST /sessions", s.createSession)
	s.mux.HandleFunc("GET /sessions/", s.getSession)
	s.mux.HandleFunc("POST /sessions/", s.sessionAction)
	s.mux.HandleFunc("POST /proxy-pods/register", s.registerPod)
	s.mux.HandleFunc("POST /proxy-pods/heartbeat", s.heartbeatPod)
	s.mux.HandleFunc("GET /proxy-pods/available", s.availablePods)
}
