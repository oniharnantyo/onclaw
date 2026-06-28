package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func TestInitCommand_Integration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-init-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("HOME", tmpDir)
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

	// Feed inputs:
	// 1 (anthropic)
	// (default profile name "anthropic") -> empty line
	// claude-3-opus (model)
	// sk-key123 (API key)
	// n (do not add another provider)
	go func() {
		defer wIn.Close()
		_, _ = io.WriteString(wIn, "1\n\nclaude-3-opus\nsk-key123\nn\n")
	}()

	err = app.Run(ctx, []string{"onclaw", "init"})
	wOut.Close()
	if err != nil {
		t.Fatalf("failed to run onclaw init: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	outStr := buf.String()

	// Verify output has welcome banner, steps, and outro
	if !strings.Contains(outStr, "Welcome to Onclaw Onboarding!") {
		t.Errorf("expected welcome banner in stdout, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Onboarding Completed Successfully!") {
		t.Errorf("expected outro banner in stdout, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Provider profile \"anthropic\" configured successfully.") {
		t.Errorf("expected provider configuration success message, got:\n%s", outStr)
	}

	// Verify BOOTSTRAP.md is present (defer onboarding)
	bootstrapPath := filepath.Join(tmpDir, ".onclaw", "workspace", "master", "BOOTSTRAP.md")
	if _, err := os.Stat(bootstrapPath); os.IsNotExist(err) {
		t.Error("expected BOOTSTRAP.md to be present in master workspace, but it does not exist")
	}

	// Verify master agent provider and model are set in DB
	resolvedPath, _ := sqlite.ResolveDbPath(dbPath)
	dbConn, err := sqlite.Open(resolvedPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer dbConn.Close()

	var provider, model string
	err = dbConn.QueryRow("SELECT provider, model FROM agents WHERE name = 'master'").Scan(&provider, &model)
	if err != nil {
		t.Fatalf("failed to query master agent: %v", err)
	}
	if provider != "anthropic" {
		t.Errorf("expected master agent provider to be 'anthropic', got %q", provider)
	}
	if model != "claude-3-opus" {
		t.Errorf("expected master agent model to be 'claude-3-opus', got %q", model)
	}

	// Idempotent re-run check (re-run allows adding another but does not disturb existing)
	rIn2, wIn2, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create second stdin pipe: %v", err)
	}
	os.Stdin = rIn2

	rOut2, wOut2, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create second stdout pipe: %v", err)
	}
	os.Stdout = wOut2

	go func() {
		defer wIn2.Close()
		// Since we already have "anthropic", adding a second one "ollama" triggers
		// a selection prompt in Agent Setup.
		// "ollama" is sorted alphabetically after "anthropic", so:
		// 1) anthropic
		// 2) ollama
		// We select 2 (ollama) for the agent.
		_, _ = io.WriteString(wIn2, "4\n\nllama3\n\nn\n2\n")
	}()

	err = app.Run(ctx, []string{"onclaw", "init"})
	wOut2.Close()
	if err != nil {
		t.Fatalf("failed to run onclaw init second time: %v", err)
	}

	var buf2 bytes.Buffer
	_, _ = io.Copy(&buf2, rOut2)
	outStr2 := buf2.String()

	if !strings.Contains(outStr2, "Provider profile \"ollama\" configured successfully.") {
		t.Errorf("expected second provider to be configured, got:\n%s", outStr2)
	}

	// Verify master agent provider and model are updated to ollama in DB
	err = dbConn.QueryRow("SELECT provider, model FROM agents WHERE name = 'master'").Scan(&provider, &model)
	if err != nil {
		t.Fatalf("failed to query master agent after second run: %v", err)
	}
	if provider != "ollama" {
		t.Errorf("expected master agent provider to be 'ollama' after second run, got %q", provider)
	}
	if model != "llama3" {
		t.Errorf("expected master agent model to be 'llama3' after second run, got %q", model)
	}
}

func TestAgentSetup_Seeding(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-seeding-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set HOME environment variable to isolate config
	t.Setenv("HOME", tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// 1. Add provider first so Agent Setup has a profile to bind
	if err := app.Run(ctx, []string{"onclaw", "provider", "add", "my-prov", "--kind", "openai", "--model", "gpt-4"}); err != nil {
		t.Fatalf("failed to add provider: %v", err)
	}

	// 2. Run Agent Setup via init
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe error: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		defer w.Close()
		// Since we already added 'my-prov', Provider Setup step in init will prompt to add another:
		// We add "my-openai"
		// Since there are now multiple profiles ("my-prov" and "my-openai"), Agent Setup will prompt choice.
		// Let's choose 2 (which is "my-prov" or "my-openai" sorted alphabetically)
		_, _ = io.WriteString(w, "2\nmy-openai\ngpt-4\nsk-key123\nn\n2\n2\n")
	}()

	if err := app.Run(ctx, []string{"onclaw", "init"}); err != nil {
		t.Fatalf("failed to run init: %v", err)
	}

	// Verify persona files exist under workspace directory, NOT flat under tmpDir/.onclaw/
	workspaceDir := filepath.Join(tmpDir, ".onclaw", "workspace", "master")
	personaFiles := []string{
		"IDENTITY.md",
		"SOUL.md",
		"CAPABILITIES.md",
		"USER.md",
		"MEMORY.md",
		"AGENTS.md",
		"BOOTSTRAP.md",
	}

	for _, filename := range personaFiles {
		path := filepath.Join(workspaceDir, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("persona file %s was not created in workspace at %s", filename, path)
		}
	}

	// Verify global USER.md was created under tmpDir/.onclaw/USER.md
	globalUser := filepath.Join(tmpDir, ".onclaw", "USER.md")
	if _, err := os.Stat(globalUser); os.IsNotExist(err) {
		t.Errorf("global USER.md was not created at %s", globalUser)
	}

	// Verify that flat persona files like tmpDir/.onclaw/IDENTITY.md were NOT created
	flatIdentity := filepath.Join(tmpDir, ".onclaw", "IDENTITY.md")
	if _, err := os.Stat(flatIdentity); !os.IsNotExist(err) {
		t.Errorf("flat IDENTITY.md should not be created in config root: %s", flatIdentity)
	}
}
