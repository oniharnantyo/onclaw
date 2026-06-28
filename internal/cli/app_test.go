package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/cli"
)

func TestProviderLifecycleAndRedaction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := cli.New()
	ctx := context.Background()

	// 1. Add provider
	args := []string{"onclaw", "provider", "add", "my-openai", "--kind", "openai", "--model", "gpt-4"}
	if err := app.Run(ctx, args); err != nil {
		t.Fatalf("failed to add provider: %v", err)
	}

	// 2. Login to provider (set API key)
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe error: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		defer w.Close()
		_, _ = io.WriteString(w, "sk-proj-testkey123\n")
	}()

	loginArgs := []string{"onclaw", "provider", "login", "my-openai"}
	if err := app.Run(ctx, loginArgs); err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	// 3. List providers and capture stdout to verify redaction/status
	listOut, err := captureStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "provider", "list"})
	})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}

	if !strings.Contains(listOut, "api_key: [configured]") {
		t.Errorf("expected api_key to show configured, got:\n%s", listOut)
	}
	if strings.Contains(listOut, "sk-proj-testkey123") {
		t.Errorf("found plaintext secret key in provider list output:\n%s", listOut)
	}

	// 4. Verify config show output has providers section and is redacted
	configOut, err := captureStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "config", "show"})
	})
	if err != nil {
		t.Fatalf("config show error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(configOut), &parsed); err != nil {
		t.Fatalf("failed to parse config show json: %v\nOutput: %s", err, configOut)
	}

	provs, ok := parsed["providers"].([]interface{})
	if !ok {
		t.Fatalf("missing providers key or not list in config show output:\n%s", configOut)
	}

	if len(provs) != 1 {
		t.Errorf("expected exactly 1 provider config, got %d", len(provs))
	}

	pMap, ok := provs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid provider structure in json")
	}

	if pMap["name"] != "my-openai" {
		t.Errorf("expected provider name my-openai, got %v", pMap["name"])
	}
	if pMap["api_key"] != "***" {
		t.Errorf("expected api_key to be redacted to ***, got %q", pMap["api_key"])
	}
}

func TestConfigShowRedactsLangfuseSecretKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)
	t.Setenv("ONCLAW_LANGFUSE_SECRET_KEY", "sk-langfuse-super-secret-key")
	t.Setenv("ONCLAW_LANGFUSE_HOST", "http://localhost:8080")
	t.Setenv("ONCLAW_LANGFUSE_PUBLIC_KEY", "pk-lf-test")

	app := cli.New()
	ctx := context.Background()

	configOut, err := captureStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "config", "show"})
	})
	if err != nil {
		t.Fatalf("config show error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(configOut), &parsed); err != nil {
		t.Fatalf("failed to parse config show json: %v\nOutput: %s", err, configOut)
	}

	lf, ok := parsed["Langfuse"].(map[string]interface{})
	if !ok {
		// Try lowercase if struct tags/unmarshal changed
		lf, ok = parsed["langfuse"].(map[string]interface{})
	}
	if !ok {
		t.Fatalf("missing Langfuse section in config show output:\n%s", configOut)
	}

	secKey, ok := lf["SecretKey"].(string)
	if !ok {
		secKey, ok = lf["secret_key"].(string)
	}
	if !ok {
		t.Fatalf("missing SecretKey in Langfuse config section:\n%s", configOut)
	}

	if secKey != "***" {
		t.Errorf("expected SecretKey to be redacted to ***, got %q", secKey)
	}

	if strings.Contains(configOut, "sk-langfuse-super-secret-key") {
		t.Errorf("found plaintext secret key in config show output:\n%s", configOut)
	}
}

func captureStdout(fn func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	errVal := fn()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), errVal
}
