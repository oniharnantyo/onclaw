package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/cloudwego/eino/adk/filesystem"
)

// fsBackend implements filesystem.Backend over the real OS, confining every
// path to the agent workspace and redacting secret patterns in returned
// content. It is the onclaw-owned seam the Eino filesystem middleware calls
// into; the middleware owns the tool surface (schema/name/prompt), this owns
// the semantics.
type fsBackend struct {
	workspace string
}

// NewFSBackend constructs a Backend confined to workspace.
func NewFSBackend(workspace string) filesystem.Backend {
	return &fsBackend{workspace: workspace}
}

// LsInfo lists the immediate children of the given directory (default:
// workspace root). Paths are returned as child names, matching `ls` output.
func (b *fsBackend) LsInfo(_ context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	dir := req.Path
	if dir == "" {
		dir = "."
	}
	absDir, err := ValidatePath(b.workspace, dir)
	if err != nil {
		return nil, wrapSentinel(ErrPathOutsideWorkspace, dir)
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, mapOSError(fmt.Errorf("failed to list directory: %w", err), dir)
	}
	result := make([]filesystem.FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, filesystem.FileInfo{
			Path:       e.Name(),
			IsDir:      e.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339),
		})
	}
	return result, nil
}

// Read returns a line-sliced view of the file (1-based offset/limit). The
// middleware applies line numbering; this returns the raw slice and redacts
// any secret patterns.
func (b *fsBackend) Read(_ context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	absPath, err := ValidatePath(b.workspace, req.FilePath)
	if err != nil {
		return nil, wrapSentinel(ErrPathOutsideWorkspace, req.FilePath)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, mapOSError(fmt.Errorf("failed to read file: %w", err), req.FilePath)
	}

	offset := req.Offset - 1
	if offset < 0 {
		offset = 0
	}
	limit := req.Limit

	lines := strings.Split(string(data), "\n")
	if offset >= len(lines) {
		return &filesystem.FileContent{}, nil
	}
	if limit <= 0 {
		limit = len(lines)
	}
	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	sliced := strings.Join(lines[offset:end], "\n")
	return &filesystem.FileContent{Content: Redact(sliced)}, nil
}

// GrepRaw searches file contents under req.Path (default workspace) for the
// pattern, honouring Glob/FileType filtering and context-line requests, and
// redacting secret patterns in matched content.
func (b *fsBackend) GrepRaw(_ context.Context, req *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	if req.Pattern == "" {
		return nil, wrapSentinel(ErrEmptyPattern, req.Pattern)
	}
	pattern := req.Pattern
	if req.CaseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, wrapSentinel(ErrInvalidRegex, pattern)
	}

	root := req.Path
	if root == "" {
		root = b.workspace
	}
	absRoot, err := ValidatePath(b.workspace, root)
	if err != nil {
		return nil, wrapSentinel(ErrPathOutsideWorkspace, root)
	}

	candidates, err := b.grepCandidates(absRoot, req.Glob, req.FileType)
	if err != nil {
		return nil, err
	}

	var matches []filesystem.GrepMatch
	for _, f := range candidates {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rel, relErr := filepath.Rel(b.workspace, f)
		if relErr != nil {
			rel = f
		}
		content := string(data)
		if req.EnableMultiline {
			matches = append(matches, findMultilineMatches(rel, content, re)...)
		} else {
			matches = append(matches, findSingleLineMatches(rel, content, re)...)
		}
	}

	if req.BeforeLines > 0 || req.AfterLines > 0 {
		matches = b.applyGrepContext(matches, req.BeforeLines, req.AfterLines)
	}

	for i := range matches {
		matches[i].Content = Redact(matches[i].Content)
	}
	return matches, nil
}

// GlobInfo returns file infos whose path matches the glob pattern under
// req.Path (default workspace), returned as workspace-relative paths.
func (b *fsBackend) GlobInfo(_ context.Context, req *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	root := req.Path
	if root == "" {
		root = b.workspace
	}
	absRoot, err := ValidatePath(b.workspace, root)
	if err != nil {
		return nil, wrapSentinel(ErrPathOutsideWorkspace, root)
	}
	files, err := b.walkFiles(absRoot)
	if err != nil {
		return nil, err
	}
	var result []filesystem.FileInfo
	for _, f := range files {
		rel, relErr := filepath.Rel(b.workspace, f)
		if relErr != nil {
			rel = f
		}
		ok, mErr := doublestar.Match(req.Pattern, rel)
		if mErr != nil {
			return nil, wrapSentinel(ErrInvalidGlob, req.Pattern)
		}
		if !ok {
			continue
		}
		info, infoErr := os.Stat(f)
		if infoErr != nil {
			continue
		}
		result = append(result, filesystem.FileInfo{
			Path:       rel,
			IsDir:      info.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Format(time.RFC3339),
		})
	}
	return result, nil
}

// Write creates or overwrites a file, creating parent directories.
func (b *fsBackend) Write(_ context.Context, req *filesystem.WriteRequest) error {
	absPath, err := ValidatePath(b.workspace, req.FilePath)
	if err != nil {
		return wrapSentinel(ErrPathOutsideWorkspace, req.FilePath)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return mapOSError(fmt.Errorf("failed to create directories: %w", err), req.FilePath)
	}
	if err := os.WriteFile(absPath, []byte(req.Content), 0644); err != nil {
		return mapOSError(fmt.Errorf("failed to write file: %w", err), req.FilePath)
	}
	return nil
}

// Edit replaces string occurrences in a file, enforcing exact-match semantics
// when ReplaceAll is false.
func (b *fsBackend) Edit(_ context.Context, req *filesystem.EditRequest) error {
	absPath, err := ValidatePath(b.workspace, req.FilePath)
	if err != nil {
		return wrapSentinel(ErrPathOutsideWorkspace, req.FilePath)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return mapOSError(fmt.Errorf("failed to read file: %w", err), req.FilePath)
	}
	content := string(data)

	if req.OldString == "" {
		return wrapSentinel(ErrEditOldStringMissing, req.FilePath)
	}
	if !strings.Contains(content, req.OldString) {
		return wrapSentinel(ErrEditOldStringMissing, req.FilePath)
	}
	if !req.ReplaceAll {
		first := strings.Index(content, req.OldString)
		if first != -1 && strings.Contains(content[first+len(req.OldString):], req.OldString) {
			return wrapSentinel(ErrEditNotUnique, req.FilePath)
		}
	}

	var newContent string
	if req.ReplaceAll {
		newContent = strings.ReplaceAll(content, req.OldString, req.NewString)
	} else {
		newContent = strings.Replace(content, req.OldString, req.NewString, 1)
	}
	if err := os.WriteFile(absPath, []byte(newContent), 0644); err != nil {
		return mapOSError(fmt.Errorf("failed to write file: %w", err), req.FilePath)
	}
	return nil
}

// grepCandidates returns regular files under absRoot that pass glob and
// filetype filters.
func (b *fsBackend) grepCandidates(absRoot, glob, fileType string) ([]string, error) {
	files, err := b.walkFiles(absRoot)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if glob != "" && !matchGlob(f, absRoot, glob) {
			continue
		}
		if fileType != "" && !matchFileType(strings.TrimPrefix(filepath.Ext(f), "."), fileType) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// walkFiles returns regular files under root. A non-directory root yields just
// itself; a missing root yields no candidates.
func (b *fsBackend) walkFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// applyGrepContext expands matches with BeforeLines/AfterLines of surrounding
// context from each file.
func (b *fsBackend) applyGrepContext(matches []filesystem.GrepMatch, before, after int) []filesystem.GrepMatch {
	if len(matches) == 0 {
		return matches
	}
	byFile := make(map[string][]filesystem.GrepMatch)
	var order []string
	for _, m := range matches {
		if _, ok := byFile[m.Path]; !ok {
			order = append(order, m.Path)
		}
		byFile[m.Path] = append(byFile[m.Path], m)
	}

	var out []filesystem.GrepMatch
	for _, p := range order {
		abs, err := ValidatePath(b.workspace, p)
		if err != nil {
			out = append(out, byFile[p]...)
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			out = append(out, byFile[p]...)
			continue
		}
		lines := strings.Split(string(data), "\n")
		seen := make(map[int]bool)
		for _, m := range byFile[p] {
			start := m.Line - before
			if start < 1 {
				start = 1
			}
			end := m.Line + after
			if end > len(lines) {
				end = len(lines)
			}
			for ln := start; ln <= end; ln++ {
				if !seen[ln] {
					seen[ln] = true
					out = append(out, filesystem.GrepMatch{Path: p, Line: ln, Content: lines[ln-1]})
				}
			}
		}
	}
	return out
}

func findSingleLineMatches(path, content string, re *regexp.Regexp) []filesystem.GrepMatch {
	var out []filesystem.GrepMatch
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if re.MatchString(line) {
			out = append(out, filesystem.GrepMatch{Path: path, Line: i + 1, Content: line})
		}
	}
	return out
}

func findMultilineMatches(path, content string, re *regexp.Regexp) []filesystem.GrepMatch {
	var out []filesystem.GrepMatch
	matches := re.FindAllStringIndex(content, -1)
	lines := strings.Split(content, "\n")
	for _, m := range matches {
		startLine := 1 + strings.Count(content[:m[0]], "\n")
		endLine := 1 + strings.Count(content[:m[1]], "\n")
		for ln := startLine; ln <= endLine && ln <= len(lines); ln++ {
			out = append(out, filesystem.GrepMatch{Path: path, Line: ln, Content: lines[ln-1]})
		}
	}
	return out
}

func matchGlob(absFile, absRoot, glob string) bool {
	rel := strings.TrimPrefix(absFile, absRoot)
	rel = strings.TrimPrefix(rel, string(os.PathSeparator))
	mp := rel
	if !strings.Contains(glob, "/") && !strings.Contains(glob, "**") {
		mp = filepath.Base(absFile)
	}
	ok, err := doublestar.Match(glob, mp)
	if err != nil {
		return false
	}
	return ok
}

func matchFileType(ext, fileType string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	fileType = strings.ToLower(strings.TrimSpace(fileType))
	if ext == fileType {
		return true
	}
	if exts, ok := fsFileTypeMap[fileType]; ok {
		for _, e := range exts {
			if e == ext {
				return true
			}
		}
	}
	return false
}

// fsFileTypeMap maps a rg-style file type to its extensions.
var fsFileTypeMap = map[string][]string{
	"go":         {"go"},
	"python":     {"py", "pyi"},
	"py":         {"py", "pyi"},
	"js":         {"js", "jsx", "mjs", "cjs"},
	"javascript": {"js", "jsx", "mjs", "cjs"},
	"ts":         {"ts", "tsx", "mts", "cts"},
	"typescript": {"ts", "tsx", "mts", "cts"},
	"rust":       {"rs"},
	"c":          {"c", "h"},
	"cpp":        {"cpp", "cc", "cxx", "hpp", "hxx"},
	"java":       {"java"},
	"ruby":       {"rb"},
	"sh":         {"sh", "bash", "zsh"},
	"shell":      {"sh", "bash", "zsh"},
	"yaml":       {"yaml", "yml"},
	"json":       {"json"},
	"markdown":   {"md", "markdown", "mdx"},
	"md":         {"md", "markdown", "mdx"},
	"html":       {"html", "htm"},
	"css":        {"css", "scss", "sass"},
	"toml":       {"toml"},
	"sql":        {"sql"},
}
