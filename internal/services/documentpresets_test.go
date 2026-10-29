package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/tools/docgen"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The document-preset CRUD service tests (add-create-document tasks 3.1,
// design D4, spec document-generation "Document presets"): the bound calls
// over a fake store, plus the tool-settings round-trip pinning the default
// preset's exact stored document for the create_document tool.

// TestSettingsDocumentPresets_CRUDRoundTrip covers create, list, update, and
// the store-assigned fields: an entry comes back with its ID, format
// (empty defaults to pdf), verbatim config, and timestamps; update rewrites
// name and config while preserving the format.
func TestSettingsDocumentPresets_CRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	created, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report",
		`{"schema_version":1,"theme":"corporate"}`)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "Client Report", created.Name)
	require.Equal(t, store.DocumentPresetFormatPDF, created.Format)
	require.JSONEq(t, `{"schema_version":1,"theme":"corporate"}`, created.Config)
	require.False(t, created.CreatedAt.IsZero())
	require.False(t, created.UpdatedAt.IsZero())

	// An empty format defaults to pdf in the store.
	fallback, err := svc.CreateDocumentPreset(ctx, "", "Invoice", `{"schema_version":1}`)
	require.NoError(t, err)
	require.Equal(t, store.DocumentPresetFormatPDF, fallback.Format)

	list, err := svc.ListDocumentPresets(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "Client Report", list[0].Name)
	require.Equal(t, "Invoice", list[1].Name)

	updated, err := svc.UpdateDocumentPreset(ctx, created.ID, "Client Report v2", `{"schema_version":1,"theme":"legal"}`)
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "Client Report v2", updated.Name)
	require.Equal(t, `{"schema_version":1,"theme":"legal"}`, updated.Config)
	require.Equal(t, store.DocumentPresetFormatPDF, updated.Format, "an edit never moves a preset between formats")

	// The update unknown id is a not-found error.
	_, err = svc.UpdateDocumentPreset(ctx, "no-such-id", "X", "{}")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSettingsDocumentPresets_RejectsDuplicateName pins the per-format
// uniqueness: a case-insensitive collision within the format is
// store.ErrDuplicate; the same name in another format is allowed.
func TestSettingsDocumentPresets_RejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	_, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report", "{}")
	require.NoError(t, err)

	_, err = svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "client report", "{}")
	require.ErrorIs(t, err, store.ErrDuplicate)

	_, err = svc.CreateDocumentPreset(ctx, "docx", "Client Report", "{}")
	require.NoError(t, err, "names collide within a format only")
}

// TestSettingsDocumentPresets_DuplicateThenEdit covers the duplicate action:
// the copy carries the source's format and config under a fresh ID, and
// editing the copy leaves the original untouched.
func TestSettingsDocumentPresets_DuplicateThenEdit(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	source, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Report",
		`{"schema_version":1,"theme":"corporate"}`)
	require.NoError(t, err)

	copyEntry, err := svc.DuplicateDocumentPreset(ctx, source.ID, "Report Copy")
	require.NoError(t, err)
	require.NotEqual(t, source.ID, copyEntry.ID)
	require.Equal(t, "Report Copy", copyEntry.Name)
	require.Equal(t, source.Format, copyEntry.Format)
	require.Equal(t, source.Config, copyEntry.Config)

	_, err = svc.UpdateDocumentPreset(ctx, copyEntry.ID, "Report Copy", `{"schema_version":1,"theme":"legal"}`)
	require.NoError(t, err)

	list, err := svc.ListDocumentPresets(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, source.Config, list[0].Config, "the original keeps its config")
	require.Equal(t, `{"schema_version":1,"theme":"legal"}`, list[1].Config)

	// Duplicating an unknown id and onto a taken name are errors.
	_, err = svc.DuplicateDocumentPreset(ctx, "no-such-id", "X")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = svc.DuplicateDocumentPreset(ctx, source.ID, "report copy")
	require.ErrorIs(t, err, store.ErrDuplicate)
}

// TestSettingsDocumentPresets_Delete covers the delete action: the row goes
// away and a repeated delete is store.ErrNotFound.
func TestSettingsDocumentPresets_Delete(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	created, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Report", "{}")
	require.NoError(t, err)

	require.NoError(t, svc.DeleteDocumentPreset(ctx, created.ID))
	list, err := svc.ListDocumentPresets(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Empty(t, list)

	err = svc.DeleteDocumentPreset(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSettingsDocumentPresets_RejectsInvalidDefinitions pins the service's
// up-front validation: a preset needs a name and JSON config before the
// store is touched.
func TestSettingsDocumentPresets_RejectsInvalidDefinitions(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	_, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "", "{}")
	require.ErrorContains(t, err, "needs a name")

	_, err = svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Report", "not json")
	require.ErrorContains(t, err, "not valid JSON")

	_, err = svc.UpdateDocumentPreset(ctx, "any", "", "{}")
	require.ErrorContains(t, err, "needs a name")
}

// TestSettingsDocumentPresets_DefaultRoundTrip covers the default-preset
// selector: none by default, set (with canonical-name storage), read back,
// and clear with the empty name — the whole state living in the
// tool_settings row under the create_document tool key.
func TestSettingsDocumentPresets_DefaultRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	_, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report", "{}")
	require.NoError(t, err)

	// No default was ever set.
	got, err := svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Empty(t, got)

	// Setting stores the canonical spelling of the matched preset name.
	require.NoError(t, svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF, "client report"))
	got, err = svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Equal(t, "Client Report", got)

	// The stored document is exactly the tool's convention.
	raw, ok, err := fs.ToolSettings().Get(ctx, "create_document")
	require.NoError(t, err)
	require.True(t, ok)
	var stored storedDocumentDefault
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, "Client Report", stored.DefaultPreset)

	// An unknown preset name is refused.
	err = svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Nope")
	require.ErrorIs(t, err, store.ErrNotFound)

	// The empty name clears the default.
	require.NoError(t, svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF, ""))
	got, err = svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestSettingsDocumentPresets_DeleteDefaultLeavesItDangling pins the
// deliberate dangling behavior: deleting the default's preset keeps the
// stored default name (the tool resolves it to "no default"); the service
// reads it back verbatim.
func TestSettingsDocumentPresets_DeleteDefaultLeavesItDangling(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())
	created, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report", "{}")
	require.NoError(t, err)
	require.NoError(t, svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report"))

	require.NoError(t, svc.DeleteDocumentPreset(ctx, created.ID))

	got, err := svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Equal(t, "Client Report", got, "the stored default dangles by design")
}

// templateFixture wires the settings service with a real on-disk template
// store over a temporary data dir — the composition root's wiring.
type templateFixture struct {
	dataDir   string
	svc       *SettingsService
	store     *fake.Store
	templates store.DocumentTemplateStore
}

func newTemplateFixture(t *testing.T) *templateFixture {
	t.Helper()
	st := fake.New()
	dataDir := t.TempDir()
	templates := store.NewDocumentTemplateStore(dataDir)
	return &templateFixture{
		dataDir:   dataDir,
		svc:       NewSettingsService(st, WithDocumentTemplates(templates)),
		store:     st,
		templates: templates,
	}
}

// b64Of is the base64 of the given literal — the upload's wire shape.
func b64Of(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// TestSettingsDocumentPresetTemplate_UploadRoundTrip covers the spec's
// round-trip scenario (document-generation "Preset round-trips"): a DOCX
// template preset's uploaded skeleton lands in the template store, the
// entry reports it, and a fresh store over the same data dir — the app
// restart — still finds the file.
func TestSettingsDocumentPresetTemplate_UploadRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := newTemplateFixture(t)
	created, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatDOCX,
		"Branded Letter", `{"schema_version":1,"flavor":"template"}`)
	require.NoError(t, err)

	require.NoError(t, f.svc.SetDocumentPresetTemplate(ctx, created.ID, "Branded.docx", b64Of("DOCX-BYTES")))

	list, err := f.svc.ListDocumentPresets(ctx, store.DocumentPresetFormatDOCX)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, list[0].HasTemplate, "the entry reports the uploaded skeleton")
	require.Equal(t, "Branded.docx", list[0].TemplateName)
	require.Contains(t, list[0].Config, `"has_template":true`, "the preset row records the upload")

	// The skeleton is on disk under the data dir, keyed by the preset ID.
	path, err := f.templates.Path(ctx, created.ID)
	require.NoError(t, err)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "DOCX-BYTES", string(onDisk))

	// The restart: a fresh store over the same data dir finds the file, and
	// the row still carries the metadata.
	reopened := store.NewDocumentTemplateStore(f.dataDir)
	again, err := reopened.Path(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, path, again)
	row, err := f.store.DocumentPresets().ByID(ctx, created.ID)
	require.NoError(t, err)
	meta := docgen.ParseTemplateMeta([]byte(row.Config))
	require.True(t, meta.HasTemplate)
	require.Equal(t, "Branded.docx", meta.TemplateName)
}

// TestSettingsDocumentPresetTemplate_UploadValidation pins the upload's
// guards: the extension must match the preset's format, PDF presets take no
// template, and the payload must decode.
func TestSettingsDocumentPresetTemplate_UploadValidation(t *testing.T) {
	ctx := context.Background()
	f := newTemplateFixture(t)
	docx, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatDOCX,
		"Branded Letter", `{"schema_version":1,"flavor":"template"}`)
	require.NoError(t, err)
	pptx, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPPTX,
		"Company Deck", `{"schema_version":1,"flavor":"template"}`)
	require.NoError(t, err)
	pdf, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report", "{}")
	require.NoError(t, err)

	// A .pptx skeleton on a DOCX preset and the reverse are refused.
	err = f.svc.SetDocumentPresetTemplate(ctx, docx.ID, "Deck.pptx", b64Of("x"))
	require.ErrorContains(t, err, "takes a .docx skeleton")
	err = f.svc.SetDocumentPresetTemplate(ctx, pptx.ID, "Letter.docx", b64Of("x"))
	require.ErrorContains(t, err, "takes a .pptx skeleton")

	// A PDF preset is config-only.
	err = f.svc.SetDocumentPresetTemplate(ctx, pdf.ID, "Report.pdf", b64Of("x"))
	require.ErrorContains(t, err, "not")

	// Undecodable payloads are refused before the store is touched.
	err = f.svc.SetDocumentPresetTemplate(ctx, docx.ID, "Letter.docx", "!!!not base64")
	require.ErrorContains(t, err, "decode")

	// An unknown preset is the store's not-found.
	err = f.svc.SetDocumentPresetTemplate(ctx, "no-such-id", "Letter.docx", b64Of("x"))
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSettingsDocumentPresetTemplate_DeleteCleansTemplate covers the
// delete path (add-document-editing design D6): deleting the preset removes
// its skeleton file; deleting a preset without an upload succeeds.
func TestSettingsDocumentPresetTemplate_DeleteCleansTemplate(t *testing.T) {
	ctx := context.Background()
	f := newTemplateFixture(t)
	created, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatDOCX,
		"Branded Letter", `{"schema_version":1,"flavor":"template"}`)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetDocumentPresetTemplate(ctx, created.ID, "Branded.docx", b64Of("DOCX")))

	require.NoError(t, f.svc.DeleteDocumentPreset(ctx, created.ID))
	_, err = f.templates.Path(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "the skeleton went with the preset")

	// A preset with no upload deletes cleanly.
	noUpload, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatDOCX,
		"Other Letter", `{"schema_version":1,"flavor":"template"}`)
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteDocumentPreset(ctx, noUpload.ID))
}

// TestSettingsDocumentPresetTemplate_DuplicateCopiesTemplate covers the
// duplicate action on a template preset: the copy's skeleton file exists
// under the copy's own ID, so the copy generates like the source.
func TestSettingsDocumentPresetTemplate_DuplicateCopiesTemplate(t *testing.T) {
	ctx := context.Background()
	f := newTemplateFixture(t)
	source, err := f.svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPPTX,
		"Company Deck", `{"schema_version":1,"flavor":"template"}`)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetDocumentPresetTemplate(ctx, source.ID, "Deck.pptx", b64Of("PPTX")))

	copyEntry, err := f.svc.DuplicateDocumentPreset(ctx, source.ID, "Deck Copy")
	require.NoError(t, err)
	require.True(t, copyEntry.HasTemplate, "the copy claims the skeleton")

	path, err := f.templates.Path(ctx, copyEntry.ID)
	require.NoError(t, err)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "PPTX", string(onDisk))
}

// TestSettingsDocumentPresets_PerFormatDefaults covers the per-format
// default selection (add-document-editing design D6): each format's default
// stores and clears independently, the PDF selection keeps riding the v1
// flat field, and a default must name a preset of its own format.
func TestSettingsDocumentPresets_PerFormatDefaults(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	templates := store.NewDocumentTemplateStore(t.TempDir())
	svc := NewSettingsService(fs, WithDocumentTemplates(templates))
	pdf, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report", "{}")
	require.NoError(t, err)
	docx, err := svc.CreateDocumentPreset(ctx, store.DocumentPresetFormatDOCX, "Branded Letter", "{}")
	require.NoError(t, err)

	// Defaults set per format.
	require.NoError(t, svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF, "Client Report"))
	require.NoError(t, svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatDOCX, "Branded Letter"))
	got, err := svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Equal(t, "Client Report", got)
	got, err = svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatDOCX)
	require.NoError(t, err)
	require.Equal(t, "Branded Letter", got)
	got, err = svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPPTX)
	require.NoError(t, err)
	require.Empty(t, got, "a format never set reads as none")

	// The PDF selection rides the v1 flat field for the tool's v1 readers.
	raw, ok, err := fs.ToolSettings().Get(ctx, "create_document")
	require.NoError(t, err)
	require.True(t, ok)
	var stored storedDocumentDefault
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, "Client Report", stored.DefaultPreset)
	require.Equal(t, "Client Report", stored.DefaultPresets["pdf"])
	require.Equal(t, "Branded Letter", stored.DefaultPresets["docx"])

	// Clearing the DOCX default leaves the PDF one standing.
	require.NoError(t, svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatDOCX, ""))
	got, err = svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatDOCX)
	require.NoError(t, err)
	require.Empty(t, got)
	got, err = svc.DefaultDocumentPreset(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Equal(t, "Client Report", got)

	// A default must name a preset of its own format.
	err = svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatPPTX, pdf.Name)
	require.ErrorIs(t, err, store.ErrNotFound)
	err = svc.SetDefaultDocumentPreset(ctx, store.DocumentPresetFormatDOCX, docx.Name)
	require.NoError(t, err)
}
