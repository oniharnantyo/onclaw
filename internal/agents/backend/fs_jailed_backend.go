package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

// DefaultMountPoint is the absolute prefix the model is told its workspace is
// mounted at. The fs tools contract absolute paths (the middleware's
// DeepAgents-style container mount), so the jail translates paths under this
// prefix into the primary root instead of rejecting them as host paths.
const DefaultMountPoint = "/workspace"

// KnowledgeMountPoint is the absolute prefix the model is told the read-only
// knowledge corpus is mounted at (design D3). Paths under it resolve
// read-only against the knowledge roots as /knowledge/global and
// /knowledge/agent; the mount root itself is virtual and write operations
// never reach it.
const KnowledgeMountPoint = "/knowledge"

// Knowledge scope names presented as subdirectories of KnowledgeMountPoint.
const (
	knowledgeScopeGlobal = "global"
	knowledgeScopeAgent  = "agent"
)

// extraRoot is one non-primary jail root: a read-only extension of absolute
// path reachability (the workspace-external skills tree, design D6). Reads
// may land in any root; writes stay confined to the primary root.
type extraRoot struct {
	dir string // resolved absolute host directory
}

// knowledgeRoot is one scope of the read-only knowledge corpus mount
// (design D3): a named subdirectory of KnowledgeMountPoint backed by a host
// directory. A root with an empty dir was configured but is absent on disk;
// it is neither listed nor reachable, and construction still succeeds.
type knowledgeRoot struct {
	name string // scope name under /knowledge ("global" or "agent")
	dir  string // resolved absolute host directory ("" when absent)
}

// fsJailedBackend is a filesystem backend that restricts all operations to a
// root directory. It prevents path escape via .., absolute paths, or
// symlinks pointing outside the jail. Optional extra roots extend read-only
// reachability by absolute path (design D6); writes always resolve against
// the primary root alone. An optional second, read-only virtual mount
// (KnowledgeMountPoint) exposes the knowledge corpus scopes read-only
// (design D3).
type fsJailedBackend struct {
	root       string
	rootAbs    string
	extraRoots []extraRoot
	knowledge  []knowledgeRoot
	mount      string
	mu         sync.RWMutex
}

var _ einofs.Backend = (*fsJailedBackend)(nil)
var _ einofs.MultiModalReader = (*fsJailedBackend)(nil)

// NewFilesystemJail creates a new filesystem jail rooted at the given directory.
// The root directory must exist and be an absolute path.
// All filesystem operations through this backend are restricted to this directory tree.
func NewFilesystemJail(root string) (einofs.Backend, error) {
	return NewFilesystemJailedWithRoots(root)
}

// NewFilesystemJailedWithRoots creates a filesystem jail with a primary root
// (read-write) and optional extra read-only roots. Extra roots that do not
// exist yet are skipped: the skills tree is absent on fresh workspaces and
// must not break agent composition.
func NewFilesystemJailedWithRoots(primary string, extra ...string) (einofs.Backend, error) {
	return newFilesystemJailedBackend(primary, extra, nil)
}

// KnowledgeRoots names the two read-only knowledge corpus directories exposed
// under /knowledge.
type KnowledgeRoots struct {
	GlobalDir string // presented as /knowledge/global
	AgentDir  string // presented as /knowledge/agent
}

// NewFilesystemJailedWithKnowledge creates a filesystem jail like
// NewFilesystemJailedWithRoots and additionally exposes the knowledge corpus
// as a second, read-only virtual mount at KnowledgeMountPoint: GlobalDir is
// presented as /knowledge/global and AgentDir as /knowledge/agent. A
// knowledge directory that does not exist (or is not a directory) is skipped:
// its scope is neither listed under /knowledge nor reachable through it, and
// construction still succeeds.
func NewFilesystemJailedWithKnowledge(primary string, knowledge KnowledgeRoots, extra ...string) (einofs.Backend, error) {
	roots := make([]knowledgeRoot, 0, 2)
	for _, k := range []knowledgeRoot{
		{name: knowledgeScopeGlobal, dir: knowledge.GlobalDir},
		{name: knowledgeScopeAgent, dir: knowledge.AgentDir},
	} {
		if k.dir == "" {
			continue
		}
		absDir, err := filepath.Abs(k.dir)
		if err != nil {
			return nil, fmt.Errorf("resolve knowledge %s directory: %w", k.name, err)
		}
		info, err := os.Stat(absDir)
		if err != nil {
			if os.IsNotExist(err) {
				// Optional root: absent on fresh installs. Keep the named
				// entry with an empty dir so the scope reports not-found
				// instead of unknown.
				roots = append(roots, knowledgeRoot{name: k.name})
				continue
			}
			return nil, fmt.Errorf("stat knowledge %s directory: %w", k.name, err)
		}
		if !info.IsDir() {
			roots = append(roots, knowledgeRoot{name: k.name})
			continue
		}
		resolved, err := filepath.EvalSymlinks(absDir)
		if err != nil {
			return nil, fmt.Errorf("resolve symlinks in knowledge %s directory: %w", k.name, err)
		}
		roots = append(roots, knowledgeRoot{
			name: k.name,
			dir:  strings.TrimSuffix(resolved, string(filepath.Separator)),
		})
	}
	return newFilesystemJailedBackend(primary, extra, roots)
}

// newFilesystemJailedBackend validates and normalizes the primary and extra
// roots, then builds the jailed backend over them plus the already-normalized
// knowledge roots.
func newFilesystemJailedBackend(primary string, extra []string, knowledge []knowledgeRoot) (einofs.Backend, error) {
	if primary == "" {
		return nil, fmt.Errorf("root directory cannot be empty")
	}

	// Convert to absolute path
	abs, err := filepath.Abs(primary)
	if err != nil {
		return nil, fmt.Errorf("resolve root path: %w", err)
	}

	// Ensure root exists
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat root directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", abs)
	}

	// Resolve symlinks in the root path to get canonical form
	resolvedRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks in root: %w", err)
	}

	// Ensure root ends without trailing separator for consistent joins
	resolvedRoot = strings.TrimSuffix(resolvedRoot, string(filepath.Separator))

	extraRoots := make([]extraRoot, 0, len(extra))
	for _, root := range extra {
		if root == "" {
			continue
		}
		absExtra, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve extra root path: %w", err)
		}
		info, err := os.Stat(absExtra)
		if err != nil {
			if os.IsNotExist(err) {
				continue // optional root: absent on fresh workspaces
			}
			return nil, fmt.Errorf("stat extra root directory: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("extra root is not a directory: %s", absExtra)
		}
		resolvedExtra, err := filepath.EvalSymlinks(absExtra)
		if err != nil {
			return nil, fmt.Errorf("resolve symlinks in extra root: %w", err)
		}
		extraRoots = append(extraRoots, extraRoot{
			dir: strings.TrimSuffix(resolvedExtra, string(filepath.Separator)),
		})
	}

	return &fsJailedBackend{
		root:       primary,
		rootAbs:    resolvedRoot,
		extraRoots: extraRoots,
		knowledge:  knowledge,
		mount:      DefaultMountPoint,
	}, nil
}

// unmount maps a mount-scoped path ("/workspace/notes.md") to a root-relative
// path ("notes.md"). ok is false for paths outside the mount prefix.
func (j *fsJailedBackend) unmount(userPath string) (string, bool) {
	if userPath == j.mount {
		return ".", true
	}
	if rel := strings.TrimPrefix(userPath, j.mount+"/"); rel != userPath && rel != "" {
		return rel, true
	}
	return "", false
}

// unmountKnowledge maps a knowledge-mount path ("/knowledge/global/doc.pdf")
// to its mount-relative path ("global/doc.pdf"). ok is false for paths
// outside the knowledge mount prefix.
func (j *fsJailedBackend) unmountKnowledge(userPath string) (string, bool) {
	if userPath == KnowledgeMountPoint {
		return ".", true
	}
	if rel := strings.TrimPrefix(userPath, KnowledgeMountPoint+"/"); rel != userPath && rel != "" {
		return rel, true
	}
	return "", false
}

// resolveKnowledgePath resolves a knowledge-mount-relative path
// ("global/doc.pdf", or "." for the mount root itself) against the knowledge
// corpus roots. The mount root is virtual — it has no host directory of its
// own (its host parent holds other agents' scopes), so only LsInfo addresses
// it, synthesizing the listing; every other operation on it fails. Scope
// paths resolve read-only under their scope's root, with symlinks required
// to stay within the knowledge roots.
func (j *fsJailedBackend) resolveKnowledgePath(rel string) (string, error) {
	if rel == "." {
		return "", fmt.Errorf("%s is a virtual root; address %s/global or %s/agent", KnowledgeMountPoint, KnowledgeMountPoint, KnowledgeMountPoint)
	}

	scope, rest, _ := strings.Cut(rel, "/")
	for _, k := range j.knowledge {
		if k.name != scope {
			continue
		}
		if k.dir == "" {
			return "", fmt.Errorf("knowledge scope %q not found (its directory is absent)", scope)
		}
		if rest == "" {
			rest = "."
		}
		return j.resolveUnder(k.dir, rest, j.isWithinAnyKnowledgeRoot)
	}
	return "", fmt.Errorf("unknown knowledge scope %q; address %s/global or %s/agent", scope, KnowledgeMountPoint, KnowledgeMountPoint)
}

// resolvePath resolves a user-provided path for read operations. Relative
// paths resolve under the primary root; absolute paths are accepted when they
// resolve (EvalSymlinks) under the primary or any extra root. Mount-scoped
// paths ("/workspace/...") resolve under the primary root no matter where the
// jail lives on the host, and knowledge-mount paths ("/knowledge/...") resolve
// read-only against the knowledge corpus roots. Returns the absolute,
// normalized path within the jail roots.
func (j *fsJailedBackend) resolvePath(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Reject obvious escape attempts
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// Mount-scoped paths ("/workspace/...") resolve under the primary root
	// no matter where the jail lives on the host — the tools contract
	// absolute paths, and this is the mount the model is told about.
	if rel, ok := j.unmount(userPath); ok {
		return j.resolveUnder(j.rootAbs, rel, j.isWithinAnyRoot)
	}

	// Knowledge-mount paths ("/knowledge/...") resolve read-only against the
	// knowledge corpus roots (design D3); the mount root itself is virtual
	// and only LsInfo addresses it.
	if rel, ok := j.unmountKnowledge(userPath); ok {
		return j.resolveKnowledgePath(rel)
	}

	// Absolute paths: resolve symlinks and accept when under any root.
	if filepath.IsAbs(userPath) {
		cleaned := filepath.Clean(userPath)
		resolved, err := filepath.EvalSymlinks(cleaned)
		if err != nil {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		if !j.isWithinAnyRoot(resolved) {
			return "", fmt.Errorf("absolute path is outside jail roots: %q resolves to %q", userPath, resolved)
		}
		return resolved, nil
	}

	return j.resolveUnder(j.rootAbs, userPath, j.isWithinAnyRoot)
}

// resolveWritablePath resolves a user-provided path for write operations:
// relative paths and mount-scoped paths under the primary root only, raw
// host absolute paths rejected (the read-only knowledge mount included —
// its paths are absolute and outside the primary root), read-only extra
// roots unreachable.
func (j *fsJailedBackend) resolveWritablePath(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Reject obvious escape attempts
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// Mount-scoped paths are writable under the primary root; raw host
	// absolute paths never are (extra roots stay read-only, and the jail
	// must not become writable just because a path exists on the host).
	if rel, ok := j.unmount(userPath); ok {
		return j.resolveUnder(j.rootAbs, rel, j.isWithinPrimary)
	}

	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("absolute paths are not allowed: %q (write under %s instead)", userPath, j.mount)
	}

	return j.resolveUnder(j.rootAbs, userPath, j.isWithinPrimary)
}

// resolveUnder resolves a relative path against the given jail root,
// walking up to the first existing parent for not-yet-created targets, and
// validating resolved symlinks with the given containment predicate. The
// primary root and the read-only extra roots share it.
func (j *fsJailedBackend) resolveUnder(rootAbs, userPath string, within func(string) bool) (string, error) {
	// Clean the path and join with root
	cleanPath := filepath.Clean(userPath)
	fullPath := filepath.Join(rootAbs, cleanPath)

	// Resolve symlinks to their target
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// If file doesn't exist yet, check parent directories recursively
	if os.IsNotExist(err) {
		// Walk up the directory tree to find the first existing parent
		currentPath := fullPath
		for {
			parentDir := filepath.Dir(currentPath)

			// If we've reached the root directory (or empty), stop
			if parentDir == currentPath {
				// Check if the root itself is within jail
				if !within(currentPath) {
					return "", fmt.Errorf("path is outside jail: %q", userPath)
				}
				return fullPath, nil
			}

			// Check if parent directory exists
			resolvedParent, err := filepath.EvalSymlinks(parentDir)
			if err != nil {
				// Parent doesn't exist or has permission issues, continue up the tree
				currentPath = parentDir
				continue
			}

			// Found an existing parent - verify it's within jail
			if !within(resolvedParent) {
				return "", fmt.Errorf("parent directory is outside jail: %q", userPath)
			}

			// For new files, return the full path without symlink resolution
			// (symlinks will be checked on actual operations)
			return fullPath, nil
		}
	}

	// Verify resolved path is within jail
	if !within(resolved) {
		return "", fmt.Errorf("path escapes jail: %q resolves to %q", userPath, resolved)
	}

	return resolved, nil
}

// isWithinAnyRoot checks if a path is within the primary root, any extra
// root, or any knowledge corpus root (reads only — writes use
// isWithinPrimary).
func (j *fsJailedBackend) isWithinAnyRoot(path string) bool {
	if j.isWithinPrimary(path) {
		return true
	}
	for _, x := range j.extraRoots {
		if isWithinRoot(x.dir, path) {
			return true
		}
	}
	for _, k := range j.knowledge {
		if k.dir != "" && isWithinRoot(k.dir, path) {
			return true
		}
	}
	return false
}

// isWithinAnyKnowledgeRoot checks if a path is within one of the knowledge
// corpus roots — the only host directories the /knowledge mount may reach.
func (j *fsJailedBackend) isWithinAnyKnowledgeRoot(path string) bool {
	for _, k := range j.knowledge {
		if k.dir != "" && isWithinRoot(k.dir, path) {
			return true
		}
	}
	return false
}

// isWithinPrimary checks if a path is within the primary (writable) root.
func (j *fsJailedBackend) isWithinPrimary(path string) bool {
	return isWithinRoot(j.rootAbs, path)
}

// isWithinRoot checks if a path is within the given resolved root.
func isWithinRoot(root, path string) bool {
	// Normalize paths for comparison
	path = filepath.Clean(path)

	// If path equals root, it's within
	if path == root {
		return true
	}

	// Check if path starts with root + separator
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// LsInfo lists file information under the given path.
func (j *fsJailedBackend) LsInfo(_ context.Context, req *einofs.LsInfoRequest) ([]einofs.FileInfo, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	// The knowledge mount root is virtual: it must never resolve to a host
	// directory (its host parent holds other agents' scopes), so its listing
	// is synthesized from the configured knowledge roots.
	if rel, ok := j.unmountKnowledge(req.Path); ok && rel == "." {
		return j.lsKnowledgeRoot(), nil
	}

	resolved, err := j.resolvePath(req.Path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var result []einofs.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		result = append(result, einofs.FileInfo{
			Path:       entry.Name(),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339Nano),
		})
	}

	return result, nil
}

// lsKnowledgeRoot synthesizes the /knowledge listing: one directory entry per
// knowledge scope whose host directory exists. Absent scopes are omitted.
func (j *fsJailedBackend) lsKnowledgeRoot() []einofs.FileInfo {
	var result []einofs.FileInfo
	for _, k := range j.knowledge {
		if k.dir == "" {
			continue
		}
		entry := einofs.FileInfo{Path: k.name, IsDir: true}
		if info, err := os.Stat(k.dir); err == nil {
			entry.Size = info.Size()
			entry.ModifiedAt = info.ModTime().Format(time.RFC3339Nano)
		}
		result = append(result, entry)
	}
	return result
}

// Read reads file content with support for line-based offset and limit.
// Office documents (DOCX, PPTX, XLSX) are presented as structured views
// instead of raw bytes (design D4): the file is never modified and the view
// is paginated like a text read.
func (j *fsJailedBackend) Read(_ context.Context, req *einofs.ReadRequest) (*einofs.FileContent, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	resolved, err := j.resolvePath(req.FilePath)
	if err != nil {
		return nil, err
	}

	if view, ok, err := renderOfficeView(resolved, req.Offset, req.Limit); ok {
		if err != nil {
			return nil, err
		}
		return &einofs.FileContent{Content: view}, nil
	}

	content, err := readTextWindow(resolved, req.Offset, req.Limit)
	if err != nil {
		return nil, err
	}
	return &einofs.FileContent{Content: content}, nil
}

// readTextWindow reads a file as text and applies the 1-based line offset and
// the line limit.
func readTextWindow(path string, offset, limit int) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	if offset > 1 {
		if offset > len(lines) {
			return "", nil
		}
		lines = lines[offset-1:]
	}

	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}

	return strings.Join(lines, "\n"), nil
}

// inlineMIMETypes maps the binary extensions MultiModalRead serves as inline
// parts to their MIME type.
var inlineMIMETypes = map[string]einofs.FileContentPart{
	".png":  {Type: einofs.FileContentPartTypeImage, MIMEType: "image/png"},
	".jpg":  {Type: einofs.FileContentPartTypeImage, MIMEType: "image/jpeg"},
	".jpeg": {Type: einofs.FileContentPartTypeImage, MIMEType: "image/jpeg"},
	".gif":  {Type: einofs.FileContentPartTypeImage, MIMEType: "image/gif"},
	".webp": {Type: einofs.FileContentPartTypeImage, MIMEType: "image/webp"},
	".pdf":  {Type: einofs.FileContentPartTypePDF, MIMEType: "application/pdf"},
}

// maxInlinePartBytes bounds one inline binary part: the bytes ride the model
// request, so a larger file is refused rather than silently dropped.
const maxInlinePartBytes = 20 << 20

// MultiModalRead reads file content supporting multimodal results. Images and
// PDFs come back as one inline binary part for the model to consume directly
// — there is no server-side rendering, so a PDF page range is not honored and
// the whole document rides along. Office documents (DOCX, PPTX, XLSX) come
// back as their structured text view (design D4) like Read. Everything else
// reads as text like Read.
func (j *fsJailedBackend) MultiModalRead(_ context.Context, req *einofs.MultiModalReadRequest) (*einofs.MultiFileContent, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	resolved, err := j.resolvePath(req.FilePath)
	if err != nil {
		return nil, err
	}

	if view, ok, err := renderOfficeView(resolved, req.Offset, req.Limit); ok {
		if err != nil {
			return nil, err
		}
		return &einofs.MultiFileContent{FileContent: &einofs.FileContent{Content: view}}, nil
	}

	if part, ok := inlineMIMETypes[strings.ToLower(filepath.Ext(resolved))]; ok {
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat file: %w", err)
		}
		if info.Size() > maxInlinePartBytes {
			return nil, fmt.Errorf("file is %d bytes; inline %s parts are capped at %d bytes", info.Size(), part.Type, maxInlinePartBytes)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		part.Data = data
		return &einofs.MultiFileContent{Parts: []einofs.FileContentPart{part}}, nil
	}

	content, err := readTextWindow(resolved, req.Offset, req.Limit)
	if err != nil {
		return nil, err
	}
	return &einofs.MultiFileContent{FileContent: &einofs.FileContent{Content: content}}, nil
}

// GrepRaw searches for content matching the specified pattern in files.
func (j *fsJailedBackend) GrepRaw(_ context.Context, req *einofs.GrepRequest) ([]einofs.GrepMatch, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	searchPath := j.rootAbs
	var pathPrefix string
	if req.Path != "" {
		resolved, err := j.resolvePath(req.Path)
		if err != nil {
			return nil, err
		}
		searchPath = resolved
		pathPrefix = req.Path + "/"
	}

	flags := 0
	if req.CaseInsensitive {
		flags |= regexFlagsCaseInsensitive
	}

	re, err := compileRegex(req.Pattern, flags)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}

	var matches []einofs.GrepMatch
	_ = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if info.IsDir() {
			return nil
		}

		if req.Glob != "" {
			matched, err := matchGlob(req.Glob, path, searchPath)
			if err != nil || !matched {
				return nil
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			if re.MatchString(line) {
				relPath := strings.TrimPrefix(path, searchPath+"/")
				matches = append(matches, einofs.GrepMatch{
					Path:    pathPrefix + relPath,
					Line:    lineNum + 1,
					Content: strings.TrimSpace(line),
				})
			}
		}

		return nil
	})

	return matches, nil
}

// GlobInfo returns file information matching the glob pattern.
func (j *fsJailedBackend) GlobInfo(_ context.Context, req *einofs.GlobInfoRequest) ([]einofs.FileInfo, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	searchPath := j.rootAbs
	var pathPrefix string
	if req.Path != "" {
		resolved, err := j.resolvePath(req.Path)
		if err != nil {
			return nil, err
		}
		searchPath = resolved
		pathPrefix = req.Path + "/"
	}

	matches, err := filepath.Glob(filepath.Join(searchPath, req.Pattern))
	if err != nil {
		return nil, fmt.Errorf("glob pattern: %w", err)
	}

	var result []einofs.FileInfo
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}

		resolved, err := filepath.EvalSymlinks(match)
		if err != nil || !j.isWithinAnyRoot(resolved) {
			continue
		}

		relPath := strings.TrimPrefix(match, searchPath+"/")
		result = append(result, einofs.FileInfo{
			Path:       pathPrefix + relPath,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: timeFormat(info.ModTime()),
		})
	}

	return result, nil
}

// Write creates or updates file content.
func (j *fsJailedBackend) Write(_ context.Context, req *einofs.WriteRequest) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	resolved, err := j.resolveWritablePath(req.FilePath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	if err := os.WriteFile(resolved, []byte(req.Content), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Edit replaces string occurrences in a file.
func (j *fsJailedBackend) Edit(_ context.Context, req *einofs.EditRequest) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	resolved, err := j.resolveWritablePath(req.FilePath)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, req.OldString) {
		return fmt.Errorf("old string not found in file")
	}

	count := strings.Count(contentStr, req.OldString)
	if !req.ReplaceAll && count != 1 {
		return fmt.Errorf("old string appears %d times; use ReplaceAll=true or provide a unique occurrence", count)
	}

	var newContent string
	if req.ReplaceAll {
		newContent = strings.ReplaceAll(contentStr, req.OldString, req.NewString)
	} else {
		newContent = strings.Replace(contentStr, req.OldString, req.NewString, 1)
	}

	if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Helper functions

const (
	regexFlagsCaseInsensitive = 1 << iota
)

func compileRegex(pattern string, flags int) (*regexp.Regexp, error) {
	if flags&regexFlagsCaseInsensitive != 0 {
		return regexp.Compile("(?i)" + pattern)
	}
	return regexp.Compile(pattern)
}

func matchGlob(globPattern, filePath, basePath string) (bool, error) {
	// Extract relative path from base
	relPath := strings.TrimPrefix(filePath, basePath+"/")
	// Use doublestar for more sophisticated glob matching if needed
	matched, err := filepath.Match(globPattern, filepath.Base(relPath))
	if err != nil {
		return false, err
	}
	return matched, nil
}

func timeFormat(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}
