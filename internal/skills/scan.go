// Package skills indexes the on-disk skill library: it scans the skills
// directory (~/.buddies/skills) for SKILL.md-bearing folders and upserts
// them into the store's skill registry. The skill bodies themselves stay on
// disk; only metadata and enable state persist.
package skills

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/buddies/internal/store"
)

// SourceDirectory marks a registry row discovered by a directory scan (as
// opposed to future install sources).
const SourceDirectory = "directory"

// SkillMD is the convention marker file of a skill directory: a markdown
// file with an optional `name` / `description` frontmatter block.
const SkillMD = "SKILL.md"

// Scan walks dir (the layout's skills directory) and upserts every
// SKILL.md-bearing subdirectory into the registry. It is startup-resilient
// by contract: a missing directory is a no-op and one broken skill
// directory is logged and skipped — a bad folder must never fail startup.
// Store failures are returned to the caller, which logs them non-fatally.
func Scan(ctx context.Context, reg store.SkillRegistry, dir string, log *slog.Logger) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no skills directory yet: nothing to index
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		skill, err := readSkillDir(path)
		if err != nil {
			log.Warn("skills: skipping unreadable skill directory",
				"dir", path, "error", err)
			continue
		}
		if err := upsert(ctx, reg, *skill); err != nil {
			log.Warn("skills: could not index skill directory",
				"dir", path, "error", err)
		}
	}
	return nil
}

// upsert indexes one discovered skill, preserving an existing row's enable
// toggle across rescans; a first discovery defaults to enabled.
func upsert(ctx context.Context, reg store.SkillRegistry, skill store.Skill) error {
	existing, err := reg.GetByName(ctx, skill.Name)
	switch {
	case err == nil:
		skill.Enabled = existing.Enabled
		skill.CreatedAt = existing.CreatedAt
	case isNotFound(err):
		skill.Enabled = true
	default:
		return err
	}
	return reg.Upsert(ctx, &skill)
}

// isNotFound reports whether err is the registry's not-found sentinel.
func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}

// readSkillDir reads one skill directory: the directory name is the skill
// name, SKILL.md (when present) carries the description frontmatter.
func readSkillDir(path string) (*store.Skill, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	skill := &store.Skill{
		Name:    filepath.Base(path),
		Version: "0.1.0",
		Source:  SourceDirectory,
	}

	raw, err := os.ReadFile(filepath.Join(path, SkillMD))
	if err != nil {
		if os.IsNotExist(err) {
			// A bare directory is a nameless, description-less skill: still
			// indexable so the pane shows it.
			return skill, nil
		}
		return nil, err
	}
	name, description := parseFrontmatter(string(raw))
	if name != "" {
		skill.Name = name
	}
	skill.Description = description
	return skill, nil
}

// parseFrontmatter extracts the name and description from a SKILL.md's
// leading YAML-ish frontmatter block:
//
//	---
//	name: my-skill
//	description: What the skill does.
//	---
//
// Everything before, after, or malformed yields empty values. Only these
// two flat keys are recognized — the base convention needs no YAML parser.
func parseFrontmatter(content string) (name, description string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break // frontmatter closed
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return name, description
}
