package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/tools"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// testEncryptionKey is exactly 32 bytes, as secrets.Encrypt requires.
const testEncryptionKey = "0123456789abcdef0123456789abcdef"

// toolSettingsFixture wires a ToolSettingsService over the in-memory store
// with a workspace to persist against.
func toolSettingsFixture(t *testing.T) (*ToolSettingsService, store.ToolSettingsStore, string) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	svc := NewToolSettingsService(st.ToolSettings(), []byte(testEncryptionKey))
	return svc, st.ToolSettings(), ws.ID
}

// searchStack builds a web.search entries config from entry maps.
func searchStack(entries ...map[string]any) map[string]any {
	list := make([]any, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	return map[string]any{"entries": list}
}

func searchEntry(name, provider string, kv ...string) map[string]any {
	e := map[string]any{"name": name, "provider": provider}
	for i := 0; i+1 < len(kv); i += 2 {
		e[kv[i]] = kv[i+1]
	}
	return e
}

func upsertToolConfig(t *testing.T, svc *ToolSettingsService, wsID string, enabled bool, config map[string]any) error {
	t.Helper()
	return svc.Upsert(context.Background(), &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     tools.Name,
		Enabled:     enabled,
		Config:      config,
	})
}

func storedWebSearchConfig(t *testing.T, tstore store.ToolSettingsStore, wsID string) map[string]any {
	t.Helper()
	row, err := tstore.Get(context.Background(), wsID, tools.Name)
	if err != nil {
		t.Fatalf("stored row: %v", err)
	}
	return row.Config
}

func assertConfigValidationError(t *testing.T, err error, wantSubstring string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected ConfigValidationError containing %q, got nil", wantSubstring)
	}
	var cfgErr *ConfigValidationError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigValidationError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), wantSubstring) {
		t.Fatalf("expected error containing %q, got %q (details: %v)", wantSubstring, err.Error(), cfgErr.Errors)
	}
}

func assertShortHexID(t *testing.T, id any, label string) string {
	t.Helper()
	s, ok := id.(string)
	if !ok || len(s) != 8 {
		t.Fatalf("%s: expected 8-char id string, got %#v", label, id)
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("%s: expected lowercase hex id, got %q", label, s)
		}
	}
	return s
}

func TestUpsert_WebSearchAssignsStableEntryIDs(t *testing.T) {
	svc, tstore, wsID := toolSettingsFixture(t)

	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Tavily 1", "tavily", "api_key", "key-abcdef1234"),
	)); err != nil {
		t.Fatalf("first save: %v", err)
	}

	entries := configEntries(storedWebSearchConfig(t, tstore, wsID))
	if len(entries) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(entries))
	}
	id := assertShortHexID(t, entries[0]["id"], "first entry")
	if !isSecretEnvelope(stringConfig(entries[0]["api_key"])) {
		t.Fatalf("expected stored api_key to be an envelope, got %q", entries[0]["api_key"])
	}
	if entries[0]["api_key_hint"] != "1234" {
		t.Fatalf("expected last-4 hint %q, got %v", "1234", entries[0]["api_key_hint"])
	}

	// Echo the id back with an empty key (write-only secret) plus a new entry:
	// the first entry keeps its id, the new one gets a fresh one.
	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		map[string]any{"id": id, "name": "Tavily 1", "provider": "tavily"},
		searchEntry("Exa 1", "exa", "api_key", "exa-key-9999"),
	)); err != nil {
		t.Fatalf("second save: %v", err)
	}

	entries = configEntries(storedWebSearchConfig(t, tstore, wsID))
	if len(entries) != 2 {
		t.Fatalf("expected 2 stored entries, got %d", len(entries))
	}
	if got := assertShortHexID(t, entries[0]["id"], "kept entry"); got != id {
		t.Fatalf("entry id changed across saves: %q -> %q", id, got)
	}
	newID := assertShortHexID(t, entries[1]["id"], "new entry")
	if newID == id {
		t.Fatalf("new entry must get a fresh id, got %q twice", id)
	}
}

func TestUpsert_KeepSecretByIDAcrossReorder(t *testing.T) {
	svc, tstore, wsID := toolSettingsFixture(t)

	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Tavily 1", "tavily", "api_key", "key-AAAA1111"),
		searchEntry("Exa 1", "exa", "api_key", "key-BBBB2222"),
	)); err != nil {
		t.Fatalf("first save: %v", err)
	}
	ids := configEntries(storedWebSearchConfig(t, tstore, wsID))
	tavilyID := ids[0]["id"]
	exaID := ids[1]["id"]

	// Reorder with the keys omitted: the credentials must follow their ids,
	// not their list positions.
	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		map[string]any{"id": exaID, "name": "Exa 1", "provider": "exa"},
		map[string]any{"id": tavilyID, "name": "Tavily 1", "provider": "tavily"},
	)); err != nil {
		t.Fatalf("reordered save: %v", err)
	}

	rows, err := svc.SettingsForWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	var decrypted []map[string]any
	for _, row := range rows {
		if row.ToolKey == tools.Name {
			decrypted = configEntries(row.Config)
		}
	}
	if len(decrypted) != 2 {
		t.Fatalf("expected 2 decrypted entries, got %d", len(decrypted))
	}
	if decrypted[0]["name"] != "Exa 1" || decrypted[0]["api_key"] != "key-BBBB2222" {
		t.Fatalf("position 0 must carry Exa's own secret, got %v", decrypted[0])
	}
	if decrypted[1]["name"] != "Tavily 1" || decrypted[1]["api_key"] != "key-AAAA1111" {
		t.Fatalf("position 1 must carry Tavily's own secret, got %v", decrypted[1])
	}
}

func TestUpsert_DuplicateEntryNameRejected(t *testing.T) {
	svc, tstore, wsID := toolSettingsFixture(t)

	err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Tavily 1", "tavily", "api_key", "key-1"),
		searchEntry("tavily 1", "exa", "api_key", "key-2"),
	))
	assertConfigValidationError(t, err, "duplicate entry name")

	// Nothing persisted on rejection.
	if _, got := tstore.Get(context.Background(), wsID, tools.Name); got != domain.ErrNotFound {
		t.Fatalf("rejected save must not persist, got %v", got)
	}
}

func TestUpsert_EnableRequiresValidEntry(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)

	err := upsertToolConfig(t, svc, wsID, true, map[string]any{"entries": []any{}})
	assertConfigValidationError(t, err, "at least one fully configured provider entry")

	// Disabled with no entries is a legitimate unconfigured state.
	if err := upsertToolConfig(t, svc, wsID, false, map[string]any{"entries": []any{}}); err != nil {
		t.Fatalf("disabled empty stack must save: %v", err)
	}

	// Enabling while still empty is rejected even with the row stored.
	err = upsertToolConfig(t, svc, wsID, true, nil)
	assertConfigValidationError(t, err, "at least one fully configured provider entry")
}

func TestUpsert_NewEntryMissingCredentialRejected(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)

	err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Tavily 1", "tavily"),
	))
	assertConfigValidationError(t, err, "api_key is required")

	err = upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Self-hosted", "searxng"),
	))
	assertConfigValidationError(t, err, "base_url is required")
}

func TestUpsert_CapsEntriesAtTwenty(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)

	build := func(n int) map[string]any {
		entries := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			entries = append(entries, searchEntry(fmt.Sprintf("Provider %d", i+1), "tavily", "api_key", fmt.Sprintf("key-%d", i)))
		}
		return searchStack(entries...)
	}

	if err := upsertToolConfig(t, svc, wsID, true, build(maxWebSearchEntries)); err != nil {
		t.Fatalf("%d entries must save: %v", maxWebSearchEntries, err)
	}

	err := upsertToolConfig(t, svc, wsID, true, build(maxWebSearchEntries+1))
	assertConfigValidationError(t, err, "at most 20 provider entries")
}

func TestUpsert_RequestTimeoutBounds(t *testing.T) {
	svc, tstore, wsID := toolSettingsFixture(t)

	for _, bad := range []any{0, -1, 61, 10.5} {
		err := upsertToolConfig(t, svc, wsID, true, map[string]any{
			"entries":                 []any{searchEntry("Tavily 1", "tavily", "api_key", "key-1")},
			"request_timeout_seconds": bad,
		})
		if err == nil {
			t.Fatalf("request_timeout_seconds %v must be rejected", bad)
		}
	}

	if err := upsertToolConfig(t, svc, wsID, true, map[string]any{
		"entries":                 []any{searchEntry("Tavily 1", "tavily", "api_key", "key-1")},
		"request_timeout_seconds": 30,
	}); err != nil {
		t.Fatalf("timeout 30 must save: %v", err)
	}
	if got := storedWebSearchConfig(t, tstore, wsID)["request_timeout_seconds"]; got != 30 {
		t.Fatalf("expected stored timeout 30, got %v", got)
	}
}

func TestViewForWorkspace_StripsCredentialKeepsHint(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)

	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Tavily 1", "tavily", "api_key", "key-abcdef1234"),
	)); err != nil {
		t.Fatalf("save: %v", err)
	}

	views, err := svc.ViewForWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatalf("views: %v", err)
	}
	view := views[tools.Name]
	if !view.Configured {
		t.Fatal("workspace with a valid entry must be Configured")
	}
	entries := configEntries(view.Config)
	if len(entries) != 1 {
		t.Fatalf("expected 1 view entry, got %d", len(entries))
	}
	if _, leaked := entries[0]["api_key"]; leaked {
		t.Fatalf("view must never carry the credential: %v", entries[0])
	}
	if entries[0]["api_key_hint"] != "1234" {
		t.Fatalf("expected nested hint %q, got %v", "1234", entries[0]["api_key_hint"])
	}
	if entries[0]["name"] != "Tavily 1" || entries[0]["provider"] != "tavily" {
		t.Fatalf("non-secret fields must pass through: %v", entries[0])
	}

	// A fresh workspace reports web.search unconfigured.
	st2 := fake.New()
	fresh := NewToolSettingsService(st2.ToolSettings(), []byte(testEncryptionKey))
	freshViews, err := fresh.ViewForWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatalf("fresh views: %v", err)
	}
	if freshViews[tools.Name].Configured {
		t.Fatal("fresh workspace must report web.search unconfigured")
	}
}

func TestToolConfigs_EntriesDecryptAndStripHints(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)

	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Tavily 1", "tavily", "api_key", "key-abcdef1234"),
	)); err != nil {
		t.Fatalf("save: %v", err)
	}

	configs, err := svc.ToolConfigs(context.Background(), wsID)
	if err != nil {
		t.Fatalf("configs: %v", err)
	}
	config := configs[tools.Name]
	entries := configEntries(config)
	if len(entries) != 1 {
		t.Fatalf("expected 1 runtime entry, got %d", len(entries))
	}
	if entries[0]["api_key"] != "key-abcdef1234" {
		t.Fatalf("runtime config must arrive decrypted, got %v", entries[0]["api_key"])
	}
	if _, leaked := entries[0]["api_key_hint"]; leaked {
		t.Fatalf("hint must never reach the runtime: %v", entries[0])
	}
	if entries[0]["id"] == "" {
		t.Fatal("runtime entry must keep its stable id")
	}
}

func TestToolConfigs_EnvFallbackSeedsEntry(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)
	t.Setenv(tools.EnvSearchProvider, "tavily")
	t.Setenv(tools.EnvTavilyAPIKey, "env-key-42")

	configs, err := svc.ToolConfigs(context.Background(), wsID)
	if err != nil {
		t.Fatalf("configs: %v", err)
	}
	entries := configEntries(configs[tools.Name])
	if len(entries) != 1 {
		t.Fatalf("expected exactly one env-seeded entry, got %d", len(entries))
	}
	if entries[0]["name"] != envSearchFallbackEntryName {
		t.Fatalf("expected %q, got %v", envSearchFallbackEntryName, entries[0]["name"])
	}
	if entries[0]["provider"] != "tavily" || entries[0]["api_key"] != "env-key-42" {
		t.Fatalf("env seed must carry provider and key: %v", entries[0])
	}
}

func TestToolConfigs_EnvFallbackIgnoredWhenEntriesExist(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)
	t.Setenv(tools.EnvSearchProvider, "tavily")
	t.Setenv(tools.EnvTavilyAPIKey, "env-key-42")

	if err := upsertToolConfig(t, svc, wsID, true, searchStack(
		searchEntry("Exa 1", "exa", "api_key", "ws-key"),
	)); err != nil {
		t.Fatalf("save: %v", err)
	}

	configs, err := svc.ToolConfigs(context.Background(), wsID)
	if err != nil {
		t.Fatalf("configs: %v", err)
	}
	entries := configEntries(configs[tools.Name])
	if len(entries) != 1 || entries[0]["name"] != "Exa 1" {
		t.Fatalf("workspace entries must win over env, got %v", entries)
	}
}

func TestToolConfigs_DDGEnvCountsAsUnset(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)
	t.Setenv(tools.EnvSearchProvider, "duckduckgo")

	configs, err := svc.ToolConfigs(context.Background(), wsID)
	if err != nil {
		t.Fatalf("configs: %v", err)
	}
	if _, present := configs[tools.Name]; present {
		t.Fatalf("duckduckgo env must count as unset, got %v", configs[tools.Name])
	}
}

func TestToolConfigs_EnvProviderWithoutKeyErrors(t *testing.T) {
	svc, _, wsID := toolSettingsFixture(t)
	t.Setenv(tools.EnvSearchProvider, "tavily")
	t.Setenv(tools.EnvTavilyAPIKey, "")

	if _, err := svc.ToolConfigs(context.Background(), wsID); err == nil {
		t.Fatal("env selecting an api-key provider without a key must error")
	}
}

// seedDisabledRow writes an enabled=false row directly through the store,
// bypassing the service — the stale-row shape the always-on exemption must
// ignore.
func seedDisabledRow(t *testing.T, tstore store.ToolSettingsStore, wsID, toolKey string) {
	t.Helper()
	if err := tstore.Upsert(context.Background(), &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     toolKey,
		Enabled:     false,
	}); err != nil {
		t.Fatalf("seed disabled row for %s: %v", toolKey, err)
	}
}

func TestEnabledTools_AlwaysOnIgnoresDisabledRows(t *testing.T) {
	svc, tstore, wsID := toolSettingsFixture(t)

	for _, key := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		seedDisabledRow(t, tstore, wsID, key)
	}
	seedDisabledRow(t, tstore, wsID, "ls")

	enabled, err := svc.EnabledTools(context.Background(), wsID)
	if err != nil {
		t.Fatalf("enabled tools: %v", err)
	}
	for _, key := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		if !enabled[key] {
			t.Errorf("%s must stay enabled despite a disabled row", key)
		}
	}
	if enabled["ls"] {
		t.Error("ls must honor its disabled row")
	}
}

func TestViewForWorkspace_AlwaysOnReadsEnabledDespiteRows(t *testing.T) {
	svc, tstore, wsID := toolSettingsFixture(t)

	for _, key := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		seedDisabledRow(t, tstore, wsID, key)
	}

	views, err := svc.ViewForWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatalf("views: %v", err)
	}
	for _, key := range []string{ChannelToolPost, ChannelToolHistory, SessionToolClose} {
		if !views[key].Enabled {
			t.Errorf("%s must read enabled despite a disabled row", key)
		}
	}
}
