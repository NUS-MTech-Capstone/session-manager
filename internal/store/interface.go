package store

import (
	"context"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

// Store is the repository boundary for session, pod, and automation run state.
type Store interface {
	RegisterPod(context.Context, model.ProxyPod) (model.ProxyPod, error)
	HeartbeatPod(context.Context, model.ProxyPod) (model.ProxyPod, error)
	AvailablePods(context.Context, time.Duration) ([]model.ProxyPod, error)
	CreateSession(context.Context, model.CreateSessionRequest, time.Duration, time.Duration) (model.Session, error)
	GetSession(context.Context, string) (model.Session, error)
	ListSessions(context.Context, model.ListSessionsFilter) ([]model.Session, error)
	HeartbeatSession(context.Context, string, model.SessionHeartbeatRequest, time.Duration) (model.Session, error)
	CloseSession(context.Context, string, model.CloseSessionRequest) (model.Session, error)
	CreateAutomationRun(context.Context, model.CreateAutomationRunRequest, time.Duration, time.Duration) (model.AutomationRun, error)
	GetAutomationRun(context.Context, string) (model.AutomationRun, error)
	CloseAutomationRun(context.Context, string) (model.AutomationRun, error)
}
