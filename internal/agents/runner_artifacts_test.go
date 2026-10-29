package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The runner-level artifact tests (add-turn-artifacts tasks 2.2): a turn's
// terminal event carries the files that appeared under the workspace's
// artifacts/ directory, nothing written outside it, nothing for a
// hook-blocked submission, and a usable turn when the artifacts path is
// blocked by a regular file.

// artifactsFixture builds the standard fixture over the given workspaces
// root and gives the agent the slug "tester" (a slugless agent owns no
// workspace, so detection would be off), returning the fixture and the
// workspace directory path.
func artifactsFixture(t *testing.T, root string, reply any, opts ...Option) (*fixture, string) {
	t.Helper()
	f := newFixture(t, reply, append(opts, WithWorkspaces(workspaces.NewManager(root)))...)
	agent := f.agent
	agent.Slug = "tester"
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	return f, filepath.Join(root, agent.Slug)
}

// completedOf returns the session log's turn_completed events.
func completedOf(t *testing.T, f *fixture, sessionID string) []sessions.Event {
	t.Helper()
	events, err := f.sessions.Events(t.Context(), sessionID)
	require.NoError(t, err)
	return eventsOfKind(events, sessions.KindTurnCompleted)
}

// TestTurn_ArtifactsCreatedUnderArtifactsDir pins the happy path (spec
// chat-artifacts "File written by a shell command is detected"): a file
// that appears under artifacts/ during the turn rides the persisted
// turn_completed event as created, workspace-relative with its size. The
// file is written from the model callback — the lightest seam that lands it
// strictly after the start snapshot and before the completion listing.
func TestTurn_ArtifactsCreatedUnderArtifactsDir(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "tester")
	f, _ := artifactsFixture(t, root, func(_ []*schema.Message) fakeReply {
		if err := os.MkdirAll(filepath.Join(workspace, workspaces.DirArtifacts), 0o700); err != nil {
			return fakeReply{err: err}
		}
		if err := os.WriteFile(filepath.Join(workspace, workspaces.DirArtifacts, "report.md"), []byte("weekly report"), 0o600); err != nil {
			return fakeReply{err: err}
		}
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "write the report")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	completed := completedOf(t, f, sess.ID)
	require.Len(t, completed, 1)
	require.Equal(t, []sessions.Artifact{
		{Path: "artifacts/report.md", Size: int64(len("weekly report")), Change: sessions.ArtifactCreated},
	}, completed[0].Artifacts)
	require.False(t, completed[0].ArtifactsTruncated)
	require.DirExists(t, filepath.Join(workspace, workspaces.DirArtifacts),
		"the artifacts directory is created on demand by the turn")
}

// TestTurn_RootLevelFileIsNotAnArtifact pins the boundary (design D2): only
// artifacts/ is watched, so a deliverable at the workspace root reports
// nothing — today's behavior, never noise.
func TestTurn_RootLevelFileIsNotAnArtifact(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "tester")
	f, _ := artifactsFixture(t, root, func(_ []*schema.Message) fakeReply {
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			return fakeReply{err: err}
		}
		if err := os.WriteFile(filepath.Join(workspace, "report.md"), []byte("root report"), 0o600); err != nil {
			return fakeReply{err: err}
		}
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "write the report")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	completed := completedOf(t, f, sess.ID)
	require.Len(t, completed, 1)
	require.Empty(t, completed[0].Artifacts)
	require.False(t, completed[0].ArtifactsTruncated)
}

// TestTurn_BlockedSubmissionCarriesNoArtifacts pins the blocked path (spec
// chat-artifacts "Blocked submission carries no artifacts"): a
// user_prompt_submit block completes the turn through blockedPromptTurn
// without the model — and its terminal event stays artifact-free even when
// artifacts already sit on disk from an earlier turn.
func TestTurn_BlockedSubmissionCarriesNoArtifacts(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "tester")
	f, _ := artifactsFixture(t, root, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "the model must never be asked"}
	})
	f.wireRealHooks()
	seedHook(t, f.store, store.Hook{
		Name:        "gate",
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"echo this prompt is not allowed >&2; exit 2"}`,
		Enabled:     true,
	})
	// Artifacts from an earlier turn are on disk; the blocked submission
	// must still report none.
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, workspaces.DirArtifacts), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, workspaces.DirArtifacts, "old.md"), []byte("old"), 0o600))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "a hook block is a normal turn end, not a failure")
	require.Empty(t, f.model.calls(), "a blocked prompt makes no model call")

	completed := completedOf(t, f, sess.ID)
	require.Len(t, completed, 1)
	require.Empty(t, completed[0].Artifacts)
	require.False(t, completed[0].ArtifactsTruncated)
}

// TestTurn_ArtifactsPathBlockedByRegularFile pins the degraded path (spec
// chat-artifacts "Artifacts path blocked by a regular file"): a regular
// file named artifacts disables detection for the turn — logged, not fatal —
// and the turn completes normally with no artifacts.
func TestTurn_ArtifactsPathBlockedByRegularFile(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "tester")
	f, _ := artifactsFixture(t, root, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	require.NoError(t, os.MkdirAll(workspace, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, workspaces.DirArtifacts), []byte("not a directory"), 0o600))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "an unusable artifacts path must not fail the turn")
	require.NotEmpty(t, f.model.calls(), "the turn ran the model normally")

	completed := completedOf(t, f, sess.ID)
	require.Len(t, completed, 1)
	require.Empty(t, completed[0].Artifacts)
	require.False(t, completed[0].ArtifactsTruncated)
}
