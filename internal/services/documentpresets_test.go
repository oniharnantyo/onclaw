package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

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
