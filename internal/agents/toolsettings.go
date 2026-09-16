package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
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
// disabled by a settings row is enabled. Always-on tools are exempt: their
// exposure is governed by the run's execution context, so a stale or hostile
// disabled row never strips them (always-on-channel-tools D2).
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
		if entry.AlwaysOn {
			enabled[entry.Key] = true
			continue
		}
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
// is merged over the stored one first: non-secret values overwrite, secret
// values are replaced only when the client supplies a non-empty one (secret
// fields are write-only), and list fields merge per entry — an entry keeps
// its stored credential by id when the client re-supplies it empty (design.md
// D5). Schema, structural, and per-entry failures return a
// *ConfigValidationError (HTTP 422 with per-field details); unknown tools and
// structurally invalid settings return domain.ErrInvalid.
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
	listFields := map[string]ConfigField{}
	for _, field := range entry.ConfigSchema {
		if field.Type == ConfigFieldList {
			listFields[field.Key] = field
		}
	}
	for k, v := range setting.Config {
		if _, isList := listFields[k]; isList {
			// List fields merge per entry below: ids decide secret
			// keep/replace, not the wholesale overwrite flat fields get.
			continue
		}
		if secretKeys[k] {
			// A secret field is replaced only by a non-empty supply; an empty
			// value means "keep the stored credential".
			if isEmptyConfigValue(v) {
				continue
			}
		}
		merged[k] = v
	}
	for key, field := range listFields {
		incoming, supplied := setting.Config[key]
		if !supplied || isEmptyConfigValue(incoming) {
			continue // absent or null keeps the stored list
		}
		incomingItems, ok := configListItems(incoming)
		if !ok {
			// Kept verbatim so schema validation reports the malformed shape.
			merged[key] = incoming
			continue
		}
		var storedItems []map[string]any
		if existing != nil {
			storedItems = configEntries(existing.Config)
		}
		mergedEntries, err := mergeConfigEntries(incomingItems, storedItems, field)
		if err != nil {
			return err
		}
		merged[key] = mergedEntries
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
		// Per-entry semantics (unique names, credential per provider kind,
		// entry cap) apply to every save; enabling additionally requires at
		// least one fully valid entry (design.md D9). Stored ciphertext counts
		// as a satisfied credential (non-empty after the merge above).
		if entry.Key == tools.Name {
			if err := validateWebSearchEntries(viewOnly, setting.Enabled); err != nil {
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
		// Always-on tools read as enabled regardless of the stored row, so
		// what the UI shows is what the runtime does (always-on-channel-tools
		// D3) — a stale disabled row never surfaces.
		if stored && !entry.AlwaysOn {
			view.Enabled = row.Enabled
		}
		if entry.Configurable {
			config := map[string]any{}
			if stored {
				for k, v := range row.Config {
					config[k] = v
				}
			}
			if entry.Key == tools.Name {
				view.Configured = webSearchHasValidEntry(config)
			} else {
				view.Configured = true
			}
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
			if field, ok := listConfigField(entry); ok {
				for _, key := range secretItemKeys(field) {
					for _, entryItem := range configEntries(config) {
						delete(entryItem, key+"_hint")
					}
				}
			}
		}
		merged, err := mergeEnvFallbacks(entry.Key, config)
		if err != nil {
			return nil, err
		}
		if len(merged) > 0 {
			configs[entry.Key] = merged
		}
	}
	return configs, nil
}

// envSearchFallbackEntryName labels the single fallback entry seeded from
// instance env (design.md D6).
const envSearchFallbackEntryName = "Instance default (env)"

// mergeEnvFallbacks overlays instance env onto a workspace tool config for
// the configurable tools with env-backed defaults. Workspace values win; env
// only fills gaps. For web.search (design.md D6) the env seeds a single
// fallback entry only when the workspace has no entries — it is never a
// silent default on top of a configured stack.
func mergeEnvFallbacks(key string, config map[string]any) (map[string]any, error) {
	merged := map[string]any{}
	for k, v := range config {
		merged[k] = v
	}
	switch key {
	case tools.Name:
		if len(configEntries(merged)) > 0 {
			break // workspace entries win; env never mixes into a populated stack
		}
		seed, err := envSearchFallbackEntry()
		if err != nil {
			return nil, err
		}
		if seed != nil {
			merged["entries"] = []any{seed}
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
	return merged, nil
}

// envSearchFallbackEntry builds the single-entry chain seeded from instance
// env when a workspace has no entries (design.md D6). Unset env — including
// "duckduckgo", the removed scraping default, which counts as unset — or an
// unknown provider returns nil. Env naming a provider whose credential is
// unavailable is an error, never a silent skip.
func envSearchFallbackEntry() (map[string]any, error) {
	provider := os.Getenv(tools.EnvSearchProvider)
	if provider == "" || provider == "duckduckgo" {
		return nil, nil
	}
	info, known := tools.SearchProviderInfoFor(provider)
	if !known {
		return nil, nil
	}
	seed := map[string]any{"name": envSearchFallbackEntryName, "provider": provider}
	switch info.Credential {
	case tools.SearchCredentialAPIKey:
		if envKey := os.Getenv(tools.EnvTavilyAPIKey); envKey != "" {
			seed["api_key"] = envKey
			return seed, nil
		}
		return nil, fmt.Errorf("web.search: %s selects %q, which requires an API key — set %s", tools.EnvSearchProvider, provider, tools.EnvTavilyAPIKey)
	case tools.SearchCredentialBaseURL:
		return nil, fmt.Errorf("web.search: %s selects %q, which requires a base URL — configure a provider entry for this workspace", tools.EnvSearchProvider, provider)
	}
	return nil, nil
}

func stringConfig(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// maxWebSearchEntries is the sanity cap on provider entries per workspace
// (design.md D9) — well above the three-entry request window.
const maxWebSearchEntries = 20

// validateWebSearchEntries enforces the per-entry semantics the declarative
// schema cannot express: non-empty names unique case-insensitively, known
// providers, and the credential each provider's kind requires (api_key kinds:
// a stored envelope carried by the merge or a newly supplied key; base_url
// kind: a non-empty base URL), the entry cap, and the timeout bounds.
// requireValidEntry gates enabling: at least one fully valid entry.
// Failures return a *ConfigValidationError naming the offending entry.
func validateWebSearchEntries(config map[string]any, requireValidEntry bool) error {
	ve := &ConfigValidationError{}
	if raw, present := config["request_timeout_seconds"]; present && !isEmptyConfigValue(raw) {
		n, ok := numericConfigValue(raw)
		switch {
		case !ok || n <= 0 || n != float64(int64(n)):
			ve.add("request_timeout_seconds", "request_timeout_seconds must be a positive whole number of seconds")
		case n > webSearchMaxTimeoutSeconds:
			ve.add("request_timeout_seconds", fmt.Sprintf("request_timeout_seconds must be at most %d", webSearchMaxTimeoutSeconds))
		}
	}

	entries := configEntries(config)
	if len(entries) > maxWebSearchEntries {
		ve.add("entries", fmt.Sprintf("web.search supports at most %d provider entries (%d configured)", maxWebSearchEntries, len(entries)))
	}
	seen := make(map[string]bool, len(entries))
	validEntries := 0
	for i, entryItem := range entries {
		prefix := fmt.Sprintf("entries[%d]", i)
		name := strings.TrimSpace(stringConfig(entryItem["name"]))
		provider := stringConfig(entryItem["provider"])
		if name == "" {
			ve.add(prefix+".name", fmt.Sprintf("entry %d: name is required", i+1))
		} else if seen[strings.ToLower(name)] {
			ve.add(prefix+".name", fmt.Sprintf("duplicate entry name %q", name))
		}
		if name != "" {
			seen[strings.ToLower(name)] = true
		}
		info, knownProvider := tools.SearchProviderInfoFor(provider)
		if !knownProvider {
			ve.add(prefix+".provider", fmt.Sprintf("entry %q: unknown search provider %q", name, provider))
		} else {
			switch info.Credential {
			case tools.SearchCredentialAPIKey:
				if stringConfig(entryItem["api_key"]) == "" {
					ve.add(prefix+".api_key", fmt.Sprintf("entry %q: api_key is required for provider %q", name, provider))
				}
			case tools.SearchCredentialBaseURL:
				if stringConfig(entryItem["base_url"]) == "" {
					ve.add(prefix+".base_url", fmt.Sprintf("entry %q: base_url is required for provider %q", name, provider))
				}
			}
		}
		if searchEntryValid(entryItem) {
			validEntries++
		}
	}
	if requireValidEntry && validEntries == 0 {
		ve.add("entries", "web.search requires at least one fully configured provider entry to enable")
	}
	if len(ve.Errors) == 0 {
		return nil
	}
	return ve
}

// searchEntryValid reports whether one entry carries a non-empty name, a
// known provider, and the credential that provider's kind requires. Envelope
// ciphertext is non-empty exactly when a secret was supplied, so the check
// works on stored (encrypted) config without decrypting.
func searchEntryValid(entryItem map[string]any) bool {
	if strings.TrimSpace(stringConfig(entryItem["name"])) == "" {
		return false
	}
	info, known := tools.SearchProviderInfoFor(stringConfig(entryItem["provider"]))
	if !known {
		return false
	}
	switch info.Credential {
	case tools.SearchCredentialAPIKey:
		return stringConfig(entryItem["api_key"]) != ""
	case tools.SearchCredentialBaseURL:
		return stringConfig(entryItem["base_url"]) != ""
	}
	return false
}

// webSearchHasValidEntry reports whether a stored config holds at least one
// fully valid entry — the Configured signal for the tools view (design.md D9).
func webSearchHasValidEntry(config map[string]any) bool {
	for _, entryItem := range configEntries(config) {
		if searchEntryValid(entryItem) {
			return true
		}
	}
	return false
}

// newEntryID returns the 8-char lowercase hex id the server assigns to an
// entry when it is first persisted (design.md D1).
func newEntryID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate web.search entry id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// mergeConfigEntries reconciles an incoming entry list against the stored one
// (design.md D1/D5): the server assigns a stable id to every entry, and an
// entry whose id matches a stored entry keeps the stored secret envelope and
// hint when the client supplies an empty credential — secrets are write-only
// and the client cannot echo them back. Entries are matched by id, not
// position, so reordering never scrambles credentials. An id that matches no
// stored entry is treated as new: reassigned, with any empty credential
// rejected later by validation rather than silently dropped.
func mergeConfigEntries(incoming, stored []map[string]any, field ConfigField) ([]any, error) {
	storedByID := make(map[string]map[string]any, len(stored))
	for _, storedItem := range stored {
		if id := stringConfig(storedItem["id"]); id != "" {
			storedByID[id] = storedItem
		}
	}
	secretKeys := secretItemKeys(field)
	out := make([]any, 0, len(incoming))
	for _, item := range incoming {
		entryItem := make(map[string]any, len(item))
		for k, v := range item {
			entryItem[k] = v
		}
		id := stringConfig(entryItem["id"])
		storedItem, knownID := storedByID[id]
		if id != "" && !knownID {
			id = ""
			delete(entryItem, "id")
		}
		for _, key := range secretKeys {
			if !isEmptyConfigValue(entryItem[key]) {
				continue // client supplies a new credential; encryption refreshes the hint
			}
			if !knownID {
				continue // nothing stored to keep
			}
			if v, ok := storedItem[key]; ok {
				entryItem[key] = v
			}
			if v, ok := storedItem[key+"_hint"]; ok {
				entryItem[key+"_hint"] = v
			}
		}
		if id == "" {
			assigned, err := newEntryID()
			if err != nil {
				return nil, err
			}
			entryItem["id"] = assigned
		}
		out = append(out, entryItem)
	}
	return out, nil
}

// encryptSecretFields encrypts the entry's plaintext secret config fields
// using the workspace ID as AAD — the same key derivation as workspace
// provider keys (design.md D6) — and records a last-4 plaintext hint beside
// each one (nested inside list entries, design.md D5). Values that are
// already envelopes (stored ciphertext merged back in) pass through
// untouched; a failing encryption is an error, never a silent plaintext
// store.
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
		envelope, err := encryptSecretValue(s.encKey, secretAAD(workspaceID), value)
		if err != nil {
			return nil, fmt.Errorf("encrypt %s config field %q: %w", entry.Key, key, err)
		}
		out[key] = envelope
		out[key+"_hint"] = lastSecretChars(value, 4)
	}
	if field, ok := listConfigField(entry); ok {
		for _, key := range secretItemKeys(field) {
			for _, entryItem := range configEntries(out) {
				value, ok := entryItem[key].(string)
				if !ok || value == "" || isSecretEnvelope(value) {
					continue
				}
				envelope, err := encryptSecretValue(s.encKey, secretAAD(workspaceID), value)
				if err != nil {
					return nil, fmt.Errorf("encrypt %s entry %q config field %q: %w", entry.Key, stringConfig(entryItem["name"]), key, err)
				}
				entryItem[key] = envelope
				entryItem[key+"_hint"] = lastSecretChars(value, 4)
			}
		}
	}
	return out, nil
}

// copyConfigEntries rebuilds a config's "entries" value as fresh per-entry
// maps so later mutation (decryption, hint stripping) never touches
// store-owned state — the in-memory store shallow-copies config maps.
func copyConfigEntries(config map[string]any) {
	entries := configEntries(config)
	if entries == nil {
		return
	}
	fresh := make([]any, 0, len(entries))
	for _, entryItem := range entries {
		cp := make(map[string]any, len(entryItem))
		for k, v := range entryItem {
			cp[k] = v
		}
		fresh = append(fresh, cp)
	}
	config["entries"] = fresh
}

// decryptSecretFields decrypts the entry's secret config fields in place —
// including per-entry secrets nested inside list fields (design.md D5).
// Values that are not valid envelopes are left as-is so plaintext values
// written by tests or older flows keep working.
func (s *ToolSettingsService) decryptSecretFields(workspaceID string, entry ToolCatalogEntry, config map[string]any) error {
	if field, ok := listConfigField(entry); ok {
		copyConfigEntries(config)
		for _, key := range secretItemKeys(field) {
			for _, entryItem := range configEntries(config) {
				envelope, ok := entryItem[key].(string)
				if !ok || envelope == "" {
					continue
				}
				plaintext, err := decryptSecretValue(s.encKey, secretAAD(workspaceID), envelope)
				if err != nil {
					continue
				}
				entryItem[key] = plaintext
			}
		}
	}
	for _, key := range SecretConfigFields(entry) {
		envelope, ok := config[key].(string)
		if !ok || envelope == "" {
			continue
		}
		plaintext, err := decryptSecretValue(s.encKey, secretAAD(workspaceID), envelope)
		if err != nil {
			continue
		}
		config[key] = plaintext
	}
	return nil
}

// configWithHints replaces secret ciphertext with the last-4 hint recorded at
// write time: flat secret values become {"hint": ...}, list entries carry the
// hint nested as the secret's "_hint" field with the credential removed —
// credentials never cross the API boundary (design.md D5).
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
	if field, ok := listConfigField(entry); ok {
		entries := configEntries(out)
		viewEntries := make([]any, 0, len(entries))
		for _, entryItem := range entries {
			viewEntry := make(map[string]any, len(entryItem))
			for k, v := range entryItem {
				viewEntry[k] = v
			}
			for _, key := range secretItemKeys(field) {
				hint, _ := viewEntry[key+"_hint"].(string)
				delete(viewEntry, key)
				viewEntry[key+"_hint"] = hint
			}
			viewEntries = append(viewEntries, viewEntry)
		}
		out["entries"] = viewEntries
	}
	return out
}
