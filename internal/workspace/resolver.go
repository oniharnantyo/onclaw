package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveWorkspace resolves the workspace directory following the priority order:
// CLI flag > agent row workspace > ONCLAW_WORKSPACE env var > config file > current working directory.
// It returns an absolute path and never calls os.Chdir - the caller is responsible
// for passing the resolved path explicitly to tools that need it.
func ResolveWorkspace(flagWorkspace, agentWorkspace, cfgWorkspace, cwd string) (string, error) {
	var workspace string

	// Priority: CLI flag > agent workspace > ONCLAW_WORKSPACE env > config > cwd
	if flagWorkspace != "" {
		workspace = flagWorkspace
	} else if agentWorkspace != "" {
		workspace = agentWorkspace
	} else if envWorkspace := os.Getenv("ONCLAW_WORKSPACE"); envWorkspace != "" {
		workspace = envWorkspace
	} else if cfgWorkspace != "" {
		workspace = cfgWorkspace
	} else {
		workspace = cwd
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for workspace %q: %w", workspace, err)
	}

	// Validate the path exists and is accessible
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("access workspace %q: %w", absPath, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("workspace %q is not a directory", absPath)
	}

	return absPath, nil
}
