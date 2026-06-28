package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidatePath ensures that the given path resolves within the workspace absolute path.
// It returns the cleaned absolute path if valid, or a traversal blocked error.
func ValidatePath(workspaceAbs, pathStr string) (string, error) {
	var targetAbs string
	if filepath.IsAbs(pathStr) {
		targetAbs = filepath.Clean(pathStr)
	} else {
		targetAbs = filepath.Clean(filepath.Join(workspaceAbs, pathStr))
	}

	rel, err := filepath.Rel(workspaceAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path traversal blocked: path %q resolves outside workspace", pathStr)
	}

	return targetAbs, nil
}
