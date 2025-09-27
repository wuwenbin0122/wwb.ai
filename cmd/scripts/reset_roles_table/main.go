package main

import (
	"context"
	"log"

	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPostgresPool(ctx, cfg.DBURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	stmts := []string{
		"DROP TABLE IF EXISTS roles CASCADE",
		`CREATE TABLE roles (
            id SERIAL PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            domain VARCHAR(255),
            tags VARCHAR(255),
            bio TEXT,
            personality JSONB DEFAULT '{}'::jsonb,
            background TEXT,
            languages TEXT[] DEFAULT ARRAY['zh', 'en'],
            skills JSONB DEFAULT '[]'::jsonb
        )`,
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			log.Fatalf("exec stmt %q: %v", stmt, err)
		}
	}

	log.Println("roles table recreated")
}
