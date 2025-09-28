package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
)

type skill struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type role struct {
	id     int64
	name   string
	domain string
	tags   string
	bio    string
	skills []byte // jsonb
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

	// Pull all roles with minimal columns needed
	rows, err := pool.Query(ctx, `SELECT id, name, domain, tags, bio, skills FROM roles ORDER BY id`)
	if err != nil {
		log.Fatalf("query roles: %v", err)
	}
	defer rows.Close()

	roles := make([]role, 0)
	for rows.Next() {
		var r role
		if err := rows.Scan(&r.id, &r.name, &r.domain, &r.tags, &r.bio, &r.skills); err != nil {
			log.Fatalf("scan role: %v", err)
		}
		roles = append(roles, r)
	}
	if rows.Err() != nil {
		log.Fatalf("iterate roles: %v", rows.Err())
	}

	updated := 0
	for _, r := range roles {
		merged, changed, err := mergeSkills(r)
		if err != nil {
			log.Printf("skip role %d(%s): %v", r.id, r.name, err)
			continue
		}
		if !changed {
			continue
		}
		payload, _ := json.Marshal(merged)
		if _, err := pool.Exec(ctx, `UPDATE roles SET skills=$1::jsonb WHERE id=$2`, string(payload), r.id); err != nil {
			log.Fatalf("update role %d(%s): %v", r.id, r.name, err)
		}
		updated++
		fmt.Printf("updated #%d %s -> skills=%s\n", r.id, r.name, string(payload))
	}

	fmt.Printf("done. roles updated: %d\n", updated)
}

func mergeSkills(r role) ([]skill, bool, error) {
	existing := parseExistingSkills(r.skills)
	existingSet := make(map[string]skill, len(existing))
	for _, s := range existing {
		if s.ID == "" {
			continue
		}
		existingSet[s.ID] = s
	}

	suggested := suggestSkills(r)

	// Merge: existing U suggested
	changed := false
	for _, s := range suggested {
		if _, ok := existingSet[s.ID]; !ok {
			existing = append(existing, s)
			existingSet[s.ID] = s
			changed = true
		}
	}

	return existing, changed, nil
}

func parseExistingSkills(raw []byte) []skill {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return nil
	}
	var arr []skill
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	// dedupe by id
	seen := make(map[string]struct{}, len(arr))
	result := make([]skill, 0, len(arr))
	for _, s := range arr {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(s.Name) == "" {
			s.Name = defaultSkillName(id)
		}
		result = append(result, s)
	}
	return result
}

func suggestSkills(r role) []skill {
	lc := strings.ToLower(strings.Join([]string{r.name, r.domain, r.tags, r.bio}, " "))
	zh := r.name + " " + r.domain + " " + r.tags + " " + r.bio
	add := func(id string) skill { return skill{ID: id, Name: defaultSkillName(id)} }
	out := make([]skill, 0, 3)

	// Philosophy / Teacher / Coach -> Socratic
	if containsAny(lc, "philosophy", "philosopher", "teacher", "coach", "mentor") || containsAny(zh, "哲学", "老师", "教练", "导师") {
		out = append(out, add("socratic_questions"))
	}

	// Historian / History / Scientist / Research / Detective -> Citation
	if containsAny(lc, "historian", "history", "scientist", "science", "research", "paper", "detective", "investigat") || containsAny(zh, "历史", "学者", "科研", "论文", "侦探") {
		out = append(out, add("citation_mode"))
	}

	// Counselor / Psych / Supportive / Heroic personas -> Emo stabilizer
	if containsAny(lc, "psych", "therap", "counsel", "support", "coach", "mentor", "friendly", "brave") || containsAny(zh, "心理", "咨询", "支持", "安抚", "勇敢", "温暖") {
		out = append(out, add("emo_stabilizer"))
	}

	// Name specific hints
	if containsAny(lc, "socrates", "plato", "aristotle", "confucius") || containsAny(zh, "苏格拉底", "柏拉图", "亚里士多德", "孔子") {
		out = append(out, add("socratic_questions"))
		out = append(out, add("citation_mode"))
	}
	if containsAny(lc, "sherlock", "holmes") || strings.Contains(zh, "福尔摩斯") {
		out = append(out, add("citation_mode"))
		out = append(out, add("socratic_questions"))
	}
	if containsAny(lc, "mulan", "harry") || containsAny(zh, "木兰", "哈利") {
		out = append(out, add("emo_stabilizer"))
	}

	// Dedupe by id
	seen := make(map[string]struct{}, len(out))
	result := make([]skill, 0, len(out))
	for _, s := range out {
		if _, ok := seen[s.ID]; ok {
			continue
		}
		seen[s.ID] = struct{}{}
		result = append(result, s)
	}
	return result
}

func defaultSkillName(id string) string {
	switch id {
	case "socratic_questions":
		return "苏格拉底式提问"
	case "citation_mode":
		return "引用原典"
	case "emo_stabilizer":
		return "情绪稳定器"
	default:
		return id
	}
}

func containsAny(s string, subs ...string) bool {
	s = strings.ToLower(s)
	for _, sub := range subs {
		if sub == "" {
			continue
		}
		if strings.Contains(s, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
