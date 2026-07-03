package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

func TestDetectPlugin(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-skills-detect-plugin")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Case 1: No plugin.json
	if skill.DetectPlugin(tmpDir) {
		t.Error("expected false when no plugin manifest exists")
	}

	// Case 2: plugin.json exists but is a directory
	badPluginDir := filepath.Join(tmpDir, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(badPluginDir, 0755); err != nil {
		t.Fatalf("failed to create bad plugin directory: %v", err)
	}
	if skill.DetectPlugin(tmpDir) {
		t.Error("expected false when plugin.json is a directory")
	}
	os.RemoveAll(badPluginDir)

	// Case 3: plugin.json exists and is a file
	pluginDir := filepath.Join(tmpDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin directory: %v", err)
	}
	pluginFile := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(pluginFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write plugin file: %v", err)
	}
	if !skill.DetectPlugin(tmpDir) {
		t.Error("expected true when plugin.json exists as a file")
	}
}
