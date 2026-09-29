package tools

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
	"golang.org/x/net/html"

	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// NameDocumentRead is the dotted capability name registered in the tool registry.
const NameDocumentRead = "document.read"

// MaxDocumentReadOutputBytes is the maximum allowed markdown output length (200 KB)
// before truncation occurs, aligned with the inline-text 200 KB ceiling.
const MaxDocumentReadOutputBytes = 200 << 10 // 204800 bytes

// defaultDocumentReadTimeout is the bounded conversion deadline applied to each
// in-process conversion (design.md D2): converters must honor context
// cancellation and never run unbounded.
const defaultDocumentReadTimeout = 30 * time.Second

// truncationDocumentReadNotice is appended when converted markdown exceeds MaxDocumentReadOutputBytes.
const truncationDocumentReadNotice = "\n\n[Output truncated at 200 KB — use filesystem or grep tools if you need to search specific sections]"

// documentConverter converts one on-disk document into markdown. Implementations
// must honor ctx cancellation and treat the file at path as read-only input.
type documentConverter func(ctx context.Context, path string) (string, error)

// defaultDocumentConverters is the in-process converter registry (design.md D2):
// a pure-Go converter per supported extension, registered like any other
// implementation of the same seam — a new format is a new registration.
func defaultDocumentConverters() map[string]documentConverter {
	return map[string]documentConverter{
		".pdf":  convertPDF,
		".xlsx": convertXLSX,
		".docx": convertDOCX,
		".pptx": convertPPTX,
		".html": convertHTML,
		".htm":  convertHTML,
		".csv":  convertDelimited(','),
		".tsv":  convertDelimited('\t'),
		".md":   convertPlainText,
		".txt":  convertPlainText,
	}
}

// supportedDocumentFormats lists the registered extensions (sorted, dotless)
// for the unsupported-format error copy.
func supportedDocumentFormats() []string {
	exts := make([]string, 0, 8)
	for ext := range defaultDocumentConverters() {
		exts = append(exts, strings.TrimPrefix(ext, "."))
	}
	sort.Strings(exts)
	return exts
}

// DocumentReadOption configures the document.read tool.
type DocumentReadOption func(*documentReadTool)

// WithReadOnlyRoots configures extra read-only roots accessible to the tool
// (e.g. drop-lane attachment run directories). Roots are stored in canonical
// spelling (canonicalizePath) so the jail check compares like with like.
func WithReadOnlyRoots(roots ...string) DocumentReadOption {
	return func(t *documentReadTool) {
		for _, r := range roots {
			if r == "" {
				continue
			}
			abs, err := filepath.Abs(r)
			if err != nil {
				continue
			}
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				// If directory doesn't exist yet, clean and store canonical form
				t.readOnlyRoots = append(t.readOnlyRoots, strings.TrimSuffix(canonicalizePath(filepath.Clean(abs)), string(filepath.Separator)))
				continue
			}
			t.readOnlyRoots = append(t.readOnlyRoots, strings.TrimSuffix(canonicalizePath(resolved), string(filepath.Separator)))
		}
	}
}

// WithDocumentReadMount overrides the default model-facing mount prefix (default: /workspace).
func WithDocumentReadMount(mount string) DocumentReadOption {
	return func(t *documentReadTool) {
		if mount != "" {
			t.mount = mount
		}
	}
}

// WithDocumentReadTimeout overrides the bounded conversion deadline.
func WithDocumentReadTimeout(timeout time.Duration) DocumentReadOption {
	return func(t *documentReadTool) {
		if timeout > 0 {
			t.timeout = timeout
		}
	}
}

type documentReadTool struct {
	agentDir      string
	readOnlyRoots []string
	mount         string
	timeout       time.Duration
	converters    map[string]documentConverter
}

// NewDocumentRead constructs the document.read tool for one agent workspace.
// A missing agentDir is created at construction (self-heal); the root resolves
// symlinks, and construction fails only on a genuinely unwritable path or a
// non-directory occupying it. Conversion is fully in-process — no external
// runtime exists to be missing (design.md D3).
func NewDocumentRead(agentDir string, opts ...DocumentReadOption) (tool.BaseTool, error) {
	if agentDir == "" {
		return nil, fmt.Errorf("agent workspace directory cannot be empty")
	}
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent workspace path: %w", err)
	}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		return nil, fmt.Errorf("agent workspace is not a directory: %s", abs)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create agent workspace directory: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve symlinks in agent workspace path: %w", err)
	}

	t := &documentReadTool{
		agentDir:   strings.TrimSuffix(canonicalizePath(resolvedRoot), string(filepath.Separator)),
		mount:      backend.DefaultMountPoint,
		timeout:    defaultDocumentReadTimeout,
		converters: defaultDocumentConverters(),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *documentReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameDocumentRead,
		Desc: "Convert a document (PDF, DOCX, XLSX, PPTX, HTML, CSV, TSV) to markdown text using built-in in-process conversion. " +
			"Provide an absolute path under " + t.mount + " or a mounted drop-lane attachment path. " +
			"Optionally scope the read: pages selects PDF pages, section reads only the matched heading, slide, or sheet section. " +
			"Corrupt, password-protected, or unsupported documents return an error result naming the document; " +
			"scanned or image-only PDFs report that no extractable text was found.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "The document file path to read.",
				Required: true,
			},
			"pages": {
				Type: schema.String,
				Desc: "Optional PDF page selection: a page number, an inclusive N-M range, or a comma-separated combination like \"1,3,5-7\". Applies to PDFs only.",
			},
			"section": {
				Type: schema.String,
				Desc: "Optional section title: return only the matched section. Case-insensitive exact match on a heading, \"Slide N\", or sheet name.",
			},
		}),
	}, nil
}

// documentReadArgs is the deserialized tool-call argument shape. Pages and
// Section are the optional scope parameters (add-reference-documents D5):
// empty/whitespace values count as absent, and at most one may be set.
type documentReadArgs struct {
	Path    string `json:"path"`
	Pages   string `json:"pages"`
	Section string `json:"section"`
}

// resolve maps the user-provided path against the allowed jail roots (the agent workspace
// and any read-only roots like drop-lane mounts).
func (t *documentReadTool) resolve(userPath string) (string, error) {
	if userPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	// Reject obvious escape attempts
	if strings.Contains(userPath, "..") {
		return "", fmt.Errorf("path escape attempt: %q contains \"..\"", userPath)
	}

	// If path starts with the workspace mount (e.g. /workspace/...), unmount relative to agentDir.
	var candidate string
	if userPath == t.mount {
		candidate = t.agentDir
	} else if rel := strings.TrimPrefix(userPath, t.mount+"/"); rel != userPath && rel != "" {
		candidate = filepath.Join(t.agentDir, filepath.Clean(rel))
	} else if filepath.IsAbs(userPath) {
		// Absolute path on host: check against agentDir or readOnlyRoots
		candidate = filepath.Clean(userPath)
	} else {
		// Relative path: resolve under agentDir
		cleaned := filepath.Clean(userPath)
		candidate = filepath.Join(t.agentDir, cleaned)
		// references/<document name> addresses the run's references mount:
		// the read-only root whose base directory is named "references" (the
		// base prompt and search-tool contract). The first path segment must
		// match that component exactly — never a prefix of an unrelated root.
		if first, rest, _ := strings.Cut(cleaned, string(filepath.Separator)); first == "references" && rest != "" {
			for _, root := range t.readOnlyRoots {
				if filepath.Base(root) == "references" {
					candidate = filepath.Join(root, rest)
					break
				}
			}
		}
	}

	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no such file: %s", userPath)
		}
		return "", fmt.Errorf("resolve path: %w", err)
	}

	// Different absolute spellings of the same file are the same file for the
	// jail check: on darwin the data-volume firmlink gives one path a
	// /System/Volumes/Data/... alias that EvalSymlinks does not collapse
	// (fix-reference-document-retrieval 3.1). Compare the canonical spelling
	// against the canonicalized roots; the read itself still uses resolved.
	canonical := canonicalizePath(resolved)
	if !t.isWithinAllowedRoots(canonical) {
		return "", outsideAllowedRootsError(userPath, canonical)
	}

	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat document file: %w", err)
	}
	if fi.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", userPath)
	}

	return resolved, nil
}

func (t *documentReadTool) isWithinAllowedRoots(path string) bool {
	cleaned := filepath.Clean(path)
	if isWithinRoot(t.agentDir, cleaned) {
		return true
	}
	for _, root := range t.readOnlyRoots {
		if isWithinRoot(root, cleaned) {
			return true
		}
	}
	return false
}

func isWithinRoot(root, path string) bool {
	if root == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// darwinDataVolumePrefix is the macOS data-volume firmlink mount of /: on
// darwin every file under / also answers at /System/Volumes/Data/..., and
// filepath.EvalSymlinks does not collapse that spelling, so one file carries
// two absolute spellings that are not prefix-equal
// (fix-reference-document-retrieval 3.1). The prefix only exists on darwin.
const darwinDataVolumePrefix = "/System/Volumes/Data"

// canonicalizePath maps a path's data-volume firmlink spelling to its plain
// form ("/System/Volumes/Data/X" ≡ "/X"); every other path passes through
// unchanged. The strip is guarded to that exact leading path component (the
// remainder must start with its own separator and name something below the
// mount root), so similar-looking prefixes and the mount root itself are
// untouched. The strip only ever removes the firmlink alias — it never
// relocates a file across the jail boundary — so the root check's security
// semantics are unchanged: what was outside the roots stays outside.
func canonicalizePath(path string) string {
	if rest := strings.TrimPrefix(path, darwinDataVolumePrefix); len(rest) > 1 && rest[0] == '/' {
		return rest
	}
	return path
}

// outsideAllowedRootsError renders the teaching rejection (design.md D4):
// the accepted path forms, so a rejected call can self-correct in one retry
// instead of dead-ending at a bare refusal. When the rejected spelling is a
// spelling of a file a references mount serves — its canonical path sits
// directly inside a directory named "references", e.g. a stale session's
// mount path — the error additionally names that same file's
// references/<document name> form. With canonicalization applied to the jail
// check itself (3.1), a firmlink alias of an allowed root file no longer
// reaches this error at all; the hint covers the residual spelling-artifact
// class the check cannot vouch for. A genuine escape (symlink resolving
// outside) never gets the hint: its canonical path does not sit inside a
// references directory.
func outsideAllowedRootsError(userPath, canonical string) error {
	msg := fmt.Sprintf("path is outside allowed directories: %q — use references/<document name>, a workspace-relative path, or /workspace/...", userPath)
	if name, ok := referencesMountFileName(canonical); ok {
		msg += fmt.Sprintf(" (%q is the same file as references/%s)", userPath, name)
	}
	return fmt.Errorf("%s", msg)
}

// referencesMountDirName is the directory component every references mount
// exposes — the base prompt and search-tool contract, and the component the
// references/<name> path form keys on.
const referencesMountDirName = "references"

// referencesMountFileName reports whether canonical names a file directly
// inside a directory named "references" — the documented mount shape — and
// returns the document name for the references/<name> retry form. Nested
// paths under other directories do not hint: the documented form is flat.
func referencesMountFileName(canonical string) (string, bool) {
	dir, file := filepath.Split(canonical)
	if file == "" {
		return "", false
	}
	dir = strings.TrimSuffix(dir, string(filepath.Separator))
	if filepath.Base(dir) != referencesMountDirName {
		return "", false
	}
	return file, true
}

// InvokableRun satisfies tool.InvokableTool. Path validation failures return
// errors (the runtime's tool-error middleware turns them into JSON error
// results); per-document conversion failures and scoped-read misses return
// structured results directly, naming the document, so the run continues
// (design.md D3; add-reference-documents D5 — scoped-read failures never fail
// the run, and the tool stays registered and selectable).
func (t *documentReadTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args documentReadArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("document.read: %w", err)
	}

	// Scope parameters (D5): empty/whitespace values are absent, and both at
	// once is a malformed request. Page-syntax validation precedes document
	// access — a malformed parameter is a tool-parameter error regardless of
	// the file.
	pagesSpec := strings.TrimSpace(args.Pages)
	sectionTitle := strings.TrimSpace(args.Section)
	if pagesSpec != "" && sectionTitle != "" {
		return "", fmt.Errorf("document.read: specify either pages or section, not both")
	}
	var selection []int
	if pagesSpec != "" {
		pages, err := parsePageSelection(pagesSpec)
		if err != nil {
			return "", fmt.Errorf("document.read: %w", err)
		}
		selection = pages
	}

	resolvedPath, err := t.resolve(args.Path)
	if err != nil {
		return "", fmt.Errorf("document.read: %w", err)
	}

	docName := filepath.Base(resolvedPath)
	ext := strings.ToLower(filepath.Ext(docName))

	// Page scoping is a PDF-only affordance (D5): on any other type return
	// the structured note naming the section alternative — a request-shape
	// mismatch, checked before conversion is attempted.
	if pagesSpec != "" && ext != ".pdf" {
		return documentReadNote(args.Path, docName, "page scoping applies to PDFs only; use the section parameter instead"), nil
	}

	converter, ok := t.converters[ext]
	if !ok {
		return documentReadFailure(args.Path, docName, fmt.Sprintf(
			"Cannot read %s: no converter is registered for the %q format. Supported formats: %s.",
			docName, ext, strings.Join(supportedDocumentFormats(), ", "))), nil
	}

	convCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	markdown, err := convertDocument(convCtx, converter, resolvedPath)
	if err != nil {
		if ctx.Err() != nil {
			// The run itself is being torn down; there is no next turn to
			// answer a result, so cancellation propagates as a real error.
			return "", fmt.Errorf("document.read: %w", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return documentReadFailure(args.Path, docName, fmt.Sprintf(
				"Cannot read %s: conversion timed out after %v.", docName, t.timeout)), nil
		}
		return documentReadFailure(args.Path, docName, fmt.Sprintf(
			"Cannot read %s: %v.", docName, err)), nil
	}

	if pagesSpec != "" {
		return t.pagesResult(args, docName, markdown, selection)
	}
	if sectionTitle != "" {
		return t.sectionResult(args, docName, markdown, sectionTitle)
	}

	// Page anchors are an indexing affordance for the references pipeline
	// (add-reference-documents tasks 3.1); the document.read output contract
	// stays anchor-free.
	markdown = StripPageMarkers(markdown)

	// D5: Output contract
	trimmed := strings.TrimSpace(markdown)
	var finalMarkdown string
	switch {
	case trimmed == "":
		finalMarkdown = fmt.Sprintf("No extractable text found in %s (scanned or image-only document).", docName)
	case len(trimmed) > MaxDocumentReadOutputBytes:
		finalMarkdown = trimmed[:MaxDocumentReadOutputBytes] + truncationDocumentReadNotice
	default:
		finalMarkdown = trimmed
	}

	out, err := json.Marshal(map[string]any{
		"path":     args.Path,
		"name":     docName,
		"markdown": finalMarkdown,
	})
	if err != nil {
		return "", fmt.Errorf("document.read: encode result: %w", err)
	}

	return string(out), nil
}

// pagesResult renders the page-scoped read (D5): the requested pages sliced
// from the anchored PDF conversion, each under a `## Page N` heading, the
// whole slice under the shared output cap. Selection order is document order;
// out-of-range pages are dropped, and a selection matching nothing is a
// structured not-found naming the document — never a run failure.
func (t *documentReadTool) pagesResult(args documentReadArgs, docName, markdown string, selection []int) (string, error) {
	segments := ParsePageSegments(markdown)
	bodies := make(map[int]string, len(segments))
	available := make([]int, 0, len(segments))
	for _, seg := range segments {
		bodies[seg.Page] = seg.Body
		available = append(available, seg.Page)
	}

	var sb strings.Builder
	matched := make([]int, 0, len(selection))
	for _, page := range selection {
		body, ok := bodies[page]
		if !ok {
			continue
		}
		matched = append(matched, page)
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("## Page %d\n\n%s", page, body))
	}
	if len(matched) == 0 {
		return documentReadScopeError(args.Path, docName, fmt.Sprintf(
			"pages not found in %s: requested %q but only %s carry readable text",
			docName, strings.TrimSpace(args.Pages), orPageList(available))), nil
	}

	out, err := json.Marshal(map[string]any{
		"path":     args.Path,
		"name":     docName,
		"markdown": capDocumentReadSlice(sb.String()),
		"pages":    renderPageSelection(matched),
	})
	if err != nil {
		return "", fmt.Errorf("document.read: encode result: %w", err)
	}
	return string(out), nil
}

// sectionResult renders the section-scoped read (D5): the first heading whose
// trimmed text equals the requested title (case-insensitive), rendered with
// its body under the shared output cap. A miss is a structured not-found
// naming the document and the requested title — never a run failure.
func (t *documentReadTool) sectionResult(args documentReadArgs, docName, markdown, title string) (string, error) {
	// Anchors stripped first: the output contract stays anchor-free even when
	// a matched section sits inside an anchored PDF page.
	heading, body, ok := sliceMarkdownSection(StripPageMarkers(markdown), title)
	if !ok {
		return documentReadScopeError(args.Path, docName, "section not found: "+title), nil
	}
	rendered := strings.TrimSpace(heading + "\n\n" + body)
	out, err := json.Marshal(map[string]any{
		"path":     args.Path,
		"name":     docName,
		"markdown": capDocumentReadSlice(rendered),
		"section":  title,
	})
	if err != nil {
		return "", fmt.Errorf("document.read: encode result: %w", err)
	}
	return string(out), nil
}

// documentReadFailure renders the structured per-document error result
// (design.md D3): a result the model reads and reacts to, never a run failure.
func documentReadFailure(userPath, docName, msg string) string {
	out, err := json.Marshal(map[string]any{
		"path":     userPath,
		"name":     docName,
		"error":    msg,
		"markdown": "",
	})
	if err != nil {
		return msg
	}
	return string(out)
}

// documentReadScopeError renders the structured scoped-read miss (D5): same
// result envelope as documentReadFailure without the empty markdown body —
// the model reads the miss and re-queries, and the run continues.
func documentReadScopeError(userPath, docName, msg string) string {
	out, err := json.Marshal(map[string]any{
		"path":  userPath,
		"name":  docName,
		"error": msg,
	})
	if err != nil {
		return msg
	}
	return string(out)
}

// documentReadNote renders the structured guidance result (D5): page scoping
// requested on a non-PDF names the limitation and the section alternative.
func documentReadNote(userPath, docName, note string) string {
	out, err := json.Marshal(map[string]any{
		"path": userPath,
		"name": docName,
		"note": note,
	})
	if err != nil {
		return note
	}
	return string(out)
}

// capDocumentReadSlice applies the shared output cap to a scoped-read slice
// (D5): the cap binds after slicing, so a narrow page/section read is never
// truncated by the rest of the document.
func capDocumentReadSlice(markdown string) string {
	trimmed := strings.TrimSpace(markdown)
	if len(trimmed) > MaxDocumentReadOutputBytes {
		return trimmed[:MaxDocumentReadOutputBytes] + truncationDocumentReadNotice
	}
	return trimmed
}

// convertDocument runs one converter under panic recovery — corrupt or
// malformed documents can make third-party parsers panic, and per design.md D3
// every such failure is a structured per-document error, not a run failure.
func convertDocument(ctx context.Context, conv documentConverter, path string) (markdown string, err error) {
	defer func() {
		if r := recover(); r != nil {
			markdown = ""
			err = fmt.Errorf("conversion failed: %v", r)
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	markdown, err = conv(ctx, path)
	if err != nil {
		return "", err
	}
	if cerr := ctx.Err(); cerr != nil {
		return "", cerr
	}
	return markdown, nil
}

// ConvertDocument converts one in-memory document to markdown using the
// default converter registry (design.md D2): the converter resolves by the
// lowercased file extension of name, conversion runs under the shared bounded
// deadline with panic recovery, and the returned markdown is UNTRUNCATED and
// retains PDF page anchors — the references upload pipeline
// (internal/references) sections on the anchors and applies its own caps.
// Text formats (md, txt) have no converter and are passed through unconverted
// by the caller.
func ConvertDocument(name string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	converter, ok := defaultDocumentConverters()[ext]
	if !ok {
		return "", fmt.Errorf("cannot convert %s: no converter is registered for the %q format. Supported formats: %s.",
			name, ext, strings.Join(supportedDocumentFormats(), ", "))
	}
	dir, err := os.MkdirTemp("", "onclaw-convert-")
	if err != nil {
		return "", fmt.Errorf("create conversion workspace: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "document"+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("stage document bytes: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultDocumentReadTimeout)
	defer cancel()
	return convertDocument(ctx, converter, path)
}

// DocumentConverterExtensions lists the conversion-registry extensions
// (sorted, dotless) — the upload-side contract for convertible document types.
func DocumentConverterExtensions() []string {
	return supportedDocumentFormats()
}

// --- PDF (ledongthuc/pdf) -------------------------------------------------

// convertPDF extracts per-page text from a PDF. Each page that yields text is
// preceded by a `<!-- onclaw:page N -->` anchor line (add-reference-documents
// tasks 3.1) so the sectioner maps headings to page locators and scoped reads
// can slice pages; StripPageMarkers removes the anchors for plain output. A
// scanned/image-only PDF yields no text runs and returns an empty string; the
// caller renders the distinct "no extractable text" result (design.md D5).
func convertPDF(ctx context.Context, path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("not a valid PDF: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("extract text from page %d: %w", i, err)
		}
		pageText := strings.TrimSpace(text)
		if pageText == "" {
			continue
		}
		sb.WriteString(pageAnchor(i))
		sb.WriteString("\n")
		sb.WriteString(pageText)
		sb.WriteString("\n\n")
	}
	return sb.String(), nil
}

// pageAnchor renders the 1-based page-break anchor line for one PDF page.
func pageAnchor(page int) string {
	return fmt.Sprintf("<!-- onclaw:page %d -->", page)
}

// pdfPageMarkerLineRe matches one anchor line (ParsePageSegments splits on it).
var pdfPageMarkerLineRe = regexp.MustCompile(`^[ \t]*<!-- onclaw:page ([0-9]+) -->[ \t]*$`)

// pdfPageMarkerStripRe removes an anchor line together with its line
// terminator, so stripped converter output is byte-identical to the
// pre-anchor page-per-paragraph join.
var pdfPageMarkerStripRe = regexp.MustCompile(`(?m)^[ \t]*<!-- onclaw:page [0-9]+ -->[ \t]*\r?\n?`)

// PageSegment is one anchor-delimited page of converted PDF markdown.
type PageSegment struct {
	Page int    // 1-based page number from the anchor
	Body string // the page's text, anchors stripped, whitespace-trimmed
}

// StripPageMarkers removes every `<!-- onclaw:page N -->` anchor line from
// converted markdown.
func StripPageMarkers(md string) string {
	return pdfPageMarkerStripRe.ReplaceAllString(md, "")
}

// ParsePageSegments splits anchor-anchored markdown into per-page segments.
// Content preceding the first anchor, if any, is prepended to the first
// segment's body; markdown without any anchors yields a single Page 1 segment
// holding the whole text; empty input yields nil.
func ParsePageSegments(md string) []PageSegment {
	lines := strings.Split(md, "\n")
	type anchor struct{ idx, page int }
	var anchors []anchor
	for i, line := range lines {
		if m := pdfPageMarkerLineRe.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			anchors = append(anchors, anchor{idx: i, page: n})
		}
	}
	if len(anchors) == 0 {
		if strings.TrimSpace(md) == "" {
			return nil
		}
		return []PageSegment{{Page: 1, Body: strings.TrimSpace(md)}}
	}
	segments := make([]PageSegment, 0, len(anchors))
	for i, a := range anchors {
		end := len(lines)
		if i+1 < len(anchors) {
			end = anchors[i+1].idx
		}
		bodyLines := lines[a.idx+1 : end]
		if i == 0 && a.idx > 0 {
			// Content before the first anchor belongs to that page.
			leading := make([]string, 0, a.idx+len(bodyLines))
			leading = append(leading, lines[:a.idx]...)
			bodyLines = append(leading, bodyLines...)
		}
		segments = append(segments, PageSegment{Page: a.page, Body: strings.TrimSpace(strings.Join(bodyLines, "\n"))})
	}
	return segments
}

// maxPageSelectionPages bounds how many distinct pages one pages parameter may
// select: the output cap makes anything beyond a few hundred pages useless,
// so a runaway range is rejected as a parameter error instead of expanded.
const maxPageSelectionPages = 5000

// parsePageSelection parses the pages parameter grammar (D5): `N`, the
// inclusive range `N-M`, and comma-separated combinations ("1,3,5-7").
// Malformed syntax is a parameter error; zero/negative page numbers never
// match (pages are 1-based). The result is deduplicated and sorted into
// document order.
func parsePageSelection(spec string) ([]int, error) {
	invalid := func(reason string) error {
		return fmt.Errorf("invalid pages value %q: %s (expected page numbers and N-M ranges like \"1,3,5-7\")", spec, reason)
	}
	seen := make(map[int]bool)
	var pages []int
	add := func(n int) bool {
		if seen[n] {
			return len(pages) <= maxPageSelectionPages
		}
		seen[n] = true
		pages = append(pages, n)
		return len(pages) <= maxPageSelectionPages
	}
	for _, token := range strings.Split(spec, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, invalid("empty entry")
		}
		if start, end, ok := parsePageRange(token); ok {
			if start > end {
				return nil, invalid(fmt.Sprintf("range start %d exceeds end %d", start, end))
			}
			for page := start; page <= end; page++ {
				if !add(page) {
					return nil, invalid(fmt.Sprintf("selection exceeds the maximum of %d pages", maxPageSelectionPages))
				}
			}
			continue
		}
		page, err := strconv.Atoi(token)
		if err != nil || page <= 0 {
			return nil, invalid(fmt.Sprintf("%q is not a page number", token))
		}
		if !add(page) {
			return nil, invalid(fmt.Sprintf("selection exceeds the maximum of %d pages", maxPageSelectionPages))
		}
	}
	sort.Ints(pages)
	return pages, nil
}

// parsePageRange reports whether token is an `N-M` pair of integers.
func parsePageRange(token string) (start, end int, ok bool) {
	parts := strings.SplitN(token, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

// renderPageSelection renders an ascending page list as the normalized echo
// form: contiguous runs collapse to `N-M`, runs join with commas ("1,3,5-7").
func renderPageSelection(pages []int) string {
	if len(pages) == 0 {
		return ""
	}
	var parts []string
	start, prev := pages[0], pages[0]
	for _, page := range pages[1:] {
		if page == prev+1 {
			prev = page
			continue
		}
		parts = append(parts, renderPageRun(start, prev))
		start, prev = page, page
	}
	parts = append(parts, renderPageRun(start, prev))
	return strings.Join(parts, ",")
}

func renderPageRun(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

// orPageList renders an available-page list for not-found copy.
func orPageList(pages []int) string {
	if rendered := renderPageSelection(pages); rendered != "" {
		return "pages " + rendered
	}
	return "no pages"
}

// atxHeadingLineRe matches one ATX heading line: up to three leading spaces,
// 1-6 `#`, whitespace, then the heading text (extracted; an optional
// whitespace-preceded closing `#` sequence is trimmed by atxHeadingText).
var atxHeadingLineRe = regexp.MustCompile(`^[ \t]{0,3}#{1,6}[ \t]+(.*)$`)

// atxHeadingText extracts a heading line's title: the text after the `#`
// run, whitespace-trimmed, with an optional closing sequence of `#`s
// (per CommonMark, only when whitespace precedes it — "C#" keeps its hash).
// ok is false for non-heading lines and headings with no title text.
func atxHeadingText(line string) (text string, ok bool) {
	m := atxHeadingLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	trimmed := strings.TrimRight(strings.TrimSpace(m[1]), " \t")
	if !strings.HasSuffix(trimmed, "#") {
		return trimmed, trimmed != ""
	}
	runStart := len(trimmed)
	for runStart > 0 && trimmed[runStart-1] == '#' {
		runStart--
	}
	if runStart > 0 && (trimmed[runStart-1] == ' ' || trimmed[runStart-1] == '\t') {
		trimmed = strings.TrimRight(trimmed[:runStart], " \t")
	}
	return trimmed, trimmed != ""
}

// sliceMarkdownSection is the local markdown-section slicer (D5; kept in the
// tools package — internal/references imports tools, never the reverse). It
// finds the first ATX heading whose trimmed title equals title
// (case-insensitive) — xlsx `## <sheet>`, pptx `## Slide N`, and html/docx
// markdown headings are all ATX lines — and returns the heading line as
// written plus the section body up to the next heading of any level. No
// match yields ok=false.
func sliceMarkdownSection(md, title string) (heading, body string, ok bool) {
	lines := strings.Split(md, "\n")
	start := -1
	for i, line := range lines {
		text, isHeading := atxHeadingText(line)
		if !isHeading {
			continue
		}
		if strings.EqualFold(text, title) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", "", false
	}
	end := len(lines)
	for j := start + 1; j < len(lines); j++ {
		if _, isHeading := atxHeadingText(lines[j]); isHeading {
			end = j
			break
		}
	}
	heading = strings.TrimRight(lines[start], " \t\r")
	body = strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))
	return heading, body, true
}

// --- XLSX (excelize/v2) ----------------------------------------------------

// convertXLSX renders each sheet as a "## <sheet name>" heading followed by a
// markdown table whose header row is the sheet's first row.
func convertXLSX(ctx context.Context, path string) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", fmt.Errorf("not a valid xlsx: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	for _, sheet := range f.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			return "", fmt.Errorf("read sheet %q: %w", sheet, err)
		}
		if len(rows) == 0 {
			continue
		}
		sb.WriteString("## " + sheet + "\n\n")
		sb.WriteString(markdownTable(rows))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// --- Markdown / plain text (pass-through) -----------------------------------

// convertPlainText passes .md and .txt bytes through as the markdown string:
// both formats are already the tool's output medium, so no conversion applies
// (the file is read from its staged path like every other converter).
func convertPlainText(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// --- CSV / TSV (encoding/csv) ----------------------------------------------

// convertDelimited renders delimiter-separated data (comma for csv, tab for
// tsv) as a markdown table with the first record as the header row.
func convertDelimited(comma rune) documentConverter {
	return func(ctx context.Context, path string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		r := csv.NewReader(f)
		r.Comma = comma
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			return "", fmt.Errorf("parse delimited data: %w", err)
		}
		for len(rows) > 0 && isEmptyDelimitedRow(rows[len(rows)-1]) {
			rows = rows[:len(rows)-1]
		}
		if len(rows) == 0 {
			return "", nil
		}
		return markdownTable(rows), nil
	}
}

func isEmptyDelimitedRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

// markdownTable renders rows as a GitHub-flavored markdown table, padding
// short rows to the widest row and escaping pipe characters and newlines.
func markdownTable(rows [][]string) string {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return ""
	}
	pad := func(row []string) []string {
		cells := make([]string, width)
		copy(cells, row)
		return cells
	}

	var sb strings.Builder
	sb.WriteString("| " + strings.Join(markdownCells(pad(rows[0])), " | ") + " |\n")
	sb.WriteString("|")
	for i := 0; i < width; i++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")
	for _, row := range rows[1:] {
		sb.WriteString("| " + strings.Join(markdownCells(pad(row)), " | ") + " |\n")
	}
	return sb.String()
}

func markdownCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.ReplaceAll(cell, "\\", "\\\\")
		cell = strings.ReplaceAll(cell, "|", "\\|")
		cell = strings.ReplaceAll(cell, "\r", " ")
		cell = strings.ReplaceAll(cell, "\n", " ")
		out[i] = strings.TrimSpace(cell)
	}
	return out
}

// --- DOCX (stdlib archive/zip + encoding/xml over word/document.xml) -------

// docxTableState accumulates one w:tbl's rows and cells; the stack supports
// tables nested inside table cells.
type docxTableState struct {
	rows    [][]string
	cells   []string
	cell    strings.Builder
	hasCell bool
}

type docxState struct {
	sb strings.Builder

	paraText  strings.Builder
	paraStyle string

	runText   strings.Builder
	runBold   bool
	runItalic bool

	tableDepth int
	tables     []docxTableState
}

// convertDOCX walks word/document.xml with encoding/xml, mapping paragraphs
// (Heading1-3 styles → #/##/###), bold/italic runs → **/*, and w:tbl tables →
// markdown tables.
func convertDOCX(ctx context.Context, path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("not a valid docx (corrupt or not a zip archive): %w", err)
	}
	defer zr.Close()

	var document *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			document = f
			break
		}
	}
	if document == nil {
		return "", fmt.Errorf("not a valid docx (missing word/document.xml)")
	}

	rc, err := document.Open()
	if err != nil {
		return "", fmt.Errorf("open word/document.xml: %w", err)
	}
	defer rc.Close()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	st := &docxState{}
	decoder := xml.NewDecoder(rc)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse word/document.xml: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			st.start(el)
		case xml.EndElement:
			st.end(el)
		case xml.CharData:
			st.text(string(el))
		}
	}
	return st.sb.String(), nil
}

func (st *docxState) start(el xml.StartElement) {
	switch el.Name.Local {
	case "p":
		st.paraText.Reset()
		st.paraStyle = ""
	case "pStyle":
		for _, attr := range el.Attr {
			if attr.Name.Local == "val" {
				st.paraStyle = attr.Value
			}
		}
	case "r":
		st.runText.Reset()
		st.runBold = false
		st.runItalic = false
	case "b":
		st.runBold = true
	case "i":
		st.runItalic = true
	case "tab":
		st.appendRunText("\t")
	case "br":
		st.appendRunText("\n")
	case "tbl":
		st.tableDepth++
		st.tables = append(st.tables, docxTableState{})
	case "tc":
		table := &st.tables[len(st.tables)-1]
		table.cell.Reset()
		table.hasCell = true
	}
}

func (st *docxState) end(el xml.EndElement) {
	switch el.Name.Local {
	case "t", "tab", "br", "b", "i", "pStyle":
		// handled at enclosing boundaries
	case "r":
		st.flushRun()
	case "p":
		st.flushParagraph()
	case "tc":
		table := &st.tables[len(st.tables)-1]
		table.cells = append(table.cells, strings.TrimSpace(table.cell.String()))
		table.cell.Reset()
		table.hasCell = false
	case "tr":
		table := &st.tables[len(st.tables)-1]
		table.rows = append(table.rows, table.cells)
		table.cells = nil
	case "tbl":
		table := st.tables[len(st.tables)-1]
		st.tables = st.tables[:len(st.tables)-1]
		st.tableDepth--
		rendered := ""
		if len(table.rows) > 0 {
			rendered = markdownTable(table.rows)
		}
		if st.tableDepth > 0 {
			// Nested table: its markdown becomes text of the enclosing cell.
			if rendered != "" {
				st.tables[len(st.tables)-1].cell.WriteString(rendered)
			}
			return
		}
		if rendered != "" {
			st.sb.WriteString("\n\n" + rendered + "\n")
		}
	}
}

func (st *docxState) text(data string) {
	if st.runText.Len() > 0 || st.runBold || st.runItalic {
		st.runText.WriteString(data)
		return
	}
	st.paraText.WriteString(data)
}

// appendRunText writes literal whitespace tokens (tab, br) into the open run,
// or the paragraph when no run is open.
func (st *docxState) appendRunText(s string) {
	if st.runText.Len() > 0 || st.runBold || st.runItalic {
		st.runText.WriteString(s)
		return
	}
	st.paraText.WriteString(s)
}

// flushRun closes a w:r, wrapping its text in markdown emphasis per the run's
// bold/italic run properties.
func (st *docxState) flushRun() {
	text := st.runText.String()
	bold, italic := st.runBold, st.runItalic
	st.runText.Reset()
	st.runBold = false
	st.runItalic = false
	if text == "" {
		return
	}
	switch {
	case bold && italic:
		text = "***" + strings.TrimSpace(text) + "***"
	case bold:
		text = "**" + strings.TrimSpace(text) + "**"
	case italic:
		text = "*" + strings.TrimSpace(text) + "*"
	}
	st.paraText.WriteString(text)
}

// flushParagraph closes a w:p: headings map to markdown ATX prefixes, plain
// paragraphs emit as-is; inside a table cell the paragraph text joins the cell.
func (st *docxState) flushParagraph() {
	text := strings.TrimSpace(st.paraText.String())
	st.paraText.Reset()
	style := st.paraStyle
	st.paraStyle = ""

	if st.tableDepth > 0 {
		table := &st.tables[len(st.tables)-1]
		if table.hasCell {
			if table.cell.Len() > 0 {
				table.cell.WriteString(" ")
			}
			table.cell.WriteString(text)
		}
		return
	}
	if text == "" {
		return
	}
	switch style {
	case "Heading1", "heading 1":
		st.sb.WriteString("\n\n# " + text + "\n\n")
	case "Heading2", "heading 2":
		st.sb.WriteString("\n\n## " + text + "\n\n")
	case "Heading3", "heading 3":
		st.sb.WriteString("\n\n### " + text + "\n\n")
	default:
		st.sb.WriteString(text + "\n\n")
	}
}

// --- PPTX (stdlib archive/zip + encoding/xml over ppt/slides/slideN.xml) ----

// convertPPTX renders each slide (sorted numerically) as a "## Slide N"
// heading followed by its a:p paragraph text runs as lines.
func convertPPTX(ctx context.Context, path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("not a valid pptx (corrupt or not a zip archive): %w", err)
	}
	defer zr.Close()

	type slideRef struct {
		num  int
		file *zip.File
	}
	var slides []slideRef
	for _, f := range zr.File {
		if num, ok := pptxSlideNumber(f.Name); ok {
			slides = append(slides, slideRef{num: num, file: f})
		}
	}
	if len(slides) == 0 {
		return "", fmt.Errorf("not a valid pptx (no slides found)")
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].num < slides[j].num })

	var sb strings.Builder
	for _, slide := range slides {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		lines, err := pptxSlideLines(slide.file)
		if err != nil {
			return "", err
		}
		sb.WriteString(fmt.Sprintf("## Slide %d\n\n", slide.num))
		for _, line := range lines {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// pptxSlideNumber matches ppt/slides/slideN.xml and returns N.
func pptxSlideNumber(zipName string) (int, bool) {
	dir, base := filepath.Split(zipName)
	if dir != "ppt/slides/" || !strings.HasPrefix(base, "slide") || !strings.HasSuffix(base, ".xml") {
		return 0, false
	}
	num, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(base, "slide"), ".xml"))
	if err != nil || num <= 0 {
		return 0, false
	}
	return num, true
}

// pptxSlideLines extracts one slide's text: each a:p paragraph becomes a line
// from its concatenated a:t text runs.
func pptxSlideLines(slide *zip.File) ([]string, error) {
	rc, err := slide.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", slide.Name, err)
	}
	defer rc.Close()

	var lines []string
	var para strings.Builder
	decoder := xml.NewDecoder(rc)
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", slide.Name, err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "p" {
				para.Reset()
			}
		case xml.EndElement:
			if el.Name.Local == "p" {
				if text := strings.TrimSpace(para.String()); text != "" {
					lines = append(lines, text)
				}
				para.Reset()
			}
		case xml.CharData:
			para.Write(el)
		}
	}
	return lines, nil
}

// --- HTML (golang.org/x/net/html) ------------------------------------------

// convertHTML walks the parsed DOM, mapping h1-h3, p, li, strong/em, and
// tables to markdown; script/style content is stripped.
func convertHTML(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	var w htmlWalker
	w.walk(doc)
	return collapseHTMLMarkdown(w.sb.String()), nil
}

// htmlWalker emits markdown while walking the DOM, tracking the last emitted
// byte and open inline-marker depth so inter-node spacing stays correct
// without trailing spaces leaking inside **/* runs.
type htmlWalker struct {
	sb          strings.Builder
	last        byte
	inlineDepth int
}

func (w *htmlWalker) writeString(s string) {
	if s == "" {
		return
	}
	w.sb.WriteString(s)
	w.last = s[len(s)-1]
}

// isWordByte reports whether b is part of a word (ASCII letters/digits and
// non-ASCII UTF-8 continuation/lead bytes).
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b >= 0x80:
		return true
	}
	return false
}

// text writes a text node, inserting a single separating space when the
// previous byte is a word character — or a closing emphasis marker.
func (w *htmlWalker) text(data string) {
	collapsed := strings.Join(strings.Fields(data), " ")
	if collapsed == "" {
		return
	}
	switch {
	case w.last == 0:
		// nothing written yet
	case isWordByte(w.last):
		w.writeString(" ")
	case w.last == '*' && w.inlineDepth == 0:
		w.writeString(" ")
	}
	w.writeString(collapsed)
}

// openInline writes an opening emphasis marker, separated from a preceding
// word by a space.
func (w *htmlWalker) openInline(marker string) {
	if isWordByte(w.last) {
		w.writeString(" ")
	}
	w.inlineDepth++
	w.writeString(marker)
}

// closeInline writes a closing emphasis marker.
func (w *htmlWalker) closeInline(marker string) {
	w.inlineDepth--
	w.writeString(marker)
}

func (w *htmlWalker) walk(n *html.Node) {
	if n.Type == html.TextNode {
		w.text(n.Data)
		return
	}
	if n.Type != html.ElementNode {
		w.walkChildren(n)
		return
	}
	switch n.Data {
	case "script", "style", "head", "noscript", "template":
		return
	case "h1", "h2", "h3":
		w.writeString("\n\n" + strings.Repeat("#", int(n.Data[1]-'0')) + " ")
		w.walkChildren(n)
		w.writeString("\n\n")
	case "p":
		w.writeString("\n\n")
		w.walkChildren(n)
		w.writeString("\n\n")
	case "br":
		w.writeString("\n")
	case "li":
		w.writeString("\n- ")
		w.walkChildren(n)
	case "strong", "b":
		w.openInline("**")
		w.walkChildren(n)
		w.closeInline("**")
	case "em", "i":
		w.openInline("*")
		w.walkChildren(n)
		w.closeInline("*")
	case "table":
		w.writeString("\n\n")
		w.writeString(htmlTableMarkdown(n))
		w.writeString("\n\n")
	default:
		w.walkChildren(n)
	}
}

func (w *htmlWalker) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c)
	}
}

// collapseHTMLMarkdown normalizes the emitted markdown: collapse runs of blank
// lines and trim the edges.
func collapseHTMLMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			blanks++
			if blanks > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blanks = 0
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// htmlTableMarkdown flattens an HTML table into a markdown table: tr rows,
// th/td cells with their inline text content.
func htmlTableMarkdown(table *html.Node) string {
	var rows [][]string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "tr":
				var cells []string
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
						cells = append(cells, strings.TrimSpace(htmlTextContent(c)))
					}
				}
				if len(cells) > 0 {
					rows = append(rows, cells)
				}
				return
			case "table":
				if n != table {
					return // nested table: its cell text is captured by htmlTextContent
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	if len(rows) == 0 {
		return ""
	}
	return markdownTable(rows)
}

// htmlTextContent collects an element subtree's text, preserving **/* emphasis
// from strong/b/em/i and collapsing whitespace.
func htmlTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.Join(strings.Fields(n.Data), " ")
	}
	if n.Type != html.ElementNode {
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			sb.WriteString(htmlTextContent(c))
		}
		return sb.String()
	}
	var sb strings.Builder
	switch n.Data {
	case "strong", "b":
		sb.WriteString("**")
	case "em", "i":
		sb.WriteString("*")
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(htmlTextContent(c))
	}
	switch n.Data {
	case "strong", "b":
		sb.WriteString("**")
	case "em", "i":
		sb.WriteString("*")
	}
	return sb.String()
}
