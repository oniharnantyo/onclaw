package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestDocumentPresets_CreateReadRoundTrip covers the full column round trip:
// the store assigns the ID, stamps the timestamps (UTC), defaults an empty
// format to pdf, and carries the config JSON verbatim.
func TestDocumentPresets_CreateReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	in := &store.DocumentPreset{
		Name:   "Report",
		Config: `{"schema_version":1,"theme":"serif"}`,
	}
	require.NoError(t, s.DocumentPresets().Create(ctx, in))
	require.NotEmpty(t, in.ID, "Create must assign the ID")
	require.False(t, in.CreatedAt.IsZero(), "Create must stamp CreatedAt")
	require.False(t, in.UpdatedAt.IsZero(), "Create must stamp UpdatedAt")
	require.Equal(t, store.DocumentPresetFormatPDF, in.Format, "an empty format must default to pdf")

	got, err := s.DocumentPresets().ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, in.ID, got.ID)
	require.Equal(t, "Report", got.Name)
	require.Equal(t, store.DocumentPresetFormatPDF, got.Format)
	require.Equal(t, `{"schema_version":1,"theme":"serif"}`, got.Config, "the config JSON must round-trip verbatim")
	require.True(t, got.CreatedAt.Equal(in.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(in.UpdatedAt))

	// An explicit format is stored verbatim.
	explicit := &store.DocumentPreset{Name: "explicit", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	require.NoError(t, s.DocumentPresets().Create(ctx, explicit))
	got, err = s.DocumentPresets().ByID(ctx, explicit.ID)
	require.NoError(t, err)
	require.Equal(t, store.DocumentPresetFormatPDF, got.Format)
}

// TestDocumentPresets_DuplicateNameWithinFormat covers the (format, name)
// unique index: a case variant within the same format is rejected, while the
// same name under a different format is allowed (uniqueness is scoped to
// (name, format) — v1 only ever writes pdf, the second row pins the
// schema's scoping).
func TestDocumentPresets_DuplicateNameWithinFormat(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	presets := s.DocumentPresets()

	first := &store.DocumentPreset{Name: "Report", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	require.NoError(t, presets.Create(ctx, first))

	dup := &store.DocumentPreset{Name: "report", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	require.ErrorIs(t, presets.Create(ctx, dup), store.ErrDuplicate,
		"the NOCASE unique index must reject case variants within the format")

	other := &store.DocumentPreset{Name: "report", Format: "docx", Config: "{}"}
	require.NoError(t, presets.Create(ctx, other),
		"the same name under a different format must be allowed")

	// An update may keep the row's own name but not take another's within
	// the format.
	first.Name = "REPORT"
	require.NoError(t, presets.Update(ctx, first), "renaming to a case variant of itself is not a collision")
	first.Name = "report"
	require.NoError(t, presets.Update(ctx, first), "the row's own name in another case must not collide with itself")

	second := &store.DocumentPreset{Name: "other", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	require.NoError(t, presets.Create(ctx, second))
	second.Name = "REPORT"
	require.ErrorIs(t, presets.Update(ctx, second), store.ErrDuplicate,
		"an update onto another preset's name within the format must be rejected")
}

// TestDocumentPresets_NotFound covers the sentinel on every addressing
// operation: ByID, ByName, Update, and Delete on an absent row.
func TestDocumentPresets_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	presets := s.DocumentPresets()

	_, err := presets.ByID(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = presets.ByName(ctx, store.DocumentPresetFormatPDF, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, presets.Update(ctx, &store.DocumentPreset{ID: "missing", Name: "x"}), store.ErrNotFound)
	require.ErrorIs(t, presets.Delete(ctx, "missing"), store.ErrNotFound)
}

// TestDocumentPresets_ListFiltersAndOrders covers List: only the requested
// format's rows, ordered by name case-insensitively.
func TestDocumentPresets_ListFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	presets := s.DocumentPresets()

	for _, name := range []string{"zeta", "alpha", "Midway"} {
		require.NoError(t, presets.Create(ctx, &store.DocumentPreset{Name: name, Format: store.DocumentPresetFormatPDF, Config: "{}"}))
	}
	require.NoError(t, presets.Create(ctx, &store.DocumentPreset{Name: "alien", Format: "docx", Config: "{}"}))

	list, err := presets.List(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Len(t, list, 3, "only the requested format's presets are listed")
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	require.Equal(t, []string{"alpha", "Midway", "zeta"}, names, "List must sort by name, case-insensitive")

	empty, err := presets.List(ctx, "pptx")
	require.NoError(t, err)
	require.Empty(t, empty)
}

// TestDocumentPresets_UpdateRewritesDefinition covers Update's field set:
// name, format, and config move, CreatedAt does not, UpdatedAt advances.
func TestDocumentPresets_UpdateRewritesDefinition(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	presets := s.DocumentPresets()

	in := &store.DocumentPreset{Name: "before", Format: store.DocumentPresetFormatPDF, Config: `{"theme":"serif"}`}
	require.NoError(t, presets.Create(ctx, in))

	in.Name = "after"
	in.Config = `{"theme":"sans"}`
	require.NoError(t, presets.Update(ctx, in))

	got, err := presets.ByID(ctx, in.ID)
	require.NoError(t, err)
	require.Equal(t, "after", got.Name)
	require.Equal(t, `{"theme":"sans"}`, got.Config)
	require.Equal(t, store.DocumentPresetFormatPDF, got.Format)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt), "Update must not touch CreatedAt")
	require.False(t, got.UpdatedAt.Before(in.UpdatedAt))
}

// TestDocumentPresets_DeleteRemovesOnlyTheRow covers the spec's delete
// scenario: the deleted row is gone; every other row survives untouched.
func TestDocumentPresets_DeleteRemovesOnlyTheRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	presets := s.DocumentPresets()

	kept := &store.DocumentPreset{Name: "kept", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	survivor := &store.DocumentPreset{Name: "survivor", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	gone := &store.DocumentPreset{Name: "gone", Format: store.DocumentPresetFormatPDF, Config: "{}"}
	for _, p := range []*store.DocumentPreset{kept, survivor, gone} {
		require.NoError(t, presets.Create(ctx, p))
	}

	require.NoError(t, presets.Delete(ctx, gone.ID))
	_, err := presets.ByID(ctx, gone.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "the deleted preset's row must be gone")

	got, err := presets.ByID(ctx, kept.ID)
	require.NoError(t, err, "another preset must survive the delete")
	require.Equal(t, "kept", got.Name)
	require.Equal(t, "{}", got.Config)

	after, err := presets.List(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Len(t, after, 2)
}

// TestDocumentPresets_PersistAcrossReopen covers the spec's restart
// scenario: a preset written before a close reads back identically after the
// database is reopened (the backfill_test open-close-reopen pattern).
func TestDocumentPresets_PersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "presets.db")

	st, err := Open(ctx, path)
	require.NoError(t, err)
	in := &store.DocumentPreset{
		Name:   "Report",
		Format: store.DocumentPresetFormatPDF,
		Config: `{"schema_version":1,"theme":"serif"}`,
	}
	require.NoError(t, st.DocumentPresets().Create(ctx, in))
	require.NoError(t, st.Close())

	again, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = again.Close() })

	got, err := again.DocumentPresets().ByID(ctx, in.ID)
	require.NoError(t, err, "the preset must survive the restart")
	require.Equal(t, "Report", got.Name)
	require.Equal(t, store.DocumentPresetFormatPDF, got.Format)
	require.Equal(t, `{"schema_version":1,"theme":"serif"}`, got.Config)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(in.UpdatedAt))
}

// TestDocumentPresets_TxCommitPersists covers the tx-scoped preset store: a
// create inside WithTx commits with the transaction.
func TestDocumentPresets_TxCommitPersists(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.WithTx(ctx, func(tx store.Store) error {
		return tx.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "in-tx", Config: "{}"})
	}))

	got, err := s.DocumentPresets().ByName(ctx, store.DocumentPresetFormatPDF, "in-tx")
	require.NoError(t, err)
	require.Equal(t, "{}", got.Config)
}

// TestDocumentPresets_TxErrorLeavesNoPartialRows covers the atomicity
// requirement with the atomicity_test pattern: a preset write that fails
// part-way (returned error or the database's own unique index) leaves no
// partial rows, a failed update leaves the stored row untouched, and the
// store stays usable afterwards.
func TestDocumentPresets_TxErrorLeavesNoPartialRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sentinel := errors.New("preset save exploded")
	err := s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "partial-a", Config: "{}"}); err != nil {
			return err
		}
		if err := tx.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "partial-b", Config: "{}"}); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	list, err := s.DocumentPresets().List(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Empty(t, list, "an aborted config write must leave no partial rows")

	// The same guarantee when the failure comes from the database itself
	// (the (format, name) unique index) rather than a returned error.
	err = s.WithTx(ctx, func(tx store.Store) error {
		if err := tx.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "dup", Config: "{}"}); err != nil {
			return err
		}
		return tx.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "dup", Config: "{}"})
	})
	require.ErrorIs(t, err, store.ErrDuplicate)
	list, err = s.DocumentPresets().List(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Empty(t, list, "a constraint violation must roll back earlier inserts")

	// An update that fails after the write leaves the stored row untouched.
	require.NoError(t, s.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "fresh", Config: "{}"}))
	loaded, err := s.DocumentPresets().ByName(ctx, store.DocumentPresetFormatPDF, "fresh")
	require.NoError(t, err)
	err = s.WithTx(ctx, func(tx store.Store) error {
		loaded.Config = `{"theme":"sans"}`
		if err := tx.DocumentPresets().Update(ctx, loaded); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	got, err := s.DocumentPresets().ByID(ctx, loaded.ID)
	require.NoError(t, err)
	require.Equal(t, "{}", got.Config, "a failed update must roll back to the stored config")

	// The store stays usable after the rolled-back transactions.
	require.NoError(t, s.DocumentPresets().Create(ctx, &store.DocumentPreset{Name: "next", Config: "{}"}))
	list, err = s.DocumentPresets().List(ctx, store.DocumentPresetFormatPDF)
	require.NoError(t, err)
	require.Len(t, list, 2)
}
