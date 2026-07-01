package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
)

// BrowseFolderEntry represents a single entry in a folder.
type BrowseFolderEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// BrowseFolderResult represents the result of browsing a folder.
type BrowseFolderResult struct {
	CurrentPath string              `json:"current_path"`
	ParentPath  string              `json:"parent_path"`
	Entries     []BrowseFolderEntry `json:"entries"`
}

// BrowseFS lists directories and files of a given path.
func (s *Service) BrowseFS(ctx context.Context, targetPath string) (BrowseFolderResult, error) {
	if targetPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			targetPath = "."
		} else {
			targetPath = home
		}
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return BrowseFolderResult{}, err
	}

	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		return BrowseFolderResult{}, err
	}

	var entries []BrowseFolderEntry
	for _, entry := range dirEntries {
		// Only list directories to help locate skill folders, and skip hidden directories (starting with .)
		if entry.IsDir() {
			name := entry.Name()
			if len(name) > 0 && name[0] == '.' && name != ".agents" { // skip hidden directories unless it's .agents
				continue
			}
			entries = append(entries, BrowseFolderEntry{
				Name:  name,
				Path:  filepath.Join(absPath, name),
				IsDir: true,
			})
		}
	}

	// Sort entries alphabetically
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	parentPath := filepath.Dir(absPath)
	if parentPath == absPath {
		parentPath = "" // we are at root
	}

	return BrowseFolderResult{
		CurrentPath: absPath,
		ParentPath:  parentPath,
		Entries:     entries,
	}, nil
}
