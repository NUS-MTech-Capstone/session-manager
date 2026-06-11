package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port            string
	StoreDriver     string
	DatabaseURL     string
	HeartbeatMaxAge time.Duration
	LeaseDuration   time.Duration
}

func Load() Config {
	return Config{
		Port:            env("PORT", "8081"),
		StoreDriver:     env("STORE_DRIVER", "memory"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		HeartbeatMaxAge: secondsEnv("HEARTBEAT_MAX_AGE_SECONDS", 15),
		LeaseDuration:   secondsEnv("SESSION_LEASE_SECONDS", 60),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func secondsEnv(key string, fallback int) time.Duration {
	v, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil {
		v = fallback
	}
	return time.Duration(v) * time.Second
}
