package models

import "time"

type Role struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
}
