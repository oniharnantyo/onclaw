package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent"
)

func TestLoadPersonaContext_AssemblyOrder(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Create persona files with unique content
	globalUserContent := "GLOBAL USER content"
	bootstrapContent := "BOOTSTRAP content"
	identityContent := "IDENTITY content"
	soulContent := "SOUL content"
	capabilitiesContent := "CAPABILITIES content"
	toolsContent := "TOOLS content"
	userContent := "USER content"
	agentsContent := "AGENTS content"

	_ = os.WriteFile(filepath.Join(tmpConfig, "USER.md"), []byte(globalUserContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "BOOTSTRAP.md"), []byte(bootstrapContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "IDENTITY.md"), []byte(identityContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "SOUL.md"), []byte(soulContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "CAPABILITIES.md"), []byte(capabilitiesContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "TOOLS.md"), []byte(toolsContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "USER.md"), []byte(userContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "AGENTS.md"), []byte(agentsContent), 0644)

	// Load persona context
	ctx := context.Background()
	result, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)
	if err != nil {
		t.Fatalf("LoadPersonaContext failed: %v", err)
	}

	// Verify the order: AGENTS, SOUL, IDENTITY, CAPABILITIES, TOOLS, global USER, per-agent USER, BOOTSTRAP
	expected := agentsContent + "\n" +
		soulContent + "\n" +
		identityContent + "\n" +
		capabilitiesContent + "\n" +
		toolsContent + "\n" +
		globalUserContent + "\n" +
		userContent + "\n" +
		bootstrapContent

	if result != expected {
		t.Errorf("Unexpected content order.\nGot: %q\nWant: %q", result, expected)
	}
}

func TestLoadPersonaContext_MissingFilesSkipped(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Create only IDENTITY.md and AGENTS.md
	identityContent := "IDENTITY content"
	agentsContent := "AGENTS content"

	_ = os.WriteFile(filepath.Join(tmpWorkspace, "IDENTITY.md"), []byte(identityContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "AGENTS.md"), []byte(agentsContent), 0644)

	// Load persona context
	ctx := context.Background()
	result, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)
	if err != nil {
		t.Fatalf("LoadPersonaContext failed: %v", err)
	}

	// Should contain only AGENTS and IDENTITY (AGENTS loads first in the order)
	expected := agentsContent + "\n" + identityContent
	if result != expected {
		t.Errorf("Unexpected content with missing files.\nGot: %q\nWant: %q", result, expected)
	}
}

func TestLoadPersonaContext_EmptyFilesSkipped(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Create empty IDENTITY.md and AGENTS.md
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "IDENTITY.md"), []byte(""), 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "AGENTS.md"), []byte(""), 0644)

	// Load persona context
	ctx := context.Background()
	result, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)
	if err != nil {
		t.Fatalf("LoadPersonaContext failed: %v", err)
	}

	// Should be empty
	if result != "" {
		t.Errorf("Expected empty result for empty files, got: %q", result)
	}
}

func TestLoadPersonaContext_ByteCapEnforced(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Create IDENTITY.md in workspace with content that exceeds maxPersonaBytes
	largeContent := make([]byte, agent.MaxPersonaBytes+100)
	for i := range largeContent {
		largeContent[i] = 'A'
	}
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "IDENTITY.md"), largeContent, 0644)

	// Load persona context
	ctx := context.Background()
	result, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)
	if err != nil {
		t.Fatalf("LoadPersonaContext failed: %v", err)
	}

	// Should be capped at maxPersonaBytes
	if len(result) > agent.MaxPersonaBytes {
		t.Errorf("Result exceeds byte cap. Got %d bytes, want max %d", len(result), agent.MaxPersonaBytes)
	}
	// Should be exactly maxPersonaBytes since we had more content available
	if len(result) != agent.MaxPersonaBytes {
		t.Errorf("Result not capped correctly. Got %d bytes, want %d", len(result), agent.MaxPersonaBytes)
	}
}

func TestLoadPersonaContext_MultiFileCap(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Create files that together exceed the cap
	firstContent := make([]byte, agent.MaxPersonaBytes/2)
	for i := range firstContent {
		firstContent[i] = 'A'
	}
	secondContent := make([]byte, agent.MaxPersonaBytes) // This should be partially included
	for i := range secondContent {
		secondContent[i] = 'B'
	}

	// SOUL loads before IDENTITY, so SOUL gets the smaller "first" content
	// (fully included under the cap) and IDENTITY gets the larger "second" content (partial).
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "SOUL.md"), firstContent, 0644)
	_ = os.WriteFile(filepath.Join(tmpWorkspace, "IDENTITY.md"), secondContent, 0644)

	// Load persona context
	ctx := context.Background()
	result, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)
	if err != nil {
		t.Fatalf("LoadPersonaContext failed: %v", err)
	}

	// Should be capped at maxPersonaBytes
	if len(result) > agent.MaxPersonaBytes {
		t.Errorf("Result exceeds byte cap. Got %d bytes, want max %d", len(result), agent.MaxPersonaBytes)
	}
	// Should contain all of first file and part of second
	if len(result) != agent.MaxPersonaBytes {
		t.Errorf("Multi-file cap not enforced correctly. Got %d bytes, want %d", len(result), agent.MaxPersonaBytes)
	}

	// Verify content contains both A and B
	hasA := false
	hasB := false
	for _, c := range result {
		if c == 'A' {
			hasA = true
		}
		if c == 'B' {
			hasB = true
		}
	}
	if !hasA {
		t.Error("Result should contain first file content (A)")
	}
	if !hasB {
		t.Error("Result should contain part of second file content (B)")
	}
}

func TestLoadPersonaContext_NoPersonaFiles(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Don't create any persona files

	// Load persona context
	ctx := context.Background()
	result, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)
	if err != nil {
		t.Fatalf("LoadPersonaContext failed: %v", err)
	}

	// Should be empty
	if result != "" {
		t.Errorf("Expected empty result when no persona files exist, got: %q", result)
	}
}

func TestLoadPersonaContext_ReadError(t *testing.T) {
	// Create temporary directories
	tmpConfig := t.TempDir()
	tmpWorkspace := t.TempDir()

	// Create a directory instead of a file in workspace to trigger read error
	_ = os.Mkdir(filepath.Join(tmpWorkspace, "IDENTITY.md"), 0755)

	// Load persona context
	ctx := context.Background()
	_, err := agent.LoadPersonaContext(ctx, tmpWorkspace, tmpConfig)

	// Should return error
	if err == nil {
		t.Error("Expected error when reading directory as file, got nil")
	}
}
