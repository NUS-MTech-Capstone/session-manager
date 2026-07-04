package store

import (
	"context"
	"strings"
	"testing"
)

func TestOpenDefaultsToMemoryStore(t *testing.T) {
	st, closeStore, err := Open(context.Background(), OpenConfig{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer closeStore()
	if _, ok := st.(*MemoryStore); !ok {
		t.Fatalf("Open() type = %T, want *MemoryStore", st)
	}
}

func TestOpenRequiresPostgresURL(t *testing.T) {
	_, _, err := Open(context.Background(), OpenConfig{Driver: "postgres"})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("Open() error = %v, want DATABASE_URL error", err)
	}
}

func TestOpenRejectsUnknownDriver(t *testing.T) {
	_, _, err := Open(context.Background(), OpenConfig{Driver: "redis"})
	if err == nil || !strings.Contains(err.Error(), "unsupported STORE_DRIVER") {
		t.Fatalf("Open() error = %v, want unsupported driver error", err)
	}
}
