package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Candidate represents a discovered skill candidate before installation.
type Candidate struct {
	Name        string
	Description string
	Version     string
	Path        string // Absolute path to the directory containing SKILL.md
	RelPath     string // Relative path from the search root
	Content     string // Full content of SKILL.md
}

// Discover recursively searches the root directory (restricted to a subpath if restrict is non-empty)
// for directories containing a SKILL.md file (case-insensitive).
func Discover(root string, restrict string) ([]*Candidate, error) {
	searchRoot := root
	if restrict != "" {
		searchRoot = filepath.Join(root, restrict)
	}

	// Verify search root exists
	if _, err := os.Stat(searchRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Return empty, not an error
		}
		return nil, err
	}

	var candidates []*Candidate

	err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			filename := d.Name()
			if strings.ToLower(filename) == "skill.md" {
				dirPath := filepath.Dir(path)
				relPath, err := filepath.Rel(root, dirPath)
				if err != nil {
					relPath = dirPath
				}

				contentBytes, err := os.ReadFile(path)
				if err != nil {
					return nil // Skip files we cannot read
				}
				content := string(contentBytes)

				fallbackName := filepath.Base(dirPath)
				_, meta, err := ParseAndNormalizeManifest(content, fallbackName)

				var name, desc, version string
				if err != nil {
					name = fallbackName
					desc = "Skill " + name
					version = "1.0.0"
				} else {
					name, _ = meta["name"].(string)
					desc, _ = meta["description"].(string)
					version, _ = meta["version"].(string)
					if version == "" {
						version = "1.0.0"
					}
				}

				candidates = append(candidates, &Candidate{
					Name:        name,
					Description: desc,
					Version:     version,
					Path:        dirPath,
					RelPath:     relPath,
					Content:     content,
				})
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return candidates, nil
}
