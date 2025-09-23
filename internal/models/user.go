package models

import "time"

// User represents an application user record.
type User struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Sanitize returns a copy of the user without sensitive fields populated.
func (u User) Sanitize() User {
	u.PasswordHash = ""
	return u
}
