package model

import "time"

const (
	SessionStatusActive = "ACTIVE"
	SessionStatusClosed = "CLOSED"
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
	Status          string    `json:"status"`
	ActiveSessions  int       `json:"activeSessions"`
	MaxSessions     int       `json:"maxSessions"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt"`
}

type CreateSessionRequest struct {
	UserID         string `json:"userId"`
	HostID         string `json:"hostId"`
	CRID           string `json:"crId"`
	ConnectionType string `json:"connectionType"`
}

type SessionHeartbeatRequest struct {
	OwnerPodID      string `json:"ownerPodId"`
	OwnerInstanceID string `json:"ownerInstanceId"`
}

type CloseSessionRequest struct {
	OwnerPodID      string `json:"ownerPodId"`
	OwnerInstanceID string `json:"ownerInstanceId"`
}
