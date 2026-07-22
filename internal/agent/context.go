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
func LoadPersonaContext(_ context.Context, workspace, userConfigDir string) (string, error) {
	// Persona files load in a fixed order: how you operate, who you are, what
	// you can do, your tools, who your human is, then your first-run ritual.
	// Missing files are skipped below; MEMORY.md is handled by the memory
	// middleware separately. The global USER.md (human's cross-agent profile)
	// loads just before the per-agent USER.md so the override wins by recency.
	filesToRead := []string{
		filepath.Join(workspace, "AGENTS.md"),
		filepath.Join(workspace, "SOUL.md"),
		filepath.Join(workspace, "IDENTITY.md"),
		filepath.Join(workspace, "CAPABILITIES.md"),
		filepath.Join(workspace, "TOOLS.md"),
		filepath.Join(userConfigDir, "USER.md"), // human's global profile (baseline)
		filepath.Join(workspace, "USER.md"),     // per-agent override
		filepath.Join(workspace, "BOOTSTRAP.md"),
	}

	var parts []string
	totalBytes := 0

	for _, path := range filesToRead {
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read persona file %s: %w", path, err)
		}

		if len(content) == 0 {
			continue
		}

		newlineSize := 0
		if len(parts) > 0 {
			newlineSize = 1
		}

		if totalBytes+newlineSize+len(content) > maxPersonaBytes {
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

	result := strings.Join(parts, "\n")
	return result, nil
}
