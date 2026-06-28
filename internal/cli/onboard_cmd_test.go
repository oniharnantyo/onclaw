package cli

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func setupTestDB(t *testing.T) (*appState, *llm.Service, *sql.DB, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "onclaw-onboard-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	dbPath := filepath.Join(tmpDir, "test.db")
	st := &appState{
		cfg: &config.Config{
			DbPath: dbPath,
		},
	}

	mgr, db, err := st.getProviderManager(nil)
	if err != nil {
		t.Fatalf("failed to get provider manager: %v", err)
	}
	return st, mgr, db, dbPath
}

func TestRunProviderSetup_KeyfulHappyPath(t *testing.T) {
	_, mgr, db, dbPath := setupTestDB(t)
	defer db.Close()

	// Inputs:
	// 1 (anthropic kind)
	// (accept default name "anthropic") -> empty line
	// claude-3-opus (model)
	// sk-key123 (api key)
	// n (do not add another)
	input := "1\n\nclaude-3-opus\nsk-key123\nn\n"
	in := bytes.NewBufferString(input)
	var out bytes.Buffer

	ctx := context.Background()
	err := runProviderSetup(ctx, mgr, db, dbPath, in, &out)
	if err != nil {
		t.Fatalf("unexpected error running provider setup: %v", err)
	}

	// Verify profile is stored correctly
	p, err := mgr.GetProfile(ctx, "anthropic")
	if err != nil {
		t.Fatalf("failed to retrieve profile: %v", err)
	}
	if p.ProviderType != "anthropic" || p.Model != "claude-3-opus" {
		t.Errorf("unexpected profile values: %+v", p)
	}

	// Verify API key is stored and matches
	key, err := mgr.ResolveSecret(ctx, "anthropic")
	if err != nil {
		t.Fatalf("failed to resolve secret: %v", err)
	}
	if key != "sk-key123" {
		t.Errorf("expected secret to be 'sk-key123', got %q", key)
	}

	// Verify default provider is silently set
	var defaultProvider string
	err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&defaultProvider)
	if err != nil {
		t.Fatalf("failed to query default provider preference: %v", err)
	}
	if defaultProvider != "anthropic" {
		t.Errorf("expected default provider to be 'anthropic', got %q", defaultProvider)
	}
}

func TestRunProviderSetup_KeylessOllama(t *testing.T) {
	_, mgr, db, dbPath := setupTestDB(t)
	defer db.Close()

	// Inputs:
	// 4 (ollama kind)
	// my-ollama (custom name)
	// llama3 (model)
	// (accept default base URL http://localhost:11434/v1) -> empty line
	// n (do not add another)
	input := "4\nmy-ollama\nllama3\n\nn\n"
	in := bytes.NewBufferString(input)
	var out bytes.Buffer

	ctx := context.Background()
	err := runProviderSetup(ctx, mgr, db, dbPath, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify profile
	p, err := mgr.GetProfile(ctx, "my-ollama")
	if err != nil {
		t.Fatalf("failed to retrieve profile: %v", err)
	}
	if p.ProviderType != "ollama" || p.APIBase != "http://localhost:11434/v1" || p.Model != "llama3" {
		t.Errorf("unexpected profile values: %+v", p)
	}

	// Verify no secret stored
	sec, err := mgr.GetSecret(ctx, "my-ollama")
	if err != nil {
		t.Fatalf("failed to get secret: %v", err)
	}
	if sec != "" {
		t.Errorf("expected no secret for keyless provider, got %q", sec)
	}
}

func TestRunProviderSetup_OpenAICompatible(t *testing.T) {
	_, mgr, db, dbPath := setupTestDB(t)
	defer db.Close()

	// Inputs:
	// 3 (openai-compatible kind)
	// my-compat (custom name)
	// my-model (model)
	// http://custom-url/v1 (base URL)
	// my-secret-key (api key)
	// n (do not add another)
	input := "3\nmy-compat\nmy-model\nhttp://custom-url/v1\nmy-secret-key\nn\n"
	in := bytes.NewBufferString(input)
	var out bytes.Buffer

	ctx := context.Background()
	err := runProviderSetup(ctx, mgr, db, dbPath, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, err := mgr.GetProfile(ctx, "my-compat")
	if err != nil {
		t.Fatalf("failed to retrieve profile: %v", err)
	}
	if p.APIBase != "http://custom-url/v1" || p.Model != "my-model" {
		t.Errorf("unexpected profile: %+v", p)
	}

	key, err := mgr.ResolveSecret(ctx, "my-compat")
	if err != nil {
		t.Fatalf("failed to resolve secret: %v", err)
	}
	if key != "my-secret-key" {
		t.Errorf("expected 'my-secret-key', got %q", key)
	}
}

func TestRunProviderSetup_NameCollisionAndEmptyModel(t *testing.T) {
	_, mgr, db, dbPath := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()
	// Pre-create anthropic profile
	err := mgr.AddProfile(ctx, &store.Profile{
		Name:         "anthropic",
		ProviderType: "anthropic",
		Model:        "claude-3-haiku",
		Enabled:      1,
	})
	if err != nil {
		t.Fatalf("failed to add initial profile: %v", err)
	}

	// Inputs:
	// 1 (anthropic)
	// anthropic (collision -> re-prompt)
	// new-anthropic (success)
	// (empty model -> re-prompt)
	// claude-3-opus (success model)
	// mykey
	// n
	// 2 (select default provider index 2: new-anthropic)
	input := "1\nanthropic\nnew-anthropic\n\nclaude-3-opus\nmykey\nn\n2\n"
	in := bytes.NewBufferString(input)
	var out bytes.Buffer

	err = runProviderSetup(ctx, mgr, db, dbPath, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify profile new-anthropic is stored
	p, err := mgr.GetProfile(ctx, "new-anthropic")
	if err != nil {
		t.Fatalf("failed to retrieve new profile: %v", err)
	}
	if p.Model != "claude-3-opus" {
		t.Errorf("expected claude-3-opus, got %q", p.Model)
	}
}

func TestRunProviderSetup_MultipleProvidersDefaultPrompt(t *testing.T) {
	_, mgr, db, dbPath := setupTestDB(t)
	defer db.Close()

	// Add anthropic then ollama, then prompt to select default provider (options: anthropic, my-ollama)
	// Inputs:
	// 1 (anthropic)
	// (accept "anthropic")
	// model1
	// key1
	// y (add another)
	// 4 (ollama)
	// my-ollama
	// model2
	// (accept default base URL)
	// n (do not add another)
	// 2 (select default provider index 2: my-ollama)
	input := "1\n\nmodel1\nkey1\ny\n4\nmy-ollama\nmodel2\n\nn\n2\n"
	in := bytes.NewBufferString(input)
	var out bytes.Buffer

	ctx := context.Background()
	err := runProviderSetup(ctx, mgr, db, dbPath, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify default_provider preference in DB is my-ollama
	var defaultProvider string
	err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&defaultProvider)
	if err != nil {
		t.Fatalf("failed to query default provider: %v", err)
	}
	if defaultProvider != "my-ollama" {
		t.Errorf("expected default provider to be my-ollama, got %q", defaultProvider)
	}
}

func TestRunProviderSetup_EOFInterruptionPreservesCompleted(t *testing.T) {
	_, mgr, db, dbPath := setupTestDB(t)
	defer db.Close()

	// Inputs:
	// 1 (anthropic)
	// (accept "anthropic")
	// model1
	// key1
	// y (add another)
	// 4 (ollama)
	// (EOF begins here)
	input := "1\n\nmodel1\nkey1\ny\n4\n"
	in := bytes.NewBufferString(input)
	var out bytes.Buffer

	ctx := context.Background()
	err := runProviderSetup(ctx, mgr, db, dbPath, in, &out)
	// Since EOF happened during the flow, it returns or breaks. Either way, the first should be saved.
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error format: %v", err)
	}

	// Verify first provider remains saved
	p, err := mgr.GetProfile(ctx, "anthropic")
	if err != nil {
		t.Fatalf("first provider was not saved after EOF: %v", err)
	}
	if p.Model != "model1" {
		t.Errorf("expected first provider model to be 'model1', got %q", p.Model)
	}

	// Second provider ("ollama") should not be created
	_, err = mgr.GetProfile(ctx, "ollama")
	if err == nil {
		t.Errorf("second provider should not have been created")
	}
}

func TestProviderSetupCommand_Integration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-setup-cmd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// Capture stdout
	oldStdout := os.Stdout
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = wOut
	defer func() {
		os.Stdout = oldStdout
	}()

	// Stdin override
	oldStdin := os.Stdin
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}
	os.Stdin = rIn
	defer func() {
		os.Stdin = oldStdin
	}()

	// Inputs:
	// 1 (anthropic)
	// (default profile name "anthropic") -> empty line
	// claude-3-opus (model)
	// sk-key123 (API key)
	// n (do not add another)
	go func() {
		defer wIn.Close()
		_, _ = io.WriteString(wIn, "1\n\nclaude-3-opus\nsk-key123\nn\n")
	}()

	err = app.Run(ctx, []string{"onclaw", "provider", "setup"})
	wOut.Close()
	if err != nil {
		t.Fatalf("failed to run onclaw provider setup: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	outStr := buf.String()

	if !strings.Contains(outStr, "Starting provider setup...") {
		t.Errorf("expected setup intro message, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Provider profile \"anthropic\" configured successfully.") {
		t.Errorf("expected provider configuration success message, got:\n%s", outStr)
	}
}
