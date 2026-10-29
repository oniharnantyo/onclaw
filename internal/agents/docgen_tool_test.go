package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/tools/docgen"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// fakeDocgenEngine is the docgenEngine stub standing in for the real
// converter: it records each Convert call (markdown, destination, resolved
// preset — per format) and writes a small distinctive payload at the
// destination, the write the real converter performs. No Chrome is ever
// launched.
type fakeDocgenEngine struct {
	mu    sync.Mutex
	calls []fakeConvertCall
	err   error
	// hangUntilDeadline makes Convert block until its context is done —
	// the timeout path.
	hangUntilDeadline bool
}

type fakeConvertCall struct {
	markdown string
	destPath string
	// format is the engine method that took the call: "pdf", "docx", or
	// "pptx" — the dispatch's decision.
	format string
	// preset is the format's parsed preset (nil = the built-in defaults).
	preset   *docgen.Preset
	docx     *docgen.DOCXPreset
	pptx     *docgen.PPTXPreset
	template []byte
}

func (f *fakeDocgenEngine) Convert(ctx context.Context, markdown, destPath string, preset *docgen.Preset) (docgen.Result, error) {
	if f.hangUntilDeadline {
		<-ctx.Done()
		return docgen.Result{}, ctx.Err()
	}
	return f.record(fakeConvertCall{format: "pdf", markdown: markdown, destPath: destPath, preset: preset})
}

func (f *fakeDocgenEngine) ConvertDOCX(_ context.Context, markdown, destPath string, preset *docgen.DOCXPreset, template []byte) (docgen.Result, error) {
	return f.record(fakeConvertCall{format: "docx", markdown: markdown, destPath: destPath, docx: preset, template: template})
}

func (f *fakeDocgenEngine) ConvertPPTX(_ context.Context, markdown, destPath string, preset *docgen.PPTXPreset, template []byte) (docgen.Result, error) {
	return f.record(fakeConvertCall{format: "pptx", markdown: markdown, destPath: destPath, pptx: preset, template: template})
}

// record carries the shared call bookkeeping and the fake's write.
func (f *fakeDocgenEngine) record(call fakeConvertCall) (docgen.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	n := len(f.calls)
	convErr := f.err
	f.mu.Unlock()
	if convErr != nil {
		return docgen.Result{}, convErr
	}
	// One byte per call, so a regenerate's fresh size is distinguishable.
	data := bytesOf(n)
	if err := os.WriteFile(call.destPath, data, 0o644); err != nil {
		return docgen.Result{}, err
	}
	return docgen.Result{Path: call.destPath, SizeBytes: int64(len(data))}, nil
}

func (f *fakeDocgenEngine) Close() error { return nil }

// recordedCall returns the n-th recorded call (1-based).
func (f *fakeDocgenEngine) recordedCall(t *testing.T, n int) fakeConvertCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.GreaterOrEqual(t, len(f.calls), n, "expected at least %d Convert calls", n)
	return f.calls[n-1]
}

// bytesOf is the fake payload: one byte per call number.
func bytesOf(n int) []byte { return []byte(strings.Repeat("a", n)) }

// docgenFixture wires the create_document tool over a fresh fake store, a
// stub engine, and a temp workspace.
type docgenFixture struct {
	st        *fake.Store
	engine    *fakeDocgenEngine
	tool      tool.BaseTool
	workspace string
	templates store.DocumentTemplateStore // nil unless wireTemplates ran
}

func newDocgenFixture(t *testing.T) *docgenFixture {
	t.Helper()
	st := fake.New()
	engine := &fakeDocgenEngine{}
	ws := t.TempDir()
	tl, err := newCreateDocumentTool(engine, st.DocumentPresets(), st.ToolSettings(), ws)
	require.NoError(t, err)
	return &docgenFixture{st: st, engine: engine, tool: tl, workspace: ws}
}

// wireTemplates rebuilds the fixture's tool with a template store over the
// given data directory — the composition root's wiring, which production
// always does.
func (f *docgenFixture) wireTemplates(t *testing.T, dataDir string) store.DocumentTemplateStore {
	t.Helper()
	f.templates = store.NewDocumentTemplateStore(dataDir)
	tl, err := newCreateDocumentTool(f.engine, f.st.DocumentPresets(), f.st.ToolSettings(), f.workspace,
		WithDocumentTemplates(f.templates))
	require.NoError(t, err)
	f.tool = tl
	return f.templates
}

// uploadTemplate stores a skeleton for the named preset of the format — the
// tests' shortcut for the user's upload in the settings dialog.
func uploadTemplate(t *testing.T, st *fake.Store, templates store.DocumentTemplateStore, format, presetName, filename, data string) {
	t.Helper()
	rows, err := st.DocumentPresets().List(t.Context(), format)
	require.NoError(t, err)
	for i := range rows {
		if strings.EqualFold(rows[i].Name, presetName) {
			_, err := templates.Save(t.Context(), rows[i].ID, filename, []byte(data))
			require.NoError(t, err)
			return
		}
	}
	t.Fatalf("preset %q not found in format %s", presetName, format)
}

// runDocgenTool runs one tool call through the InvokableTool surface the
// constructor returns as BaseTool and decodes the result.
func runDocgenTool(t *testing.T, base tool.BaseTool, args string) createDocumentOutput {
	t.Helper()
	invokable, ok := base.(tool.InvokableTool)
	require.True(t, ok, "create_document tool %T does not implement InvokableTool", base)
	out, err := invokable.InvokableRun(t.Context(), args)
	require.NoError(t, err, "rejections and rendering failures must be results, not errors: args %s", args)
	var res createDocumentOutput
	require.NoError(t, json.Unmarshal([]byte(out), &res), "result %q", out)
	return res
}

// seedPreset stores one valid PDF preset and returns its name.
func seedPreset(t *testing.T, st *fake.Store, name, theme string) {
	t.Helper()
	config, err := docgen.MarshalPreset(docgen.Preset{Theme: theme})
	require.NoError(t, err)
	require.NoError(t, st.DocumentPresets().Create(t.Context(),
		&store.DocumentPreset{Name: name, Format: store.DocumentPresetFormatPDF, Config: string(config)}))
}

// seedDOCXPreset stores one valid DOCX preset with the given flavor.
func seedDOCXPreset(t *testing.T, st *fake.Store, name string, preset docgen.DOCXPreset) {
	t.Helper()
	config, err := docgen.MarshalDOCXPreset(preset)
	require.NoError(t, err)
	require.NoError(t, st.DocumentPresets().Create(t.Context(),
		&store.DocumentPreset{Name: name, Format: store.DocumentPresetFormatDOCX, Config: string(config)}))
}

// seedPPTXPreset stores one valid PPTX preset.
func seedPPTXPreset(t *testing.T, st *fake.Store, name string, preset docgen.PPTXPreset) {
	t.Helper()
	config, err := docgen.MarshalPPTXPreset(preset)
	require.NoError(t, err)
	require.NoError(t, st.DocumentPresets().Create(t.Context(),
		&store.DocumentPreset{Name: name, Format: store.DocumentPresetFormatPPTX, Config: string(config)}))
}

// setDefaultPreset writes the default-preset tool-settings row (the
// add-create-document convention: {"defaultPreset":"<name>"}).
func setDefaultPreset(t *testing.T, st *fake.Store, name string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"defaultPreset": name})
	require.NoError(t, err)
	require.NoError(t, st.ToolSettings().Set(t.Context(), ToolCreateDocument, string(raw)))
}

// setFormatDefaultPreset writes the per-format default (the
// add-document-editing convention: the row's defaultPresets map).
func setFormatDefaultPreset(t *testing.T, st *fake.Store, format, name string) {
	t.Helper()
	raw, err := json.Marshal(map[string]map[string]string{"defaultPresets": {format: name}})
	require.NoError(t, err)
	require.NoError(t, st.ToolSettings().Set(t.Context(), ToolCreateDocument, string(raw)))
}

// TestCreateDocumentTool_Info pins the generated schema's identity.
func TestCreateDocumentTool_Info(t *testing.T) {
	f := newDocgenFixture(t)
	info, err := f.tool.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, ToolCreateDocument, info.Name)
	require.NotEmpty(t, info.Desc)
}

// TestCreateDocumentTool_RendersToArtifacts covers the spec's happy path
// (spec agent-tools "Markdown renders to a workspace PDF"): the PDF exists
// at <workspace>/artifacts under the plain name and the result carries the
// name and size. The fixture starts from a workspace without artifacts/, so
// this also pins the on-demand directory creation (add-turn-artifacts
// design D6).
func TestCreateDocumentTool_RendersToArtifacts(t *testing.T) {
	f := newDocgenFixture(t)
	_, err := os.Stat(filepath.Join(f.workspace, "artifacts"))
	require.True(t, os.IsNotExist(err), "the fixture starts without an artifacts directory")
	res := runDocgenTool(t, f.tool, `{"markdown":"# Q3 Report\n\nHello.","filename":"q3-report.pdf"}`)
	require.True(t, res.OK, "error field: %s", res.Error)
	require.Equal(t, "q3-report.pdf", res.Filename, "the result reports the plain name")
	require.Equal(t, int64(1), res.Size)

	// The file is under artifacts/, exactly where the tool reported.
	data, err := os.ReadFile(filepath.Join(f.workspace, "artifacts", "q3-report.pdf"))
	require.NoError(t, err)
	require.Equal(t, bytesOf(1), data)

	// The engine received the markdown verbatim and the joined destination.
	call := f.engine.recordedCall(t, 1)
	require.Equal(t, "# Q3 Report\n\nHello.", call.markdown)
	require.Equal(t, filepath.Join(f.workspace, "artifacts", "q3-report.pdf"), call.destPath)
}

// TestCreateDocumentTool_RegenerateOverwrites covers the spec's overwrite
// scenario: the new document replaces the old file and the result reports
// the fresh size.
func TestCreateDocumentTool_RegenerateOverwrites(t *testing.T) {
	f := newDocgenFixture(t)
	first := runDocgenTool(t, f.tool, `{"markdown":"draft","filename":"report.pdf"}`)
	require.True(t, first.OK, "error field: %s", first.Error)
	second := runDocgenTool(t, f.tool, `{"markdown":"final","filename":"report.pdf"}`)
	require.True(t, second.OK, "error field: %s", second.Error)
	require.NotEqual(t, first.Size, second.Size, "the fresh size is reported")
	require.Equal(t, int64(2), second.Size)

	data, err := os.ReadFile(filepath.Join(f.workspace, "artifacts", "report.pdf"))
	require.NoError(t, err)
	require.Equal(t, bytesOf(2), data, "the new document replaced the old file")
}

// TestCreateDocumentTool_EmptyMarkdownIsRejected pins the content guard: an
// empty markdown is a readable result, the engine is never called.
func TestCreateDocumentTool_EmptyMarkdownIsRejected(t *testing.T) {
	f := newDocgenFixture(t)
	res := runDocgenTool(t, f.tool, `{"markdown":"   ","filename":"report.pdf"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "markdown")
	require.Empty(t, f.engine.calls)
	_, err := os.Stat(filepath.Join(f.workspace, "artifacts", "report.pdf"))
	require.True(t, os.IsNotExist(err))
}

// TestCreateDocumentTool_FilenameCannotEscapeWorkspace covers the spec's
// traversal scenario (spec agent-tools "File name cannot escape the
// workspace"): directory components, traversal, and absolute paths return an
// out-of-scope error result and no file is written — not even the artifacts/
// directory, which is only created after the name validates. A .pdf suffix
// is not mandated.
func TestCreateDocumentTool_FilenameCannotEscapeWorkspace(t *testing.T) {
	cases := []struct {
		name     string
		filename string
	}{
		{"empty", ""},
		{"directory component", "sub/report.pdf"},
		{"traversal", "../escape.pdf"},
		{"deep traversal", "a/../../escape.pdf"},
		{"absolute", "/etc/passwd"},
		{"dot", "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDocgenFixture(t)
			res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":`+quote(tc.filename)+`}`)
			require.False(t, res.OK)
			require.Contains(t, res.Error, "filename")
			require.Empty(t, f.engine.calls, "nothing written: the engine is never called")
			entries, err := os.ReadDir(f.workspace)
			require.NoError(t, err)
			require.Empty(t, entries, "nothing written: the workspace stays empty, artifacts/ included")
		})
	}

	t.Run("plain name without .pdf suffix is accepted", func(t *testing.T) {
		f := newDocgenFixture(t)
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		require.Equal(t, "report", res.Filename)
	})
}

// TestCreateDocumentTool_UnknownPresetIsReadableError covers the spec's
// unknown-preset scenario: an error result naming the miss, nothing written,
// and no Go error — the turn continues.
func TestCreateDocumentTool_UnknownPresetIsReadableError(t *testing.T) {
	f := newDocgenFixture(t)
	seedPreset(t, f.st, "Client Report", "corporate")
	res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf","preset":"Invoice"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "Invoice", "the error names the miss")
	require.Empty(t, f.engine.calls)
	entries, err := os.ReadDir(f.workspace)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing written")
}

// TestCreateDocumentTool_PresetResolutionOrder pins the resolution order
// (add-create-document task 4.2): an explicit name wins over the configured
// default, the configured default beats the library defaults, and an
// unresolvable default falls back to the library's built-in defaults (nil).
func TestCreateDocumentTool_PresetResolutionOrder(t *testing.T) {
	t.Run("explicit name beats the configured default", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPreset(t, f.st, "Client Report", "corporate")
		seedPreset(t, f.st, "Invoice", "invoice")
		setDefaultPreset(t, f.st, "Client Report")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf","preset":"Invoice"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.NotNil(t, call.preset)
		require.Equal(t, "invoice", call.preset.Theme)
	})

	t.Run("no name uses the configured default", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPreset(t, f.st, "Client Report", "corporate")
		setDefaultPreset(t, f.st, "Client Report")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.NotNil(t, call.preset)
		require.Equal(t, "corporate", call.preset.Theme)
	})

	t.Run("no name and no configured default uses the library defaults", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPreset(t, f.st, "Client Report", "corporate")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.Nil(t, call.preset, "a nil preset is the library's built-in defaults")
	})

	t.Run("unresolvable default falls back to the library defaults", func(t *testing.T) {
		f := newDocgenFixture(t)
		setDefaultPreset(t, f.st, "Gone Report")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf"}`)
		require.True(t, res.OK, "a deleted default degrades, it does not reject: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.Nil(t, call.preset)
	})

	t.Run("an unresolvable DOCX default falls back to the built-in template", func(t *testing.T) {
		// The spec's reset scenario rides the same resolution for every
		// format (document-generation "Deleting the default preset resets
		// the default"): the stored default dangles, the tool call without
		// a preset name still renders — through the built-in defaults.
		f := newDocgenFixture(t)
		setFormatDefaultPreset(t, f.st, store.DocumentPresetFormatDOCX, "Gone Letter")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx"}`)
		require.True(t, res.OK, "a deleted default degrades, it does not reject: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.Equal(t, "docx", call.format)
		require.Nil(t, call.docx, "the built-in default template")
	})
}

// TestCreateDocumentTool_ConversionFailureIsReadable covers the spec's
// failure scenario (spec add-create-document "Conversion failure is a
// readable error"): a rendering failure comes back as an error result and
// the call never returns a Go error, so the turn continues.
func TestCreateDocumentTool_ConversionFailureIsReadable(t *testing.T) {
	f := newDocgenFixture(t)
	f.engine.err = errors.New("renderer exploded")
	res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "report.pdf")
	require.Contains(t, res.Error, "renderer exploded")
}

// TestCreateDocumentTool_ConversionTimeoutIsReadable covers the timeout half
// of the failure scenario (add-create-document design D9): a conversion
// exceeding its 60s bound is a readable result, not a turn failure.
func TestCreateDocumentTool_ConversionTimeoutIsReadable(t *testing.T) {
	origTimeout := docgenConvertTimeout
	docgenConvertTimeout = 50 * time.Millisecond
	t.Cleanup(func() { docgenConvertTimeout = origTimeout })

	f := newDocgenFixture(t)
	f.engine.hangUntilDeadline = true
	res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "timed out")
}

// TestCreateDocumentTool_CorruptPresetConfigIsReadable pins the corrupt-row
// degradation: a preset whose stored config no longer parses rejects the
// call naming the preset instead of failing the turn.
func TestCreateDocumentTool_CorruptPresetConfigIsReadable(t *testing.T) {
	f := newDocgenFixture(t)
	require.NoError(t, f.st.DocumentPresets().Create(t.Context(),
		&store.DocumentPreset{Name: "Broken", Format: store.DocumentPresetFormatPDF, Config: `{"theme":"no-such-theme"}`}))
	res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf","preset":"Broken"}`)
	require.False(t, res.OK)
	require.Contains(t, res.Error, "Broken")
	require.Empty(t, f.engine.calls)
}

// TestCreateDocumentTool_InfraFailuresAreGoErrors pins the boundary the
// scheduler tool drew: store I/O failures are Go errors, not results.
func TestCreateDocumentTool_InfraFailuresAreGoErrors(t *testing.T) {
	st := fake.New()
	engine := &fakeDocgenEngine{}
	tl, err := newCreateDocumentTool(engine, st.DocumentPresets(), &boomSettings{err: errors.New("disk on fire")}, t.TempDir())
	require.NoError(t, err)
	invokable := tl.(tool.InvokableTool)
	_, err = invokable.InvokableRun(t.Context(), `{"markdown":"hello","filename":"report.pdf"}`)
	require.ErrorContains(t, err, "disk on fire")
}

// boomSettings is a ToolSettingStore whose reads fail — the store-I/O stand-in.
type boomSettings struct {
	err error
}

func (b *boomSettings) Set(_ context.Context, _, _ string) error { return b.err }
func (b *boomSettings) Get(_ context.Context, _ string) (string, bool, error) {
	return "", false, b.err
}

// TestCreateDocumentTool_ConstructorGuards pins the wiring-bug failures: a
// tool built without its dependencies or workspace refuses construction.
func TestCreateDocumentTool_ConstructorGuards(t *testing.T) {
	st := fake.New()
	engine := &fakeDocgenEngine{}
	_, err := newCreateDocumentTool(nil, st.DocumentPresets(), st.ToolSettings(), t.TempDir())
	require.ErrorContains(t, err, "no document converter configured")
	_, err = newCreateDocumentTool(engine, nil, st.ToolSettings(), t.TempDir())
	require.ErrorContains(t, err, "no document preset store configured")
	_, err = newCreateDocumentTool(engine, st.DocumentPresets(), nil, t.TempDir())
	require.ErrorContains(t, err, "no tool settings store configured")
	_, err = newCreateDocumentTool(engine, st.DocumentPresets(), st.ToolSettings(), "")
	require.ErrorContains(t, err, "no workspace configured")
}

// TestCreateDocumentTool_RealConverterSatisfiesEngine pins the seam: the
// manager-held *docgen.Converter satisfies the tool's narrow engine
// interface (add-create-document task 4.2), so production wiring needs no
// adapter.
func TestCreateDocumentTool_RealConverterSatisfiesEngine(t *testing.T) {
	var engine docgenEngine = docgen.NewConverter()
	require.NoError(t, engine.Close())
}

// TestPresetGuidance pins the composed-context glue (add-create-document
// design D6; spec document-generation "Preset awareness in turns"): the
// appendix renders only when the tool is offered AND presets exist, the
// default is marked, and a store failure degrades to no appendix.
func TestPresetGuidance(t *testing.T) {
	ctx := context.Background()

	t.Run("tool not offered composes nothing", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
		seedPreset(t, fs, "Client Report", "corporate")
		require.Empty(t, m.presetGuidance(ctx, false))
	})

	t.Run("no presets composes nothing", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
		require.Empty(t, m.presetGuidance(ctx, true))
	})

	t.Run("offered with presets lists names and marks the default", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
		seedPreset(t, fs, "Client Report", "corporate")
		seedPreset(t, fs, "Invoice", "invoice")
		setDefaultPreset(t, fs, "Client Report")
		got := m.presetGuidance(ctx, true)
		require.Equal(t, presetAppendix([]presetFormatSummary{
			{Label: "PDF", Names: []string{"Client Report", "Invoice"}, Default: "Client Report"},
		}), got)
		require.Contains(t, got, "Client Report (default)")
	})

	t.Run("presets of every format list under their format", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
		seedPreset(t, fs, "Client Report", "corporate")
		seedDOCXPreset(t, fs, "Branded Letter", docgen.DOCXPreset{Flavor: docgen.PresetFlavorConfig})
		seedPPTXPreset(t, fs, "Company Deck", docgen.PPTXPreset{Flavor: docgen.PresetFlavorTemplate})
		// One settings row carrying both defaults — the shape the settings
		// dialog's per-format selectors produce.
		raw, err := json.Marshal(map[string]any{
			"defaultPreset":  "Client Report",
			"defaultPresets": map[string]string{store.DocumentPresetFormatDOCX: "Branded Letter"},
		})
		require.NoError(t, err)
		require.NoError(t, fs.ToolSettings().Set(t.Context(), ToolCreateDocument, string(raw)))
		got := m.presetGuidance(t.Context(), true)
		require.Contains(t, got, "- PDF presets: Client Report (default)")
		require.Contains(t, got, "- DOCX presets: Branded Letter (default)")
		require.Contains(t, got, "- PPTX presets: Company Deck")
	})

	t.Run("unreadable settings row leaves the list unmarked", func(t *testing.T) {
		fs := fake.New()
		m := newManager(fs, sessions.NewService(fs), &fakeRegistry{})
		seedPreset(t, fs, "Client Report", "corporate")
		require.NoError(t, fs.ToolSettings().Set(ctx, ToolCreateDocument, "{not json"))
		got := m.presetGuidance(ctx, true)
		require.Contains(t, got, "Client Report")
		require.NotContains(t, got, "(default)")
	})
}

// TestCreateDocumentTool_ExtensionSelectsEngine pins the per-format
// dispatch (add-document-editing design D6): .docx renders through the DOCX
// engine, .pptx through the PPTX engine, and anything else — .pdf and the
// extensionless names — through the existing PDF path.
func TestCreateDocumentTool_ExtensionSelectsEngine(t *testing.T) {
	t.Run("docx extension reaches the DOCX engine", func(t *testing.T) {
		f := newDocgenFixture(t)
		res := runDocgenTool(t, f.tool, `{"markdown":"# H","filename":"letter.docx"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.Equal(t, "docx", call.format, "the DOCX engine took the call")
		require.Nil(t, call.docx, "no preset means the built-in defaults")
		require.Nil(t, call.template, "no preset means no skeleton")
	})

	t.Run("pptx extension reaches the PPTX engine", func(t *testing.T) {
		f := newDocgenFixture(t)
		templates := f.wireTemplates(t, t.TempDir())
		seedPPTXPreset(t, f.st, "Company Deck", docgen.PPTXPreset{Flavor: docgen.PresetFlavorTemplate, HasTemplate: true})
		setFormatDefaultPreset(t, f.st, store.DocumentPresetFormatPPTX, "Company Deck")
		uploadTemplate(t, f.st, templates, store.DocumentPresetFormatPPTX, "Company Deck", "Deck.pptx", "SKELETON")
		res := runDocgenTool(t, f.tool, `{"markdown":"# H","filename":"deck.pptx"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.Equal(t, "pptx", call.format)
		require.Equal(t, []byte("SKELETON"), call.template, "the uploaded skeleton rode to the engine")
	})

	t.Run("extensionless and .pdf names stay on the PDF path", func(t *testing.T) {
		for _, filename := range []string{"report.pdf", "report"} {
			f := newDocgenFixture(t)
			res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":`+quote(filename)+`}`)
			require.True(t, res.OK, "error field: %s", res.Error)
			require.Equal(t, "pdf", f.engine.recordedCall(t, 1).format, "%s took the PDF path", filename)
		}
	})
}

// TestCreateDocumentTool_PerFormatPresetResolution pins the per-format
// resolution (add-document-editing design D6): .docx resolves against the
// DOCX presets and the DOCX default, a PDF preset's name is a miss for a
// DOCX output, and the v1 flat PDF default keeps working for .pdf calls.
func TestCreateDocumentTool_PerFormatPresetResolution(t *testing.T) {
	t.Run("docx preset resolves for a docx output", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedDOCXPreset(t, f.st, "Branded Letter", docgen.DOCXPreset{
			Flavor: docgen.PresetFlavorConfig,
			Fonts:  &docgen.DOCXFonts{BodyFamily: "Georgia", BodySize: 12},
		})
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx","preset":"Branded Letter"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.NotNil(t, call.docx)
		require.Equal(t, docgen.PresetFlavorConfig, call.docx.Flavor)
		require.Equal(t, "Georgia", call.docx.Fonts.BodyFamily, "the config parameters reached the engine")
		require.Nil(t, call.template, "a config flavor needs no skeleton")
	})

	t.Run("the DOCX default resolves without a name", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedDOCXPreset(t, f.st, "Branded Letter", docgen.DOCXPreset{Flavor: docgen.PresetFlavorConfig})
		setFormatDefaultPreset(t, f.st, store.DocumentPresetFormatDOCX, "Branded Letter")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		require.NotNil(t, f.engine.recordedCall(t, 1).docx)
	})

	t.Run("a PDF preset name is a readable miss for a DOCX output", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPreset(t, f.st, "Client Report", "corporate")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx","preset":"Client Report"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "no DOCX preset named")
		require.Contains(t, res.Error, "Client Report")
		require.Empty(t, f.engine.calls)
		entries, err := os.ReadDir(f.workspace)
		require.NoError(t, err)
		require.Empty(t, entries, "nothing written, artifacts/ included")
	})

	t.Run("the v1 flat default keeps serving PDF calls", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPreset(t, f.st, "Client Report", "corporate")
		setDefaultPreset(t, f.st, "Client Report")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"report.pdf"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		require.Equal(t, "corporate", f.engine.recordedCall(t, 1).preset.Theme)
	})

	t.Run("a DOCX output ignores the PDF default", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPreset(t, f.st, "Client Report", "corporate")
		setDefaultPreset(t, f.st, "Client Report")
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.Equal(t, "docx", call.format, "the DOCX output rendered through the DOCX engine")
		require.Nil(t, call.docx, "with no DOCX preset it fell back to the built-in defaults, not the PDF preset")
	})
}

// TestCreateDocumentTool_PPTXNeedsTemplate covers the spec's no-template
// scenario (add-document-editing design D6): PPTX generation without a
// template preset available returns a readable error naming the need, and
// the engine is never called.
func TestCreateDocumentTool_PPTXNeedsTemplate(t *testing.T) {
	t.Run("no preset at all names the need", func(t *testing.T) {
		f := newDocgenFixture(t)
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"deck.pptx"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "PPTX output needs a template preset")
		require.Empty(t, f.engine.calls)
		entries, err := os.ReadDir(f.workspace)
		require.NoError(t, err)
		require.Empty(t, entries, "nothing written")
	})

	t.Run("a preset without an upload names the missing skeleton", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPPTXPreset(t, f.st, "Company Deck", docgen.PPTXPreset{Flavor: docgen.PresetFlavorTemplate})
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"deck.pptx","preset":"Company Deck"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "Company Deck")
		require.Contains(t, res.Error, "no template document uploaded")
		require.Empty(t, f.engine.calls)
	})

	t.Run("an unwired template store degrades readably", func(t *testing.T) {
		f := newDocgenFixture(t)
		seedPPTXPreset(t, f.st, "Company Deck", docgen.PPTXPreset{Flavor: docgen.PresetFlavorTemplate, HasTemplate: true})
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"deck.pptx","preset":"Company Deck"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "template store")
		require.Empty(t, f.engine.calls)
	})

	t.Run("a vanished skeleton file names the re-upload", func(t *testing.T) {
		f := newDocgenFixture(t)
		templates := f.wireTemplates(t, t.TempDir())
		seedPPTXPreset(t, f.st, "Company Deck", docgen.PPTXPreset{Flavor: docgen.PresetFlavorTemplate, HasTemplate: true})
		uploadTemplate(t, f.st, templates, store.DocumentPresetFormatPPTX, "Company Deck", "Deck.pptx", "SKELETON")
		// The user (or a cleanup sweep) removed the file behind the store.
		path, err := templates.Path(t.Context(), presetID(t, f.st, store.DocumentPresetFormatPPTX, "Company Deck"))
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"deck.pptx","preset":"Company Deck"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "missing")
		require.Empty(t, f.engine.calls)
	})
}

// TestCreateDocumentTool_DOCXTemplateFlavor pins the DOCX template flavor's
// plumbing: the uploaded skeleton rides to the DOCX engine, and a missing
// one rejects the call.
func TestCreateDocumentTool_DOCXTemplateFlavor(t *testing.T) {
	t.Run("the uploaded skeleton rides to the engine", func(t *testing.T) {
		f := newDocgenFixture(t)
		templates := f.wireTemplates(t, t.TempDir())
		seedDOCXPreset(t, f.st, "Branded Letter", docgen.DOCXPreset{Flavor: docgen.PresetFlavorTemplate, HasTemplate: true})
		uploadTemplate(t, f.st, templates, store.DocumentPresetFormatDOCX, "Branded Letter", "Branded.docx", "SKELETON")

		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx","preset":"Branded Letter"}`)
		require.True(t, res.OK, "error field: %s", res.Error)
		call := f.engine.recordedCall(t, 1)
		require.NotNil(t, call.docx)
		require.Equal(t, docgen.PresetFlavorTemplate, call.docx.Flavor)
		require.Equal(t, []byte("SKELETON"), call.template)
	})

	t.Run("a template flavor without an upload rejects readably", func(t *testing.T) {
		f := newDocgenFixture(t)
		f.wireTemplates(t, t.TempDir())
		seedDOCXPreset(t, f.st, "Branded Letter", docgen.DOCXPreset{Flavor: docgen.PresetFlavorTemplate})
		res := runDocgenTool(t, f.tool, `{"markdown":"hello","filename":"letter.docx","preset":"Branded Letter"}`)
		require.False(t, res.OK)
		require.Contains(t, res.Error, "no template document uploaded")
		require.Empty(t, f.engine.calls)
	})
}

// presetID returns the named preset's store ID.
func presetID(t *testing.T, st *fake.Store, format, name string) string {
	t.Helper()
	rows, err := st.DocumentPresets().List(t.Context(), format)
	require.NoError(t, err)
	for _, r := range rows {
		if strings.EqualFold(r.Name, name) {
			return r.ID
		}
	}
	t.Fatalf("preset %q not found in format %s", name, format)
	return ""
}
