package store

import "context"

// GatewaySettingsStore manages the singleton gateway configuration row.
type GatewaySettingsStore interface {
	// Get returns the saved settings; (nil, nil) when nothing was ever
	// saved — absence is a normal state and callers apply defaults.
	Get(ctx context.Context) (*GatewaySettings, error)
	// Save create-or-replaces the singleton settings row.
	Save(ctx context.Context, settings *GatewaySettings) error
}
