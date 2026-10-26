package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The generated-document bound read's tests (add-create-document tasks 3.2,
// design D7, spec chat-tool-cards "Generated document access"): a plain
// basename is served from the agent's workspace root only; anything carrying
// directory components or traversing is refused before the filesystem is
// touched; a missing file is a not-found error. Reuses the chat-surface
// harness: a fake store plus a workspace manager in a temp root.

// newDocumentReadService returns the chat service with an agent ("scout")
// whose workspace exists, ready to serve generated documents from its root.
func newDocumentReadService(t *testing.T) (*ChatService, *store.Agent, string) {
	t.Helper()
	svc, st, ws := newSurfaceChatService(t)
	agent := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(context.Background(), agent))
	dir, err := ws.Create("scout")
	require.NoError(t, err)
	return svc, agent, dir
}

// TestChatService_GeneratedDocumentServesValidName is the acceptance path:
// the tool's reported file name reads back the bytes and the size from the
// workspace root, and reports only the name — never the on-disk path.
func TestChatService_GeneratedDocumentServesValidName(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	payload := "%PDF-1.7 generated-bytes"
	require.NoError(t, osWrite(filepath.Join(dir, "report.pdf"), payload))

	got, err := svc.GeneratedDocument(context.Background(), agent.ID, "report.pdf")
	require.NoError(t, err)
	require.Equal(t, "report.pdf", got.Name)
	require.Equal(t, int64(len(payload)), got.SizeBytes)
	require.Equal(t, []byte(payload), decodeBase64(t, got.Data))
}

// TestChatService_GeneratedDocumentRefusesBadNames pins the refusal rules
// (spec: refused before the filesystem is touched): traversal, directory
// components, and absolute paths are refused even when a file exists at the
// addressed location — and nothing outside the workspace is read.
func TestChatService_GeneratedDocumentRefusesBadNames(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	// Files that exist at the addressed locations: a traversal target
	// outside the workspace and a nested file.
	outside := filepath.Join(dir, "..", "outside.pdf")
	require.NoError(t, osWrite(outside, "outside"))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "sub", "x.pdf"), "nested"))
	outsideBefore, err := os.ReadFile(outside)
	require.NoError(t, err)

	for _, name := range []string{
		"../x",         // traversal — must never escape the workspace
		"..\\x",        // windows-style traversal
		"sub/x.pdf",    // directory component, file exists
		"sub/../x.pdf", // interior directory component
		"/abs/x.pdf",   // absolute path
		".",            // the workspace root itself
		"..",           // the root's parent
		"",             // empty
	} {
		_, err := svc.GeneratedDocument(context.Background(), agent.ID, name)
		require.Error(t, err, "name %q must be refused", name)
		require.ErrorContains(t, err, "refused", "name %q must be refused before the filesystem", name)
	}

	// The outside file is untouched and was never served.
	outsideAfter, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, outsideBefore, outsideAfter)
}

// TestChatService_GeneratedDocumentMissingIsNotFound pins the not-found
// contract: a well-formed name with no file behind it wraps
// store.ErrNotFound, which the card renders as an unavailable state.
func TestChatService_GeneratedDocumentMissingIsNotFound(t *testing.T) {
	svc, agent, _ := newDocumentReadService(t)

	_, err := svc.GeneratedDocument(context.Background(), agent.ID, "missing.pdf")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChatService_GeneratedDocumentUnknownAgent pins the agent resolution:
// the read resolves the agent exactly like the workspace preview surface, so
// an unknown agent id is a not-found error.
func TestChatService_GeneratedDocumentUnknownAgent(t *testing.T) {
	svc, _, _ := newDocumentReadService(t)

	_, err := svc.GeneratedDocument(context.Background(), "no-such-agent", "report.pdf")
	require.ErrorIs(t, err, store.ErrNotFound)
}
