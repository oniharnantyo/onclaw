package fake_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestDocumentPresets_Fake_CRUDAndNotFound mirrors the sqlite adapter's
// lifecycle over the fake: create stamps, assigns, and defaults the format,
// reads copy (no aliasing), update rewrites the definition, and every
// absent-row path is the sentinel.
func TestDocumentPresets_Fake_CRUDAndNotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	presets := s.DocumentPresets()

	in := &store.DocumentPreset{Name: "Report", Config: `{"schema_version":1}`}
	require.NoError(t, presets.Create(ctx, in))
	require.NotEmpty(t, in.ID, "Create must assign the ID")
	require.False(t, in.CreatedAt.IsZero(), "Create must stamp CreatedAt")
	require.False(t, in.UpdatedAt.IsZero(), "Create must stamp UpdatedAt")
	require.Equal(t, store.DocumentPresetFormatPDF, in.Format, "an empty format must default to pdf")

	got, err := presets.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, "Report", got.Name)
	require.Equal(t, `{"schema_version":1}`, got.Config)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(in.UpdatedAt))

	// Mutating a returned preset must not alias the fake's state.
	got.Config = `{"mutated":true}`
	again, err := presets.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, `{"schema_version":1}`, again.Config, "returned presets must be copies")

	// List orders by name, case-insensitive.
	require.NoError(t, presets.Create(ctx, &store.DocumentPreset{Name: "zeta", Config: "{}"}))
	require.NoError(t, presets.Create(ctx, &store.DocumentPreset{Name: "Midway", Config: "{}"}))
	list, err := presets.List(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	require.Equal(t, []string{"Midway", "Report", "zeta"}, names)

	// Update rewrites the definition and stamps UpdatedAt.
	in.Name = "renamed"
	in.Config = `{"theme":"sans"}`
	require.NoError(t, presets.Update(ctx, in))
	got, err = presets.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
	require.Equal(t, `{"theme":"sans"}`, got.Config)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt), "Update must not touch CreatedAt")

	// Absent paths are the sentinel.
	_, err = presets.ByID(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = presets.ByName(ctx, store.DocumentPresetFormatPDF, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, presets.Update(ctx, &store.DocumentPreset{ID: "missing", Name: "x"}), store.ErrNotFound)
	require.ErrorIs(t, presets.Delete(ctx, "missing"), store.ErrNotFound)

	require.NoError(t, presets.Delete(ctx, in.ID))
	_, err = presets.ByID(ctx, in.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestDocumentPresets_Fake_DuplicateNameWithinFormat mirrors the sqlite
// (format, name) unique index: case variants within a format are rejected,
// the same name under another format is allowed.
func TestDocumentPresets_Fake_DuplicateNameWithinFormat(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	presets := s.DocumentPresets()

	first := &store.DocumentPreset{Name: "Report", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	require.NoError(t, presets.Create(ctx, first))

	require.ErrorIs(t,
		presets.Create(ctx, &store.DocumentPreset{Name: "report", Format: store.DocumentPresetFormatPDF, Config: "{}"}),
		store.ErrDuplicate)

	// The same name under a different format is a different key.
	other := &store.DocumentPreset{Name: "report", Format: "docx", Config: "{}"}
	require.NoError(t, presets.Create(ctx, other),
		"the same name under a different format must be allowed")

	// A row may keep its own name in another case.
	first.Name = "REPORT"
	require.NoError(t, presets.Update(ctx, first))

	second := &store.DocumentPreset{Name: "other", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	require.NoError(t, presets.Create(ctx, second))
	second.Name = "report"
	require.ErrorIs(t, presets.Update(ctx, second), store.ErrDuplicate)
}
