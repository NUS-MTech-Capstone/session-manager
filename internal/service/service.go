package service

import (
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

type Service struct {
	store            store.Store
	heartbeatMaxAge  time.Duration
	leaseDuration    time.Duration
	automationRunTTL time.Duration
}

func New(store store.Store, heartbeatMaxAge, leaseDuration time.Duration, options ...any) *Service {
	runTTL := time.Hour
	for _, option := range options {
		if value, ok := option.(time.Duration); ok && value > 0 {
			runTTL = value
		}
	}
	return &Service{
		store: store, heartbeatMaxAge: heartbeatMaxAge,
		leaseDuration: leaseDuration, automationRunTTL: runTTL,
	}
}
