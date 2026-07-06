package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/NUS-MTech-Capstone/session-manager/internal/config"
	"github.com/NUS-MTech-Capstone/session-manager/internal/events"
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

	eventPublisher, err := events.NewPublisher(context.Background(), cfg.EventPublisher, cfg.AWSRegion, cfg.EventsTopicARN)
	if err != nil {
		slog.Error("metadata event publisher setup failed", "error", err)
		os.Exit(1)
	}
	svc := service.New(st, cfg.HeartbeatMaxAge, cfg.LeaseDuration, cfg.AutomationRunTTL, eventPublisher)
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
	return store.Open(ctx, store.OpenConfig{Driver: cfg.StoreDriver, DatabaseURL: cfg.DatabaseURL})
}
