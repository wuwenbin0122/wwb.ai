package main

import (
    "context"
    "fmt"
    "log"
    "time"

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
        `ALTER TABLE roles ADD COLUMN IF NOT EXISTS personality JSONB DEFAULT '{}'::jsonb`,
        `ALTER TABLE roles ADD COLUMN IF NOT EXISTS background TEXT`,
        `ALTER TABLE roles ADD COLUMN IF NOT EXISTS languages TEXT[] DEFAULT ARRAY['zh','en']`,
        `ALTER TABLE roles ADD COLUMN IF NOT EXISTS skills JSONB DEFAULT '[]'::jsonb`,
        // backfill defaults for existing rows that may have NULL languages/skills
        `UPDATE roles SET languages = ARRAY['zh','en'] WHERE languages IS NULL`,
        `UPDATE roles SET skills = '[]'::jsonb WHERE skills IS NULL`,
        `UPDATE roles SET personality = '{}'::jsonb WHERE personality IS NULL`,
    }

    tx, err := pool.Begin(ctx)
    if err != nil {
        log.Fatalf("begin tx: %v", err)
    }
    defer tx.Rollback(ctx)

    for i, stmt := range stmts {
        if _, err := tx.Exec(ctx, stmt); err != nil {
            log.Fatalf("exec migration %d failed: %v\nSQL: %s", i+1, err, stmt)
        }
    }

    if err := tx.Commit(ctx); err != nil {
        log.Fatalf("commit migration: %v", err)
    }

    // quick verify
    verify := `SELECT column_name, data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='roles' ORDER BY ordinal_position`
    rows, err := pool.Query(ctx, verify)
    if err != nil {
        log.Fatalf("verify columns: %v", err)
    }
    defer rows.Close()

    fmt.Println("roles columns after migration:")
    for rows.Next() {
        var name, dtype string
        if err := rows.Scan(&name, &dtype); err != nil {
            log.Fatalf("scan: %v", err)
        }
        fmt.Printf("- %s (%s)\n", name, dtype)
    }
    if rows.Err() != nil {
        log.Fatalf("rows: %v", rows.Err())
    }

    fmt.Printf("done at %s\n", time.Now().Format(time.RFC3339))
}

