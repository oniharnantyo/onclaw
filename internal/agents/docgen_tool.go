package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents/tools/docgen"
	"github.com/oniharnantyo/buddies/internal/store"
)

// createDocumentToolDescription is the model-facing description of the
// create_document tool; the settings surfaces render the catalog entry's
// shorter one (builtin_tools.go).
const createDocumentToolDescription = "Render markdown into a styled PDF document written at your workspace root. " +
	"Supply the markdown content and a plain file name (no directories); a .pdf suffix is conventional but not required. " +
	"Presentation — theme, page setup, cover, footer, watermark, table of contents — comes from the user's presets: " +
	"pass a preset name from the Document presets list in your context, or omit preset to use the default. " +
	"Regenerating to an existing file name overwrites it. " +
	"Rejections (bad file name, unknown preset) and rendering failures are reported in the result's error field; they do not fail the call."

// docgenConvertTimeout bounds one conversion (add-create-document design D9):
// Chrome startup plus rendering for a long document. A variable so tests can
// drive the timeout path.
var docgenConvertTimeout = 60 * time.Second

// docgenEngine is the slice of the document converter the create_document
// tool drives: the narrow seam (add-create-document task 4.2) that keeps
// headless Chrome out of unit tests — *docgen.Converter satisfies it, and
// the tests inject a fake engine instead.
type docgenEngine interface {
	Convert(ctx context.Context, markdown, destPath string, preset *docgen.Preset) (docgen.Result, error)
	Close() error
}

// newCreateDocumentTool builds the create_document invokable tool for one
// agent workspace (add-create-document design D2/D8): markdown renders
// through the manager-held converter into a plain file name at the workspace
// root, styled by the named preset, the configured default, or the library's
// built-in defaults. Bad file names, unknown presets, and rendering failures
// come back as readable {ok:false} tool results so the model can react and
// the turn continues (D9); a Go error is returned only for infrastructure
// failures (store I/O).
func newCreateDocumentTool(engine docgenEngine, presets store.DocumentPresetStore, settings store.ToolSettingStore, workspace string) (tool.BaseTool, error) {
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
	return utils.InferTool(ToolCreateDocument, createDocumentToolDescription, st.run)
}

// createDocumentTool is the create_document tool's closed-over dependencies:
// the converter it renders through, the preset store it resolves names
// against, the tool settings store holding the default preset, and the
// workspace directory every output is rooted at.
type createDocumentTool struct {
	engine    docgenEngine
	presets   store.DocumentPresetStore
	settings  store.ToolSettingStore
	workspace string
}

// createDocumentArgs is the tool's whole input (add-create-document design
// D2): no styling fields on the tool surface — every presentation decision
// lives in presets.
type createDocumentArgs struct {
	// Markdown is the document content.
	Markdown string `json:"markdown"`
	// Filename is the output's plain name at the workspace root.
	Filename string `json:"filename"`
	// Preset names a stored presentation preset; empty uses the configured
	// default, falling back to the rendering library's built-in defaults.
	Preset string `json:"preset,omitempty"`
}

// createDocumentOutput is the tool's whole output: ok=false with Error for
// every rejected request (bad file name, unknown preset, rendering
// failure), the written file's workspace-relative name and size on success.
type createDocumentOutput struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Filename string `json:"filename,omitempty"`
	Size     int64  `json:"size,omitempty"`
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

	preset, failure, err := t.resolvePreset(ctx, in.Preset)
	if err != nil {
		return createDocumentOutput{}, err
	}
	if failure != "" {
		return createDocumentOutput{OK: false, Error: failure}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, docgenConvertTimeout)
	defer cancel()
	res, err := t.engine.Convert(ctx, in.Markdown, filepath.Join(t.workspace, filename), preset)
	if err != nil {
		// Rendering failures — including the 60s timeout — are readable tool
		// errors, never turn failures (add-create-document design D9): the
		// model can retry or report, and the turn continues.
		if ctx.Err() != nil {
			return createDocumentOutput{OK: false,
				Error: fmt.Sprintf("creating %s timed out after %s: the conversion did not finish in time", filename, docgenConvertTimeout),
			}, nil
		}
		return createDocumentOutput{OK: false,
			Error: fmt.Sprintf("creating %s failed: %s", filename, err.Error()),
		}, nil
	}
	return createDocumentOutput{OK: true, Filename: filename, Size: res.SizeBytes}, nil
}

// resolvePreset maps the tool's preset argument onto a styling preset:
// empty means the configured default (add-create-document convention — the
// create_document tool_settings row holds {"defaultPreset":"<name>"}),
// falling back to the library's built-in defaults (nil) when none is
// configured or the configured one is gone; a named preset that does not
// exist is a readable failure naming the miss, nothing written. Store I/O
// failures are Go errors.
func (t *createDocumentTool) resolvePreset(ctx context.Context, named string) (*docgen.Preset, string, error) {
	name := named
	if name == "" {
		def, err := readDefaultPresetName(ctx, t.settings)
		if err != nil {
			return nil, "", err
		}
		// An unset, deleted, or vanished default degrades to the library's
		// built-in defaults (spec document-generation "Deleting the default
		// preset resets the default").
		name = def
	}
	if name == "" {
		return nil, "", nil
	}
	p, err := t.presets.ByName(ctx, store.DocumentPresetFormatPDF, name)
	if errors.Is(err, store.ErrNotFound) {
		if named == "" {
			return nil, "", nil
		}
		return nil, fmt.Sprintf("preset: no PDF preset named %q — omit preset for the default, or use one of the names in the Document presets list", named), nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("look up document preset %s: %w", name, err)
	}
	preset, _, err := docgen.ParsePreset([]byte(p.Config))
	if err != nil {
		// A preset whose stored config no longer parses is a rejected call,
		// not a turn failure: the user owns the preset and must see the
		// breakage.
		return nil, fmt.Sprintf("preset %q: its configuration does not parse: %s", p.Name, err.Error()), nil
	}
	return &preset, "", nil
}

// readDefaultPresetName reads the default preset's name from the
// create_document tool-settings row (add-create-document convention: the row
// holds {"defaultPreset":"<name>"}), the way the storeSearchConfigSource and
// storeBrowserConfigSource adapters read their tools' rows. An empty name
// means no default is configured.
func readDefaultPresetName(ctx context.Context, settings store.ToolSettingStore) (string, error) {
	raw, ok, err := settings.Get(ctx, ToolCreateDocument)
	if err != nil || !ok {
		return "", err
	}
	var cfg struct {
		DefaultPreset string `json:"defaultPreset"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", fmt.Errorf("decode %s settings: %w", ToolCreateDocument, err)
	}
	return cfg.DefaultPreset, nil
}

// validateDocFilename enforces the plain workspace-root output name
// (add-create-document design D8): no directory components, no path
// separators, no traversal, no absolute paths — and a final containment
// check on the joined destination mirroring the fs jail backend's
// validation (backend.resolveWritablePath / isWithinRoot). The failure text
// is empty when the name is acceptable; nothing is ever written for a
// rejected name. A .pdf suffix is conventional but not required.
func validateDocFilename(workspace, name string) (clean, failure string) {
	if name == "" {
		return "", "filename: filename is required (a plain name at the workspace root)"
	}
	if filepath.IsAbs(name) {
		return "", fmt.Sprintf("filename: %q must be a plain file name at the workspace root — absolute paths are not allowed", name)
	}
	if !isPlainDocName(name) {
		return "", fmt.Sprintf("filename: %q must be a plain file name at the workspace root — no directory components and no %q", name, "..")
	}
	// Belt-and-braces containment on the resolved destination, mirroring the
	// jail's isWithinRoot: the joined path must stay strictly inside the
	// workspace (a name can only land on the root itself if it cleaned to
	// ".", which isPlainDocName already refuses).
	dest := filepath.Join(workspace, name)
	if !isWithinWorkspace(workspace, dest) {
		return "", fmt.Sprintf("filename: %q resolves outside the workspace", name)
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
