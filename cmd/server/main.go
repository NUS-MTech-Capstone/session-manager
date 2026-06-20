package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/config"
	"github.com/NUS-MTech-Capstone/session-manager/internal/httpapi"
	"github.com/NUS-MTech-Capstone/session-manager/internal/observability"
	"github.com/NUS-MTech-Capstone/session-manager/internal/service"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

func main() {
	observability.ConfigureLogger("session-manager")
	cfg := config.Load()
	st, closeStore, err := newStore(context.Background(), cfg)
	if err != nil {
		slog.Error("initialize store failed", "error", err)
		os.Exit(1)
	}
	defer closeStore()

	svc := service.New(st, cfg.HeartbeatMaxAge, cfg.LeaseDuration, cfg.AutomationRunTTL)
	addr := ":" + cfg.Port
	slog.Info("server_listening", "addr", addr, "store", cfg.StoreDriver)
	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(svc),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
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
