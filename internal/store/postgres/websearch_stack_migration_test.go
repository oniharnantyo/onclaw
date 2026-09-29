//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// TestIntegration_WebSearchProviderStackMigration covers migration 000023
// (design.md D8): flat web.search tool config is re-nested into a provider
// stack without touching credential envelope strings, ddg/empty rows become
// explicitly unconfigured, already-stacked rows are left alone, and the down
// migration best-effort restores entry 0 to the flat shape.
func TestIntegration_WebSearchProviderStackMigration(t *testing.T) {
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_wsstack_%s", hex.EncodeToString(b))

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	})

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s,public", baseDSN, separator, schemaName)
	mig := postgres.NewMigrator(schemaDSN)

	// Data operations must run against the test schema, so open the fixture
	// connection with the schema-scoped DSN (conn above stays schema-free).
	schemaConn, err := pgx.Connect(ctx, schemaDSN)
	if err != nil {
		t.Fatalf("failed to connect to test schema: %v", err)
	}
	defer schemaConn.Close(ctx)

	// 1. Migrate to the version just before 000023 so flat-shape fixtures can
	// be seeded, then bring 000023 up on top of them.
	if err := mig.MigrateToVersion(22); err != nil {
		t.Fatalf("failed to migrate to version 22: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 22 || dirty {
		t.Fatalf("expected clean version 22 before fixtures, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	const tavilyEnvelope = "enc:v1:AAAA:ZmFrZS1lbmNsb3NlZA=="
	fixtures := []struct {
		slug   string
		config string
	}{
		{"ws-tavily", `{"provider": "tavily", "api_key": "` + tavilyEnvelope + `", "api_key_hint": "c2Ft"}`},
		{"ws-ddg", `{"provider": "duckduckgo"}`},
		{"ws-searxng", `{"provider": "searxng", "base_url": "http://searxng:8080"}`},
		{"ws-empty", `{}`},
		// Already in entries shape: the idempotent up must not touch it.
		{"ws-premigrated", `{"entries": [{"id": "feedface", "name": "Brave 1", "provider": "brave", "api_key": "enc:v1:cHJlbWlncmF0ZWQ="}]}`},
	}

	for _, f := range fixtures {
		if _, err := schemaConn.Exec(ctx,
			`INSERT INTO workspaces (slug, name) VALUES ($1, $1)`, f.slug,
		); err != nil {
			t.Fatalf("failed to insert workspace %s: %v", f.slug, err)
		}
		if _, err := schemaConn.Exec(ctx,
			`INSERT INTO workspace_tool_settings (workspace_id, tool_key, enabled, config)
			 SELECT id, 'web.search', true, $1::jsonb FROM workspaces WHERE slug = $2`,
			f.config, f.slug,
		); err != nil {
			t.Fatalf("failed to insert tool settings for %s: %v", f.slug, err)
		}
	}

	// 2. Apply 000023.
	if err := mig.Up(); err != nil {
		t.Fatalf("failed to migrate up: %v", err)
	}
	// >= 23 rather than == 23: other changes may append later migrations.
	if v, dirty, err := mig.Status(); err != nil || v < 23 || dirty {
		t.Fatalf("expected clean version >= 23 after up, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	// 3. Tavily flat row becomes a single auto-named entry with a
	// byte-identical envelope and a generated 8-hex id.
	tavily := webSearchStackConfig(t, ctx, schemaConn, "ws-tavily")
	if _, ok := tavily["request_timeout_seconds"]; ok {
		t.Error("request_timeout_seconds must be absent after the up migration")
	}
	if _, ok := tavily["provider"]; ok {
		t.Error("flat provider key must be gone after the up migration")
	}
	entries, ok := tavily["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected tavily row to have exactly 1 entry, got %#v", tavily["entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("expected entry object, got %#v", entries[0])
	}
	if entry["api_key"] != tavilyEnvelope {
		t.Errorf("envelope must be byte-identical, got %q want %q", entry["api_key"], tavilyEnvelope)
	}
	id, _ := entry["id"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(id) {
		t.Errorf("expected generated 8-char lowercase hex id, got %q", id)
	}
	if entry["name"] != "Tavily 1" {
		t.Errorf("expected auto name %q, got %q", "Tavily 1", entry["name"])
	}
	if entry["provider"] != "tavily" {
		t.Errorf("expected provider unchanged, got %q", entry["provider"])
	}
	if entry["api_key_hint"] != "c2Ft" {
		t.Errorf("expected api_key_hint moved into entry, got %v", entry["api_key_hint"])
	}
	if _, ok := entry["base_url"]; ok {
		t.Error("base_url must stay absent when the flat row had none")
	}

	// 4. DuckDuckGo and empty rows become explicitly unconfigured.
	for _, slug := range []string{"ws-ddg", "ws-empty"} {
		cfg := webSearchStackConfig(t, ctx, schemaConn, slug)
		entries, ok := cfg["entries"].([]any)
		if !ok || len(entries) != 0 {
			t.Errorf("expected %s to have empty entries, got %#v", slug, cfg["entries"])
		}
	}

	// 5. SearXNG row keeps its base_url inside the entry.
	searxng := webSearchStackConfig(t, ctx, schemaConn, "ws-searxng")
	entries, ok = searxng["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected searxng row to have exactly 1 entry, got %#v", searxng["entries"])
	}
	searxngEntry, _ := entries[0].(map[string]any)
	if searxngEntry["base_url"] != "http://searxng:8080" {
		t.Errorf("expected base_url preserved, got %v", searxngEntry["base_url"])
	}
	if searxngEntry["name"] != "SearXNG 1" {
		t.Errorf("expected auto name %q, got %q", "SearXNG 1", searxngEntry["name"])
	}

	// 6. Already-stacked row is untouched (idempotent up).
	pre := webSearchStackConfig(t, ctx, schemaConn, "ws-premigrated")
	preEntries, ok := pre["entries"].([]any)
	if !ok || len(preEntries) != 1 {
		t.Fatalf("expected premigrated row to keep 1 entry, got %#v", pre["entries"])
	}
	preEntry, _ := preEntries[0].(map[string]any)
	if preEntry["id"] != "feedface" || preEntry["api_key"] != "enc:v1:cHJlbWlncmF0ZWQ=" {
		t.Errorf("premigrated row must be left alone, got %#v", preEntry)
	}

	// 7. Up is idempotent: re-running it changes nothing.
	if err := mig.Up(); err != nil {
		t.Fatalf("expected idempotent up, got %v", err)
	}
	reTavily := webSearchStackConfig(t, ctx, schemaConn, "ws-tavily")
	if fmt.Sprint(reTavily) != fmt.Sprint(tavily) {
		t.Errorf("expected second up to leave the migrated row unchanged, got %#v want %#v", reTavily, tavily)
	}

	// 8. Down restores entry 0 to the flat shape best-effort. Down to version
	// 22 explicitly rather than one step: other changes may append later
	// migrations on top of 000023 (same reason the up assertion is >= 23).
	if err := mig.MigrateToVersion(22); err != nil {
		t.Fatalf("failed to migrate down to version 22: %v", err)
	}
	if v, dirty, err := mig.Status(); err != nil || v != 22 || dirty {
		t.Fatalf("expected clean version 22 after down, got v=%d dirty=%v err=%v", v, dirty, err)
	}

	downTavily := webSearchStackConfig(t, ctx, schemaConn, "ws-tavily")
	if downTavily["provider"] != "tavily" || downTavily["api_key"] != tavilyEnvelope || downTavily["api_key_hint"] != "c2Ft" {
		t.Errorf("expected flat shape restored, got %#v", downTavily)
	}
	if _, ok := downTavily["entries"]; ok {
		t.Error("expected entries key gone after down")
	}

	downSearxng := webSearchStackConfig(t, ctx, schemaConn, "ws-searxng")
	if downSearxng["provider"] != "searxng" || downSearxng["base_url"] != "http://searxng:8080" {
		t.Errorf("expected searxng flat shape with base_url restored, got %#v", downSearxng)
	}

	downPre := webSearchStackConfig(t, ctx, schemaConn, "ws-premigrated")
	if downPre["provider"] != "brave" || downPre["api_key"] != "enc:v1:cHJlbWlncmF0ZWQ=" {
		t.Errorf("expected down to restore entry 0 flat, got %#v", downPre)
	}

	// Empty-entries rows are documented as not fully reversible: they stay
	// {"entries": []} after down.
	for _, slug := range []string{"ws-ddg", "ws-empty"} {
		cfg := webSearchStackConfig(t, ctx, schemaConn, slug)
		entries, ok := cfg["entries"].([]any)
		if !ok || len(entries) != 0 {
			t.Errorf("expected %s to stay empty after down, got %#v", slug, cfg["entries"])
		}
	}
}

// webSearchStackConfig fetches the web.search tool config for a fixture
// workspace and decodes it as JSON.
func webSearchStackConfig(t *testing.T, ctx context.Context, conn *pgx.Conn, slug string) map[string]any {
	t.Helper()
	var raw string
	err := conn.QueryRow(ctx, `
		SELECT s.config::text
		FROM workspace_tool_settings s
		JOIN workspaces w ON w.id = s.workspace_id
		WHERE w.slug = $1 AND s.tool_key = 'web.search'`, slug).Scan(&raw)
	if err != nil {
		t.Fatalf("failed to load web.search config for %s: %v", slug, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("failed to decode config for %s: %v", slug, err)
	}
	return cfg
}
