package domain

import "time"

type User struct {
	ID            string
	Email         string
	Name          string
	Locale        string
	PasswordHash  string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     *time.Time
}
