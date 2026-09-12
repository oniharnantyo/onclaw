package agents

import (
	"context"
	"strings"
	"testing"
)

// TestSessionTitle pins the birth-title rule byte-for-byte against the web's
// deriveSessionTitle (agent-session-index D2): first line, trimmed, truncated
// at 42 runes with an ellipsis; empty and whitespace-only inputs (and
// whitespace-only first lines) yield the empty title.
func TestSessionTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "short single line", input: "Fix the login bug", want: "Fix the login bug"},
		{name: "first line wins", input: "Fix the login bug\nwith reproduction steps", want: "Fix the login bug"},
		{name: "trims surrounding whitespace", input: "  Fix the login bug  ", want: "Fix the login bug"},
		{name: "trims the first line only", input: "   Fix the login bug  \nsecond", want: "Fix the login bug"},
		{name: "long input truncates at 42 with ellipsis", input: strings.Repeat("a", 50), want: strings.Repeat("a", 42) + "…"},
		{name: "exactly 42 is not truncated", input: strings.Repeat("a", 42), want: strings.Repeat("a", 42)},
		{name: "multibyte truncation is rune-safe", input: strings.Repeat("ä", 50), want: strings.Repeat("ä", 42) + "…"},
		{name: "empty input", input: "", want: ""},
		{name: "whitespace-only input", input: "   \n\t ", want: ""},
		{name: "leading newline yields empty title", input: "\nsecond line", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionTitle(tt.input); got != tt.want {
				t.Fatalf("sessionTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestRun_PersistentRunIndexesSession covers the D2 write at persistent run
// start: the birth turn indexes the session with the input-derived title, and
// a later turn bumps the row's activity without retitling it.
func TestRun_PersistentRunIndexesSession(t *testing.T) {
	m := &manyDeltaModel{deltas: 2}
	runner, req := setupLifecycleRunner(t, "sess-indexed", m)
	req.Input = "Fix the login bug\nwith reproduction steps"
	ctx := context.Background()

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected birth turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err := runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one indexed session, got %d", len(rows))
	}
	if rows[0].SessionID != "sess-indexed" {
		t.Fatalf("expected session_id sess-indexed, got %q", rows[0].SessionID)
	}
	if rows[0].Title != "Fix the login bug" {
		t.Fatalf("expected birth title from the first input line, got %q", rows[0].Title)
	}
	if rows[0].WorkspaceID != req.WorkspaceID || rows[0].AgentID != req.AgentID || rows[0].UserID != req.UserID {
		t.Fatalf("index row misattributed: %+v", rows[0])
	}
	birthActivity := rows[0].LastActiveAt

	// The later turn bumps activity only — the birth-only title rule keeps
	// the original title even though the new input differs.
	req.Input = "A completely different topic for the second turn"
	second, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if ev := collectStream(t, second); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected second turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err = runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions after second turn: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the second turn must not spawn a second row, got %d", len(rows))
	}
	if rows[0].Title != "Fix the login bug" {
		t.Fatalf("later turn retitled the session: %q", rows[0].Title)
	}
	if !rows[0].LastActiveAt.After(birthActivity) {
		t.Fatalf("later turn did not bump last_active_at: birth %v, now %v", birthActivity, rows[0].LastActiveAt)
	}
}

// TestRun_CompactTurnIndexesWithoutTitle: the compact turn is a persistent
// execution and indexes its session, but its summarizer focus text never
// becomes the title — an empty title stores '' on birth and rewrites nothing
// on conflict (agent-session-index spec, "Compact turn does not title").
func TestRun_CompactTurnIndexesWithoutTitle(t *testing.T) {
	m := &manyDeltaModel{deltas: 1}
	runner, req := setupLifecycleRunner(t, "sess-compact-index", m)
	req.Command = CommandCompact
	req.Input = "keep the deployment runbook details"
	ctx := context.Background()

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run (compact): %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected compact turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err := runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the compact turn to index its session, got %d rows", len(rows))
	}
	if rows[0].Title != "" {
		t.Fatalf("compact focus text became the title: %q", rows[0].Title)
	}
}

// TestRun_EphemeralRunSkipsIndex: RunEphemeral never touches the durable
// index — no row is created for the session (agent-session-index spec,
// "Ephemeral runs skip the index").
func TestRun_EphemeralRunSkipsIndex(t *testing.T) {
	m := &manyDeltaModel{deltas: 1}
	runner, req := setupLifecycleRunner(t, "sess-ephemeral", m)
	req.Input = "Fix the login bug"
	ctx := context.Background()

	stream, err := runner.RunEphemeral(ctx, req)
	if err != nil {
		t.Fatalf("RunEphemeral: %v", err)
	}
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected ephemeral turn to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	rows, err := runner.agentSessions.ListAgentSessions(ctx, req.WorkspaceID, req.AgentID, req.UserID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ephemeral run must not touch the index, got %+v", rows)
	}
}

// TestRunner_ActiveRunSessionIDs: the Runner pass-through surfaces the
// manager's live session ids for the workspace+agent pair while a run is
// executing, and drops it after completion (agent-session-index D3).
func TestRunner_ActiveRunSessionIDs(t *testing.T) {
	gate := newModelGate()
	m := newGatedModel(&manyDeltaModel{deltas: 2}, gate)
	runner, req := setupLifecycleRunner(t, "sess-live-ids", m)
	ctx := context.Background()

	if ids := runner.ActiveRunSessionIDs(req.WorkspaceID, req.AgentID); len(ids) != 0 {
		t.Fatalf("expected no live sessions before the run, got %v", ids)
	}

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	gate.arrived() // park the run mid-turn

	ids := runner.ActiveRunSessionIDs(req.WorkspaceID, req.AgentID)
	if len(ids) != 1 || ids[0] != req.SessionID {
		t.Fatalf("expected live session %q, got %v", req.SessionID, ids)
	}
	// A different agent's enumeration stays empty.
	if ids := runner.ActiveRunSessionIDs(req.WorkspaceID, "other-agent"); len(ids) != 0 {
		t.Fatalf("other agent must have no live sessions, got %v", ids)
	}

	gate.unblock()
	if ev := collectStream(t, stream); !hasKind(ev, TranscriptEventTurnCompleted) {
		t.Fatalf("expected run to complete, got %+v", ev)
	}
	waitRunDone(t, runner, runKey(req))

	if ids := runner.ActiveRunSessionIDs(req.WorkspaceID, req.AgentID); len(ids) != 0 {
		t.Fatalf("expected no live sessions after completion, got %v", ids)
	}
}

// compile-time guard: the runner satisfies the handler seam the session
// listing asserts on (agent-session-index D3).
var _ interface {
	ActiveRunSessionIDs(workspaceID, agentID string) []string
} = (*Runner)(nil)
