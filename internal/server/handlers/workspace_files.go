package handlers

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceFilesHandlers serves read-only access to an agent's workspace
// (jail) directory over the authenticated workspace-files API
// (add-right-panel tasks 2.1–2.5): single-file byte reads and one-level
// directory listings with strict path confinement and content-type-safe
// serving headers.
type workspaceFilesHandlers struct {
	agents store.AgentStore
	// workspaceDir is the workspace root: <onclaw dir>/workspaces. Agent
	// jail directories derive from it plus the workspace and agent slugs
	// (domain.AgentWorkspaceDir); no path is recorded on any row.
	workspaceDir string
}

// NewWorkspaceFilesHandlers creates a new workspaceFilesHandlers instance
// with injected dependencies.
func NewWorkspaceFilesHandlers(agentStore store.AgentStore, workspaceDir string) *workspaceFilesHandlers {
	return &workspaceFilesHandlers{
		agents:       agentStore,
		workspaceDir: workspaceDir,
	}
}

// workspaceFileEntry is one row of a directory listing. Listings carry
// exactly one level of the tree per response, so callers walk lazily.
type workspaceFileEntry struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // "file" | "directory"
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// ServeWorkspaceFiles answers GET .../workspaces/:ws/agents/:agent/files
// with the requested file's bytes, or — with mode=list — one directory level
// as JSON. The workspace in scope is the authenticated member's context
// workspace (RequireWorkspace bound it from the authenticated membership):
// the URL slug locates, never authorizes. Every confinement rejection and
// every miss collapses to not found — no existence oracle.
func (h *workspaceFilesHandlers) ServeWorkspaceFiles(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agent, err := h.resolveJailAgent(c, ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	jail := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug)
	rel := c.Query("path")

	if c.Query("mode") == "list" {
		h.list(c, jail, rel)
		return
	}
	if strings.TrimSpace(rel) == "" {
		RespondError(c, fmt.Errorf("%w: path query parameter is required", domain.ErrInvalid))
		return
	}
	h.read(c, jail, rel)
}

// resolveJailAgent resolves an agent strictly inside the context workspace by
// slug first, falling back to ID. Because both lookups are workspace-scoped,
// another workspace's agent is indistinguishable from a missing one.
func (h *workspaceFilesHandlers) resolveJailAgent(c *gin.Context, workspaceID string) (*domain.Agent, error) {
	identifier := c.Param("agent")
	if identifier == "" {
		return nil, domain.ErrNotFound
	}
	agent, err := h.agents.BySlug(c.Request.Context(), workspaceID, identifier)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return h.agents.ByID(c.Request.Context(), workspaceID, identifier)
}

// read streams one file's bytes. Content type resolves from the extension
// with a magic-byte sniff fallback; HTML and SVG — the types that can execute
// in a browser context — always ride an attachment disposition so stored
// content never renders on the application origin.
func (h *workspaceFilesHandlers) read(c *gin.Context, jail, rel string) {
	path, ok := resolveInJail(jail, rel)
	if !ok {
		AbortNotFound(c, "file not found")
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		AbortNotFound(c, "file not found")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		AbortNotFound(c, "file not found")
		return
	}
	defer f.Close()

	name := filepath.Base(path)
	ctype := detectFileContentType(name, f)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", ctype)
	if isExecutableContentType(ctype) {
		c.Header("Content-Disposition", attachmentDisposition(name))
	}
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), f)
}

// list returns exactly one level of the directory at rel. A path that is not
// a directory — including a regular file — is not found.
func (h *workspaceFilesHandlers) list(c *gin.Context, jail, rel string) {
	path, ok := resolveInJail(jail, rel)
	if !ok {
		AbortNotFound(c, "directory not found")
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		AbortNotFound(c, "directory not found")
		return
	}
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		RespondError(c, err)
		return
	}

	entries := make([]workspaceFileEntry, 0, len(dirEntries))
	for _, e := range dirEntries {
		fi, err := e.Info()
		if err != nil {
			continue // vanished between ReadDir and Info; skip the entry
		}
		kind := "file"
		if e.IsDir() {
			kind = "directory"
		}
		entries = append(entries, workspaceFileEntry{Name: e.Name(), Kind: kind, Size: fi.Size(), Modified: fi.ModTime()})
	}

	// The listing echoes the confined directory the entries are relative to,
	// so a caller walking the tree can address children without re-cleaning.
	echo := ""
	if clean, ok := cleanJailRel(rel); ok {
		echo = filepath.ToSlash(clean)
	}

	c.Header("X-Content-Type-Options", "nosniff")
	RespondOK(c, gin.H{"path": echo, "entries": entries})
}

// cleanJailRel reduces a caller-supplied relative path to its clean slash
// form, reporting false when the path escapes the jail (parent traversal or
// an absolute path).
func cleanJailRel(rel string) (string, bool) {
	if filepath.IsAbs(rel) {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

// resolveInJail confines rel strictly inside root, returning the resolved
// path only when every component below the root exists and none of them —
// final entry included — is a symbolic link. Parent traversal, absolute
// paths, symlink escapes, and missing paths all report false so the caller
// can collapse every rejection into one not-found response.
func resolveInJail(root, rel string) (string, bool) {
	clean, ok := cleanJailRel(rel)
	if !ok {
		return "", false
	}
	root = filepath.Clean(root)
	full := filepath.Join(root, clean)
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", false
	}
	if full == root {
		return root, true
	}
	// Lstat each component below the root: a symlink anywhere along the way
	// (an intermediate directory pointing outside, or the final entry itself)
	// rejects the whole path outright.
	cur := root
	for _, seg := range strings.Split(strings.TrimPrefix(full, root+string(filepath.Separator)), string(filepath.Separator)) {
		cur = filepath.Join(cur, seg)
		info, err := os.Lstat(cur)
		if err != nil {
			return "", false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
	}
	return cur, true
}

// detectFileContentType resolves a file's content type from its extension,
// falling back to a magic-byte sniff of the first 512 bytes (read from f and
// rewound so the caller's stream is untouched).
func detectFileContentType(name string, f *os.File) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "application/octet-stream"
	}
	return http.DetectContentType(head[:n])
}

// isExecutableContentType reports whether the type can execute in a browser
// context and therefore must never render inline on the application origin:
// HTML documents and SVG images.
func isExecutableContentType(ctype string) bool {
	base := strings.TrimSpace(strings.Split(ctype, ";")[0])
	return base == "text/html" || base == "image/svg+xml"
}

// attachmentDisposition builds an attachment Content-Disposition carrying the
// file name, escaping it per the media-type grammar.
func attachmentDisposition(name string) string {
	if d := mime.FormatMediaType("attachment", map[string]string{"filename": name}); d != "" {
		return d
	}
	return "attachment"
}
