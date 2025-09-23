package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wuwenbin0122/wwb.ai/internal/utils"
)

type Postgres struct {
	Pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, cfg utils.PostgresConfig) (*Postgres, error) {
	dsn := cfg.BuildDSN()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolConfig.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns >= 0 {
		poolConfig.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}

	return &Postgres{Pool: pool}, nil
}

func (p *Postgres) Close() {
	if p == nil || p.Pool == nil {
		return
	}
	p.Pool.Close()
}

func (p *Postgres) Ping(ctx context.Context) error {
	if p == nil || p.Pool == nil {
		return fmt.Errorf("postgres: pool not initialised")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.Pool.Ping(ctx)
}

func (p *Postgres) EnsureSchema(ctx context.Context) error {
	if p == nil || p.Pool == nil {
		return fmt.Errorf("postgres: pool not initialised")
	}

	statements := []string{
		strings.Join([]string{
			"CREATE TABLE IF NOT EXISTS users (",
			"    id TEXT PRIMARY KEY,",
			"    username TEXT NOT NULL UNIQUE,",
			"    password TEXT NOT NULL,",
			"    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
			")",
		}, "\n"),
		strings.Join([]string{
			"CREATE TABLE IF NOT EXISTS roles (",
			"    id TEXT PRIMARY KEY,",
			"    name TEXT NOT NULL UNIQUE,",
			"    description TEXT NOT NULL DEFAULT '',",
			"    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
			")",
		}, "\n"),
		strings.Join([]string{
			"CREATE TABLE IF NOT EXISTS conversations (",
			"    id TEXT PRIMARY KEY,",
			"    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,",
			"    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,",
			"    content TEXT NOT NULL,",
			"    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()",
			")",
		}, "\n"),
	}

	for _, stmt := range statements {
		if _, err := p.Pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("postgres: ensure schema: %w", err)
		}
	}

	return nil
}
