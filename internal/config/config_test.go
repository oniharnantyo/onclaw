package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("ONCLAW_LOG_LEVEL", "")
	t.Setenv("ONCLAW_CONCURRENCY", "")
	cfg, err := Load("")
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
	if cfg.MaxContextTokens != 8192 {
		t.Errorf("MaxContextTokens = %d, want 8192", cfg.MaxContextTokens)
	}
	if cfg.LoadedFrom != "" {
		t.Errorf("LoadedFrom = %q, want empty", cfg.LoadedFrom)
	}
}

func TestLoadEnvOverridesDefault(t *testing.T) {
	t.Setenv("ONCLAW_LOG_LEVEL", "debug")
	t.Setenv("ONCLAW_CONCURRENCY", "4")
	cfg, err := Load("")
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
	path := filepath.Join(dir, "config.yaml")
	content := []byte("log_level: warn\nlog_format: json\nconcurrency: 2\nmax_context_tokens: 4096\nmodel: gpt-test\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Ensure host env cannot interfere with the file-override assertions.
	t.Setenv("ONCLAW_LOG_LEVEL", "")
	t.Setenv("ONCLAW_LOG_FORMAT", "")
	t.Setenv("ONCLAW_CONCURRENCY", "")

	cfg, err := Load(path)
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

func TestSearchPathsAlwaysIncludesCwdAndEtc(t *testing.T) {
	paths := SearchPaths()
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
