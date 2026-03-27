package api

import (
	"time"
)

// User represents a 42-intranet user in the system
// swagger:model User
type User struct {
	// ID is the unique identifier of the user
	ID string `json:"id" example:"user_01HZ0MMK4S6VQW4WPHB6NZ7R7X"`

	// FtID is the numeric 42-intranet ID of the user
	FtID int `json:"ft_id" example:"1492"`

	// FtLogin is the 42-intranet login handle (e.g. "heinz")
	FtLogin string `json:"ft_login" example:"heinz"`

	// FtIsStaff indicates whether the user is a 42-intranet staff member
	FtIsStaff bool `json:"ft_is_staff" example:"true"`

	// PhotoURL is the URL to the user’s 42-intranet profile picture
	PhotoURL string `json:"ft_photo" example:"https://intra.42.fr/some-login/some-id"`

	// LastSeen is the UTC timestamp of the user’s last activity
	LastSeen time.Time `json:"last_seen" example:"2025-02-18T15:00:00Z"`

	// IsStaff indicates whether the user has staff privileges within Pan Bagnat
	IsStaff bool `json:"is_staff" example:"true"`

	// IsBlacklisted indicates whether the user is blacklisted
	IsBlacklisted bool `json:"is_blacklisted" example:"false"`
}

// Session represents a user session (device) in the system
// swagger:model Session
type Session struct {
	ID          string    `json:"id" example:"eyJhbGciOiJIUz..."`
	UserAgent   string    `json:"user_agent" example:"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)..."`
	IP          string    `json:"ip" example:"192.168.0.12"`
	DeviceLabel string    `json:"device_label" example:"MacBook Pro"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
	ExpiresAt   time.Time `json:"expires_at"`
	IsCurrent   bool      `json:"is_current"`
}
