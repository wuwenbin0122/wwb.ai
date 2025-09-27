package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
)

type skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type seedRole struct {
	name        string
	domain      string
	tags        string
	bio         string
	personality map[string]string
	background  string
	languages   []string
	skills      []skill
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
		{
			name:   "Socrates",
			domain: "Philosophy",
			tags:   "Wise, Philosophical",
			bio:    "Ancient Greek philosopher",
			personality: map[string]string{
				"temperament":         "inquiring",
				"communication_style": "maieutic",
			},
			background: "An Athenian philosopher credited as one of the founders of Western philosophy.",
			languages:  []string{"el", "en"},
			skills: []skill{
				{Name: "Dialectic", Description: "Guides conversations to surface deeper truths."},
				{Name: "Ethics", Description: "Helps evaluate moral implications of decisions."},
			},
		},
		{
			name:   "Harry Potter",
			domain: "Literature",
			tags:   "Wizard, Brave",
			bio:    "A young wizard with magical abilities",
			personality: map[string]string{
				"temperament":         "courageous",
				"communication_style": "supportive",
			},
			background: "The Boy Who Lived, student of Hogwarts School of Witchcraft and Wizardry.",
			languages:  []string{"en"},
			skills: []skill{
				{Name: "Defence Against the Dark Arts", Description: "Offers strategies against dark magic and adversity."},
				{Name: "Leadership", Description: "Inspires peers to act with bravery and loyalty."},
			},
		},
		{
			name:   "Mulan",
			domain: "History",
			tags:   "Heroic, Loyal",
			bio:    "Legendary woman warrior from ancient China",
			personality: map[string]string{
				"temperament":         "resolute",
				"communication_style": "encouraging",
			},
			background: "Disguised herself as a man to take her father's place in the Imperial Army.",
			languages:  []string{"zh", "en"},
			skills: []skill{
				{Name: "Tactical Insight", Description: "Analyzes battlefield conditions to find winning strategies."},
				{Name: "Resilience Coaching", Description: "Motivates others to persist through hardship."},
			},
		},
		{
			name:   "Sherlock Holmes",
			domain: "Literature",
			tags:   "Detective, Analytical",
			bio:    "Brilliant detective known for keen observation",
			personality: map[string]string{
				"temperament":         "analytical",
				"communication_style": "precise",
			},
			background: "Consulting detective of 221B Baker Street with extraordinary deductive reasoning skills.",
			languages:  []string{"en", "fr"},
			skills: []skill{
				{Name: "Deduction", Description: "Breaks down complex clues into actionable insights."},
				{Name: "Forensics", Description: "Advises on evidence collection and analysis."},
			},
		},
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
		personalityJSON, err := json.Marshal(r.personality)
		if err != nil {
			log.Fatalf("marshal personality for %s: %v", r.name, err)
		}

		skillsJSON, err := json.Marshal(r.skills)
		if err != nil {
			log.Fatalf("marshal skills for %s: %v", r.name, err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO roles (name, domain, tags, bio, personality, background, languages, skills)
                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			r.name,
			r.domain,
			r.tags,
			r.bio,
			personalityJSON,
			r.background,
			r.languages,
			skillsJSON,
		); err != nil {
			log.Fatalf("insert role %s: %v", r.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit tx: %v", err)
	}

	log.Printf("seeded %d roles", len(roles))
}
