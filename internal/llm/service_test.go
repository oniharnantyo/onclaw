package llm

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func testSetup(t *testing.T) (*sql.DB, *Service) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	if err := sqlite.Migrate(db); err != nil {
		db.Close()
		t.Fatalf("migration failed: %v", err)
	}

	dek, err := secrets.GenerateDEK()
	if err != nil {
		db.Close()
		t.Fatalf("failed to generate DEK: %v", err)
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	km := secrets.NewKeyManager(dek)
	ar := adapter.NewRegistry()
	ar.Register("openai", func() adapter.Adapter { return adapter.NewStubAdapter() })
	ar.Register("ollama", func() adapter.Adapter { return adapter.NewStubAdapter() })

	srv := NewService(ps, ss, km, ar, as)
	return db, srv
}

func newEnabledProfile(name, providerType string) *store.Profile {
	return &store.Profile{Name: name, ProviderType: providerType, Enabled: 1}
}

func TestProviderCRUD(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	badProfile := &store.Profile{Name: "", ProviderType: "openai"}
	if err := srv.AddProfile(ctx, badProfile); err == nil {
		t.Error("expected error for empty name, got nil")
	}

	p1 := newEnabledProfile("openai-test", "openai")
	p1.APIBase = "https://api.openai.com/v1"
	if err := srv.AddProfile(ctx, p1); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}

	if err := srv.AddProfile(ctx, p1); err == nil {
		t.Error("expected error when adding duplicate profile, got nil")
	}

	retrieved, err := srv.GetProfile(ctx, "openai-test")
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}
	if retrieved.Name != p1.Name || retrieved.ProviderType != p1.ProviderType || retrieved.APIBase != p1.APIBase {
		t.Errorf("retrieved profile mismatch: %+v", retrieved)
	}

	_, err = srv.GetProfile(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent profile, got nil")
	}

	p2 := newEnabledProfile("claude-test", "openai")
	if err := srv.AddProfile(ctx, p2); err != nil {
		t.Fatalf("failed to add second profile: %v", err)
	}

	list, err := srv.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("failed to list profiles: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(list))
	}

	if err := srv.RemoveProfile(ctx, "openai-test"); err != nil {
		t.Fatalf("failed to remove profile: %v", err)
	}

	list2, err := srv.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("failed to list profiles after removal: %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("expected 1 profile, got %d", len(list2))
	}
}

func TestEncryptionAtRest(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	err := srv.SetSecret(ctx, "non-existent", "my-secret-key")
	if err == nil {
		t.Error("expected failure setting secret for non-existent profile, got nil")
	}

	p := newEnabledProfile("openai-test", "openai")
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}

	apiKey := "super-secret-api-key-123"
	if err := srv.SetSecret(ctx, "openai-test", apiKey); err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}

	gotKey, err := srv.GetSecret(ctx, "openai-test")
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}
	if gotKey != apiKey {
		t.Errorf("secret mismatch: got %q, want %q", gotKey, apiKey)
	}

	var dbVal string
	err = db.QueryRowContext(ctx, "SELECT encrypted_value FROM config_secrets WHERE key = ?", "provider:openai-test").Scan(&dbVal)
	if err != nil {
		t.Fatalf("failed to query config_secrets table directly: %v", err)
	}

	if dbVal == apiKey {
		t.Fatal("security violation: API key stored in plaintext!")
	}

	if _, err := base64.StdEncoding.DecodeString(dbVal); err != nil {
		t.Errorf("stored encrypted value is not valid base64: %v (val: %s)", err, dbVal)
	}
}

func TestSecretResolutionPrecedence(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	pName := "openai-test"
	p := newEnabledProfile(pName, "openai")
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}

	_, err := srv.ResolveSecret(ctx, pName)
	if err == nil {
		t.Fatal("expected error resolving unset secret, got nil")
	}
	if !errors.Is(err, ErrSecretNotSet) {
		t.Errorf("expected ErrSecretNotSet, got %v", err)
	}
	if !strings.Contains(err.Error(), "API key for provider openai-test is not set") {
		t.Errorf("expected 'not set' guidance in error, got %q", err.Error())
	}

	dbKey := "db-api-key"
	if err := srv.SetSecret(ctx, pName, dbKey); err != nil {
		t.Fatalf("failed to set secret in DB: %v", err)
	}

	resolved, err := srv.ResolveSecret(ctx, pName)
	if err != nil {
		t.Fatalf("failed to resolve secret from DB: %v", err)
	}
	if resolved != dbKey {
		t.Errorf("expected resolved key to be %q, got %q", dbKey, resolved)
	}

	envKeyName := "ONCLAW_PROVIDER_OPENAI_TEST_API_KEY"
	envValue := "env-api-key"
	os.Setenv(envKeyName, envValue)
	defer os.Unsetenv(envKeyName)

	resolved, err = srv.ResolveSecret(ctx, pName)
	if err != nil {
		t.Fatalf("failed to resolve secret with env set: %v", err)
	}
	if resolved != envValue {
		t.Errorf("expected resolved key to be overridden by env (%q), got %q", envValue, resolved)
	}
}

func TestBuild(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	_, err := srv.BuildAgent(ctx, "non-existent")
	if err == nil {
		t.Error("expected error building non-existent agent, got nil")
	}

	p := newEnabledProfile("openai-test", "openai")
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}

	pCopy := *p
	_, err = srv.BuildWithProfile(ctx, &pCopy, "gpt-4")
	if err == nil {
		t.Error("expected error building profile with no secret, got nil")
	}

	if err := srv.SetSecret(ctx, "openai-test", "dummy-key"); err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}

	pCopy2, _ := srv.GetProfile(ctx, "openai-test")
	model, err := srv.BuildWithProfile(ctx, pCopy2, "gpt-4")
	if err != nil {
		t.Fatalf("failed to build model: %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil ChatModel, got nil")
	}
}

func TestBuildKeylessProviderWithoutSecret(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	// Ollama is keyless: a local server that needs no API key.
	// Building must succeed with neither a stored nor an env secret.
	p := newEnabledProfile("ollama-local", "ollama")
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add ollama profile: %v", err)
	}

	pCopy, _ := srv.GetProfile(ctx, "ollama-local")
	chatModel, err := srv.BuildWithProfile(ctx, pCopy, "llama3")
	if err != nil {
		t.Fatalf("expected keyless provider to build without an API key, got error: %v", err)
	}
	if chatModel == nil {
		t.Fatal("expected non-nil ChatModel for keyless provider, got nil")
	}
}

func TestBuildAgentKeylessProviderWithoutSecret(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	p := newEnabledProfile("ollama-local", "ollama")
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add ollama profile: %v", err)
	}

	a := &store.Agent{Name: "local-agent", Provider: "ollama-local", Model: "llama3"}
	if err := srv.AddAgent(ctx, a); err != nil {
		t.Fatalf("failed to add agent: %v", err)
	}

	chatModel, err := srv.BuildAgent(ctx, "local-agent")
	if err != nil {
		t.Fatalf("expected keyless agent to build without an API key, got error: %v", err)
	}
	if chatModel == nil {
		t.Fatal("expected non-nil ChatModel for keyless agent, got nil")
	}
}

func TestHotReload(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-hotreload-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, "onclaw_test.db")

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	dek, err := secrets.GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	km := secrets.NewKeyManager(dek)
	ar := adapter.NewRegistry()
	ar.Register("openai", func() adapter.Adapter { return adapter.NewStubAdapter() })

	srv := NewService(ps, ss, km, ar, as)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher, err := StartDBWatcher(ctx, tmpPath, srv)
	if err != nil {
		t.Fatalf("failed to start DB watcher: %v", err)
	}
	defer watcher.Close()

	profiles, err := srv.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("failed to list profiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}

	db2, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		t.Fatalf("failed to open second db connection: %v", err)
	}
	_, err = db2.Exec("INSERT INTO llm_providers (name, provider_type, api_base, settings, enabled, created_at, updated_at) VALUES ('external-openai', 'openai', 'https://api.openai.com/v1', '{}', 1, '2026-06-23T21:00:00Z', '2026-06-23T21:00:00Z')")
	if err != nil {
		db2.Close()
		t.Fatalf("failed to insert external profile: %v", err)
	}
	db2.Close()

	success := false
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		profiles, err = srv.ListProfiles(ctx)
		if err != nil {
			t.Fatalf("ListProfiles failed: %v", err)
		}
		if len(profiles) == 1 {
			if profiles[0].Name == "external-openai" {
				success = true
				break
			}
		}
	}

	if !success {
		t.Error("failed to pick up external profile update via hot-reload/watcher")
	}
}

func TestFakeStoreReplaceability(t *testing.T) {
	fakePS := newFakeProfileStore()
	fakeSS := newFakeSecretStore()
	fakeAS := newFakeAgentStore()

	dek, err := secrets.GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	km := secrets.NewKeyManager(dek)
	ar := adapter.NewRegistry()
	ar.Register("openai", func() adapter.Adapter { return adapter.NewStubAdapter() })

	srv := NewService(fakePS, fakeSS, km, ar, fakeAS)
	ctx := context.Background()

	p := &store.Profile{Name: "openai-fake", ProviderType: "openai", APIBase: "https://api.openai.com/v1", Enabled: 1}
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add profile to fake store: %v", err)
	}

	got, err := srv.GetProfile(ctx, "openai-fake")
	if err != nil {
		t.Fatalf("failed to get profile from fake store: %v", err)
	}
	if got.Name != p.Name {
		t.Errorf("profile name mismatch: got %q, want %q", got.Name, p.Name)
	}

	list, err := srv.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("failed to list profiles from fake store: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 profile, got %d", len(list))
	}

	apiKey := "fake-api-key-123"
	if err := srv.SetSecret(ctx, "openai-fake", apiKey); err != nil {
		t.Fatalf("failed to set secret in fake store: %v", err)
	}

	gotKey, err := srv.GetSecret(ctx, "openai-fake")
	if err != nil {
		t.Fatalf("failed to get secret from fake store: %v", err)
	}
	if gotKey != apiKey {
		t.Errorf("secret mismatch: got %q, want %q", gotKey, apiKey)
	}

	if err := srv.RemoveProfile(ctx, "openai-fake"); err != nil {
		t.Fatalf("failed to remove profile from fake store: %v", err)
	}

	list2, err := srv.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("failed to list profiles after removal: %v", err)
	}
	if len(list2) != 0 {
		t.Errorf("expected 0 profiles after removal, got %d", len(list2))
	}
}

func TestBuildWithDisabledProfile(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	p := &store.Profile{Name: "disabled-test", ProviderType: "openai", Enabled: 0}
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add disabled profile: %v", err)
	}

	_, err := srv.BuildWithProfile(ctx, p, "gpt-4")
	if err == nil {
		t.Error("expected error building disabled profile, got nil")
	}
	if err.Error() != "provider disabled-test is disabled" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentCRUDAndResolution(t *testing.T) {
	db, srv := testSetup(t)
	defer db.Close()

	ctx := context.Background()

	// 1. Add provider
	p := &store.Profile{Name: "openai-prov", ProviderType: "openai", Enabled: 1}
	if err := srv.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}
	if err := srv.SetSecret(ctx, "openai-prov", "sk-test-key"); err != nil {
		t.Fatalf("failed to set secret: %v", err)
	}

	// 2. Add agent
	a := &store.Agent{
		Name:            "agent-1",
		Provider:        "openai-prov",
		Model:           "gpt-4o",
		ReasoningEffort: "medium",
		SystemPrompt:    "System instructions",
		Workspace:       "/tmp/agent-ws",
		Tools:           "read_file,write_file",
		MaxIterations:   5,
	}
	if err := srv.AddAgent(ctx, a); err != nil {
		t.Fatalf("failed to add agent: %v", err)
	}

	// 3. Get agent
	gotA, err := srv.GetAgent(ctx, "agent-1")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if gotA.Name != a.Name || gotA.Model != a.Model {
		t.Errorf("got agent fields mismatch")
	}

	// 4. Resolve agent profile
	effProfile, err := srv.ResolveAgentProfile(ctx, "agent-1")
	if err != nil {
		t.Fatalf("failed to resolve agent profile: %v", err)
	}

	// settings should contain reasoning_effort = medium
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(effProfile.Settings), &settings); err != nil {
		t.Fatalf("failed to parse effective settings: %v", err)
	}
	if settings["reasoning_effort"] != "medium" {
		t.Errorf("expected settings.reasoning_effort to be medium, got %v", settings["reasoning_effort"])
	}

	// 5. Build agent ChatModel (should use stub adapter successfully)
	chatModel, err := srv.BuildAgent(ctx, "agent-1")
	if err != nil {
		t.Fatalf("failed to build agent chat model: %v", err)
	}
	if chatModel == nil {
		t.Errorf("built agent chat model is nil")
	}

	// Verify agent metadata update
	gotA.ModelMetadata = `{"context_window":12345,"thinking":false,"input_modalities":["text","image"]}`
	if err := srv.UpdateAgent(ctx, gotA); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	gotA2, err := srv.GetAgent(ctx, "agent-1")
	if err != nil {
		t.Fatalf("failed to get agent after update: %v", err)
	}
	var meta store.ModelMetadata
	if err := json.Unmarshal([]byte(gotA2.ModelMetadata), &meta); err != nil {
		t.Fatalf("failed to unmarshal agent model metadata: %v", err)
	}
	if meta.ContextWindow != 12345 {
		t.Errorf("expected context window in agent metadata to be 12345, got %d", meta.ContextWindow)
	}

	// 6. List agents
	list, err := srv.ListAgents(ctx)
	if err != nil {
		t.Fatalf("failed to list agents: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 agent, got %d", len(list))
	}

	// 7. Remove agent
	if err := srv.RemoveAgent(ctx, "agent-1"); err != nil {
		t.Fatalf("failed to remove agent: %v", err)
	}
	_, err = srv.GetAgent(ctx, "agent-1")
	if err == nil {
		t.Error("expected error getting removed agent, got nil")
	}
}
