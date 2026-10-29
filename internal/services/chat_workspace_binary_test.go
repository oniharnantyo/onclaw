package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The workspace binary bound read's tests (add-turn-artifacts tasks 3.3,
// design D8, spec chat-panels "Preview sources" — the "Workspace PDF opens
// as a document" and "Workspace reader stays jailed" scenarios): a `.pdf`
// serves as application/pdf with base64 bytes, anything else as
// application/octet-stream, traversal is refused, and a missing file is an
// error. Reuses the chat-surface harness: a fake store plus a workspace
// manager in a temp root.

// newBinaryReadService returns the chat service with an agent ("scout")
// whose workspace exists, ready to serve binary reads from it.
func newBinaryReadService(t *testing.T) (*ChatService, *store.Agent, string) {
	t.Helper()
	svc, st, ws := newSurfaceChatService(t)
	agent := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(context.Background(), agent))
	dir, err := ws.Create("scout")
	require.NoError(t, err)
	return svc, agent, dir
}

// TestChatService_WorkspaceFileBinaryServesPDF is the preview path: a PDF
// under artifacts/ serves with the pdf mime, the byte size, and data that
// decodes to the original bytes.
func TestChatService_WorkspaceFileBinaryServesPDF(t *testing.T) {
	svc, agent, dir := newBinaryReadService(t)
	payload := []byte("%PDF-1.7 \xe2\xe3\xcf\xd3 binary-bytes")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifacts", "report.pdf"), payload, 0600))

	got, err := svc.WorkspaceFileBinary(context.Background(), agent.ID, "artifacts/report.pdf")
	require.NoError(t, err)
	require.Equal(t, "application/pdf", got.Mime)
	require.Equal(t, int64(len(payload)), got.SizeBytes)
	require.Equal(t, payload, decodeBase64(t, got.Data))
}

// TestChatService_WorkspaceFileBinaryNonPDFOctetStream pins the mime rule:
// any non-.pdf extension serves as application/octet-stream, byte-exact.
func TestChatService_WorkspaceFileBinaryNonPDFOctetStream(t *testing.T) {
	svc, agent, dir := newBinaryReadService(t)
	require.NoError(t, osWrite(filepath.Join(dir, "notes.md"), "just text"))

	got, err := svc.WorkspaceFileBinary(context.Background(), agent.ID, "notes.md")
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", got.Mime)
	require.Equal(t, int64(len("just text")), got.SizeBytes)
	require.Equal(t, []byte("just text"), decodeBase64(t, got.Data))
}

// TestChatService_WorkspaceFileBinaryRefusesTraversal pins the jail (spec
// chat-panels "Workspace reader stays jailed"): traversal is refused and no
// content is returned, even when the target exists outside the workspace.
func TestChatService_WorkspaceFileBinaryRefusesTraversal(t *testing.T) {
	svc, agent, dir := newBinaryReadService(t)
	outside := filepath.Join(dir, "..", "outside.pdf")
	require.NoError(t, osWrite(outside, "outside"))
	before, err := os.ReadFile(outside)
	require.NoError(t, err)

	for _, relPath := range []string{
		"../x",            // traversal — must never escape the workspace
		"..\\x",           // windows-style traversal
		"/etc/hosts",      // absolute foreign path
		"artifacts/../..", // interior traversal
	} {
		_, err := svc.WorkspaceFileBinary(context.Background(), agent.ID, relPath)
		require.Error(t, err, "path %q must be refused", relPath)
	}

	after, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, before, after, "the outside file was never touched")
}

// TestChatService_WorkspaceFileBinaryMissingIsError pins the missing-file
// path: a jailed, well-formed path with no file behind it is an error.
func TestChatService_WorkspaceFileBinaryMissingIsError(t *testing.T) {
	svc, agent, _ := newBinaryReadService(t)

	_, err := svc.WorkspaceFileBinary(context.Background(), agent.ID, "artifacts/missing.pdf")
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist, "the not-found cause rides through the wrap")
}
