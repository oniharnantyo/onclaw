package systemskills

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed web-research/SKILL.md document-read/SKILL.md
var embeddedSkills embed.FS

// SyncSystemSkills mirrors embedded files into destDir.
// Overwrites when content differs, removes extraneous files/dirs not in the embedded set.
// Idempotent: subsequent runs are no-ops if the destination matches the embedded set.
func SyncSystemSkills(destDir string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Get list of embedded skill directories
	entries, err := embeddedSkills.ReadDir(".")
	if err != nil {
		return fmt.Errorf("failed to read embedded skills: %w", err)
	}

	// Track which embedded skills exist
	embeddedSet := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		embeddedSet[entry.Name()] = true
	}

	// Mirror embedded skills to destination
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		destSkillDir := filepath.Join(destDir, skillName)

		// Create skill directory
		if err := os.MkdirAll(destSkillDir, 0755); err != nil {
			return fmt.Errorf("failed to create skill directory %s: %w", destSkillDir, err)
		}

		// Read embedded SKILL.md
		srcPath := filepath.Join(skillName, "SKILL.md")
		srcContent, err := embeddedSkills.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read embedded skill %s: %w", srcPath, err)
		}

		// Check if destination file exists and compare content
		destPath := filepath.Join(destSkillDir, "SKILL.md")
		destContent, err := os.ReadFile(destPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read destination skill %s: %w", destPath, err)
		}

		// Overwrite if content differs
		if !os.IsNotExist(err) && string(destContent) == string(srcContent) {
			continue // Content matches, skip
		}

		// Write/update the file
		if err := os.WriteFile(destPath, srcContent, 0644); err != nil {
			return fmt.Errorf("failed to write skill %s: %w", destPath, err)
		}
	}

	// Remove extraneous files/dirs not in the embedded set
	destEntries, err := os.ReadDir(destDir)
	if err != nil {
		return fmt.Errorf("failed to read destination directory: %w", err)
	}

	for _, entry := range destEntries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		if !embeddedSet[skillName] {
			// This directory is not in the embedded set, remove it
			destPath := filepath.Join(destDir, skillName)
			if err := os.RemoveAll(destPath); err != nil {
				return fmt.Errorf("failed to remove extraneous skill directory %s: %w", destPath, err)
			}
		}
	}

	return nil
}

// ListEmbedded returns the names of all embedded skills.
func ListEmbedded() ([]string, error) {
	entries, err := embeddedSkills.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded skills: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	return names, nil
}

// GetEmbeddedSkill returns the content of an embedded skill by name.
func GetEmbeddedSkill(name string) (string, error) {
	path := filepath.Join(name, "SKILL.md")
	content, err := embeddedSkills.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded skill %s: %w", name, err)
	}

	return string(content), nil
}

// CopyTo copies an embedded skill to a destination directory.
func CopyTo(name, destDir string) error {
	content, err := GetEmbeddedSkill(name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write skill: %w", err)
	}

	return nil
}

// VerifyChecksum checks if the destination skill matches the embedded version.
func VerifyChecksum(name, destDir string) (bool, error) {
	srcContent, err := GetEmbeddedSkill(name)
	if err != nil {
		return false, err
	}

	destPath := filepath.Join(destDir, name, "SKILL.md")
	destContent, err := os.ReadFile(destPath)
	if err != nil {
		return false, err
	}

	return string(destContent) == srcContent, nil
}
