package store

import (
	"context"
	"fmt"
	"strings"
)

type OpenConfig struct {
	Driver      string
	DatabaseURL string
}

func Open(ctx context.Context, cfg OpenConfig) (Store, func(), error) {
	switch strings.ToLower(cfg.Driver) {
	case "", "memory":
		return NewMemoryStore(), func() {}, nil
	case "postgres":
		if cfg.DatabaseURL == "" {
			return nil, nil, fmt.Errorf("DATABASE_URL is required when STORE_DRIVER=postgres")
		}
		st, err := NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}
		return st, func() { _ = st.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported STORE_DRIVER %q", cfg.Driver)
	}
}
