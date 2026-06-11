package main

import (
	"context"
	"strings"
	"testing"

	"github.com/NUS-MTech-Capstone/session-manager/internal/config"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

func TestNewStoreDefaultsToMemory(t *testing.T) {
	st, closeStore, err := newStore(context.Background(), config.Config{})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	defer closeStore()
	if _, ok := st.(*store.MemoryStore); !ok {
		t.Fatalf("newStore() type = %T, want *store.MemoryStore", st)
	}
}

func TestNewStoreRequiresPostgresURL(t *testing.T) {
	_, _, err := newStore(context.Background(), config.Config{StoreDriver: "postgres"})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("newStore() error = %v, want DATABASE_URL error", err)
	}
}

func TestNewStoreRejectsUnknownDriver(t *testing.T) {
	_, _, err := newStore(context.Background(), config.Config{StoreDriver: "redis"})
	if err == nil || !strings.Contains(err.Error(), "unsupported STORE_DRIVER") {
		t.Fatalf("newStore() error = %v, want unsupported driver error", err)
	}
}
