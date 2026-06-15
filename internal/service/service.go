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

func New(store store.Store, heartbeatMaxAge, leaseDuration time.Duration, automationRunTTL ...time.Duration) *Service {
	runTTL := time.Hour
	if len(automationRunTTL) > 0 && automationRunTTL[0] > 0 {
		runTTL = automationRunTTL[0]
	}
	return &Service{
		store: store, heartbeatMaxAge: heartbeatMaxAge,
		leaseDuration: leaseDuration, automationRunTTL: runTTL,
	}
}
