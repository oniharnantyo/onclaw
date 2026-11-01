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
// design D7, spec chat-tool-cards "Generated document access"; the
// artifacts-first order is add-turn-artifacts task 3.2, design D6): a plain
// basename is served from the agent's workspace artifacts/ directory first,
// falling back to the legacy workspace root when absent there; anything
// carrying directory components or traversing is refused before the
// filesystem is touched; a file in neither location is a not-found error.
// The Save As export's tests (add-artifact-save-as tasks 2.1/2.2, design D1)
// pin the same refusals, the same jailed read, and the dialog seam: the fake
// dialog records the seeded name and returns a configurable destination.
// Reuses the chat-surface harness: a fake store plus a workspace manager in
// a temp root.

// newDocumentReadService returns the chat service with an agent ("scout")
// whose workspace exists, ready to serve generated documents from its
// artifacts/ directory (or its legacy root).
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
// workspace's artifacts/ directory, and reports only the name — never the
// on-disk path.
func TestChatService_GeneratedDocumentServesValidName(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	payload := "%PDF-1.7 generated-bytes"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), payload))

	got, err := svc.GeneratedDocument(context.Background(), agent.ID, "", "report.pdf")
	require.NoError(t, err)
	require.Equal(t, "report.pdf", got.Name)
	require.Equal(t, int64(len(payload)), got.SizeBytes)
	require.Equal(t, []byte(payload), decodeBase64(t, got.Data))
}

// TestChatService_GeneratedDocumentLegacyRootFallback covers the spec's
// legacy scenario (spec chat-tool-cards "Legacy root document still
// renders"): a name whose file exists only at the workspace root — generated
// before artifacts/ was introduced — is served through the fallback.
func TestChatService_GeneratedDocumentLegacyRootFallback(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	payload := "%PDF-1.4 pre-artifacts-bytes"
	require.NoError(t, osWrite(filepath.Join(dir, "old-report.pdf"), payload))

	got, err := svc.GeneratedDocument(context.Background(), agent.ID, "", "old-report.pdf")
	require.NoError(t, err)
	require.Equal(t, "old-report.pdf", got.Name)
	require.Equal(t, []byte(payload), decodeBase64(t, got.Data))
}

// TestChatService_GeneratedDocumentArtifactsTakesPrecedence covers the spec's
// precedence scenario (spec chat-tool-cards "Artifacts copy takes
// precedence"): a name present both under artifacts/ and at the root serves
// the artifacts copy.
func TestChatService_GeneratedDocumentArtifactsTakesPrecedence(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), "artifacts-copy"))
	require.NoError(t, osWrite(filepath.Join(dir, "report.pdf"), "legacy-root-copy"))

	got, err := svc.GeneratedDocument(context.Background(), agent.ID, "", "report.pdf")
	require.NoError(t, err)
	require.Equal(t, []byte("artifacts-copy"), decodeBase64(t, got.Data), "the artifacts copy wins")
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
		_, err := svc.GeneratedDocument(context.Background(), agent.ID, "", name)
		require.Error(t, err, "name %q must be refused", name)
		require.ErrorContains(t, err, "refused", "name %q must be refused before the filesystem", name)
	}

	// The outside file is untouched and was never served.
	outsideAfter, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, outsideBefore, outsideAfter)
}

// TestChatService_GeneratedDocumentMissingIsNotFound pins the not-found
// contract: a well-formed name with no file behind it — neither under
// artifacts/ nor at the legacy root — wraps store.ErrNotFound, which the
// card renders as an unavailable state.
func TestChatService_GeneratedDocumentMissingIsNotFound(t *testing.T) {
	svc, agent, _ := newDocumentReadService(t)

	_, err := svc.GeneratedDocument(context.Background(), agent.ID, "", "missing.pdf")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChatService_GeneratedDocumentUnknownAgent pins the agent resolution:
// the read resolves the agent exactly like the workspace preview surface, so
// an unknown agent id is a not-found error.
func TestChatService_GeneratedDocumentUnknownAgent(t *testing.T) {
	svc, _, _ := newDocumentReadService(t)

	_, err := svc.GeneratedDocument(context.Background(), "no-such-agent", "", "report.pdf")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// fakeSaveDialog is the hand-written SaveDialog fake: it records the name the
// service seeded the dialog with and answers with a configurable
// destination/error.
type fakeSaveDialog struct {
	gotName string
	dest    string
	err     error
}

// SaveFile records the seeded name and returns the configured answer.
func (f *fakeSaveDialog) SaveFile(name string) (string, error) {
	f.gotName = name
	return f.dest, f.err
}

// TestChatService_SaveGeneratedDocumentAsCopiesArtifactsFile is the
// acceptance path (add-artifact-save-as design D1): the dialog is seeded
// with the artifact name, the chosen destination receives a byte-identical
// copy, and the destination is the method's result.
func TestChatService_SaveGeneratedDocumentAsCopiesArtifactsFile(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	payload := "%PDF-1.7 export-bytes"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), payload))
	dest := filepath.Join(t.TempDir(), "saved.pdf")
	dialog := &fakeSaveDialog{dest: dest}
	WithSaveDialog(dialog)(svc)

	got, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", "report.pdf")
	require.NoError(t, err)
	require.Equal(t, dest, got)
	require.Equal(t, "report.pdf", dialog.gotName, "the dialog is seeded with the artifact name")
	written, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, []byte(payload), written, "the copy is byte-identical")
}

// TestChatService_SaveGeneratedDocumentAsLegacyRootFallback covers the
// legacy scenario: a name whose file exists only at the workspace root is
// still exported through the fallback.
func TestChatService_SaveGeneratedDocumentAsLegacyRootFallback(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	payload := "%PDF-1.4 pre-artifacts-bytes"
	require.NoError(t, osWrite(filepath.Join(dir, "old-report.pdf"), payload))
	dest := filepath.Join(t.TempDir(), "saved.pdf")
	WithSaveDialog(&fakeSaveDialog{dest: dest})(svc)

	got, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", "old-report.pdf")
	require.NoError(t, err)
	require.Equal(t, dest, got)
	written, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, []byte(payload), written)
}

// TestChatService_SaveGeneratedDocumentAsArtifactsTakesPrecedence covers the
// precedence scenario: a name present both under artifacts/ and at the root
// exports the artifacts copy's bytes.
func TestChatService_SaveGeneratedDocumentAsArtifactsTakesPrecedence(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), "artifacts-copy"))
	require.NoError(t, osWrite(filepath.Join(dir, "report.pdf"), "legacy-root-copy"))
	dest := filepath.Join(t.TempDir(), "saved.pdf")
	WithSaveDialog(&fakeSaveDialog{dest: dest})(svc)

	_, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", "report.pdf")
	require.NoError(t, err)
	written, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, []byte("artifacts-copy"), written, "the artifacts copy wins")
}

// TestChatService_SaveGeneratedDocumentAsRefusesBadNames pins that the same
// refusal rules as GeneratedDocument run BEFORE the dialog or the
// filesystem: the fake dialog is never opened and no destination is written.
func TestChatService_SaveGeneratedDocumentAsRefusesBadNames(t *testing.T) {
	svc, agent, _ := newDocumentReadService(t)
	dialog := &fakeSaveDialog{dest: filepath.Join(t.TempDir(), "saved.pdf")}
	WithSaveDialog(dialog)(svc)

	for _, name := range []string{
		"",           // empty
		".",          // the workspace root itself
		"..",         // the root's parent
		"../x.pdf",   // traversal
		"a/b.pdf",    // directory component
		"a\\b.pdf",   // windows-style path
		"/tmp/x.pdf", // absolute path
	} {
		_, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", name)
		require.Error(t, err, "name %q must be refused", name)
		require.ErrorContains(t, err, "refused", "name %q must be refused before the dialog", name)
	}
	require.Empty(t, dialog.gotName, "the dialog must never be opened for a refused name")
}

// TestChatService_SaveGeneratedDocumentAsCancelled pins the cancellation
// contract: a dialog answered with no destination is exactly
// ErrExportCancelled and nothing is written.
func TestChatService_SaveGeneratedDocumentAsCancelled(t *testing.T) {
	svc, agent, _ := newDocumentReadService(t)
	WithSaveDialog(&fakeSaveDialog{})(svc)

	_, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", "report.pdf")
	require.ErrorIs(t, err, ErrExportCancelled)
}

// TestChatService_SaveGeneratedDocumentAsUnavailable pins the default: with
// no WithSaveDialog option the service carries NoSaveDialog, so the export
// fails with exactly ErrExportUnavailable (no dialog on this host).
func TestChatService_SaveGeneratedDocumentAsUnavailable(t *testing.T) {
	svc, agent, _ := newDocumentReadService(t)

	_, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", "report.pdf")
	require.ErrorIs(t, err, ErrExportUnavailable)
}

// TestChatService_SaveGeneratedDocumentAsWriteFailure pins that a failing
// copy is a wrapped write error, not a sentinel: a destination in a
// nonexistent directory cannot be written.
func TestChatService_SaveGeneratedDocumentAsWriteFailure(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), "payload"))
	dest := filepath.Join(t.TempDir(), "no-such-dir", "saved.pdf")
	WithSaveDialog(&fakeSaveDialog{dest: dest})(svc)

	_, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, "", "report.pdf")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrExportUnavailable)
	require.NotErrorIs(t, err, ErrExportCancelled)
	require.ErrorContains(t, err, "write")
}

// TestChatService_GeneratedDocumentSessionArtifactsFirst covers the
// per-session layout: the calling session's sessions/<id>/artifacts/ copy
// wins over the legacy workspace-root artifacts/ one, and a name present
// only in the legacy home still serves with an empty session id.
func TestChatService_GeneratedDocumentSessionArtifactsFirst(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	const sessionID = "0ae4aa0a3c954cbd9dfaf078ae573eae"

	// Both homes hold the name: the session copy is the one served.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), "legacy-bytes"))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sessions", sessionID, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "sessions", sessionID, "artifacts", "report.pdf"), "session-bytes"))

	got, err := svc.GeneratedDocument(context.Background(), agent.ID, sessionID, "report.pdf")
	require.NoError(t, err)
	require.Equal(t, []byte("session-bytes"), decodeBase64(t, got.Data),
		"the session's artifacts copy takes precedence over the legacy home")

	// The legacy home serves when the session's has no such file.
	got, err = svc.GeneratedDocument(context.Background(), agent.ID, sessionID, "old.pdf")
	require.Error(t, err, "old.pdf does not exist in either home")
	legacyOnly := "%PDF-1.7 legacy-only"
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "only-legacy.pdf"), legacyOnly))
	got, err = svc.GeneratedDocument(context.Background(), agent.ID, sessionID, "only-legacy.pdf")
	require.NoError(t, err)
	require.Equal(t, []byte(legacyOnly), decodeBase64(t, got.Data),
		"the legacy artifacts home serves when the session's copy is absent")
}

// TestChatService_GeneratedDocumentRefusesBadSessionID pins the session-id
// jail on the read path: a non-conforming session id is refused before any
// filesystem call, exactly like a bad name.
func TestChatService_GeneratedDocumentRefusesBadSessionID(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "artifacts", "report.pdf"), "%PDF-1.7"))

	for _, bad := range []string{"../../evil", "a/b", "short", "0AE4AA0A3C954CBD9DFAF078AE573EAE", "."} {
		_, err := svc.GeneratedDocument(context.Background(), agent.ID, bad, "report.pdf")
		require.Error(t, err, "session id %q must refuse", bad)
		require.Contains(t, err.Error(), "refused session id")
	}
}

// TestChatService_SaveGeneratedDocumentAsSessionArtifacts pins the export
// path through the per-session layout: the dialog is seeded as always and
// the bytes come from the calling session's artifacts directory.
func TestChatService_SaveGeneratedDocumentAsSessionArtifacts(t *testing.T) {
	svc, agent, dir := newDocumentReadService(t)
	const sessionID = "0ae4aa0a3c954cbd9dfaf078ae573eae"
	payload := "%PDF-1.7 session-export"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sessions", sessionID, "artifacts"), 0o700))
	require.NoError(t, osWrite(filepath.Join(dir, "sessions", sessionID, "artifacts", "report.pdf"), payload))
	dest := filepath.Join(t.TempDir(), "saved.pdf")
	dialog := &fakeSaveDialog{dest: dest}
	WithSaveDialog(dialog)(svc)

	got, err := svc.SaveGeneratedDocumentAs(context.Background(), agent.ID, sessionID, "report.pdf")
	require.NoError(t, err)
	require.Equal(t, dest, got)
	require.Equal(t, "report.pdf", dialog.gotName)
	written, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, []byte(payload), written, "the session copy exports byte-identical")
}
