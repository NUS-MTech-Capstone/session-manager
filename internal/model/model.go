package model

import "time"

const (
	SessionStatusActive = "ACTIVE"
	SessionStatusClosed = "CLOSED"
	AutomationRunActive = "ACTIVE"
	AutomationRunClosed = "CLOSED"
	PodStatusReady      = "READY"
)

type Session struct {
	SessionID       string    `json:"sessionId"`
	UserID          string    `json:"userId"`
	HostID          string    `json:"hostId"`
	CRID            string    `json:"crId"`
	ConnectionType  string    `json:"connectionType"`
	OwnerPodID      string    `json:"ownerPodId"`
	OwnerInstanceID string    `json:"ownerInstanceId"`
	WebsocketURL    string    `json:"websocketUrl"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
	LeaseExpiresAt  time.Time `json:"leaseExpiresAt"`
}

type ProxyPod struct {
	PodID           string    `json:"podId"`
	InstanceID      string    `json:"instanceId"`
	PodIP           string    `json:"podIp"`
	WebsocketURL    string    `json:"websocketUrl"`
	OperationsURL   string    `json:"operationsUrl"`
	Status          string    `json:"status"`
	ActiveSessions  int       `json:"activeSessions"`
	MaxSessions     int       `json:"maxSessions"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
}

type AutomationRun struct {
	RunID           string    `json:"runId"`
	IncidentID      string    `json:"incidentId"`
	HostID          string    `json:"hostId"`
	OwnerPodID      string    `json:"ownerPodId"`
	OwnerInstanceID string    `json:"ownerInstanceId"`
	OperationsURL   string    `json:"operationsUrl"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type CreateAutomationRunRequest struct {
	IncidentID string `json:"incidentId"`
	HostID     string `json:"hostId"`
}

type CreateSessionRequest struct {
	UserID         string `json:"userId"`
	HostID         string `json:"hostId"`
	CRID           string `json:"crId"`
	ConnectionType string `json:"connectionType"`
}

type ListSessionsFilter struct {
	UserID string
	HostID string
	CRID   string
	Status string
}

type ListSessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

type SessionHeartbeatRequest struct {
	OwnerPodID      string `json:"ownerPodId"`
	OwnerInstanceID string `json:"ownerInstanceId"`
}

type CloseSessionRequest struct {
	OwnerPodID      string `json:"ownerPodId"`
	OwnerInstanceID string `json:"ownerInstanceId"`
}
