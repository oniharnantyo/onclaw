package agent

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/*
var TemplatesFS embed.FS

// GetTemplate returns the string content of a template by its relative filename (e.g. "IDENTITY.md").
func GetTemplate(name string) (string, error) {
	data, err := TemplatesFS.ReadFile(filepath.Join("templates", name))
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", name, err)
	}
	return string(data), nil
}

// SeedWorkspace seeds the agent persona/memory files in the given workspace directory
// from their corresponding templates, only if they are absent. This operation is non-destructive.
func SeedWorkspace(workspace string) error {
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return fmt.Errorf("create workspace dir %s: %w", workspace, err)
	}

	mappings := map[string]string{
		"IDENTITY.md":     "IDENTITY.md",
		"SOUL.md":         "SOUL.md",
		"CAPABILITIES.md": "CAPABILITIES.md",
		"TOOLS.md":        "TOOLS.md",
		"USER.md":         "USER.md",
		"MEMORY.md":       "MEMORY.md",
		"AGENTS.md":       "AGENTS.md",
	}

	for destFile, srcTemplate := range mappings {
		destPath := filepath.Join(workspace, destFile)
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			content, err := GetTemplate(srcTemplate)
			if err != nil {
				return err
			}
			if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("write seeded file %s: %w", destPath, err)
			}
		}
	}

	return nil
}

// SeedGlobalUser seeds the global USER.md file in the user config directory from USER.md template,
// only if it is absent. This operation is non-destructive.
func SeedGlobalUser(userConfigDir string) error {
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		return fmt.Errorf("create user config dir %s: %w", userConfigDir, err)
	}

	destPath := filepath.Join(userConfigDir, "USER.md")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		content, err := GetTemplate("USER.md")
		if err != nil {
			return err
		}
		if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write seeded global USER.md: %w", err)
		}
	}
	return nil
}

// SeedBootstrap seeds BOOTSTRAP.md into the workspace from the BOOTSTRAP.md template,
// only if it is absent. This operation is non-destructive.
func SeedBootstrap(workspace string) error {
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return fmt.Errorf("create workspace dir %s: %w", workspace, err)
	}

	destPath := filepath.Join(workspace, "BOOTSTRAP.md")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		content, err := GetTemplate("BOOTSTRAP.md")
		if err != nil {
			return err
		}
		if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write seeded BOOTSTRAP.md: %w", err)
		}
	}
	return nil
}
