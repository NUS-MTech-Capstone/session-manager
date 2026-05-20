package main

import (
	"log"
	"net/http"

	"github.com/NUS-MTech-Capstone/session-manager/internal/config"
	"github.com/NUS-MTech-Capstone/session-manager/internal/httpapi"
	"github.com/NUS-MTech-Capstone/session-manager/internal/service"
	"github.com/NUS-MTech-Capstone/session-manager/internal/store"
)

func main() {
	cfg := config.Load()
	svc := service.New(store.NewMemoryStore(), cfg.HeartbeatMaxAge, cfg.LeaseDuration)
	addr := ":" + cfg.Port
	log.Printf("session-manager listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, httpapi.New(svc)))
}
