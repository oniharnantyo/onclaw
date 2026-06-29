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
	"github.com/urfave/cli/v3"
)

func TestVersionCommand(t *testing.T) {
	app := New()
	ctx := context.Background()

	buf := &bytes.Buffer{}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := app.Run(ctx, []string{"onclaw", "version"})
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	_, _ = io.Copy(buf, r)
	out := buf.String()
	if out == "" {
		t.Error("expected version output, got empty string")
	}
}

func TestAppBeforeErrorAndFlags(t *testing.T) {
	// 1. Invalid env file to trigger config load error
	tmpFile, err := os.CreateTemp("", "invalid-*.env")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("ONCLAW_LOG_LEVEL=\"debug\n"); err != nil {
		t.Fatalf("failed to write invalid env: %v", err)
	}
	tmpFile.Close()

	app := New()
	ctx := context.Background()
	err = app.Run(ctx, []string{"onclaw", "--config", tmpFile.Name(), "version"})
	if err == nil {
		t.Error("expected error for invalid config file syntax, got nil")
	}

	// 2. Use valid custom config path
	tmpValid, err := os.CreateTemp("", "valid-*.env")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpValid.Name())
	if _, err := tmpValid.WriteString("ONCLAW_LOG_LEVEL=info\nONCLAW_LOG_FORMAT=json\n"); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	tmpValid.Close()

	// 3. Test log-level and log-format flags override
	err = app.Run(ctx, []string{"onclaw", "--config", tmpValid.Name(), "--log-level", "debug", "--log-format", "json", "version"})
	if err != nil {
		t.Errorf("expected success with log flags, got: %v", err)
	}

	// 4. Test invalid logging configuration (invalid log format)
	err = app.Run(ctx, []string{"onclaw", "--log-format", "invalid-format", "version"})
	if err == nil {
		t.Error("expected error for invalid log format, got nil")
	}
}

func TestProviderCommandInvalidArgs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-prov-invalid-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// Initialize DB by listing
	if err := app.Run(ctx, []string{"onclaw", "provider", "list"}); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	// 1. Add command missing name
	err = app.Run(ctx, []string{"onclaw", "provider", "add", "--kind", "openai"})
	if err == nil || !strings.Contains(err.Error(), "provider name is required") {
		t.Errorf("expected provider name is required error, got %v", err)
	}

	// 2. Login command missing name
	err = app.Run(ctx, []string{"onclaw", "provider", "login"})
	if err == nil || !strings.Contains(err.Error(), "provider name is required") {
		t.Errorf("expected provider name is required error, got %v", err)
	}

	// 3. Login command nonexistent provider
	err = app.Run(ctx, []string{"onclaw", "provider", "login", "nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected profile not found error, got %v", err)
	}

	// 4. Use command missing name
	err = app.Run(ctx, []string{"onclaw", "provider", "use"})
	if err == nil || !strings.Contains(err.Error(), "provider name is required") {
		t.Errorf("expected provider name is required error, got %v", err)
	}

	// 5. Use command nonexistent provider
	err = app.Run(ctx, []string{"onclaw", "provider", "use", "nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected profile not found error, got %v", err)
	}

	// 6. Remove command missing name
	err = app.Run(ctx, []string{"onclaw", "provider", "remove"})
	if err == nil || !strings.Contains(err.Error(), "provider name is required") {
		t.Errorf("expected provider name is required error, got %v", err)
	}
}

func TestProviderUseAndRemoveSuccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-prov-use-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// Add profile
	if err := app.Run(ctx, []string{"onclaw", "provider", "add", "my-prov", "--kind", "ollama", "--base-url", "http://localhost:11434"}); err != nil {
		t.Fatalf("failed to add: %v", err)
	}

	// Use profile
	if err := app.Run(ctx, []string{"onclaw", "provider", "use", "my-prov"}); err != nil {
		t.Fatalf("failed to use: %v", err)
	}

	// List
	listOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "provider", "list"})
	})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(listOut, "my-prov") {
		t.Errorf("expected my-prov in list output, got %q", listOut)
	}

	// Remove profile
	if err := app.Run(ctx, []string{"onclaw", "provider", "remove", "my-prov"}); err != nil {
		t.Fatalf("failed to remove: %v", err)
	}
}



func TestRunCommandScenarios(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-run-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// 1. Run with no providers
	err = app.Run(ctx, []string{"onclaw", "run", "hello"})
	if err == nil || !strings.Contains(err.Error(), "no provider profiles found") {
		t.Errorf("expected 'no provider profiles found' error, got: %v", err)
	}

	// 2. Add one provider (Anthropic, which uses the stub adapter)
	if err := app.Run(ctx, []string{"onclaw", "provider", "add", "anthropic-local", "--kind", "anthropic"}); err != nil {
		t.Fatalf("failed to add anthropic: %v", err)
	}

	t.Setenv("ONCLAW_PROVIDER_ANTHROPIC_LOCAL_API_KEY", "dummykey")

	// Now run should succeed (with exactly 1 profile)
	runOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "run", "hello"})
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(runOut, "Stub streaming response") && !strings.Contains(runOut, "Stub response") {
		t.Errorf("unexpected run output: %q", runOut)
	}

	// 3. Add second provider (kind anthropic to keep it stubbed/mocked)
	if err := app.Run(ctx, []string{"onclaw", "provider", "add", "openai-remote", "--kind", "anthropic"}); err != nil {
		t.Fatalf("failed to add openai: %v", err)
	}
	t.Setenv("ONCLAW_PROVIDER_OPENAI_REMOTE_API_KEY", "dummykey2")

	// Running now should error because there are multiple providers and no default set
	err = app.Run(ctx, []string{"onclaw", "run", "hello"})
	if err == nil || !strings.Contains(err.Error(), "multiple providers available") {
		t.Errorf("expected 'multiple providers available' error, got: %v", err)
	}

	// 4. Run specifying --provider flag
	runOut2, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "run", "--provider", "openai-remote", "hello"})
	})
	if err != nil {
		t.Fatalf("run with --provider failed: %v", err)
	}
	if !strings.Contains(runOut2, "Stub streaming response") && !strings.Contains(runOut2, "Stub response") {
		t.Errorf("unexpected run output: %q", runOut2)
	}

	// 5. Use provider to set it as default
	if err := app.Run(ctx, []string{"onclaw", "provider", "use", "openai-remote"}); err != nil {
		t.Fatalf("failed to set default provider: %v", err)
	}

	// Running now should pick default (openai-remote) automatically
	runOut3, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "run", "hello default"})
	})
	if err != nil {
		t.Fatalf("run with default failed: %v", err)
	}
	if !strings.Contains(runOut3, "Stub streaming response") && !strings.Contains(runOut3, "Stub response") {
		t.Errorf("unexpected run output: %q", runOut3)
	}

	// 6. Test build model failure path when API key is missing
	t.Setenv("ONCLAW_PROVIDER_OPENAI_REMOTE_API_KEY", "")
	err = app.Run(ctx, []string{"onclaw", "run", "--provider", "openai-remote", "hello"})
	if err == nil || !strings.Contains(err.Error(), "failed to build model") {
		t.Errorf("expected build model failure, got: %v", err)
	}

	// 7. Verify model fallback: --model flag -> agent.Model -> config.model -> error
	if err := app.Run(ctx, []string{"onclaw", "provider", "use", "anthropic-local"}); err != nil {
		t.Fatalf("failed to set default provider: %v", err)
	}

	dbConn, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open DB: %v", err)
	}
	defer dbConn.Close()
	_, err = dbConn.Exec("UPDATE agents SET model = '' WHERE name = 'master'")
	if err != nil {
		t.Fatalf("failed to clear master agent model: %v", err)
	}

	// Without a model on the agent, and config.model is empty, and no flag, it should fail.
	t.Setenv("ONCLAW_MODEL", "")
	err = app.Run(ctx, []string{"onclaw", "run", "hello"})
	if err == nil || !strings.Contains(err.Error(), "no model specified for agent") {
		t.Errorf("expected run failure with no model, got: %v", err)
	}

	// If we set ONCLAW_MODEL env var (config.model), it should succeed.
	t.Setenv("ONCLAW_MODEL", "config-model")
	t.Setenv("ONCLAW_PROVIDER_ANTHROPIC_LOCAL_API_KEY", "dummykey")
	_, err = captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "run", "hello"})
	})
	if err != nil {
		t.Errorf("expected success falling back to config model, got: %v", err)
	}

	// If we pass --model flag, it should override everything and succeed.
	t.Setenv("ONCLAW_MODEL", "") // clear config model
	_, err = captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "run", "--model", "flag-model", "hello"})
	})
	if err != nil {
		t.Errorf("expected success with --model flag override, got: %v", err)
	}
}

func TestConfigPathCommand(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-config-path-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// Test config path output when config file is empty
	out, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "config", "path"})
	})
	if err != nil {
		t.Fatalf("failed to run config path: %v", err)
	}
	if !strings.Contains(out, "No .env file found") {
		t.Errorf("expected 'No .env file found', got %q", out)
	}

	// Test config path output with custom config file
	tmpValid, err := os.CreateTemp("", "valid-*.env")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpValid.Name())
	if _, err := tmpValid.WriteString("ONCLAW_LOG_LEVEL=info\n"); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	tmpValid.Close()

	out2, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "--config", tmpValid.Name(), "config", "path"})
	})
	if err != nil {
		t.Fatalf("failed to run config path: %v", err)
	}
	if !strings.Contains(out2, "Config file:") {
		t.Errorf("expected 'Config file:', got %q", out2)
	}
}

func TestEnsureNilCfg(t *testing.T) {
	st := &appState{}
	c := &cli.Command{}
	err := st.ensure(c)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if st.cfg == nil {
		t.Error("expected cfg to be populated after ensure")
	}
}

func TestStdinErrorPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-stdin-err-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// Initialize DB and add profile
	if err := app.Run(ctx, []string{"onclaw", "provider", "add", "p1", "--kind", "openai"}); err != nil {
		t.Fatalf("failed to add profile: %v", err)
	}

	// Test config show when API key is not configured (empty value branch)
	showOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "config", "show"})
	})
	if err != nil {
		t.Fatalf("failed to show config: %v", err)
	}
	if !strings.Contains(showOut, `"api_key": ""`) {
		t.Errorf("expected empty api_key string, got: %s", showOut)
	}

	// 1. Mock stdin error for provider login by using a closed pipe
	oldStdin := os.Stdin
	rClosed, wClosed, _ := os.Pipe()
	_ = rClosed.Close()
	_ = wClosed.Close()
	os.Stdin = rClosed
	defer func() { os.Stdin = oldStdin }()

	err = app.Run(ctx, []string{"onclaw", "provider", "login", "p1"})
	if err == nil || !strings.Contains(err.Error(), "read API key") {
		t.Errorf("expected read API key error, got: %v", err)
	}

	// 2. Mock stdin empty API key error via immediate EOF (closed write end)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	w.Close() // Send EOF immediately

	err = app.Run(ctx, []string{"onclaw", "provider", "login", "p1"})
	if err == nil || !strings.Contains(err.Error(), "API key cannot be empty") {
		t.Errorf("expected 'API key cannot be empty' error, got: %v", err)
	}
	_ = r.Close()

	// 6. DB open/migration failure path (using nonexistent directory)
	st := &appState{}
	c := &cli.Command{}
	_ = st.ensure(c)
	st.cfg.DbPath = "/nonexistent-dir/test.db"
	_, db, err := st.getProviderManager(c)
	if err == nil {
		t.Error("expected getProviderManager to fail for nonexistent db directory path")
	}
	if db != nil {
		db.Close()
	}

	// 8. Too wide keyfile permissions error in getProviderManager
	st.cfg.DbPath = dbPath
	_, err = sqlite.ResolveDbPath(dbPath)
	if err != nil {
		t.Fatalf("resolve db path: %v", err)
	}
	keyfilePath := filepath.Join(tmpDir, "master.key")
	_ = os.WriteFile(keyfilePath, make([]byte, 32), 0666) // create keyfile with 0666 permissions
	_ = os.Chmod(keyfilePath, 0666)                       // force permissions to be too wide regardless of umask

	_, db, err = st.getProviderManager(c)
	if err == nil || !strings.Contains(err.Error(), "too wide") {
		t.Errorf("expected permissions too wide error, got: %v", err)
	}
	if db != nil {
		db.Close()
	}

	// 9. Keyfile wrong size error in getProviderManager
	_ = os.Remove(keyfilePath)
	_ = os.WriteFile(keyfilePath, make([]byte, 10), 0600)
	_, db, err = st.getProviderManager(c)
	if err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
		t.Errorf("expected size 32 bytes error, got: %v", err)
	}
	if db != nil {
		db.Close()
	}

	// Remove corrupted keyfile to restore keyfile generation behavior
	_ = os.Remove(keyfilePath)
}

func TestResolveDbPathHomeError(t *testing.T) {
	// Clear environments to cause ResolveDbPath to fail on empty dbPath
	oldHome := os.Getenv("HOME")
	oldXdg := os.Getenv("XDG_DATA_HOME")
	oldDb := os.Getenv("ONCLAW_DB_PATH")

	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("ONCLAW_DB_PATH", "")

	defer func() {
		t.Setenv("HOME", oldHome)
		t.Setenv("XDG_DATA_HOME", oldXdg)
		t.Setenv("ONCLAW_DB_PATH", oldDb)
	}()

	app := New()
	ctx := context.Background()

	err := app.Run(ctx, []string{"onclaw", "provider", "list"})
	if err == nil || !strings.Contains(err.Error(), "resolve db path") {
		t.Errorf("expected resolve db path error, got: %v", err)
	}
}

func TestPidFileEdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-pid-edge-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	pidPath := filepath.Join(tmpDir, "onclaw.pid")

	// 1. Invalid PID content
	if err := os.WriteFile(pidPath, []byte("invalid-pid\n"), 0644); err != nil {
		t.Fatalf("failed to write pid: %v", err)
	}
	if err := signalRunningProcess(dbPath); err != nil {
		t.Errorf("expected no error for invalid pid, got: %v", err)
	}

	// 2. Non-existent PID
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("failed to write pid: %v", err)
	}
	if err := signalRunningProcess(dbPath); err != nil {
		t.Errorf("expected no error for non-existent pid, got: %v", err)
	}

	// 3. PID path is a directory (should return read error)
	pidDir := filepath.Join(tmpDir, "onclaw.pid")
	_ = os.Remove(pidPath)
	if err := os.Mkdir(pidDir, 0755); err != nil {
		t.Fatalf("failed to create pid dir: %v", err)
	}
	if err := signalRunningProcess(dbPath); err == nil {
		t.Error("expected error when pid file is a directory")
	}
}

func TestProviderManagerInitWriteError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-pm-init-err-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "dbdir")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	dbPath := filepath.Join(subDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// Initialize DB (any command)
	if err := app.Run(ctx, []string{"onclaw", "provider", "list"}); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	// Delete wrapped_dek row
	resolvedPath, _ := sqlite.ResolveDbPath(dbPath)
	dbConn, err := sqlite.Open(resolvedPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	_, _ = dbConn.Exec("DELETE FROM preferences WHERE key = 'wrapped_dek'")
	dbConn.Close()

	// Make directory read-only
	if err := os.Chmod(subDir, 0500); err != nil {
		t.Fatalf("failed to chmod: %v", err)
	}
	defer func() {
		_ = os.Chmod(subDir, 0700)
	}()

	st := &appState{}
	c := &cli.Command{}
	_ = st.ensure(c)

	mgr, db, err := st.getProviderManager(c)
	if err == nil {
		t.Error("expected getProviderManager to fail to write KEK keyfile")
	}
	if db != nil {
		db.Close()
	}
	if mgr != nil {
		// satisfy compiler
	}
}

func captureLocalStdout(fn func() error) (string, error) {
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

func TestWritePIDFileError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-pid-err-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	subDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(subDir, 0500); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	defer func() {
		_ = os.Chmod(subDir, 0700)
	}()

	_, err = writePIDFile(filepath.Join(subDir, "test.db"))
	if err == nil {
		t.Error("expected writePIDFile to fail on read-only directory, got nil")
	}
}

func TestAgentCLI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-test-agent-cli-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("ONCLAW_DB_PATH", dbPath)

	app := New()
	ctx := context.Background()

	// 1. Setup a provider profile first (so we can reference it)
	if err := app.Run(ctx, []string{"onclaw", "provider", "add", "prov-1", "--kind", "openai"}); err != nil {
		t.Fatalf("failed to add provider: %v", err)
	}

	err = app.Run(ctx, []string{
		"onclaw", "agent", "add", "agent-1",
		"--provider", "prov-1",
		"--model", "gpt-4o",
		"--system-prompt", "You are agent 1.",
	})
	if err != nil {
		t.Fatalf("failed to add agent: %v", err)
	}

	// Verify agent workspace folder was created
	home, _ := os.UserHomeDir()
	expectedWS := filepath.Join(home, ".onclaw", "workspace", "agent-1")
	if _, err := os.Stat(expectedWS); os.IsNotExist(err) {
		t.Errorf("expected agent workspace directory to be created, not found: %s", expectedWS)
	}
	// clean up the workspace folder we created in home dir during test
	defer os.RemoveAll(filepath.Join(home, ".onclaw", "workspace", "agent-1"))

	// 3. List agents
	listOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "agent", "list"})
	})
	if err != nil {
		t.Fatalf("failed to list agents: %v", err)
	}
	if !strings.Contains(listOut, "agent-1") {
		t.Errorf("list output does not contain agent-1: %s", listOut)
	}

	// 4. Show agent details
	showOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "agent", "show", "agent-1"})
	})
	if err != nil {
		t.Fatalf("failed to show agent: %v", err)
	}
	if !strings.Contains(showOut, "Name:             agent-1") || !strings.Contains(showOut, "Model Override:   gpt-4o") {
		t.Errorf("show output missing expected details: %s", showOut)
	}

	// 5. Use agent (set default)
	useOut, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "agent", "use", "agent-1"})
	})
	if err != nil {
		t.Fatalf("failed to set default agent: %v", err)
	}
	if !strings.Contains(useOut, "Default agent set to \"agent-1\"") {
		t.Errorf("unexpected use output: %s", useOut)
	}

	// 6. Remove agent
	if err := app.Run(ctx, []string{"onclaw", "agent", "remove", "agent-1"}); err != nil {
		t.Fatalf("failed to remove agent: %v", err)
	}

	// Verify agent is no longer in list
	listOut2, err := captureLocalStdout(func() error {
		return app.Run(ctx, []string{"onclaw", "agent", "list"})
	})
	if err != nil {
		t.Fatalf("failed to list agents after removal: %v", err)
	}
	if strings.Contains(listOut2, "agent-1") {
		t.Errorf("agent-1 still present in list after removal: %s", listOut2)
	}
}
