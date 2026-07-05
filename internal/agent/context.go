package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxPersonaBytes = 16 * 1024 // 16KB cap for persona context
)

// LoadPersonaContext loads persona context files in fixed order and concatenates them.
// Missing files are skipped. Empty files contribute nothing. Total size is capped at maxPersonaBytes.
// CURATED CORE INTEGRATION: The memory middleware will now handle MEMORY.md independently with its own cap.
func LoadPersonaContext(ctx context.Context, workspace, userConfigDir string) (string, error) {
	var filesToRead []string

	// 1. Global USER.md
	filesToRead = append(filesToRead, filepath.Join(userConfigDir, "USER.md"))

	// 2. Per-agent workspace files (excluding MEMORY.md as it's handled by memory middleware)
	workspaceFiles := []string{
		"BOOTSTRAP.md",
		"IDENTITY.md",
		"SOUL.md",
		"CAPABILITIES.md",
		"USER.md",
	}
	for _, f := range workspaceFiles {
		filesToRead = append(filesToRead, filepath.Join(workspace, f))
	}

	// 3. AGENTS.md in workspace
	filesToRead = append(filesToRead, filepath.Join(workspace, "AGENTS.md"))

	var parts []string
	totalBytes := 0

	for _, path := range filesToRead {
		content, err := os.ReadFile(path)
		if err != nil {
			// Skip missing files
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read persona file %s: %w", path, err)
		}

		// Skip empty files
		if len(content) == 0 {
			continue
		}

		// Calculate size including newline that will be added
		newlineSize := 0
		if len(parts) > 0 {
			newlineSize = 1
		}

		// Check if adding this file would exceed the cap
		if totalBytes+newlineSize+len(content) > maxPersonaBytes {
			// Add what we can and stop
			available := maxPersonaBytes - totalBytes - newlineSize
			if available > 0 {
				parts = append(parts, string(content[:available]))
				totalBytes = maxPersonaBytes
			}
			break
		}

		parts = append(parts, string(content))
		totalBytes += newlineSize + len(content)
	}

	// Join with newlines between files
	result := strings.Join(parts, "\n")
	return result, nil
}
