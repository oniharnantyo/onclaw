package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ONCLAW_LOG_LEVEL", "")
	t.Setenv("ONCLAW_CONCURRENCY", "")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want text", cfg.LogFormat)
	}
	if cfg.Concurrency != 1 {
		t.Errorf("Concurrency = %d, want 1", cfg.Concurrency)
	}
	if cfg.MaxContextTokens != 64000 {
		t.Errorf("MaxContextTokens = %d, want 64000", cfg.MaxContextTokens)
	}
	if cfg.LoadedFrom != "" {
		t.Errorf("LoadedFrom = %q, want empty", cfg.LoadedFrom)
	}
}

func TestLoadDefaultsShellConfig(t *testing.T) {
	t.Setenv("ONCLAW_TOOLS_SHELL_POLICY", "")
	t.Setenv("ONCLAW_TOOLS_SHELL_ALLOWLIST", "")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tools.Shell.Policy != "denylist" {
		t.Errorf("Tools.Shell.Policy = %q, want denylist", cfg.Tools.Shell.Policy)
	}
	expected := []string{"ls", "cat", "git", "go", "make", "npm", "node", "python3", "python", "docker"}
	if len(cfg.Tools.Shell.Allowlist) != len(expected) {
		t.Fatalf("Tools.Shell.Allowlist len = %d, want %d (%v)", len(cfg.Tools.Shell.Allowlist), len(expected), cfg.Tools.Shell.Allowlist)
	}
	for i, v := range expected {
		if cfg.Tools.Shell.Allowlist[i] != v {
			t.Errorf("Tools.Shell.Allowlist[%d] = %q, want %q", i, cfg.Tools.Shell.Allowlist[i], v)
		}
	}
	if len(cfg.Tools.Shell.Denylist) == 0 {
		t.Errorf("Tools.Shell.Denylist len = 0, want non-empty default floor")
	}
}

func TestLoadDenylistEnvOverride(t *testing.T) {
	t.Setenv("ONCLAW_TOOLS_SHELL_DENYLIST", "rm -rf /,curl|sh,/dev/tcp/")
	t.Setenv("ONCLAW_TOOLS_SHELL_POLICY", "")
	t.Setenv("ONCLAW_TOOLS_SHELL_ALLOWLIST", "")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	expected := []string{"rm -rf /", "curl|sh", "/dev/tcp/"}
	if len(cfg.Tools.Shell.Denylist) != len(expected) {
		t.Fatalf("Denylist len = %d, want %d (%v)", len(cfg.Tools.Shell.Denylist), len(expected), cfg.Tools.Shell.Denylist)
	}
	for i, v := range expected {
		if cfg.Tools.Shell.Denylist[i] != v {
			t.Errorf("Denylist[%d] = %q, want %q", i, cfg.Tools.Shell.Denylist[i], v)
		}
	}
}

func TestLoadEnvOverridesDefault(t *testing.T) {
	t.Setenv("ONCLAW_LOG_LEVEL", "debug")
	t.Setenv("ONCLAW_CONCURRENCY", "4")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug (env)", cfg.LogLevel)
	}
	if cfg.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4 (env)", cfg.Concurrency)
	}
}

func TestLoadFileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("ONCLAW_LOG_LEVEL=warn\nONCLAW_LOG_FORMAT=json\nONCLAW_CONCURRENCY=2\nONCLAW_MAX_CONTEXT_TOKENS=4096\nONCLAW_MODEL=gpt-test\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Ensure host env cannot interfere with the file-override assertions.
	t.Setenv("ONCLAW_LOG_LEVEL", "")
	t.Setenv("ONCLAW_LOG_FORMAT", "")
	t.Setenv("ONCLAW_CONCURRENCY", "")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "warn" || cfg.LogFormat != "json" || cfg.Concurrency != 2 ||
		cfg.MaxContextTokens != 4096 || cfg.Model != "gpt-test" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.LoadedFrom != path {
		t.Errorf("LoadedFrom = %q, want %q", cfg.LoadedFrom, path)
	}
}

func TestLoadEnvFileWithComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("# This is a comment\nONCLAW_LOG_LEVEL=debug\n\nONCLAW_CONCURRENCY=2\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	t.Setenv("ONCLAW_LOG_LEVEL", "")
	t.Setenv("ONCLAW_CONCURRENCY", "")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.Concurrency != 2 {
		t.Errorf("Concurrency = %d, want 2", cfg.Concurrency)
	}
}

func TestLoadEnvFileWithQuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("ONCLAW_WORKSPACE=\"/home/user/my project\"\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Workspace != "/home/user/my project" {
		t.Errorf("Workspace = %q, want /home/user/my project", cfg.Workspace)
	}
}

func TestEnvFileCommaSeparatedArrays(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("ONCLAW_TOOLS_SHELL_ALLOWLIST=ls,cat,git\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	expected := []string{"ls", "cat", "git"}
	if len(cfg.Tools.Shell.Allowlist) != len(expected) {
		t.Errorf("Allowlist len = %d, want %d", len(cfg.Tools.Shell.Allowlist), len(expected))
	} else {
		for i, v := range expected {
			if cfg.Tools.Shell.Allowlist[i] != v {
				t.Errorf("Allowlist[%d] = %q, want %q", i, cfg.Tools.Shell.Allowlist[i], v)
			}
		}
	}
}

func TestEnvFileTypeConversion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("ONCLAW_CONCURRENCY=4\nONCLAW_LANGFUSE_MASK=false\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", cfg.Concurrency)
	}
	if cfg.Langfuse.Mask != false {
		t.Errorf("Langfuse.Mask = %t, want false", cfg.Langfuse.Mask)
	}
}

func TestSearchPathsAlwaysIncludesCwdAndEtc(t *testing.T) {
	paths := config.SearchPaths()
	if len(paths) < 2 {
		t.Fatalf("expected at least 2 search paths, got %v", paths)
	}
	if paths[0] != "." {
		t.Errorf("first search path = %q, want .", paths[0])
	}
	foundEtc := false
	for _, p := range paths {
		if p == "/etc/onclaw" {
			foundEtc = true
		}
	}
	if !foundEtc {
		t.Errorf("/etc/onclaw missing from search paths: %v", paths)
	}
}

func TestLoadLangfuseConfig(t *testing.T) {
	t.Setenv("ONCLAW_LANGFUSE_HOST", "http://localhost:8080")
	t.Setenv("ONCLAW_LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("ONCLAW_LANGFUSE_SECRET_KEY", "sk-test")
	t.Setenv("ONCLAW_LANGFUSE_SESSION_ID", "sess-test")
	t.Setenv("ONCLAW_LANGFUSE_RELEASE", "v1.0.0")
	t.Setenv("ONCLAW_LANGFUSE_MASK", "false")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Langfuse.Host != "http://localhost:8080" {
		t.Errorf("Langfuse.Host = %q, want http://localhost:8080", cfg.Langfuse.Host)
	}
	if cfg.Langfuse.PublicKey != "pk-test" {
		t.Errorf("Langfuse.PublicKey = %q, want pk-test", cfg.Langfuse.PublicKey)
	}
	if cfg.Langfuse.SecretKey != "sk-test" {
		t.Errorf("Langfuse.SecretKey = %q, want sk-test", cfg.Langfuse.SecretKey)
	}
	if cfg.Langfuse.SessionID != "sess-test" {
		t.Errorf("Langfuse.SessionID = %q, want sess-test", cfg.Langfuse.SessionID)
	}
	if cfg.Langfuse.Release != "v1.0.0" {
		t.Errorf("Langfuse.Release = %q, want v1.0.0", cfg.Langfuse.Release)
	}
	if cfg.Langfuse.Mask != false {
		t.Errorf("Langfuse.Mask = %t, want false", cfg.Langfuse.Mask)
	}
}

func TestEnvFileInvalidTypeConversion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("ONCLAW_CONCURRENCY=invalid\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed on invalid type: %v", err)
	}
	if cfg.Concurrency != 1 {
		t.Errorf("Concurrency = %d, want 1 (default)", cfg.Concurrency)
	}
}

func TestEnvVarInvalidTypeConversion(t *testing.T) {
	t.Setenv("ONCLAW_CONCURRENCY", "invalid-env")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load failed on invalid env: %v", err)
	}
	if cfg.Concurrency != 1 {
		t.Errorf("Concurrency = %d, want 1 (default)", cfg.Concurrency)
	}
}
