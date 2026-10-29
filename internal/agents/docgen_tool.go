package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents/tools/docgen"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// createDocumentToolDescription is the model-facing description of the
// create_document tool; the settings surfaces render the catalog entry's
// shorter one (builtin_tools.go).
const createDocumentToolDescription = "Render markdown into a styled document — PDF, DOCX, or PPTX, chosen by the file name's extension (.pdf, .docx, .pptx; anything else renders PDF) — written under artifacts/ in your workspace. " +
	"Supply the markdown content and a plain file name (no directories). " +
	"Presentation comes from the user's presets, which are per format: pass a preset name from the Document presets list in your context, or omit preset to use the format's default. " +
	"DOCX and PPTX render through the preset's template document when it has one; PPTX output always needs a template preset. " +
	"Regenerating to an existing file name overwrites it. " +
	"Rejections (bad file name, unknown preset) and rendering failures are reported in the result's error field; they do not fail the call."

// docgenConvertTimeout bounds one conversion (add-create-document design D9):
// Chrome startup plus rendering for a long document. A variable so tests can
// drive the timeout path.
var docgenConvertTimeout = 60 * time.Second

// docArtifactsDirPerm is the create_document outputs directory's mode,
// matching the workspaces package's private-to-user dirPerm.
const docArtifactsDirPerm os.FileMode = 0o700

// docgenEngine is the slice of the document converter the create_document
// tool drives: the narrow seam (add-create-document task 4.2) that keeps the
// rendering libraries out of unit tests — *docgen.Converter satisfies it,
// and the tests inject a fake engine instead. One method per output format
// (add-document-editing design D6): the tool picks by file name extension.
type docgenEngine interface {
	Convert(ctx context.Context, markdown, destPath string, preset *docgen.Preset) (docgen.Result, error)
	ConvertDOCX(ctx context.Context, markdown, destPath string, preset *docgen.DOCXPreset, template []byte) (docgen.Result, error)
	ConvertPPTX(ctx context.Context, markdown, destPath string, preset *docgen.PPTXPreset, template []byte) (docgen.Result, error)
	Close() error
}

// newCreateDocumentTool builds the create_document invokable tool for one
// agent workspace (add-create-document design D2/D8): markdown renders
// through the manager-held converter into a plain file name under the
// workspace's artifacts/ directory (add-turn-artifacts design D6), styled by
// the named per-format preset, the format's configured default, or the
// rendering library's built-in defaults. Bad file names, unknown presets,
// and rendering failures come back as readable {ok:false} tool results so
// the model can react and the turn continues (D9); a Go error is returned
// only for infrastructure failures (store I/O). The template store — the
// uploaded skeletons of template-document presets (add-document-editing
// design D6) — is optional at construction; generation that needs one
// without it degrades to a readable result until the wiring supplies it.
func newCreateDocumentTool(engine docgenEngine, presets store.DocumentPresetStore, settings store.ToolSettingStore, workspace string, opts ...createDocumentOption) (tool.BaseTool, error) {
	if engine == nil {
		return nil, errors.New("create_document: no document converter configured")
	}
	if presets == nil {
		return nil, errors.New("create_document: no document preset store configured")
	}
	if settings == nil {
		return nil, errors.New("create_document: no tool settings store configured")
	}
	if workspace == "" {
		return nil, errors.New("create_document: no workspace configured")
	}
	st := &createDocumentTool{engine: engine, presets: presets, settings: settings, workspace: workspace}
	for _, opt := range opts {
		opt(st)
	}
	return utils.InferTool(ToolCreateDocument, createDocumentToolDescription, st.run)
}

// createDocumentOption configures the create_document tool.
type createDocumentOption func(*createDocumentTool)

// WithDocumentTemplates supplies the store holding the uploaded skeletons
// of template-document presets (add-document-editing design D6). Unset —
// the default — DOCX template flavors and every PPTX generation read as a
// readable "template not available" result; the composition root always
// wires it beside the preset store.
func WithDocumentTemplates(templates store.DocumentTemplateStore) createDocumentOption {
	return func(t *createDocumentTool) {
		if templates != nil {
			t.templates = templates
		}
	}
}

// createDocumentTool is the create_document tool's closed-over dependencies:
// the converter it renders through, the preset store it resolves names
// against, the tool settings store holding the per-format defaults, the
// optional template store for template-document presets, and the workspace
// directory every output is rooted at (its artifacts/ subdirectory,
// specifically — see run).
type createDocumentTool struct {
	engine    docgenEngine
	presets   store.DocumentPresetStore
	settings  store.ToolSettingStore
	templates store.DocumentTemplateStore
	workspace string
}

// createDocumentArgs is the tool's whole input (add-create-document design
// D2): no styling fields on the tool surface — every presentation decision
// lives in presets.
type createDocumentArgs struct {
	// Markdown is the document content.
	Markdown string `json:"markdown"`
	// Filename is the output's plain name under the workspace's artifacts/
	// directory; the result reports it unchanged. The extension selects the
	// output format (.docx, .pptx; anything else renders PDF).
	Filename string `json:"filename"`
	// Preset names a stored presentation preset for the output format;
	// empty uses the format's configured default, falling back to the
	// rendering library's built-in defaults.
	Preset string `json:"preset,omitempty"`
}

// createDocumentOutput is the tool's whole output: ok=false with Error for
// every rejected request (bad file name, unknown preset, rendering
// failure), the written file's plain name and size on success.
type createDocumentOutput struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// resolvedDocPreset is the per-format preset resolution's outcome: exactly
// one parsed preset is set — nil, nil (no row) means the rendering
// library's built-in defaults — with the store row alongside for the
// template lookup.
type resolvedDocPreset struct {
	pdf  *docgen.Preset
	docx *docgen.DOCXPreset
	pptx *docgen.PPTXPreset
	row  *store.DocumentPreset
}

// run executes one create_document tool call.
func (t *createDocumentTool) run(ctx context.Context, in createDocumentArgs) (createDocumentOutput, error) {
	if strings.TrimSpace(in.Markdown) == "" {
		return createDocumentOutput{OK: false,
			Error: "markdown: markdown is required (the document's content)",
		}, nil
	}
	filename, failure := validateDocFilename(t.workspace, in.Filename)
	if failure != "" {
		return createDocumentOutput{OK: false, Error: failure}, nil
	}
	format := docFormatForName(filename)

	// The per-format preset resolves before anything is written — a
	// rejected preset leaves not even the artifacts/ directory behind.
	preset, failure, err := t.resolvePreset(ctx, in.Preset, format)
	if err != nil {
		return createDocumentOutput{}, err
	}
	if failure != "" {
		return createDocumentOutput{OK: false, Error: failure}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, docgenConvertTimeout)
	defer cancel()
	// The output roots at <workspace>/artifacts (add-turn-artifacts design
	// D6), created on demand so the first create_document call brings the
	// directory into being — no startup sweep (Migration Plan step 4); 0700
	// matches the workspaces package's private-to-user dir mode. A failure
	// (for example `artifacts` existing as a regular file) is a readable
	// result, not fatal.
	dir := filepath.Join(t.workspace, workspaces.DirArtifacts)
	if err := os.MkdirAll(dir, docArtifactsDirPerm); err != nil {
		return createDocumentOutput{OK: false,
			Error: fmt.Sprintf("creating the artifacts directory failed: %s", err.Error()),
		}, nil
	}
	destPath := filepath.Join(dir, filename)

	// Template-flavor generations need the uploaded skeleton before the
	// engine runs; a missing one is a readable rejection, still nothing
	// written.
	var template []byte
	if format == store.DocumentPresetFormatPPTX ||
		(format == store.DocumentPresetFormatDOCX && preset.docx != nil && preset.docx.Flavor == docgen.PresetFlavorTemplate) {
		template, failure, err = t.templateBytes(ctx, preset)
		if err != nil {
			return createDocumentOutput{}, err
		}
		if failure != "" {
			return createDocumentOutput{OK: false, Error: failure}, nil
		}
	}

	var (
		res  docgen.Result
		cerr error
	)
	// The output format — the extension's decision — picks the engine; a
	// format with no resolved preset renders through its built-in defaults
	// (PPTX refused the call earlier: it has none).
	switch format {
	case store.DocumentPresetFormatPPTX:
		res, cerr = t.engine.ConvertPPTX(ctx, in.Markdown, destPath, preset.pptx, template)
	case store.DocumentPresetFormatDOCX:
		res, cerr = t.engine.ConvertDOCX(ctx, in.Markdown, destPath, preset.docx, template)
	default:
		res, cerr = t.engine.Convert(ctx, in.Markdown, destPath, preset.pdf)
	}
	if cerr != nil {
		// Rendering failures — including the 60s timeout — are readable tool
		// errors, never turn failures (add-create-document design D9): the
		// model can retry or report, and the turn continues.
		if ctx.Err() != nil {
			return createDocumentOutput{OK: false,
				Error: fmt.Sprintf("creating %s timed out after %s: the conversion did not finish in time", filename, docgenConvertTimeout),
			}, nil
		}
		return createDocumentOutput{OK: false,
			Error: fmt.Sprintf("creating %s failed: %s", filename, cerr.Error()),
		}, nil
	}
	return createDocumentOutput{OK: true, Filename: filename, Size: res.SizeBytes}, nil
}

// docFormatForName maps the output name onto the preset format: the
// extension decides (.docx, .pptx), everything else — .pdf included, and
// the extensionless names the v1 tool accepted — renders PDF.
func docFormatForName(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return store.DocumentPresetFormatDOCX
	case ".pptx":
		return store.DocumentPresetFormatPPTX
	default:
		return store.DocumentPresetFormatPDF
	}
}

// docFormatLabel is the failure copy's name for a format.
func docFormatLabel(format string) string {
	switch format {
	case store.DocumentPresetFormatDOCX:
		return "DOCX"
	case store.DocumentPresetFormatPPTX:
		return "PPTX"
	default:
		return "PDF"
	}
}

// resolvePreset maps the tool's preset argument onto the format's preset,
// parsed: empty means the format's configured default (the create_document
// tool_settings row's per-format entry, the v1 flat PDF field for the pdf
// format), falling back to the rendering library's built-in defaults (a nil
// PDF preset, a nil DOCX config preset) when none is configured or the
// configured one is gone. PPTX has no library defaults — generation is
// template-only — so no preset is a readable failure naming the need (spec
// document-generation: PPTX generation without a template preset). A named
// preset that does not exist in the format is a readable failure naming the
// miss, nothing written. Store I/O failures are Go errors.
func (t *createDocumentTool) resolvePreset(ctx context.Context, named, format string) (resolvedDocPreset, string, error) {
	name := named
	if name == "" {
		def, err := readDefaultPresetName(ctx, t.settings, format)
		if err != nil {
			return resolvedDocPreset{}, "", err
		}
		// An unset, deleted, or vanished default degrades to the library's
		// built-in defaults (spec document-generation "Deleting the default
		// preset resets the default").
		name = def
	}
	if name == "" {
		if format == store.DocumentPresetFormatPPTX {
			return resolvedDocPreset{}, "preset: PPTX output needs a template preset — create one from an uploaded .pptx skeleton in the create_document settings and pass its name", nil
		}
		return resolvedDocPreset{}, "", nil
	}
	row, err := t.presets.ByName(ctx, format, name)
	if errors.Is(err, store.ErrNotFound) {
		if named == "" {
			return resolvedDocPreset{}, "", nil
		}
		return resolvedDocPreset{}, fmt.Sprintf("preset: no %s preset named %q — omit preset for the default, or use one of the names in the Document presets list", docFormatLabel(format), named), nil
	}
	if err != nil {
		return resolvedDocPreset{}, "", fmt.Errorf("look up document preset %s: %w", name, err)
	}
	switch format {
	case store.DocumentPresetFormatDOCX:
		preset, _, err := docgen.ParseDOCXPreset([]byte(row.Config))
		if err != nil {
			// A preset whose stored config no longer parses is a rejected
			// call, not a turn failure: the user owns the preset and must
			// see the breakage.
			return resolvedDocPreset{}, fmt.Sprintf("preset %q: its configuration does not parse: %s", row.Name, err.Error()), nil
		}
		return resolvedDocPreset{docx: &preset, row: row}, "", nil
	case store.DocumentPresetFormatPPTX:
		preset, _, err := docgen.ParsePPTXPreset([]byte(row.Config))
		if err != nil {
			return resolvedDocPreset{}, fmt.Sprintf("preset %q: its configuration does not parse: %s", row.Name, err.Error()), nil
		}
		return resolvedDocPreset{pptx: &preset, row: row}, "", nil
	default:
		preset, _, err := docgen.ParsePreset([]byte(row.Config))
		if err != nil {
			return resolvedDocPreset{}, fmt.Sprintf("preset %q: its configuration does not parse: %s", row.Name, err.Error()), nil
		}
		return resolvedDocPreset{pdf: &preset, row: row}, "", nil
	}
}

// templateBytes loads a template-flavor generation's uploaded skeleton: the
// preset's own flag says whether an upload happened, the template store
// finds the file. Every gap is a readable failure the user fixes in the
// settings — no upload, unwired store, vanished file — except store I/O,
// which is a Go error.
func (t *createDocumentTool) templateBytes(ctx context.Context, preset resolvedDocPreset) ([]byte, string, error) {
	name := "the preset"
	if preset.row != nil {
		name = preset.row.Name
	}
	var (
		hasTemplate bool
		extension   string
	)
	if preset.docx != nil {
		hasTemplate, extension = preset.docx.HasTemplate, ".docx"
	} else {
		hasTemplate, extension = preset.pptx.HasTemplate, ".pptx"
	}
	if !hasTemplate {
		return nil, fmt.Sprintf("preset %q has no template document uploaded — upload the %s skeleton in the create_document settings", name, extension), nil
	}
	if t.templates == nil {
		return nil, "document templates are not available: the create_document configuration is missing its template store", nil
	}
	path, err := t.templates.Path(ctx, preset.row.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Sprintf("preset %q's template document is missing — upload it again in the create_document settings", name), nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("look up document template: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read document template: %w", err)
	}
	return data, "", nil
}

// readDefaultPresetName reads the format's default preset name from the
// create_document tool-settings row. The row holds the per-format defaults
// ({"defaultPresets":{"docx":"<name>"}}) beside the v1 flat field
// {"defaultPreset":"<name>"} — the PDF default — which stays the pdf
// format's fallback so rows written before the per-format map keep working.
// An empty name means no default is configured.
func readDefaultPresetName(ctx context.Context, settings store.ToolSettingStore, format string) (string, error) {
	raw, ok, err := settings.Get(ctx, ToolCreateDocument)
	if err != nil || !ok {
		return "", err
	}
	var cfg struct {
		DefaultPreset  string            `json:"defaultPreset"`
		DefaultPresets map[string]string `json:"defaultPresets"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", fmt.Errorf("decode %s settings: %w", ToolCreateDocument, err)
	}
	if name := cfg.DefaultPresets[format]; name != "" {
		return name, nil
	}
	if format == store.DocumentPresetFormatPDF {
		return cfg.DefaultPreset, nil
	}
	return "", nil
}

// validateDocFilename enforces the plain output name written under the
// workspace's artifacts/ directory (add-create-document design D8; the
// artifacts root itself is add-turn-artifacts design D6): no directory
// components, no path separators, no traversal, no absolute paths — and a
// final containment check on the joined destination mirroring the fs jail
// backend's validation (backend.resolveWritablePath / isWithinRoot), now
// against the artifacts directory. The failure text is empty when the name
// is acceptable; nothing is ever written for a rejected name. A .pdf suffix
// is conventional but not required.
func validateDocFilename(workspace, name string) (clean, failure string) {
	if name == "" {
		return "", "filename: filename is required (a plain file name written under artifacts/)"
	}
	if filepath.IsAbs(name) {
		return "", fmt.Sprintf("filename: %q must be a plain file name written under artifacts/ — absolute paths are not allowed", name)
	}
	if !isPlainDocName(name) {
		return "", fmt.Sprintf("filename: %q must be a plain file name written under artifacts/ — no directory components and no %q", name, "..")
	}
	// Belt-and-braces containment on the resolved destination, mirroring the
	// jail's isWithinRoot: the joined path must stay strictly inside the
	// artifacts directory (a name can only land on that root itself if it
	// cleaned to ".", which isPlainDocName already refuses).
	dest := filepath.Join(workspace, workspaces.DirArtifacts, name)
	if !isWithinWorkspace(filepath.Join(workspace, workspaces.DirArtifacts), dest) {
		return "", fmt.Sprintf("filename: %q resolves outside the artifacts directory", name)
	}
	return name, ""
}

// isPlainDocName reports whether name is a bare file name: no separators,
// no traversal (the jail's blunt ".." check), no volume prefix. The dot
// itself is not a file name.
func isPlainDocName(name string) bool {
	if name == "." || strings.Contains(name, "..") {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') ||
		strings.ContainsRune(name, filepath.Separator) || strings.ContainsRune(name, filepath.ListSeparator) {
		return false
	}
	return filepath.VolumeName(name) == ""
}

// isWithinWorkspace mirrors the fs jail backend's isWithinRoot: the cleaned
// path must be the root itself or live under it.
func isWithinWorkspace(root, path string) bool {
	root, path = filepath.Clean(root), filepath.Clean(path)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
