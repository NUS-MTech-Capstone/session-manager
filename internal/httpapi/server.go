package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/service"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

type Server struct {
	svc *service.Service
	mux *http.ServeMux
}

func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}

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

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req model.CreateSessionRequest
	if !decode(w, r, &req) {
		return
	}
	if req.UserID == "" || req.HostID == "" || req.CRID == "" {
		writeError(w, http.StatusBadRequest, "userId, hostId and crId are required")
		return
	}
	session, err := s.svc.CreateSession(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNoAvailablePod) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	log.Printf("SessionCreated sessionId=%s ownerPodId=%s", session.SessionID, session.OwnerPodID)
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id, action := parseSessionPath(r.URL.Path)
	if action != "" {
		http.NotFound(w, r)
		return
	}
	session, err := s.svc.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) sessionAction(w http.ResponseWriter, r *http.Request) {
	id, action := parseSessionPath(r.URL.Path)
	switch action {
	case "heartbeat":
		var req model.SessionHeartbeatRequest
		if !decode(w, r, &req) {
			return
		}
		session, err := s.svc.HeartbeatSession(r.Context(), id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, session)
	case "close":
		session, err := s.svc.CloseSession(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		log.Printf("SessionClosed sessionId=%s", session.SessionID)
		writeJSON(w, http.StatusOK, session)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) registerPod(w http.ResponseWriter, r *http.Request) {
	var pod model.ProxyPod
	if !decode(w, r, &pod) {
		return
	}
	if pod.PodID == "" || pod.InstanceID == "" || pod.WebsocketURL == "" {
		writeError(w, http.StatusBadRequest, "podId, instanceId and websocketUrl are required")
		return
	}
	writeJSON(w, http.StatusCreated, s.svc.RegisterPod(r.Context(), pod))
}

func (s *Server) heartbeatPod(w http.ResponseWriter, r *http.Request) {
	var pod model.ProxyPod
	if !decode(w, r, &pod) {
		return
	}
	if pod.PodID == "" || pod.InstanceID == "" || pod.WebsocketURL == "" {
		writeError(w, http.StatusBadRequest, "podId, instanceId and websocketUrl are required")
		return
	}
	writeJSON(w, http.StatusOK, s.svc.HeartbeatPod(r.Context(), pod))
}

func (s *Server) availablePods(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.AvailablePods(r.Context()))
}

func parseSessionPath(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "sessions" {
		return "", ""
	}
	if len(parts) == 2 {
		return parts[1], ""
	}
	return parts[1], parts[2]
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}
