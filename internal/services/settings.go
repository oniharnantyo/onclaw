package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

// SettingsService is the bound service for shared configuration: providers,
// skills, hooks, MCP servers, browser profiles, and the gateway settings
// singleton. It is a CRUD wrapper over the store with no business logic
// (task 5.1); provider model listings come from the runtime's built-in
// catalog.
type SettingsService struct {
	store store.Store
	// browserDir is the browser tool's home directory the profile
	// management methods root at (spec add-browser-tool D10): the profile
	// registry lives at <browserDir>/profiles.
	browserDir string
	// invalidateMCP drops the runtime's cached MCP connection for one server
	// id, called after every mutating MCP write so config changes take
	// effect immediately (design D7). Never nil: the constructor installs a
	// no-op until the composition root injects the real cache.
	invalidateMCP func(serverID string)
	// emitter forwards service events to the host's event bus — the MCP
	// OAuth completion announcement (design D2). Never nil: the constructor
	// installs Noop{} until the composition root injects the host adapter.
	emitter Emitter
	// urlOpener opens URLs in the user's system browser — the MCP OAuth
	// authorization page (design D8). Never nil: the constructor installs
	// NoBrowser{} (headless hosts fail the flow with ErrNoBrowser).
	urlOpener URLOpener
}

// SettingsOption configures the settings service.
type SettingsOption func(*SettingsService)

// WithMCPInvalidate supplies the callback that drops the MCP connection
// cache entry for a server id after a mutating write. A nil callback is
// ignored — writes must keep working without invalidation (design D7).
func WithMCPInvalidate(invalidate func(serverID string)) SettingsOption {
	return func(s *SettingsService) {
		if invalidate != nil {
			s.invalidateMCP = invalidate
		}
	}
}

// WithBrowserDir supplies the browser tool's home directory the browser
// profile methods root at (spec add-browser-tool D10). Unset — the default —
// those methods operate on an empty root; the composition root always wires
// the layout's browser directory.
func WithBrowserDir(dir string) SettingsOption {
	return func(s *SettingsService) {
		if dir != "" {
			s.browserDir = dir
		}
	}
}

// WithEmitter supplies the event sink the service announces events through
// (design D2: the MCP OAuth completion announcement). A nil emitter is
// ignored — the no-op default stands (mirrors WithMCPInvalidate).
func WithEmitter(e Emitter) SettingsOption {
	return func(s *SettingsService) {
		if e != nil {
			s.emitter = e
		}
	}
}

// WithURLOpener supplies the seam that opens URLs in the user's system
// browser (design D8: the MCP OAuth authorization page). A nil opener is
// ignored — the NoBrowser default stands (mirrors WithMCPInvalidate).
func WithURLOpener(o URLOpener) SettingsOption {
	return func(s *SettingsService) {
		if o != nil {
			s.urlOpener = o
		}
	}
}

// NewSettingsService returns the settings service over s, with options
// applied on top of the no-op defaults.
func NewSettingsService(s store.Store, opts ...SettingsOption) *SettingsService {
	svc := &SettingsService{
		store:         s,
		invalidateMCP: func(string) {},
		emitter:       Noop{},
		urlOpener:     NoBrowser{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Gateway returns the gateway settings singleton. When nothing was ever
// saved the defaults are returned: the gateway enabled, no default agent.
func (s *SettingsService) Gateway(ctx context.Context) (store.GatewaySettings, error) {
	settings, err := s.store.GatewaySettings().Get(ctx)
	if err != nil {
		return store.GatewaySettings{}, fmt.Errorf("get gateway settings: %w", err)
	}
	if settings == nil {
		return DefaultGatewaySettings(), nil
	}
	return *settings, nil
}

// SaveGateway create-or-replaces the gateway settings singleton.
func (s *SettingsService) SaveGateway(ctx context.Context, settings store.GatewaySettings) error {
	if err := s.store.GatewaySettings().Save(ctx, &settings); err != nil {
		return fmt.Errorf("save gateway settings: %w", err)
	}
	return nil
}

// DefaultGatewaySettings returns the gateway settings applied when no
// settings were ever saved: the gateway is enabled, with no default agent.
func DefaultGatewaySettings() store.GatewaySettings {
	return store.GatewaySettings{Enabled: true}
}
