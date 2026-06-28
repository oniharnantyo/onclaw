package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestResolveWorkspace_PriorityOrder(t *testing.T) {
	// Create test directories
	flagDir := setupTestDir(t)
	agentDir := setupTestDir(t)
	configDir := setupTestDir(t)
	envDir := setupTestDir(t)
	cwd := setupTestDir(t)

	// Set environment variable
	t.Setenv("ONCLAW_WORKSPACE", envDir)

	// Test 1: Flag overrides everything (flag > agent > env > config > cwd)
	result, err := ResolveWorkspace(flagDir, agentDir, configDir, cwd)
	if err != nil {
		t.Fatalf("ResolveWorkspace failed: %v", err)
	}
	if result != flagDir {
		t.Errorf("expected flag dir %q, got %q", flagDir, result)
	}

	// Test 2: Agent overrides env, config, and cwd
	result, err = ResolveWorkspace("", agentDir, configDir, cwd)
	if err != nil {
		t.Fatalf("ResolveWorkspace failed: %v", err)
	}
	if result != agentDir {
		t.Errorf("expected agent dir %q, got %q", agentDir, result)
	}

	// Test 3: Env overrides config and cwd
	result, err = ResolveWorkspace("", "", configDir, cwd)
	if err != nil {
		t.Fatalf("ResolveWorkspace failed: %v", err)
	}
	if result != envDir {
		t.Errorf("expected env dir %q, got %q", envDir, result)
	}

	// Test 4: Config overrides cwd
	t.Setenv("ONCLAW_WORKSPACE", "")
	result, err = ResolveWorkspace("", "", configDir, cwd)
	if err != nil {
		t.Fatalf("ResolveWorkspace failed: %v", err)
	}
	if result != configDir {
		t.Errorf("expected config dir %q, got %q", configDir, result)
	}

	// Test 5: Cwd as fallback
	result, err = ResolveWorkspace("", "", "", cwd)
	if err != nil {
		t.Fatalf("ResolveWorkspace failed: %v", err)
	}
	if result != cwd {
		t.Errorf("expected cwd %q, got %q", cwd, result)
	}
}

func TestResolveWorkspace_AbsolutePathNormalization(t *testing.T) {
	// Create a test directory structure
	baseDir := setupTestDir(t)
	relDir := filepath.Join(baseDir, "subdir")
	if err := os.Mkdir(relDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Save original working directory
	originalWd, _ := os.Getwd()

	// Change to the base directory for this test
	if err := os.Chdir(baseDir); err != nil {
		t.Fatalf("failed to change to test dir: %v", err)
	}
	defer os.Chdir(originalWd)

	// Test with relative path - should return absolute path
	result, err := ResolveWorkspace("", "", "", "./subdir")
	if err != nil {
		t.Fatalf("ResolveWorkspace failed: %v", err)
	}

	// Result should be an absolute path
	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got %q", result)
	}

	// Get the expected absolute path and compare
	expected, err := filepath.Abs("./subdir")
	if err != nil {
		t.Fatalf("failed to get expected absolute path: %v", err)
	}

	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}

	// Verify the path exists and is a directory
	info, err := os.Stat(result)
	if err != nil {
		t.Fatalf("failed to stat result path: %v", err)
	}

	if !info.IsDir() {
		t.Error("expected result to be a directory")
	}
}

func TestResolveWorkspace_NonExistentPath(t *testing.T) {
	// Test with a non-existent path
	nonExistent := "/tmp/onclaw-test-nonexistent-xyz123"

	_, err := ResolveWorkspace("", "", "", nonExistent)
	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
}

func TestResolveWorkspace_FileInsteadOfDirectory(t *testing.T) {
	// Create a temporary file
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test with a file path instead of directory
	_, err := ResolveWorkspace("", "", "", tmpFile)
	if err == nil {
		t.Error("expected error for file path, got nil")
	}
}
