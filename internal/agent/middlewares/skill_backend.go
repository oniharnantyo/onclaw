package middlewares

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"gopkg.in/yaml.v3"
)

// multiDirBackend implements einoskill.Backend by scanning multiple skill
// directories in precedence order. This is the agent runtime read-path
// (disk-only); skill install/management lives in internal/skill.
type multiDirBackend struct {
	dirs []string
}

// NewMultiDirBackend creates a skill backend that searches multiple directories
// in order of precedence (first wins).
func NewMultiDirBackend(dirs []string) einoskill.Backend {
	return &multiDirBackend{dirs: dirs}
}

// List scans the immediate subdirectories of each configured directory, parses
// each SKILL.md, and returns the frontmatter. Deduplication is by name with the
// first directory in precedence order winning.
func (b *multiDirBackend) List(ctx context.Context) ([]einoskill.FrontMatter, error) {
	var list []einoskill.FrontMatter
	seen := make(map[string]bool)

	for _, dir := range b.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// If directory does not exist, skip it silently
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Look for SKILL.md (case-insensitive) in the subdirectory
			subdirPath := filepath.Join(dir, entry.Name())
			skillFile, err := findSkillFile(subdirPath)
			if err != nil || skillFile == "" {
				continue
			}

			fm, err := parseFrontMatter(skillFile, entry.Name())
			if err != nil {
				continue
			}

			if fm.Name == "" {
				continue
			}

			// Precedence check: first wins
			if !seen[fm.Name] {
				seen[fm.Name] = true
				list = append(list, fm)
			}
		}
	}

	return list, nil
}

// Get finds a skill by its name using the precedence order of directories.
// It returns the parsed FrontMatter and the raw markdown body (content) of the skill.
func (b *multiDirBackend) Get(ctx context.Context, name string) (einoskill.Skill, error) {
	for _, dir := range b.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			subdirPath := filepath.Join(dir, entry.Name())
			skillFile, err := findSkillFile(subdirPath)
			if err != nil || skillFile == "" {
				continue
			}

			fm, err := parseFrontMatter(skillFile, entry.Name())
			if err != nil {
				continue
			}

			if fm.Name == name {
				contentBytes, err := os.ReadFile(skillFile)
				if err != nil {
					return einoskill.Skill{}, err
				}

				content := string(contentBytes)
				// Extract the body (everything after frontmatter)
				body := content
				if strings.HasPrefix(content, "---") {
					parts := strings.SplitN(content, "---", 3)
					if len(parts) >= 3 {
						body = parts[2]
					}
				}

				return einoskill.Skill{
					FrontMatter: fm,
					Content:     body,
				}, nil
			}
		}
	}

	return einoskill.Skill{}, fmt.Errorf("skill not found: %s", name)
}

// findSkillFile searches a directory for a file named skill.md (case-insensitive) and returns its path.
func findSkillFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.ToLower(entry.Name()) == "skill.md" {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return "", nil
}

// parseFrontMatter parses the yaml frontmatter of a skill file.
func parseFrontMatter(filePath string, fallbackName string) (einoskill.FrontMatter, error) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return einoskill.FrontMatter{}, err
	}

	content := string(contentBytes)
	var frontmatterYAML string

	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			frontmatterYAML = parts[1]
		}
	}

	var fm einoskill.FrontMatter
	if frontmatterYAML != "" {
		if err := yaml.Unmarshal([]byte(frontmatterYAML), &fm); err != nil {
			// If YAML parsing fails, we construct basic metadata
			fm.Name = fallbackName
			fm.Description = "Skill " + fallbackName
		}
	} else {
		fm.Name = fallbackName
		fm.Description = "Skill " + fallbackName
	}

	if fm.Name == "" {
		fm.Name = fallbackName
	}
	if fm.Description == "" {
		fm.Description = "Skill " + fm.Name
	}

	// Normalize context if it is set to fork values (eino fails fork under AgenticMessage)
	if strings.HasPrefix(string(fm.Context), "fork") {
		fm.Context = einoskill.ContextMode("inline")
	}

	return fm, nil
}
