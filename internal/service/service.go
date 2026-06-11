package service

import (
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

type Service struct {
	store           store.Store
	heartbeatMaxAge time.Duration
	leaseDuration   time.Duration
}

func New(store store.Store, heartbeatMaxAge, leaseDuration time.Duration) *Service {
	return &Service{store: store, heartbeatMaxAge: heartbeatMaxAge, leaseDuration: leaseDuration}
}
