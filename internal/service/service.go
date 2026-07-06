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
	auditRetention   time.Duration
	auditMaxBytes    int
}

func New(store store.Store, heartbeatMaxAge, leaseDuration time.Duration, options ...any) *Service {
	runTTL := time.Hour
	auditRetention := 7 * 24 * time.Hour
	auditMaxBytes := 16 * 1024 * 1024
	durationCount := 0
	for _, option := range options {
		switch value := option.(type) {
		case time.Duration:
			durationCount++
			if value > 0 && durationCount == 1 {
				runTTL = value
			} else if value > 0 && durationCount == 2 {
				auditRetention = value
			}
		case int:
			if value > 0 {
				auditMaxBytes = value
			}
		}
	}
	return &Service{
		store: store, heartbeatMaxAge: heartbeatMaxAge,
		leaseDuration: leaseDuration, automationRunTTL: runTTL,
		auditRetention: auditRetention, auditMaxBytes: auditMaxBytes,
	}
}
