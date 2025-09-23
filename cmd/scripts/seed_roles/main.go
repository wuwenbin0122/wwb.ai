package main

import (
	"context"
	"log"

	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
)

type seedRole struct {
	name   string
	domain string
	tags   string
	bio    string
}

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

	roles := []seedRole{
		{name: "Socrates", domain: "Philosophy", tags: "Wise, Philosophical", bio: "Ancient Greek philosopher"},
		{name: "Harry Potter", domain: "Literature", tags: "Wizard, Brave", bio: "A young wizard with magical abilities"},
		{name: "Mulan", domain: "History", tags: "Heroic, Loyal", bio: "Legendary woman warrior from ancient China"},
		{name: "Sherlock Holmes", domain: "Literature", tags: "Detective, Analytical", bio: "Brilliant detective known for keen observation"},
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, r.name)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM roles WHERE name = ANY($1)", names); err != nil {
		log.Fatalf("delete existing roles: %v", err)
	}

	for _, r := range roles {
		if _, err := tx.Exec(ctx,
			"INSERT INTO roles (name, domain, tags, bio) VALUES ($1, $2, $3, $4)",
			r.name, r.domain, r.tags, r.bio,
		); err != nil {
			log.Fatalf("insert role %s: %v", r.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit tx: %v", err)
	}

	log.Printf("seeded %d roles", len(roles))
}
