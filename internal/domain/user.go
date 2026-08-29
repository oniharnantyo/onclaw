package domain

import "time"

// User represents an identity in OnClaw.
type User struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	PasswordHash *string    `json:"-"`
	AvatarKey    *string    `json:"avatar_key,omitempty"`
	AvatarURL    *string    `json:"avatar_url,omitempty"`
	DisabledAt   *time.Time `json:"disabled_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	MembershipCount int        `json:"membership_count"`
	IsSuperadmin    bool       `json:"is_superadmin"`
}

// IsDisabled reports whether the user account has been disabled.
func (u *User) IsDisabled() bool {
	return u != nil && u.DisabledAt != nil
}
