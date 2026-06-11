package service

import (
	"context"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

type Service struct {
	store           *store.MemoryStore
	heartbeatMaxAge time.Duration
	leaseDuration   time.Duration
}

func New(store *store.MemoryStore, heartbeatMaxAge, leaseDuration time.Duration) *Service {
	return &Service{store: store, heartbeatMaxAge: heartbeatMaxAge, leaseDuration: leaseDuration}
}
