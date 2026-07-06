package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/observability"
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
	if req.ConnectionType == "" {
		req.ConnectionType = "ssh"
	}
	if req.ConnectionType != "ssh" {
		writeError(w, http.StatusBadRequest, "only ssh connectionType is supported")
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
	slog.InfoContext(r.Context(), "SessionCreated",
		"requestId", observability.RequestID(r.Context()),
		"sessionId", session.SessionID,
		"userId", session.UserID,
		"hostId", session.HostID,
		"crId", session.CRID,
		"podId", session.OwnerPodID,
		"instanceId", session.OwnerInstanceID,
		"action", "start",
		"mode", "owner",
	)
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id, action := parseSessionPath(r.URL.Path)
	if action == "audit/records" {
		s.listSessionAuditRecords(w, r, id)
		return
	}
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

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	sessions, err := s.svc.ListSessions(r.Context(), model.ListSessionsFilter{
		UserID: query.Get("userId"),
		HostID: query.Get("hostId"),
		CRID:   query.Get("crId"),
		Status: query.Get("status"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, model.ListSessionsResponse{Sessions: sessions})
}

func (s *Server) listAuditSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	from, err := parseOptionalTime(query.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "from must be RFC3339")
		return
	}
	to, err := parseOptionalTime(query.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "to must be RFC3339")
		return
	}
	response, err := s.svc.ListAuditSessions(r.Context(), model.ListAuditSessionsFilter{
		SessionID: query.Get("sessionId"),
		UserID:    query.Get("userId"),
		HostID:    query.Get("hostId"),
		CRID:      query.Get("crId"),
		From:      from,
		To:        to,
		Limit:     parsePositiveInt(query.Get("limit"), 50),
		Offset:    parsePositiveInt(query.Get("cursor"), 0),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listSessionAuditRecords(w http.ResponseWriter, r *http.Request, sessionID string) {
	query := r.URL.Query()
	afterSequence, err := parseOptionalInt64(query.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cursor must be an integer sequence")
		return
	}
	response, err := s.svc.ListSessionAuditRecords(r.Context(), sessionID, model.ListSessionAuditRecordsFilter{
		AfterSequence: afterSequence,
		Limit:         parsePositiveInt(query.Get("limit"), 100),
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
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
		var req model.CloseSessionRequest
		if !decode(w, r, &req) {
			return
		}
		if req.OwnerPodID == "" || req.OwnerInstanceID == "" {
			writeError(w, http.StatusBadRequest, "ownerPodId and ownerInstanceId are required")
			return
		}
		session, err := s.svc.CloseSession(r.Context(), id, req)
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
		slog.InfoContext(r.Context(), "SessionClosed",
			"requestId", observability.RequestID(r.Context()),
			"sessionId", session.SessionID,
			"userId", session.UserID,
			"hostId", session.HostID,
			"crId", session.CRID,
			"podId", session.OwnerPodID,
			"instanceId", session.OwnerInstanceID,
		)
		writeJSON(w, http.StatusOK, session)
	case "audit/records":
		var req model.AppendSessionAuditRecordsRequest
		if !decode(w, r, &req) {
			return
		}
		if req.OwnerPodID == "" || req.OwnerInstanceID == "" {
			writeError(w, http.StatusBadRequest, "ownerPodId and ownerInstanceId are required")
			return
		}
		records, err := s.svc.AppendSessionAuditRecords(r.Context(), id, req)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, store.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, model.ListSessionAuditRecordsResponse{Records: records})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) registerPod(w http.ResponseWriter, r *http.Request) {
	var pod model.ProxyPod
	if !decode(w, r, &pod) {
		return
	}
	if pod.PodID == "" || pod.InstanceID == "" || pod.WebsocketURL == "" || pod.OperationsURL == "" {
		writeError(w, http.StatusBadRequest, "podId, instanceId, websocketUrl and operationsUrl are required")
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
	if pod.PodID == "" || pod.InstanceID == "" || pod.WebsocketURL == "" || pod.OperationsURL == "" {
		writeError(w, http.StatusBadRequest, "podId, instanceId, websocketUrl and operationsUrl are required")
		return
	}
	heartbeat, err := s.svc.HeartbeatPod(r.Context(), pod)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, heartbeat)
}

func (s *Server) createAutomationRun(w http.ResponseWriter, r *http.Request) {
	var req model.CreateAutomationRunRequest
	if !decode(w, r, &req) {
		return
	}
	if req.IncidentID == "" || req.HostID == "" {
		writeError(w, http.StatusBadRequest, "incidentId and hostId are required")
		return
	}
	run, err := s.svc.CreateAutomationRun(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNoAvailablePod) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	slog.InfoContext(r.Context(), "AutomationRunCreated",
		"requestId", observability.RequestID(r.Context()),
		"runId", run.RunID,
		"incidentId", run.IncidentID,
		"hostId", run.HostID,
		"podId", run.OwnerPodID,
		"instanceId", run.OwnerInstanceID,
	)
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) getAutomationRun(w http.ResponseWriter, r *http.Request) {
	id, action := parseAutomationRunPath(r.URL.Path)
	if id == "" || action != "" {
		http.NotFound(w, r)
		return
	}
	run, err := s.svc.GetAutomationRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "automation run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) automationRunAction(w http.ResponseWriter, r *http.Request) {
	id, action := parseAutomationRunPath(r.URL.Path)
	if id == "" || action != "close" {
		http.NotFound(w, r)
		return
	}
	run, err := s.svc.CloseAutomationRun(r.Context(), id)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	slog.InfoContext(r.Context(), "AutomationRunClosed",
		"requestId", observability.RequestID(r.Context()),
		"runId", run.RunID,
		"incidentId", run.IncidentID,
	)
	writeJSON(w, http.StatusOK, run)
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
	return parts[1], strings.Join(parts[2:], "/")
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func parseOptionalInt64(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func parseOptionalTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func parseAutomationRunPath(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "automation" || parts[1] != "runs" {
		return "", ""
	}
	if len(parts) == 3 {
		return parts[2], ""
	}
	return parts[2], parts[3]
}
