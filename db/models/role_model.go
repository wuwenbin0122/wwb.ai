package models

import "encoding/json"

// Role represents a character definition stored in the relational database.
type Role struct {
	ID          int64           `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Domain      string          `json:"domain" db:"domain"`
	Tags        string          `json:"tags" db:"tags"`
	Bio         string          `json:"bio" db:"bio"`
	Personality json.RawMessage `json:"personality" db:"personality"`
	Background  string          `json:"background" db:"background"`
	Languages   []string        `json:"languages" db:"languages"`
	Skills      json.RawMessage `json:"skills" db:"skills"`
}
