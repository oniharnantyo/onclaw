package agents

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ToolPolicy is the narrow workspace gate consulted during tool resolution
// (design.md D4): EnabledTools returns the workspace's effective enabled set
// keyed by catalog tool key, and ToolConfigs the resolved per-tool execution
// configuration (design.md D5). Tools absent from the enabled map are treated
// as enabled (absence of a settings row means the default).
type ToolPolicy interface {
	EnabledTools(ctx context.Context, workspaceID string) (map[string]bool, error)
	ToolConfigs(ctx context.Context, workspaceID string) (map[string]map[string]any, error)
}

// ToolSettingsService reads and writes workspace tool settings on top of the
// ToolSettingsStore: it merges catalog defaults, encrypts secret config
// fields on write (ciphertext at rest, keyed like workspace provider keys),
// and returns hints instead of secrets on read (design.md D6). It is the
// composition root's ToolPolicy implementation.
type ToolSettingsService struct {
	settings store.ToolSettingsStore
	encKey   []byte
}

// NewToolSettingsService builds the service from its granular dependencies.
func NewToolSettingsService(settings store.ToolSettingsStore, encKey []byte) *ToolSettingsService {
	return &ToolSettingsService{settings: settings, encKey: encKey}
}

// EnabledTools implements ToolPolicy. Every catalog key not explicitly
// disabled by a settings row is enabled.
func (s *ToolSettingsService) EnabledTools(ctx context.Context, workspaceID string) (map[string]bool, error) {
	rows, err := s.settings.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	disabled := make(map[string]bool, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			disabled[row.ToolKey] = true
		}
	}
	enabled := make(map[string]bool)
	for _, entry := range ToolCatalog() {
		enabled[entry.Key] = !disabled[entry.Key]
	}
	return enabled, nil
}

// SettingsForWorkspace returns the workspace's settings rows with secret
// config fields decrypted to plaintext. Callers that surface config to users
// must re-apply hints (ConfigView); this plaintext form is for the runtime.
func (s *ToolSettingsService) SettingsForWorkspace(ctx context.Context, workspaceID string) ([]domain.WorkspaceToolSetting, error) {
	rows, err := s.settings.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		entry, ok := ToolCatalogEntryByKey(rows[i].ToolKey)
		if !ok || !entry.Configurable {
			continue
		}
		if err := s.decryptSecretFields(rows[i].WorkspaceID, entry, rows[i].Config); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// Setting returns one workspace tool setting, or domain.ErrNotFound when the
// workspace has no row for the tool.
func (s *ToolSettingsService) Setting(ctx context.Context, workspaceID, toolKey string) (*domain.WorkspaceToolSetting, error) {
	return s.settings.Get(ctx, workspaceID, toolKey)
}

// Upsert validates and persists a workspace tool setting. The incoming config
// is merged over the stored one first: non-secret values overwrite, and a
// secret value is replaced only when the client supplies a non-empty one
// (secret fields are write-only). Schema and credential-requirement failures
// return a *ConfigValidationError (HTTP 422 with per-field details); unknown
// tools and structurally invalid settings return domain.ErrInvalid.
func (s *ToolSettingsService) Upsert(ctx context.Context, setting *domain.WorkspaceToolSetting) error {
	entry, known := ToolCatalogEntryByKey(setting.ToolKey)
	if !known {
		return fmt.Errorf("%w: unknown tool %q", domain.ErrInvalid, setting.ToolKey)
	}
	if setting.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", domain.ErrInvalid)
	}

	existing, err := s.settings.Get(ctx, setting.WorkspaceID, setting.ToolKey)
	if err != nil && err != domain.ErrNotFound {
		return err
	}

	merged := map[string]any{}
	if existing != nil && existing.Config != nil {
		for k, v := range existing.Config {
			merged[k] = v
		}
	}
	secretKeys := map[string]bool{}
	for _, key := range SecretConfigFields(entry) {
		secretKeys[key] = true
		// Hint fields are server-managed: neither writable nor validatable.
		delete(merged, key+"_hint")
	}
	for k, v := range setting.Config {
		if secretKeys[k] {
			// A secret field is replaced only by a non-empty supply; an empty
			// value means "keep the stored credential".
			if isEmptyConfigValue(v) {
				continue
			}
		}
		merged[k] = v
	}

	if entry.Configurable {
		// Validate the config without server-managed hint fields.
		viewOnly := map[string]any{}
		for k, v := range merged {
			if strings.HasSuffix(k, "_hint") && secretKeys[strings.TrimSuffix(k, "_hint")] {
				continue
			}
			viewOnly[k] = v
		}
		if err := validateStructuralConfig(viewOnly); err != nil {
			return err
		}
		if err := ValidateToolConfig(entry, viewOnly); err != nil {
			return err
		}
		// Credential requirements gate enabling only: a workspace may keep an
		// unconfigured configurable tool disabled. Stored ciphertext counts as
		// a satisfied credential (non-empty).
		if setting.Enabled {
			if err := validateCredentialRequirements(entry, viewOnly); err != nil {
				return err
			}
		}
		encrypted, err := s.encryptSecretFields(setting.WorkspaceID, entry, merged)
		if err != nil {
			return err
		}
		setting.Config = encrypted
	} else {
		setting.Config = nil
	}
	return s.settings.Upsert(ctx, setting)
}

// ConfigView describes one tool's settings for API responses: non-secret
// config values verbatim, secret values replaced by their last-4 hint.
type ConfigView struct {
	Enabled    bool           `json:"enabled"`
	Configured bool           `json:"configured"`
	Config     map[string]any `json:"config"`
}

// ViewForWorkspace merges the catalog with the workspace's stored settings:
// one view per catalog key, defaults applied where no row exists.
func (s *ToolSettingsService) ViewForWorkspace(ctx context.Context, workspaceID string) (map[string]ConfigView, error) {
	rows, err := s.settings.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]domain.WorkspaceToolSetting, len(rows))
	for _, row := range rows {
		byKey[row.ToolKey] = row
	}

	views := make(map[string]ConfigView)
	for _, entry := range ToolCatalog() {
		view := ConfigView{Enabled: true, Config: map[string]any{}}
		row, stored := byKey[entry.Key]
		if stored {
			view.Enabled = row.Enabled
		}
		if entry.Configurable {
			config := map[string]any{}
			if stored {
				for k, v := range row.Config {
					config[k] = v
				}
			}
			view.Configured = configSatisfiesRequirements(entry, config)
			view.Config = configWithHints(entry, config)
		}
		views[entry.Key] = view
	}
	return views, nil
}

// ToolConfigs resolves the per-execution config for every configurable tool in
// the workspace (design.md D5): settings values with secrets decrypted,
// instance env merged as the fallback where a workspace has no explicit
// value. Only tools with rows or env fallbacks appear.
func (s *ToolSettingsService) ToolConfigs(ctx context.Context, workspaceID string) (map[string]map[string]any, error) {
	rows, err := s.SettingsForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]domain.WorkspaceToolSetting, len(rows))
	for _, row := range rows {
		byKey[row.ToolKey] = row
	}

	configs := make(map[string]map[string]any)
	for _, entry := range ToolCatalog() {
		if !entry.Configurable {
			continue
		}
		config := map[string]any{}
		if row, stored := byKey[entry.Key]; stored {
			for k, v := range row.Config {
				// Server-managed hint fields never reach the runtime.
				if strings.HasSuffix(k, "_hint") {
					continue
				}
				config[k] = v
			}
		}
		merged := mergeEnvFallbacks(entry.Key, config)
		if len(merged) > 0 {
			configs[entry.Key] = merged
		}
	}
	return configs, nil
}

// mergeEnvFallbacks overlays instance env onto a workspace tool config for
// the configurable tools with env-backed defaults. Workspace values win;
// env only fills gaps (proposal: env remains the soft fallback).
func mergeEnvFallbacks(key string, config map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range config {
		merged[k] = v
	}
	switch key {
	case tools.Name:
		if _, present := merged["provider"]; !present {
			provider := os.Getenv(tools.EnvSearchProvider)
			if provider == "" {
				provider = tools.SearchProviderDuckDuckGo
			}
			merged["provider"] = provider
		}
		if info, ok := tools.SearchProviderInfoFor(stringConfig(merged["provider"])); ok && info.Credential == tools.SearchCredentialAPIKey {
			if _, present := merged["api_key"]; !present {
				if envKey := os.Getenv(tools.EnvTavilyAPIKey); envKey != "" {
					merged["api_key"] = envKey
				}
			}
		}
	case BrowserToolAlias:
		if _, present := merged["remote_cdp_url"]; !present {
			if cdp := os.Getenv(tools.EnvBrowserCDPURL); cdp != "" {
				merged["remote_cdp_url"] = cdp
			}
		}
		if _, present := merged["headless"]; !present {
			merged["headless"] = true
		}
		if _, present := merged["max_pages"]; !present {
			merged["max_pages"] = 3
		}
	}
	return merged
}

func stringConfig(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// validateCredentialRequirements enforces the dynamic requiredness the static
// schema cannot express: web.search's credential field follows the chosen
// provider's credential kind. Failures return a *ConfigValidationError naming
// the missing field.
func validateCredentialRequirements(entry ToolCatalogEntry, config map[string]any) error {
	if entry.Key != tools.Name {
		return nil
	}
	provider := stringConfig(config["provider"])
	if provider == "" {
		// Provider absent falls back to the env-selected or default provider;
		// its credential, if any, must come from settings or env at runtime.
		return nil
	}
	info, known := tools.SearchProviderInfoFor(provider)
	if !known {
		ve := &ConfigValidationError{}
		ve.add("provider", fmt.Sprintf("unknown search provider %q", provider))
		return ve
	}
	ve := &ConfigValidationError{}
	switch info.Credential {
	case tools.SearchCredentialAPIKey:
		if stringConfig(config["api_key"]) == "" {
			ve.add("api_key", fmt.Sprintf("web.search provider %q requires an API key", provider))
		}
	case tools.SearchCredentialBaseURL:
		if stringConfig(config["base_url"]) == "" {
			ve.add("base_url", fmt.Sprintf("web.search provider %q requires a base URL", provider))
		}
	}
	if len(ve.Errors) == 0 {
		return nil
	}
	return ve
}

// configSatisfiesRequirements reports whether a stored (encrypted) config has
// every credential field its chosen provider requires. Encrypted ciphertext
// is non-empty exactly when a secret was supplied, so the check works without
// decrypting.
func configSatisfiesRequirements(entry ToolCatalogEntry, config map[string]any) bool {
	return validateCredentialRequirements(entry, config) == nil
}

// encryptSecretFields encrypts the entry's plaintext secret config fields
// using the workspace ID as AAD — the same key derivation as workspace
// provider keys (design.md D6) — and records a last-4 plaintext hint beside
// each one. Values that are already envelopes (stored ciphertext merged back
// in) pass through untouched; a failing encryption is an error, never a
// silent plaintext store.
func (s *ToolSettingsService) encryptSecretFields(workspaceID string, entry ToolCatalogEntry, config map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(config))
	for k, v := range config {
		out[k] = v
	}
	for _, key := range SecretConfigFields(entry) {
		value, ok := out[key].(string)
		if !ok || value == "" || isSecretEnvelope(value) {
			continue
		}
		envelope, err := secrets.Encrypt(s.encKey, []byte(workspaceID), []byte(value))
		if err != nil {
			return nil, fmt.Errorf("encrypt %s config field %q: %w", entry.Key, key, err)
		}
		out[key] = envelope
		hint := value
		if len(hint) > 4 {
			hint = hint[len(hint)-4:]
		}
		out[key+"_hint"] = hint
	}
	return out, nil
}

// isSecretEnvelope reports whether a config value is already an encrypted
// envelope ("v1:...") rather than plaintext.
func isSecretEnvelope(value string) bool {
	return strings.HasPrefix(value, secrets.Version1Prefix)
}

// decryptSecretFields decrypts the entry's secret config fields in place.
// Values that are not valid envelopes are left as-is so plaintext values
// written by tests or older flows keep working.
func (s *ToolSettingsService) decryptSecretFields(workspaceID string, entry ToolCatalogEntry, config map[string]any) error {
	for _, key := range SecretConfigFields(entry) {
		envelope, ok := config[key].(string)
		if !ok || envelope == "" {
			continue
		}
		plaintext, err := secrets.Decrypt(s.encKey, []byte(workspaceID), envelope)
		if err != nil {
			continue
		}
		config[key] = string(plaintext)
	}
	return nil
}

// configWithHints replaces secret ciphertext with the last-4 hint recorded at
// write time; raw hint fields are not emitted.
func configWithHints(entry ToolCatalogEntry, config map[string]any) map[string]any {
	out := make(map[string]any, len(config))
	for k, v := range config {
		out[k] = v
	}
	for _, key := range SecretConfigFields(entry) {
		delete(out, key+"_hint")
		envelope, ok := out[key].(string)
		if !ok || envelope == "" {
			continue
		}
		hint, _ := config[key+"_hint"].(string)
		out[key] = map[string]any{"hint": hint}
	}
	return out
}
