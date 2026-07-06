package config

import (
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndOverrides(t *testing.T) {
	t.Setenv("PORT", "9091")
	t.Setenv("STORE_DRIVER", "postgres")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("HEARTBEAT_MAX_AGE_SECONDS", "20")
	t.Setenv("SESSION_LEASE_SECONDS", "120")
	t.Setenv("AUTOMATION_RUN_TTL_SECONDS", "600")
	t.Setenv("SESSION_AUDIT_RETENTION_SECONDS", "86400")
	t.Setenv("SESSION_AUDIT_MAX_BYTES_PER_SESSION", "4096")

	cfg := Load()
	if cfg.Port != "9091" || cfg.StoreDriver != "postgres" || cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("Load() = %#v", cfg)
	}
	if cfg.HeartbeatMaxAge != 20*time.Second || cfg.LeaseDuration != 120*time.Second || cfg.AutomationRunTTL != 600*time.Second {
		t.Fatalf("durations = %#v", cfg)
	}
	if cfg.AuditRetention != 24*time.Hour || cfg.AuditMaxBytes != 4096 {
		t.Fatalf("audit config = %#v", cfg)
	}
}

func TestLoadFallsBackForEmptyAndInvalidValues(t *testing.T) {
	t.Setenv("HEARTBEAT_MAX_AGE_SECONDS", "bad")
	t.Setenv("SESSION_LEASE_SECONDS", "0")

	cfg := Load()
	if cfg.Port != "8081" || cfg.StoreDriver != "memory" {
		t.Fatalf("defaults = %#v", cfg)
	}
	if cfg.HeartbeatMaxAge != 15*time.Second || cfg.LeaseDuration != 0 || cfg.AutomationRunTTL != time.Hour {
		t.Fatalf("fallback durations = %#v", cfg)
	}
	if cfg.AuditRetention != 7*24*time.Hour || cfg.AuditMaxBytes != 16*1024*1024 {
		t.Fatalf("audit defaults = %#v", cfg)
	}
}
