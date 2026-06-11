package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

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
			status := http.StatusBadRequest
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			} else if errors.Is(err, store.ErrSessionNotActive) {
				status = http.StatusConflict
			}
			writeError(w, status, err.Error())
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
	registered, err := s.svc.RegisterPod(r.Context(), pod)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, registered)
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
	heartbeat, err := s.svc.HeartbeatPod(r.Context(), pod)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, heartbeat)
}

func (s *Server) availablePods(w http.ResponseWriter, r *http.Request) {
	pods, err := s.svc.AvailablePods(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pods)
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
