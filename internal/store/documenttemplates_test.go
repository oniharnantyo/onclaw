package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The document-template store tests (add-document-editing task 4.1, spec
// document-generation "Preset round-trips"): the skeleton lands under the
// data dir, survives a fresh store instance (the restart), and deleting is
// idempotent.

// newTemplateStore builds a store over a temporary data dir, the shape the
// composition root wires (config.Resolve's root).
func newTemplateStore(t *testing.T) (DocumentTemplateStore, string) {
	t.Helper()
	dataDir := t.TempDir()
	return NewDocumentTemplateStore(dataDir), dataDir
}

// TestDocumentTemplateStore_SavePathRoundTrip covers the spec's round-trip
// scenario: an uploaded skeleton is stored under the data dir at
// presets/templates/<presetID><ext> and is still found by a fresh store
// instance — the app restart.
func TestDocumentTemplateStore_SavePathRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, dataDir := newTemplateStore(t)

	path, err := s.Save(ctx, "preset-1", "Branded Letter.docx", []byte("DOCX-BYTES"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dataDir, "presets", "templates", "preset-1.docx"), path)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("DOCX-BYTES"), onDisk)

	// The restart: a store built over the same data dir finds the file.
	reopened := NewDocumentTemplateStore(dataDir)
	got, err := reopened.Path(ctx, "preset-1")
	require.NoError(t, err)
	require.Equal(t, path, got)

	// The same round-trip for a PPTX skeleton.
	_, err = s.Save(ctx, "preset-2", "Deck.pptx", []byte("PPTX-BYTES"))
	require.NoError(t, err)
	got, err = reopened.Path(ctx, "preset-2")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dataDir, "presets", "templates", "preset-2.pptx"), got)
}

// TestDocumentTemplateStore_SaveReplaces covers one-template-per-preset: a
// re-upload under another extension replaces the stored file, so Path never
// sees two candidates.
func TestDocumentTemplateStore_SaveReplaces(t *testing.T) {
	ctx := context.Background()
	s, dataDir := newTemplateStore(t)

	_, err := s.Save(ctx, "preset-1", "Old.docx", []byte("OLD"))
	require.NoError(t, err)
	path, err := s.Save(ctx, "preset-1", "New.pptx", []byte("NEW"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dataDir, "presets", "templates", "preset-1.pptx"), path)

	_, err = os.Stat(filepath.Join(dataDir, "presets", "templates", "preset-1.docx"))
	require.True(t, os.IsNotExist(err), "the replaced extension is gone")

	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("NEW"), onDisk)
}

// TestDocumentTemplateStore_PathNotFound pins the miss: a preset with no
// template on disk is store.ErrNotFound.
func TestDocumentTemplateStore_PathNotFound(t *testing.T) {
	ctx := context.Background()
	s, _ := newTemplateStore(t)
	_, err := s.Path(ctx, "no-such-preset")
	require.ErrorIs(t, err, ErrNotFound)
}

// TestDocumentTemplateStore_DeleteIsIdempotent covers the delete path: the
// file goes away, and deleting a preset whose upload never happened (or was
// already deleted) succeeds — the preset's delete must not fail on it.
func TestDocumentTemplateStore_DeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, dataDir := newTemplateStore(t)

	_, err := s.Save(ctx, "preset-1", "Letter.docx", []byte("DOCX"))
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, "preset-1"))
	_, err = s.Path(ctx, "preset-1")
	require.ErrorIs(t, err, ErrNotFound)

	// Already-absent deletes stay successful.
	require.NoError(t, s.Delete(ctx, "preset-1"))
	require.NoError(t, s.Delete(ctx, "never-saved"))
	_, err = os.Stat(filepath.Join(dataDir, "presets", "templates"))
	require.NoError(t, err, "the templates directory survives")
}

// TestDocumentTemplateStore_RejectsUnsafeIDs pins the boundary guard: the
// preset ID is joined into a file name, so separators and traversal are
// refused at the store, and so is an empty upload file name.
func TestDocumentTemplateStore_RejectsUnsafeIDs(t *testing.T) {
	ctx := context.Background()
	s, _ := newTemplateStore(t)

	for _, id := range []string{"", "../escape", "a/b", `a\b`, ".."} {
		_, err := s.Save(ctx, id, "Letter.docx", []byte("x"))
		require.Error(t, err, "preset id %q", id)
		_, err = s.Path(ctx, id)
		require.Error(t, err, "preset id %q", id)
	}
	_, err := s.Save(ctx, "preset-1", "", []byte("x"))
	require.ErrorContains(t, err, "needs a file name")
}
