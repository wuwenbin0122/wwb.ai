package models

import (
	"encoding/json"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

// Role represents a character definition stored in the relational database.
type Role struct {
	ID          int64                                 `json:"id"`
	Name        string                                `json:"name"`
	Domain      string                                `json:"domain"`
	Tags        pq.StringArray                        `gorm:"type:text[]" json:"tags"`
	Bio         string                                `json:"bio"`
	Personality datatypes.JSONType[map[string]string] `gorm:"type:jsonb" json:"personality"`
	Background  string                                `json:"background"`
	Languages   pq.StringArray                        `gorm:"type:text[]" json:"languages"`
	Skills      datatypes.JSONType[[]Skill]           `gorm:"type:jsonb" json:"skills"`
}

// Skill models a structured skill definition associated with a role.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MarshalSkills converts a slice of Skill into JSON for persistence helpers that expect raw JSON bytes.
func MarshalSkills(skills []Skill) ([]byte, error) {
	return json.Marshal(skills)
}

// MarshalPersonality converts the map form personality into JSON for inserts.
func MarshalPersonality(personality map[string]string) ([]byte, error) {
	return json.Marshal(personality)
}
