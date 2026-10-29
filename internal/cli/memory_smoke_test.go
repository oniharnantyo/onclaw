package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/memory"
	"github.com/oniharnantyo/buddies/internal/services"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestMemorySmoke is the env-gated headless live smoke for the
// add-agent-memory change (tasks.md 9.2). It runs real model turns against a
// cloned config (see the recipe in .team/smoke) and is skipped unless
// BUDDIES_MEMORY_SMOKE=1:
//
//	HOME=$TMP BUDDIES_MEMORY_SMOKE=1 go test ./internal/cli -run TestMemorySmoke -v -count=1
func TestMemorySmoke(t *testing.T) {
	if os.Getenv("BUDDIES_MEMORY_SMOKE") == "" {
		t.Skip("set BUDDIES_MEMORY_SMOKE=1 (and HOME to a cloned config) to run the live memory smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	app, err := Assemble(ctx, services.Noop{}, services.NoBrowser{})
	require.NoError(t, err)
	defer func() { require.NoError(t, app.Close()) }()

	agentList, err := app.Store.Agents().List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, agentList, "cloned config has no agents")
	agent := agentList[0]
	for _, a := range agentList {
		if a.Name == "Scout" {
			agent = a
		}
	}
	require.NotEmpty(t, agent.Model, "chosen agent has no model: %s", agent.Name)
	t.Logf("smoke agent: %s (model %s / provider %s)", agent.Name, agent.Model, agent.ProviderID)
	wsDir := filepath.Join(app.Layout.Workspaces, agent.Slug)

	// Turn 1: the agent records a non-guessable fact and searches memory.
	sess1, err := app.Runtime.CreateSession(ctx, agent.ID)
	require.NoError(t, err)
	turn1, err := app.Runtime.Submit(ctx, agents.SendRequest{SessionID: sess1.ID,
		Message: "Remember this about me: my cat is named Hoptail. You MUST (1) save it as a " +
			"memory note using your write tool with the frontmatter schema from your memory " +
			"guidance, and (2) call the memory_search tool with the query \"Hoptail\" before " +
			"answering. Reply DONE when both are complete."})
	require.NoError(t, err)
	require.NoError(t, turn1.Wait())

	memDir := filepath.Join(wsDir, "memory")
	entries, err := os.ReadDir(memDir)
	require.NoError(t, err, "memory/ must exist after the turn")
	var noteFiles []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "MEMORY.md" && !strings.HasPrefix(e.Name(), "20") {
			noteFiles = append(noteFiles, e.Name())
		}
	}
	require.NotEmpty(t, noteFiles, "agent wrote no fact note; files: %v", entries)
	t.Logf("memory dir after turn 1: %v", entries)

	traces, err := app.Store.Memory().ListTracesSince(ctx, agent.ID, time.Time{})
	require.NoError(t, err)
	require.NotEmpty(t, traces, "memory_search recorded no trace")
	require.Equal(t, "user", traces[0].Origin)
	t.Logf("trace: origin=%s query=%q hits=%d", traces[0].Origin, traces[0].Query, len(traces[0].Hits))

	// Forced sweep 1: managed index + note state appear. The sweep skips
	// files modified within WriteGuardWindow (mid-write guard), so wait the
	// window out before forcing it.
	time.Sleep(memory.WriteGuardWindow + 2*time.Second)
	sw := memory.NewSweeper(app.Store)
	res := sw.Sweep(ctx, []memory.SweepTarget{{AgentID: agent.ID, WorkspaceDir: wsDir}})
	require.Len(t, res, 1)
	require.NoError(t, res[0].Err)

	indexRaw, err := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
	require.NoError(t, err, "sweep must produce the index")
	require.Contains(t, string(indexRaw), "<!-- memory:managed begin -->")
	states, err := app.Store.Memory().ListNoteStates(ctx, agent.ID)
	require.NoError(t, err)
	require.NotEmpty(t, states, "sweep recorded no note state")
	var hoptail *store.MemoryNoteState
	for i := range states {
		if strings.Contains(strings.ToLower(states[i].Name), "hoptail") {
			hoptail = &states[i]
		}
	}
	require.NotNil(t, hoptail, "no note state for the Hoptail note; states: %v", states)
	require.GreaterOrEqual(t, hoptail.Retention, 0.3, "note below injection threshold — recall turn would be inconclusive")
	t.Logf("note state: %s retention=%.3f", hoptail.Name, hoptail.Retention)

	// Sweep 2 with no new input: MEMORY.md byte-identical (design D14).
	snapshot := append([]byte(nil), indexRaw...)
	res = sw.Sweep(ctx, []memory.SweepTarget{{AgentID: agent.ID, WorkspaceDir: wsDir}})
	require.NoError(t, res[0].Err)
	after, err := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
	require.NoError(t, err)
	require.True(t, bytes.Equal(snapshot, after), "MEMORY.md changed on a no-op sweep")

	// Turn 2, fresh session: the agent recalls from the memory block.
	sess2, err := app.Runtime.CreateSession(ctx, agent.ID)
	require.NoError(t, err)
	turn2, err := app.Runtime.Submit(ctx, agents.SendRequest{SessionID: sess2.ID,
		Message: "What is my cat's name? Answer with just the name."})
	require.NoError(t, err)
	require.NoError(t, turn2.Wait())

	transcript, err := app.Services.Chat.Transcript(ctx, sess2.ID)
	require.NoError(t, err)
	var reply string
	for _, ev := range transcript {
		if ev.Kind == sessions.KindAssistantMessage {
			reply += ev.Text
		}
		if ev.Kind == sessions.KindTurnError {
			t.Fatalf("recall turn errored: %s", ev.Text)
		}
	}
	t.Logf("recall turn reply: %s", reply)
	require.True(t, strings.Contains(strings.ToLower(reply), "hoptail"),
		"fresh-session turn did not recall the note; transcript events: %d", len(transcript))
}
