package store

import "context"

// NotificationSettingsStore manages the singleton desktop-notification
// configuration row (add-desktop-notifications design D4).
type NotificationSettingsStore interface {
	// Get returns the saved settings; (nil, nil) when nothing was ever
	// saved — absence is a normal state and callers apply defaults.
	Get(ctx context.Context) (*NotificationSettings, error)
	// Save create-or-replaces the singleton settings row.
	Save(ctx context.Context, settings *NotificationSettings) error
}
