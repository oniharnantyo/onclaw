package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// TestWorkspacePromptDocs_ComposesAndRereads covers the adapter's core: the
// three documents are read fresh from the slug's workspace directory on
// every call (design D11 — no cache, an external edit applies to the next
// read), in composition order.
func TestWorkspacePromptDocs_ComposesAndRereads(t *testing.T) {
	const slug = "tester"
	root := t.TempDir()
	m := workspaces.NewManager(root)
	_, err := m.Create(slug)
	require.NoError(t, err)

	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, slug, name), []byte(content), 0o600))
	}
	write(workspaces.DocIdentity, "I am Tester.")
	write(workspaces.DocSoul, "I keep my word.")
	write(workspaces.DocBootstrap, "Start politely.")

	docs := NewWorkspacePromptDocs(m)
	identity, soul, bootstrap, err := docs.Documents(t.Context(), slug)
	require.NoError(t, err)
	require.Equal(t, "I am Tester.", identity)
	require.Equal(t, "I keep my word.", soul)
	require.Equal(t, "Start politely.", bootstrap)

	// The user edits SOUL.md on disk; the next read must see it.
	write(workspaces.DocSoul, "I keep it short.")
	_, soul, _, err = docs.Documents(t.Context(), slug)
	require.NoError(t, err)
	require.Equal(t, "I keep it short.", soul, "documents are read fresh, never cached")
}

// TestWorkspacePromptDocs_MissingDocuments covers the absent state: a
// workspace without the documents (a failed generation, a partial
// directory) degrades to empty contents with no error.
func TestWorkspacePromptDocs_MissingDocuments(t *testing.T) {
	root := t.TempDir()
	m := workspaces.NewManager(root)
	_, err := m.Create("tester")
	require.NoError(t, err)

	identity, soul, bootstrap, err := NewWorkspacePromptDocs(m).Documents(t.Context(), "tester")
	require.NoError(t, err)
	require.Empty(t, identity)
	require.Empty(t, soul)
	require.Empty(t, bootstrap)
}

// TestWorkspacePromptDocs_ReadError covers the failure state: a document
// that exists but cannot be read is a real IO failure and surfaces as an
// error (the turn error path decides what to do with it).
func TestWorkspacePromptDocs_ReadError(t *testing.T) {
	root := t.TempDir()
	m := workspaces.NewManager(root)
	dir, err := m.Create("tester")
	require.NoError(t, err)
	// A directory where IDENTITY.md belongs makes the read fail without
	// being a not-exist miss.
	require.NoError(t, os.Mkdir(filepath.Join(dir, workspaces.DocIdentity), 0o700))

	_, _, _, err = NewWorkspacePromptDocs(m).Documents(t.Context(), "tester")
	require.Error(t, err)
}

// TestWorkspacePromptDocs_EmptySlug covers the slug-less agent: with no slug
// there is no workspace at all, so the documents degrade to absent rather
// than failing the turn.
func TestWorkspacePromptDocs_EmptySlug(t *testing.T) {
	docs := NewWorkspacePromptDocs(workspaces.NewManager(t.TempDir()))
	identity, soul, bootstrap, err := docs.Documents(t.Context(), "")
	require.NoError(t, err)
	require.Empty(t, identity)
	require.Empty(t, soul)
	require.Empty(t, bootstrap)
}

// TestPersonaText pins the composition order and tolerance: identity, soul,
// bootstrap in that order, each trimmed, blank documents skipped, an
// all-absent persona rendering empty.
func TestPersonaText(t *testing.T) {
	t.Run("composition order", func(t *testing.T) {
		p := persona{identity: "identity", soul: "soul", bootstrap: "bootstrap"}
		require.Equal(t, "identity\n\nsoul\n\nbootstrap", p.text())
	})
	t.Run("blank documents are skipped", func(t *testing.T) {
		p := persona{identity: "  identity  ", bootstrap: "\nbootstrap\n"}
		require.Equal(t, "identity\n\nbootstrap", p.text())
	})
	t.Run("all absent renders empty", func(t *testing.T) {
		require.Empty(t, persona{}.text())
	})
}
