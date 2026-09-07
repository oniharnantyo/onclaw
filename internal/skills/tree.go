package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// fileInfo is the minimal stat shape walkTree reports.
type fileInfo interface {
	Name() string
	Size() int64
	Mode() fs.FileMode
	IsDir() bool
}

// walkTree walks dir depth-first, reporting each entry (dirs included) as a
// slash-separated path relative to dir. Any symlink anywhere in the tree is
// rejected — resolve-then-prefix-check against the tree root, symlinks
// included (design D4 sanitization).
func walkTree(dir string, fn func(rel string, info fileInfo) error) error {
	root := filepath.Clean(dir)
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(filepath.Clean(p), root)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		if rel == "" {
			return nil // the root itself
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return hostile(fmt.Sprintf("entry %q is a symlink", rel))
		}
		return fn(filepath.ToSlash(rel), info)
	})
}

// readFile is os.ReadFile by absolute path (kept as a seam for symmetry
// with walkTree).
func readFile(p string) ([]byte, error) {
	return os.ReadFile(p)
}
