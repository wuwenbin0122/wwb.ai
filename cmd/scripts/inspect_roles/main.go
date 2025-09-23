package main

import (
    "context"
    "fmt"

    "github.com/wuwenbin0122/wwb.ai/config"
    "github.com/wuwenbin0122/wwb.ai/db"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    pool, err := db.NewPostgresPool(ctx, cfg.DBURL)
    if err != nil {
        panic(err)
    }
    defer pool.Close()

    const query = `SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'roles' ORDER BY ordinal_position`
    rows, err := pool.Query(ctx, query)
    if err != nil {
        panic(err)
    }
    defer rows.Close()

    fmt.Println("columns:")
    for rows.Next() {
        var name, dataType string
        if err := rows.Scan(&name, &dataType); err != nil {
            panic(err)
        }
        fmt.Printf("- %s (%s)\n", name, dataType)
    }

    if rows.Err() != nil {
        panic(rows.Err())
    }
}
