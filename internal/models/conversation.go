package models

import "time"

type Conversation struct {
	ID        string
	UserID    string
	RoleID    string
	Content   string
	Timestamp time.Time
}
