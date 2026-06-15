package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/NUS-MTech-Capstone/session-manager/internal/config"
	"github.com/NUS-MTech-Capstone/session-manager/internal/httpapi"
	"github.com/NUS-MTech-Capstone/session-manager/internal/service"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

func main() {
	cfg := config.Load()
	st, closeStore, err := newStore(context.Background(), cfg)
	if err != nil {
		log.Fatalf("initialize store failed: %v", err)
	}
	defer closeStore()

	svc := service.New(st, cfg.HeartbeatMaxAge, cfg.LeaseDuration, cfg.AutomationRunTTL)
	addr := ":" + cfg.Port
	log.Printf("session-manager listening on %s store=%s", addr, cfg.StoreDriver)
	log.Fatal(http.ListenAndServe(addr, httpapi.New(svc)))
}

func newStore(ctx context.Context, cfg config.Config) (store.Store, func(), error) {
	switch strings.ToLower(cfg.StoreDriver) {
	case "", "memory":
		return store.NewMemoryStore(), func() {}, nil
	case "postgres":
		if cfg.DatabaseURL == "" {
			return nil, nil, fmt.Errorf("DATABASE_URL is required when STORE_DRIVER=postgres")
		}
		st, err := store.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}
		return st, func() { _ = st.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported STORE_DRIVER %q", cfg.StoreDriver)
	}
}
