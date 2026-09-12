package channels

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestChannelSessionID(t *testing.T) {
	if got := ChannelSessionID("ch-1", "agent-9"); got != "chan_ch-1_agent-9" {
		t.Fatalf("ChannelSessionID = %q, want chan_ch-1_agent-9", got)
	}
}

func TestDrain_RunSummaryWriteback(t *testing.T) {
	e := newTestEnv(t)
	turnID := ""
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turnID = "turn-" + req.SessionID
		toolFinished := func(name string) agents.TranscriptEvent {
			return agents.TranscriptEvent{
				Kind:       agents.TranscriptEventToolCallFinished,
				TurnID:     turnID,
				ToolResult: &agents.ToolResultPayload{Name: name},
			}
		}
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turnID},
			toolFinished("grafana.query"),
			toolFinished("grafana.query"),
			toolFinished("files.write"),
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turnID, Message: &agents.CompletedMessage{
				Role: "assistant", Content: "dashboards updated",
			}},
		}
	}

	root := mustPost(t, e, "@atlas refresh the dashboards")

	// The auto-posted final re-enters the pipeline and is stamped with the
	// run's (session_id, turn_id).
	var autoPost domain.ChannelMessage
	waitFor(t, func() bool {
		for _, msg := range feedMessages(t, e) {
			if msg.AuthorAgentID == e.atlas.ID {
				autoPost = msg
				return true
			}
		}
		return false
	}, "auto-posted final persisted")
	if autoPost.SessionID == nil || *autoPost.SessionID != ChannelSessionID(e.channel.ID, e.atlas.ID) {
		t.Fatalf("auto-post session = %v, want the deterministic channel session", autoPost.SessionID)
	}
	if autoPost.TurnID == nil || *autoPost.TurnID != turnID {
		t.Fatalf("auto-post turn = %v, want %q", autoPost.TurnID, turnID)
	}
	if autoPost.RootMessageID == nil || *autoPost.RootMessageID != root.ID || autoPost.ChainDepth != 1 {
		t.Fatalf("auto-post chain = root %v depth %d, want root %s depth 1", autoPost.RootMessageID, autoPost.ChainDepth, root.ID)
	}
	if autoPost.Body != "dashboards updated" {
		t.Fatalf("auto-post body = %q", autoPost.Body)
	}

	// The run summary lands on the messages the run posted, found by
	// (session_id, turn_id).
	waitFor(t, func() bool {
		for _, msg := range feedMessages(t, e) {
			if msg.AuthorAgentID == e.atlas.ID && msg.RunSummary != nil {
				autoPost = msg
				return true
			}
		}
		return false
	}, "run summary written back")

	if autoPost.RunSummary == nil {
		t.Fatal("auto-posted final must carry the run summary")
	}
	if autoPost.RunSummary.Tools["grafana.query"] != 2 || autoPost.RunSummary.Tools["files.write"] != 1 {
		t.Fatalf("run summary tools = %+v", autoPost.RunSummary.Tools)
	}
	if autoPost.RunSummary.DurationMS < 0 {
		t.Fatalf("duration = %d ms", autoPost.RunSummary.DurationMS)
	}

	// run_finished completes the event arc with status completed.
	waitFor(t, func() bool { return len(e.events.ofType(EventRunFinished)) == 1 }, "run_finished broadcast")
	finished := e.events.ofType(EventRunFinished)[0]
	var payload struct {
		ChannelID string `json:"channel_id"`
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(finished.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != RunStatusCompleted ||
		payload.AgentID != e.atlas.ID ||
		payload.SessionID != ChannelSessionID(e.channel.ID, e.atlas.ID) ||
		payload.ChannelID != e.channel.ID {
		t.Fatalf("run_finished payload = %+v", payload)
	}
}

func TestDrain_FailedRun_PostsNothing(t *testing.T) {
	e := newTestEnv(t)
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turn := "turn-" + req.SessionID
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventError, TurnID: turn, Error: "provider exploded"},
		}
	}

	mustPost(t, e, "@atlas do the thing")

	waitFor(t, func() bool { return len(e.events.ofType(EventRunFinished)) == 1 }, "run_finished broadcast")

	if len(feedMessages(t, e)) != 1 {
		t.Fatalf("feed rows = %d, want only the human message (failed runs post nothing)", len(feedMessages(t, e)))
	}
	var payload struct {
		Status string `json:"status"`
	}
	finished := e.events.ofType(EventRunFinished)[0]
	if err := json.Unmarshal(finished.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != RunStatusFailed {
		t.Fatalf("run_finished status = %q, want failed", payload.Status)
	}
}

func TestDrain_CancelledRun_PostsNothing(t *testing.T) {
	e := newTestEnv(t)
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turn := "turn-" + req.SessionID
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventCancelled, TurnID: turn, CancelReason: "user cancelled"},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turn, Message: &agents.CompletedMessage{Role: "assistant", Content: "late text"}},
		}
	}

	mustPost(t, e, "@atlas cancel this")

	waitFor(t, func() bool { return len(e.events.ofType(EventRunFinished)) == 1 }, "run_finished broadcast")

	if len(feedMessages(t, e)) != 1 {
		t.Fatalf("feed rows = %d, want only the human message (cancelled runs post nothing)", len(feedMessages(t, e)))
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(e.events.ofType(EventRunFinished)[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != RunStatusCancelled {
		t.Fatalf("run_finished status = %q, want cancelled", payload.Status)
	}
}

func TestSubmit_Error_FailedBroadcast(t *testing.T) {
	e := newTestEnv(t)
	e.submitter.err = context.DeadlineExceeded

	mustPost(t, e, "@atlas unreachable")

	// run_started then run_finished(failed) close the indicator loop even
	// when the submitter rejects the run.
	waitFor(t, func() bool { return len(e.events.ofType(EventRunFinished)) == 1 }, "run_finished broadcast")
	if len(e.events.ofType(EventRunStarted)) != 1 {
		t.Fatalf("run_started events = %d, want 1", len(e.events.ofType(EventRunStarted)))
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(e.events.ofType(EventRunFinished)[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != RunStatusFailed {
		t.Fatalf("status = %q, want failed", payload.Status)
	}
}

func TestDrain_SummaryWriteback_MatchesOnlyThisRunsPosts(t *testing.T) {
	// Two consecutive summons of the same agent share the deterministic
	// session; the second run's summary must not land on the first run's
	// rows (turn-scoped writeback).
	e := newTestEnv(t)
	runs := 0
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		runs++
		turn := "turn-" + req.SessionID + "-r" + string(rune('0'+runs))
		text := ""
		if runs == 1 {
			text = "first reply"
		}
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turn, Message: &agents.CompletedMessage{Role: "assistant", Content: text}},
		}
	}

	mustPost(t, e, "@atlas first")
	waitFor(t, func() bool {
		for _, msg := range feedMessages(t, e) {
			if msg.AuthorAgentID == e.atlas.ID {
				return true
			}
		}
		return false
	}, "first auto-post persisted")

	// Second summon: same deterministic session, new turn. No auto-post
	// (empty final).
	mustPost(t, e, "@atlas second")
	waitFor(t, func() bool {
		return len(e.events.ofType(EventRunFinished)) == 2
	}, "second run finished")

	msgs := feedMessages(t, e)
	var firstPost *domain.ChannelMessage
	for i := range msgs {
		if msgs[i].AuthorAgentID == e.atlas.ID {
			firstPost = &msgs[i]
		}
	}
	if firstPost == nil || firstPost.RunSummary == nil {
		t.Fatal("first run's auto-post must carry the first run's summary")
	}
	if firstPost.RunSummary.DurationMS < 0 {
		t.Fatalf("summary = %+v", firstPost.RunSummary)
	}
	// The second run posted nothing; its writeback matched no rows — the
	// first post's summary must be untouched (exactly one feed row with a
	// summary).
	withSummary := 0
	for _, msg := range msgs {
		if msg.RunSummary != nil {
			withSummary++
		}
	}
	if withSummary != 1 {
		t.Fatalf("rows with run_summary = %d, want 1", withSummary)
	}
}

func TestPost_ZeroValueChokepointGuards(t *testing.T) {
	// The 3-arg pinned constructor defaults: decider declines, handles
	// resolve nothing (mentions stay plain text), everything compiles and
	// runs without the composition root's wiring.
	st := fake.New()
	ctx := context.Background()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	sarah := &domain.User{Email: "s@example.com", Name: "S"}
	if err := st.Users().Create(ctx, sarah); err != nil {
		t.Fatal(err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatal(err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: sarah.ID, RoleID: role.ID}); err != nil {
		t.Fatal(err)
	}
	channel := &domain.Channel{WorkspaceID: ws.ID, Name: "Ops", Slug: "ops"}
	if err := st.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatal(err)
	}
	if err := st.Channels().AddChannelMember(ctx, &domain.ChannelMember{
		WorkspaceID: ws.ID, ChannelID: channel.ID,
		MemberType: domain.ChannelMemberTypeUser, UserID: sarah.ID,
	}); err != nil {
		t.Fatal(err)
	}

	hub := NewHub()
	cp := NewChokepoint(st.Channels(), &fakeRunSubmitter{}, hub)
	msg, err := cp.Post(ctx, ws.ID, channel.ID, Author{Type: "user", UserID: sarah.ID}, "unresolved @atlas here")
	if err != nil {
		t.Fatalf("post with defaults: %v", err)
	}
	if len(msg.Mentions) != 0 {
		t.Fatalf("mentions = %+v, want none resolved (handles unwired)", msg.Mentions)
	}
}
