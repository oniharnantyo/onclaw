/*
 * Copyright 2025 OnClaw Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the the License for the specific language governing permissions and
 * limitations under the License.
 */

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

// ProjectMountPoint is the absolute prefix a channel's shared project space
// is mounted read-write at (channel-teams D5).
const ProjectMountPoint = "/project"

// WritableMount pairs a model-facing absolute mount prefix with the host
// directory it exposes read-write (channel-teams D5: the channel's shared
// /project). The same escape/symlink rules as the primary root apply.
type WritableMount struct {
	// Mount is the model-facing absolute prefix (e.g. /project).
	Mount string
	// Dir is the existing host directory the prefix maps onto.
	Dir string
}

// extraRoot is one non-primary jail root. Read-only extra roots (the
// workspace skills tree, design D6) carry an empty mount: they extend
// absolute-path reachability for reads only. A root with a mount prefix is
// additionally writable under that prefix — the same extra-roots mechanism
// with the read-write state carried by the prefix (channel-teams D5).
type extraRoot struct {
	dir   string // resolved absolute host directory
	mount string // model-facing absolute prefix; "" = read-only reachability
}

// fsJailedBackend is a filesystem backend that restricts all operations to a root directory.
// It prevents path escape via .., absolute paths, or symlinks pointing outside the jail.
// Optional extra roots extend reachability: read-only ones (design D6: the
// workspace skills tree) for reads by absolute path; mounted ones
// (channel-teams D5: /project) for reads and writes under their prefix.
type fsJailedBackend struct {
	root       string
	rootAbs    string
	extraRoots []extraRoot
	mount      string
	mu         sync.RWMutex
}

// NewFilesystemJail creates a new filesystem jail rooted at the given directory.
// The root directory must exist and be an absolute path.
// All filesystem operations through this backend are restricted to this directory tree.
func NewFilesystemJail(root string) (einofs.Backend, error) {
	return NewFilesystemJailedWithRoots(root)
}

// NewFilesystemJailedWithRoots creates a filesystem jail with a primary root
// (read-write) and optional extra read-only roots. Extra roots that do not
// exist yet are skipped: the workspace skills tree is absent on fresh
// workspaces and must not break agent composition.
func NewFilesystemJailedWithRoots(primary string, extra ...string) (einofs.Backend, error) {
	return NewFilesystemJailedWithMounts(primary, nil, extra...)
}

// NewFilesystemJailedWithMounts creates the jail with read-write mounts on
// top of the primary root and the extra read-only roots. Mount directories
// must already exist (the project space's Ensure materializes them before
// composition) — a mounted root is load-bearing, unlike the optional
// read-only roots, and its absence is a wiring error, not a skip.
func NewFilesystemJailedWithMounts(primary string, mounts []WritableMount, extra ...string) (einofs.Backend, error) {
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

	// Validate the writable mounts first so prefix-shape errors surface
	// before directory I/O errors. prefixes stays parallel to mounts.
	prefixes := make([]string, 0, len(mounts))
	for _, m := range mounts {
		prefix, err := validateMountPrefix(m.Mount, prefixes)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}

	extraRoots := make([]extraRoot, 0, len(mounts)+len(extra))
	for i, m := range mounts {
		absDir, err := filepath.Abs(m.Dir)
		if err != nil {
			return nil, fmt.Errorf("resolve mount %s directory: %w", prefixes[i], err)
		}
		info, err := os.Stat(absDir)
		if err != nil {
			return nil, fmt.Errorf("stat mount %s directory: %w", prefixes[i], err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("mount %s is not a directory: %s", prefixes[i], absDir)
		}
		resolved, err := filepath.EvalSymlinks(absDir)
		if err != nil {
			return nil, fmt.Errorf("resolve symlinks in mount %s: %w", prefixes[i], err)
		}
		extraRoots = append(extraRoots, extraRoot{
			dir:   strings.TrimSuffix(resolved, string(filepath.Separator)),
			mount: prefixes[i],
		})
	}
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
		mount:      DefaultMountPoint,
	}, nil
}

// validateMountPrefix checks one mount prefix: absolute, no "..", not the
// primary mount, and neither nesting nor nested in a previously validated
// mount. It returns the cleaned prefix.
func validateMountPrefix(mount string, prior []string) (string, error) {
	if mount == "" {
		return "", fmt.Errorf("mount prefix cannot be empty")
	}
	cleaned := filepath.Clean(mount)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("mount prefix %q must be an absolute path", mount)
	}
	if cleaned == DefaultMountPoint {
		return "", fmt.Errorf("mount prefix %q shadows the primary mount", mount)
	}
	for _, p := range prior {
		if cleaned == p || strings.HasPrefix(cleaned, p+"/") || strings.HasPrefix(p, cleaned+"/") {
			return "", fmt.Errorf("mount prefix %q overlaps mount %q", mount, p)
		}
	}
	return cleaned, nil
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

// mountedRoot finds the writable mount whose prefix contains userPath and
// returns it with the prefix-relative remainder. ok is false outside every
// mount prefix. Mounted prefixes never overlap each other or the primary
// mount (validated at construction).
func (j *fsJailedBackend) mountedRoot(userPath string) (extraRoot, string, bool) {
	for _, x := range j.extraRoots {
		if x.mount == "" {
			continue
		}
		if userPath == x.mount {
			return x, ".", true
		}
		if rel := strings.TrimPrefix(userPath, x.mount+"/"); rel != userPath && rel != "" {
			return x, rel, true
		}
	}
	return extraRoot{}, "", false
}

// resolvePath resolves a user-provided path for read operations. Relative
// paths resolve under the primary root; absolute paths are accepted when they
// resolve (EvalSymlinks) under the primary or any extra root. Mounted roots
// are additionally reachable at their model-facing prefix. Returns the
// absolute, normalized path within the jail roots.
func (j *fsJailedBackend) resolvePath(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Reject obvious escape attempts
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// Mounted roots ("/project/...") resolve under their host directory with
	// the same containment rules as the primary mount: a read may land in any
	// jail root, an escape in none.
	if x, rel, ok := j.mountedRoot(userPath); ok {
		return j.resolveUnder(x.dir, rel, j.isWithinAnyRoot)
	}

	// Mount-scoped paths ("/workspace/...") resolve under the primary root
	// no matter where the jail lives on the host — the tools contract
	// absolute paths, and this is the mount the model is told about.
	if rel, ok := j.unmount(userPath); ok {
		return j.resolveUnder(j.rootAbs, rel, j.isWithinAnyRoot)
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
// the primary root and writable mounts only, raw host absolute paths
// rejected, read-only extra roots unreachable.
func (j *fsJailedBackend) resolveWritablePath(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Reject obvious escape attempts
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// Mounted roots are writable at their prefix, contained to their own
	// host directory (channel-teams D5: /project is read-write for member
	// agents). Raw host absolute paths stay rejected: read-only extra roots
	// stay unwritable, and the jail must not become writable just because a
	// path exists on the host.
	if x, rel, ok := j.mountedRoot(userPath); ok {
		return j.resolveUnder(x.dir, rel, func(path string) bool {
			return isWithinRoot(x.dir, path)
		})
	}

	// Mount-scoped paths are writable under the primary root.
	if rel, ok := j.unmount(userPath); ok {
		return j.resolveUnder(j.rootAbs, rel, j.isWithinPrimary)
	}

	// Absolute paths are never writable (extra roots are read-only).
	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("absolute paths are not allowed: %q (write under %s instead)", userPath, j.mount)
	}

	return j.resolveUnder(j.rootAbs, userPath, j.isWithinPrimary)
}

// resolveUnder resolves a relative path against the given jail root,
// walking up to the first existing parent for not-yet-created targets, and
// validating resolved symlinks with the given containment predicate. The
// primary root and the writable mounts share it (the read-only extra roots
// are never resolved relatively — no prefix maps onto them).
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

// isWithinAnyRoot checks if a path is within the primary root or any extra root.
func (j *fsJailedBackend) isWithinAnyRoot(path string) bool {
	if j.isWithinPrimary(path) {
		return true
	}
	for _, x := range j.extraRoots {
		if isWithinRoot(x.dir, path) {
			return true
		}
	}
	return false
}

// isWithinPrimary checks if a path is within the primary (writable) root.
func (j *fsJailedBackend) isWithinPrimary(path string) bool {
	return isWithinRoot(j.rootAbs, path)
}

// isWithinJail is retained as an alias of the primary-root check for
// historical call sites; reads use isWithinAnyRoot.
func (j *fsJailedBackend) isWithinJail(path string) bool {
	return j.isWithinPrimary(path)
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
func (j *fsJailedBackend) LsInfo(ctx context.Context, req *einofs.LsInfoRequest) ([]einofs.FileInfo, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

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

// Read reads file content with support for line-based offset and limit.
func (j *fsJailedBackend) Read(ctx context.Context, req *einofs.ReadRequest) (*einofs.FileContent, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	resolved, err := j.resolvePath(req.FilePath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Apply offset and limit
	lines := strings.Split(string(content), "\n")
	if req.Offset > 1 {
		if req.Offset > len(lines) {
			return &einofs.FileContent{Content: ""}, nil
		}
		lines = lines[req.Offset-1:]
	}

	if req.Limit > 0 && req.Limit < len(lines) {
		lines = lines[:req.Limit]
	}

	return &einofs.FileContent{
		Content: strings.Join(lines, "\n"),
	}, nil
}

// GrepRaw searches for content matching the specified pattern in files.
func (j *fsJailedBackend) GrepRaw(ctx context.Context, req *einofs.GrepRequest) ([]einofs.GrepMatch, error) {
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

	// Compile regex pattern
	flags := 0
	if req.CaseInsensitive {
		flags |= regexFlagsCaseInsensitive
	}

	re, err := compileRegex(req.Pattern, flags)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}

	var matches []einofs.GrepMatch
	err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if info.IsDir() {
			return nil
		}

		// Apply glob filter if specified
		if req.Glob != "" {
			matched, err := matchGlob(req.Glob, path, searchPath)
			if err != nil || !matched {
				return nil
			}
		}

		// Read file and search for pattern
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip files we can't read
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			if re.MatchString(line) {
				// Build path relative to jail root
				relPath := strings.TrimPrefix(path, searchPath+"/")
				fullPath := pathPrefix + relPath

				matches = append(matches, einofs.GrepMatch{
					Path:    fullPath,
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
func (j *fsJailedBackend) GlobInfo(ctx context.Context, req *einofs.GlobInfoRequest) ([]einofs.FileInfo, error) {
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

	pattern := filepath.Join(searchPath, req.Pattern)

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob pattern: %w", err)
	}

	var result []einofs.FileInfo
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}

		// Verify the match is within jail
		resolved, err := filepath.EvalSymlinks(match)
		if err != nil || !j.isWithinAnyRoot(resolved) {
			continue
		}

		// Build path relative to jail root
		relPath := strings.TrimPrefix(match, searchPath+"/")
		fullPath := pathPrefix + relPath

		result = append(result, einofs.FileInfo{
			Path:       fullPath,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: timeFormat(info.ModTime()),
		})
	}

	return result, nil
}

// Write creates or updates file content.
func (j *fsJailedBackend) Write(ctx context.Context, req *einofs.WriteRequest) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	resolved, err := j.resolveWritablePath(req.FilePath)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(resolved)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
	}

	if err := os.WriteFile(resolved, []byte(req.Content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Edit replaces string occurrences in a file.
func (j *fsJailedBackend) Edit(ctx context.Context, req *einofs.EditRequest) error {
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

	// Check if old string exists
	if !strings.Contains(contentStr, req.OldString) {
		return fmt.Errorf("old string not found in file")
	}

	// Count occurrences for ReplaceAll=false
	count := strings.Count(contentStr, req.OldString)
	if !req.ReplaceAll && count != 1 {
		return fmt.Errorf("old string appears %d times; use ReplaceAll=true or provide a unique occurrence", count)
	}

	// Replace
	var newContent string
	if req.ReplaceAll {
		newContent = strings.ReplaceAll(contentStr, req.OldString, req.NewString)
	} else {
		newContent = strings.Replace(contentStr, req.OldString, req.NewString, 1)
	}

	if err := os.WriteFile(resolved, []byte(newContent), 0644); err != nil {
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
