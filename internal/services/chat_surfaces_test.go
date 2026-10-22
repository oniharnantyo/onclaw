package services

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// decodeBase64 decodes a served content payload for assertions.
func decodeBase64(t *testing.T, raw string) []byte {
	t.Helper()
	out, err := base64.StdEncoding.DecodeString(raw)
	require.NoError(t, err)
	return out
}

// osWrite writes a file with the private-to-user mode, matching the layout.
func osWrite(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

// The chat attachment, workspace-preview, transcript-page, and delete
// service-surface tests (tasks 5.2/6.1/6.5/8.1): the bound calls over real
// stores in temp roots.

// newSurfaceChatService wires a ChatService over a real sessions service and
// fake store, with the attachment store and workspace manager rooted in temp
// directories.
func newSurfaceChatService(t *testing.T) (*ChatService, *fake.Store, *workspaces.Manager) {
	t.Helper()
	st := fake.New()
	ws := workspaces.NewManager(t.TempDir())
	svc := NewChatService(newFakeAgentManager(), sessions.NewService(st), &recordingEmitter{},
		st, attachments.NewStore(t.TempDir(), st.Attachments()), ws)
	return svc, st, ws
}

// TestChatService_SaveAttachmentAndContentRoundTrip covers the bound
// attachment surface (design D5): the upload stores and serves by capability
// key, and the served content decodes to the original bytes.
func TestChatService_SaveAttachmentAndContentRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newSurfaceChatService(t)

	payload := []byte("png-bytes-here")
	att, err := svc.SaveAttachment(ctx, "shot.png", "image/png", base64.StdEncoding.EncodeToString(payload))
	require.NoError(t, err)
	require.Regexp(t, `^[0-9a-f]{32}$`, att.Key)
	require.Equal(t, "shot.png", att.Name)
	require.Equal(t, "image/png", att.Mime)
	require.Equal(t, int64(len(payload)), att.Size)

	content, err := svc.AttachmentContent(ctx, att.Key)
	require.NoError(t, err)
	require.Equal(t, att.Key, content.Key)
	require.Equal(t, "shot.png", content.Name)
	require.Equal(t, "image/png", content.Mime)
	require.Equal(t, int64(len(payload)), content.Size)
	require.Equal(t, payload, decodeBase64(t, content.Data))

	// The rejection reason rides the bound error.
	_, err = svc.SaveAttachment(ctx, "x.bin", "application/octet-stream", base64.StdEncoding.EncodeToString([]byte("no")))
	require.ErrorIs(t, err, attachments.ErrRejected)

	// An unknown capability key is refused.
	_, err = svc.AttachmentContent(ctx, "ffffffffffffffffffffffffffffffff")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChatService_DeleteSession pins the bound delete (spec chat-view):
// remove removes the session and its transcript, an unknown session is a
// safe no-op, and a session with a turn in flight is refused.
func TestChatService_DeleteSession(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newSurfaceChatService(t)
	sessionSvc := sessions.NewService(st)

	sess, err := sessionSvc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)
	require.NoError(t, sessionSvc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "keep me? no"},
	}))

	// Busy sessions are refused (the transcript is being written).
	turns := newFakeAgentManager()
	turns.busy[sess.ID] = true
	busySvc := NewChatService(turns, sessionSvc, &recordingEmitter{},
		st, attachments.NewStore(t.TempDir(), st.Attachments()), workspaces.NewManager(t.TempDir()))
	err = busySvc.DeleteSession(ctx, sess.ID)
	require.ErrorIs(t, err, agents.ErrTurnInProgress)

	// The quiet path removes session and transcript.
	require.NoError(t, svc.DeleteSession(ctx, sess.ID))
	_, err = sessionSvc.Session(ctx, sess.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	events, err := svc.Transcript(ctx, sess.ID)
	require.NoError(t, err)
	require.Empty(t, events)

	// An unknown session deletes as a safe no-op.
	require.NoError(t, svc.DeleteSession(ctx, "never-existed"))
}

// TestChatService_TranscriptPage pins the bound window (design D6): the page
// endpoints replay newest-first and page backward through the sessions
// service.
func TestChatService_TranscriptPage(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newSurfaceChatService(t)
	sessionSvc := sessions.NewService(st)

	sess, err := sessionSvc.CreateSession(ctx, "agent-1", "")
	require.NoError(t, err)
	require.NoError(t, sessionSvc.Append(ctx, sess.ID, []sessions.Event{
		{Kind: sessions.KindUserMessage, Text: "one"},
		{Kind: sessions.KindUserMessage, Text: "two"},
		{Kind: sessions.KindUserMessage, Text: "three"},
	}))

	page, err := svc.TranscriptPage(ctx, sess.ID, 0, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, "two", page[0].Text)
	require.Equal(t, "three", page[1].Text, "the newest page ends at the log's tail")

	older, err := svc.TranscriptPage(ctx, sess.ID, page[0].Seq, 2)
	require.NoError(t, err)
	require.Len(t, older, 1)
	require.Equal(t, "one", older[0].Text)
}

// TestChatService_WorkspaceReaders pin the bound jailed readers (design D8):
// in-workspace reads serve content, traversal and foreign paths are refused,
// and unknown agents are refused.
func TestChatService_WorkspaceReaders(t *testing.T) {
	ctx := context.Background()
	svc, st, ws := newSurfaceChatService(t)

	agent := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))
	dir, err := ws.Create("scout")
	require.NoError(t, err)
	require.NoError(t, osWrite(dir+"/notes.md", "workspace notes"))

	content, err := svc.WorkspaceFile(ctx, agent.ID, "notes.md")
	require.NoError(t, err)
	require.Equal(t, "workspace notes", content)

	entries, err := svc.ListWorkspace(ctx, agent.ID, "")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "notes.md", entries[0].Name)
	require.Equal(t, int64(len("workspace notes")), entries[0].Size)

	_, err = svc.WorkspaceFile(ctx, agent.ID, "../escape.md")
	require.Error(t, err, "traversal is refused")

	_, err = svc.WorkspaceFile(ctx, agent.ID, "/etc/passwd")
	require.Error(t, err, "absolute foreign paths are refused")

	_, err = svc.WorkspaceFile(ctx, "no-such-agent", "notes.md")
	require.ErrorIs(t, err, store.ErrNotFound, "unknown agents are refused")
}
