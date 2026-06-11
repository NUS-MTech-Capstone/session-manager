package store

import (
	"context"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

type Store interface {
	RegisterPod(context.Context, model.ProxyPod) (model.ProxyPod, error)
	HeartbeatPod(context.Context, model.ProxyPod) (model.ProxyPod, error)
	AvailablePods(context.Context, time.Duration) ([]model.ProxyPod, error)
	CreateSession(context.Context, model.CreateSessionRequest, time.Duration, time.Duration) (model.Session, error)
	GetSession(context.Context, string) (model.Session, error)
	HeartbeatSession(context.Context, string, model.SessionHeartbeatRequest, time.Duration) (model.Session, error)
	CloseSession(context.Context, string) (model.Session, error)
}
