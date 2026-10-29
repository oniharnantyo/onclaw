package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestStore_DocumentTemplatesAccessor pins the aggregate accessor's wiring
// (add-document-editing task 5.2): the sqlite store exposes a working
// template store rooted at the database's data directory, with the
// interface's Save/Path/Delete round trip.
func TestStore_DocumentTemplatesAccessor(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	templates := st.DocumentTemplates()
	require.NotNil(t, templates)

	path, err := templates.Save(ctx, "preset-1", "Branded.docx", []byte("SKELETON"))
	require.NoError(t, err)
	require.FileExists(t, path, "the template file landed on disk")

	got, err := templates.Path(ctx, "preset-1")
	require.NoError(t, err)
	data, err := os.ReadFile(got)
	require.NoError(t, err)
	require.Equal(t, []byte("SKELETON"), data, "the stored skeleton round-trips")

	// The store roots at the database's parent — the resolved layout's data
	// directory (the backfill's precedent), so the templates live under
	// <dataDir>/presets/templates.
	require.Contains(t, filepath.Dir(path), "presets", "the template roots under the data directory")

	require.NoError(t, templates.Delete(ctx, "preset-1"))
	_, err = templates.Path(ctx, "preset-1")
	require.ErrorIs(t, err, store.ErrNotFound, "the delete cleans the template")
}
