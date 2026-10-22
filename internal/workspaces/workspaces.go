// Package workspaces manages agents' on-disk workspace directories: one
// directory per agent, named by its slug, holding the file-backed prompt
// documents (design D1). Writes are atomic with a backup-before-overwrite
// safety net; the manager never caches file content, so external edits
// apply to the next read for free.
package workspaces

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Document names living in every agent workspace.
const (
	DocIdentity  = "IDENTITY.md"
	DocSoul      = "SOUL.md"
	DocBootstrap = "BOOTSTRAP.md"
)

// Subdirectory names inside an agent workspace (design D6): skills/ holds
// the agent-tier skill library (shadowing the global tier on name
// conflict), .tasks/ is the reserved task-store directory.
const (
	DirSkills = "skills"
	DirTasks  = ".tasks"
)

// maxSlugLen is the slug length cap (DNS label limit). The slug doubles as
// the workspace directory name, which is why it must be a safe name.
const maxSlugLen = 63

// dirPerm matches the app layout's private-to-user directory mode
// (internal/config); filePerm the file mode.
const (
	dirPerm  os.FileMode = 0700
	filePerm os.FileMode = 0600
)

// Manager resolves and maintains workspace directories under one root
// (normally ~/.buddies/workspaces). It is safe for concurrent use: every
// method touches distinct paths derived from the immutable root.
type Manager struct {
	root string
}

// NewManager returns a manager rooting workspaces at root (e.g.
// ~/.buddies/workspaces). The root is injected so tests can isolate it.
func NewManager(root string) *Manager {
	return &Manager{root: root}
}

// Root returns the workspaces root directory.
func (m *Manager) Root() string { return m.root }

// Dir returns the workspace directory path for the validated slug. It
// performs no filesystem access — creating the directory is Create's job.
func (m *Manager) Dir(slug string) (string, error) {
	if !ValidSlug(slug) {
		return "", fmt.Errorf("workspaces: invalid agent slug %q", slug)
	}
	return filepath.Join(m.root, slug), nil
}

// Create makes the agent's workspace directory (mkdir -p) and returns its
// path. It runs at agent-create time, before generation finishes — partial
// directories from failed generations are legitimate (design D11).
func (m *Manager) Create(slug string) (string, error) {
	dir, err := m.Dir(slug)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("workspaces: create %s: %w", dir, err)
	}
	return dir, nil
}

// Remove deletes the agent's workspace directory tree. Removing an already
// absent workspace succeeds (nil), mirroring the caller's best-effort
// cleanup contract.
func (m *Manager) Remove(slug string) error {
	dir, err := m.Dir(slug)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("workspaces: remove %s: %w", dir, err)
	}
	return nil
}

// SkillsDir returns the workspace's skills directory (design D6: the
// agent-tier skill library), creating it on demand (idempotent mkdir -p) so
// agents can author skills into it through the jailed file tools.
func (m *Manager) SkillsDir(slug string) (string, error) {
	return m.ensureDir(slug, DirSkills)
}

// TasksDir returns the workspace's .tasks directory, creating it on demand
// (idempotent mkdir -p).
func (m *Manager) TasksDir(slug string) (string, error) {
	return m.ensureDir(slug, DirTasks)
}

// ensureDir resolves the slug's workspace and creates the named
// subdirectory inside it.
func (m *Manager) ensureDir(slug, name string) (string, error) {
	dir, err := m.Dir(slug)
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, dirPerm); err != nil {
		return "", fmt.Errorf("workspaces: create %s: %w", sub, err)
	}
	return sub, nil
}

// ReadDocument reads one document from the workspace directory dir. A
// missing document is the normal absent state: ("", false, nil). Any other
// read failure is returned.
func (m *Manager) ReadDocument(dir, name string) (content string, exists bool, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("workspaces: read %s: %w", name, err)
	}
	return string(raw), true, nil
}

// Entry is one listed workspace child (design D8): the base name, whether it
// is a directory, and the file's byte size (0 for directories).
type Entry struct {
	Name string
	Dir  bool
	Size int64
}

// ReadFile reads one file from the slug's workspace through the jailed
// reader (design D8, spec chat-panels "Workspace reader stays jailed"):
// relPath is workspace-relative; absolute paths, any ".." element, and
// symlink escapes are refused BEFORE the file is opened, and nothing outside
// the workspace is ever served. A missing file is a clean not-found error.
func (m *Manager) ReadFile(slug, relPath string) (string, error) {
	target, err := m.resolveJailed(slug, relPath, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("workspaces: read %s: %w", relPath, mapAbsent(err))
	}
	if info.IsDir() {
		return "", fmt.Errorf("workspaces: read %s: is a directory", relPath)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("workspaces: read %s: %w", relPath, err)
	}
	return string(raw), nil
}

// ListDir lists one directory of the slug's workspace through the jailed
// reader, jailed exactly like ReadFile. relPath "" (or "/") lists the
// workspace root. The result is never nil; children sort by name.
func (m *Manager) ListDir(slug, relPath string) ([]Entry, error) {
	target, err := m.resolveJailed(slug, relPath, true)
	if err != nil {
		return nil, err
	}
	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("workspaces: list %s: %w", relPath, mapAbsent(err))
	}
	out := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entry := Entry{Name: de.Name(), Dir: de.IsDir()}
		if !de.IsDir() {
			if info, ierr := de.Info(); ierr == nil {
				entry.Size = info.Size()
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// resolveJailed resolves a workspace-relative path to its absolute target,
// refusing everything the jail must refuse (design D8): a validated slug,
// no absolute paths, no ".." elements, the cleaned join inside the
// workspace, and — before the caller opens anything — the fully
// symlink-resolved target still inside the symlink-resolved workspace root.
// When allowRoot is false the workspace root itself is refused (a file read
// must name a file).
func (m *Manager) resolveJailed(slug, relPath string, allowRoot bool) (string, error) {
	dir, err := m.Dir(slug)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("workspaces: resolve %s workspace: %w", slug, err)
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("workspaces: refused %q: absolute paths are outside the workspace", relPath)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspaces: refused %q: traversal outside the workspace", relPath)
	}
	target := filepath.Join(realRoot, clean)
	if target != realRoot && !strings.HasPrefix(target, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("workspaces: refused %q: resolves outside the workspace", relPath)
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("workspaces: resolve %s: %w", relPath, mapAbsent(err))
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("workspaces: refused %q: crosses a symlink outside the workspace", relPath)
	}
	if !allowRoot && real == realRoot {
		return "", fmt.Errorf("workspaces: refused %q: names the workspace root, not a file", relPath)
	}
	return real, nil
}

// mapAbsent renders not-exist failures as a clean not-found error.
func mapAbsent(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("not found: %w", err)
	}
	return err
}

// WriteDocumentWithBackup replaces the document name in dir with content.
// When the document already exists, its current content is first copied to
// <name>.bak beside it; the new content then lands through a temp file plus
// rename in the same directory (atomic on the same filesystem). Any backup
// failure aborts the whole call leaving the target untouched (design D6).
func (m *Manager) WriteDocumentWithBackup(dir, name, content string) error {
	target := filepath.Join(dir, name)

	if _, err := os.Stat(target); err == nil {
		if err := copyFile(target, target+".bak", filePerm); err != nil {
			return fmt.Errorf("workspaces: back up %s: %w", name, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("workspaces: stat %s: %w", name, err)
	}

	if err := writeFileAtomic(target, content, filePerm); err != nil {
		return fmt.Errorf("workspaces: write %s: %w", name, err)
	}
	return nil
}

// copyFile copies src to a fresh dst (dst must not exist as a directory —
// a failed creation is the caller's abort signal).
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy content: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close backup: %w", err)
	}
	return nil
}

// writeFileAtomic writes content to path via a temp file in the same
// directory plus a rename, so readers never observe a partial document.
func writeFileAtomic(path, content string, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Any failure below removes the temp file; the target is untouched.
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		return fail(fmt.Errorf("write content: %w", err))
	}
	if err := tmp.Chmod(perm); err != nil {
		return fail(fmt.Errorf("set mode: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return fail(fmt.Errorf("close temp file: %w", err))
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// ValidSlug reports whether slug is a DNS label: ^[a-z0-9](-?[a-z0-9])*$ and
// at most 63 characters (design D3). Only such names may name a workspace
// directory or an agent.
func ValidSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > maxSlugLen {
		return false
	}
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-':
			// No leading, trailing, or doubled dashes.
			if i == 0 || i == len(slug)-1 || slug[i-1] == '-' {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Slugify folds a free-form agent name into a valid slug: lowercase, every
// run of characters outside [a-z0-9] becomes a single dash, leading and
// trailing dashes are trimmed, and over-long results truncate to 63
// characters at a dash boundary (a hard cut when no dash exists in range).
// The empty result is possible (a name with no alphanumerics); callers
// decide what to do with it.
func Slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !alnum {
			prevDash = true
			continue
		}
		if prevDash && b.Len() > 0 {
			b.WriteByte('-')
		}
		prevDash = false
		b.WriteRune(r)
	}
	s := b.String() // already trimmed: never starts or ends with '-'
	if len(s) <= maxSlugLen {
		return s
	}
	s = s[:maxSlugLen]
	// Break at the last dash in range so the slug splits between words
	// rather than mid-word; trim any dash the cut left dangling.
	if i := strings.LastIndexByte(s, '-'); i > 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, "-")
}
