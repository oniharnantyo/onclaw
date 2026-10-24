package services

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/store"
)

// TestChatService_TaskTranscriptRoundTrip pins the delegated-task transcript
// seam end to end (design D7): a JSONL transcript the runtime writes through
// DiskAppendOpener under the agent workspace resolves through the jailed
// reader at exactly the workspace-relative path a TaskActivity carries
// (".tasks/<task tool-call id>.output"), and a missing transcript is a clean
// read error for the degraded preview state.
func TestChatService_TaskTranscriptRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, st, ws := newSurfaceChatService(t)

	agent := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))
	agentDir, err := ws.Dir("scout")
	require.NoError(t, err)

	const transcriptPath = ".tasks/call-42.output"
	const transcript = "{\"step\":\"task\",\"content\":\"scan the repo\"}\n" +
		"{\"step\":\"result\",\"content\":\"done\"}\n"
	opener := &subagents.DiskAppendOpener{BaseDir: agentDir}
	w, err := opener.OpenAppend(ctx, &filesystem.OpenAppendRequest{FilePath: transcriptPath})
	require.NoError(t, err)
	_, err = w.Write([]byte(transcript))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	got, err := svc.WorkspaceFile(ctx, agent.ID, transcriptPath)
	require.NoError(t, err)
	require.Equal(t, transcript, got)

	t.Run("missing transcript is a clean error", func(t *testing.T) {
		_, err := svc.WorkspaceFile(ctx, agent.ID, ".tasks/gone.output")
		require.Error(t, err, "a task without a materialized transcript reads as a read error")
	})
}
