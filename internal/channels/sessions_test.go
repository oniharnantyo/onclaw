package channels

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// -------------------------------------------------------------------------
// Work-session test scaffolding: the v1 env plus a facilitator (charlie) and
// a wired WorkSessionStore. Reuses the v1 helpers (fakeRunSubmitter,
// scriptedDecider, eventCollector, storeHandles, waitFor) from
// chokepoint_test.go.
// -------------------------------------------------------------------------

type sessionEnv struct {
	*testEnv
	wsStore store.WorkSessionStore
}

// newSessionEnv is newTestEnv with charlie promoted to facilitator and the
// work-session store wired into the chokepoint.
func newSessionEnv(t *testing.T) *sessionEnv {
	t.Helper()
	e := &sessionEnv{testEnv: newTestEnv(t)}

	// Promote charlie to the channel's single facilitator.
	roster, err := e.channels.ListChannelMembers(context.Background(), e.ws.ID, e.channel.ID)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	for _, m := range roster {
		if m.MemberType == domain.ChannelMemberTypeAgent && m.AgentID == e.charlie.ID {
			if err := e.channels.UpdateChannelMemberRole(context.Background(), e.ws.ID, e.channel.ID, m.ID, domain.ChannelMemberRoleFacilitator); err != nil {
				t.Fatalf("promote charlie: %v", err)
			}
		}
	}

	e.wsStore = e.st.WorkSessions()
	e.cp = NewChokepoint(e.st.Channels(), e.submitter, e.hub,
		WithSilenceDecider(e.decider),
		WithWorkSessionStore(e.wsStore),
		WithChannelHandles(storeHandles{st: e.st}),
	)
	e.events = collectEvents(t, e.hub, e.channel.ID)
	return e
}

// kickoff opens a session as Sarah and returns the (message, session) pair.
func (e *sessionEnv) kickoff(t *testing.T, body string) (domain.ChannelMessage, *domain.WorkSession) {
	t.Helper()
	msg, session, err := e.cp.PostKickoff(context.Background(), e.ws.ID, e.channel.ID, Author{
		Type:   string(domain.ChannelMemberTypeUser),
		UserID: e.sarah.ID,
	}, body)
	if err != nil {
		t.Fatalf("kickoff %q: %v", body, err)
	}
	return msg, session
}

// feedMessages forwards the shared helper to the embedded env.
func (e *sessionEnv) feedMessages(t *testing.T) []domain.ChannelMessage {
	t.Helper()
	return feedMessages(t, e.testEnv)
}

// mustPost forwards the shared helper to the embedded env.
func (e *sessionEnv) mustPost(t *testing.T, body string) domain.ChannelMessage {
	t.Helper()
	return mustPost(t, e.testEnv, body)
}

// notifyRuns returns the facilitator's synthetic notification runs (watchdog
// and budget status), identified by their input marker — the kickoff run is
// excluded.
func (e *sessionEnv) notifyRuns(marker string) []agents.ExecRequest {
	var out []agents.ExecRequest
	for _, req := range e.submitter.requestsFor(e.charlie.ID) {
		if strings.Contains(req.Input, marker) {
			out = append(out, req)
		}
	}
	return out
}

// sessionUpdatedPayloads decodes every session_updated event seen so far.
func (e *sessionEnv) sessionUpdatedPayloads(t *testing.T) []domain.WorkSession {
	t.Helper()
	events := e.events.ofType(EventSessionUpdated)
	out := make([]domain.WorkSession, 0, len(events))
	for _, ev := range events {
		var payload struct {
			Session domain.WorkSession `json:"session"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("decode session_updated: %v", err)
		}
		out = append(out, payload.Session)
	}
	return out
}

// -------------------------------------------------------------------------
// Kickoff
// -------------------------------------------------------------------------

func TestKickoff_OpensSession_SummonsFacilitator_Hop1(t *testing.T) {
	e := newSessionEnv(t)

	msg, session := e.kickoff(t, "add dark mode to settings")
	if !msg.IsKickoff {
		t.Fatal("kickoff message must carry the is_kickoff flag")
	}
	if session.Status != domain.WorkSessionOpen || session.Budget != domain.DefaultWorkSessionBudget {
		t.Fatalf("session = %+v", session)
	}
	if session.HopsUsed != 1 {
		t.Fatalf("hops_used = %d, want 1 (the kickoff summon bills hop 1)", session.HopsUsed)
	}
	if session.RootMessageID != msg.ID || session.Goal != "add dark mode to settings" {
		t.Fatalf("session root/goal = %q/%q", session.RootMessageID, session.Goal)
	}

	// The kickoff message persists with the flag and the session link.
	var stored domain.ChannelMessage
	for _, m := range e.feedMessages(t) {
		if m.ID == msg.ID {
			stored = m
		}
	}
	if !stored.IsKickoff || stored.WorkSessionID == nil || *stored.WorkSessionID != session.ID {
		t.Fatalf("kickoff row = is_kickoff %v work_session_id %v", stored.IsKickoff, stored.WorkSessionID)
	}

	// The facilitator is summoned with the v1 attributed input, the kickoff
	// message as chain root, hop 1 billed.
	reqs := e.submitter.requestsFor(e.charlie.ID)
	if len(reqs) != 1 {
		t.Fatalf("facilitator runs = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.WorkSessionID != session.ID {
		t.Fatalf("run work_session_id = %q, want %q", req.WorkSessionID, session.ID)
	}
	if req.RootMessageID != msg.ID || req.ChainDepth != 0 {
		t.Fatalf("run chain = root %q depth %d, want kickoff root depth 0", req.RootMessageID, req.ChainDepth)
	}
	if req.UserID != e.sarah.ID {
		t.Fatalf("run user = %q, want the kickoff author", req.UserID)
	}
	if req.Input != "[#ops] @sarah-chen: add dark mode to settings" {
		t.Fatalf("run input = %q", req.Input)
	}
	if req.Origin != agents.OriginChannel || req.ChannelID != e.channel.ID {
		t.Fatalf("run origin/channel = %q/%q", req.Origin, req.ChannelID)
	}
	if len(e.submitter.requests()) != 1 {
		t.Fatalf("total runs = %d, want only the facilitator", len(e.submitter.requests()))
	}

	// Broadcasts: message_posted then session_updated carrying the session.
	posted := waitForEvents(t, e.events, EventMessagePosted, 1, "message_posted")
	if posted[0].Seq != msg.Seq {
		t.Fatalf("posted seq = %d, want %d", posted[0].Seq, msg.Seq)
	}
	waitForEvents(t, e.events, EventSessionUpdated, 1, "session_updated")
	sessions := e.sessionUpdatedPayloads(t)
	if len(sessions) != 1 || sessions[0].ID != session.ID || sessions[0].Status != domain.WorkSessionOpen {
		t.Fatalf("session_updated payloads = %+v", sessions)
	}

	// Kickoff is human-only.
	_, _, err := e.cp.PostKickoff(context.Background(), e.ws.ID, e.channel.ID, Author{
		Type:    string(domain.ChannelMemberTypeAgent),
		AgentID: e.atlas.ID,
	}, "agent kickoff")
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("agent kickoff = %v, want ErrInvalid", err)
	}

	// A second kickoff while the session lives conflicts.
	_, _, err = e.cp.PostKickoff(context.Background(), e.ws.ID, e.channel.ID, Author{
		Type:   string(domain.ChannelMemberTypeUser),
		UserID: e.sarah.ID,
	}, "another goal")
	if !errors.Is(err, domain.ErrWorkSessionActive) {
		t.Fatalf("second kickoff = %v, want ErrWorkSessionActive", err)
	}
}

func TestKickoff_RequiresFacilitator(t *testing.T) {
	// The v1 env's channel has no facilitator; a wired-but-facilitator-less
	// channel refuses the kickoff.
	e := &sessionEnv{testEnv: newTestEnv(t)}
	e.wsStore = e.st.WorkSessions()
	e.cp = NewChokepoint(e.st.Channels(), e.submitter, e.hub, WithSilenceDecider(e.decider), WithWorkSessionStore(e.wsStore))

	_, _, err := e.cp.PostKickoff(context.Background(), e.ws.ID, e.channel.ID, Author{
		Type:   string(domain.ChannelMemberTypeUser),
		UserID: e.sarah.ID,
	}, "goal")
	if err == nil || !strings.Contains(err.Error(), "no facilitator") {
		t.Fatalf("facilitator-less kickoff = %v, want the no-facilitator validation error", err)
	}
	if _, err := e.st.WorkSessions().ActiveWorkSession(context.Background(), e.ws.ID, e.channel.ID); err != nil {
		t.Fatalf("session lookup: %v", err)
	}
	active, _ := e.st.WorkSessions().ActiveWorkSession(context.Background(), e.ws.ID, e.channel.ID)
	if active != nil {
		t.Fatalf("failed kickoff minted a session: %+v", active)
	}
}

// -------------------------------------------------------------------------
// In-session bounds: chain caps suspended, hops billed
// -------------------------------------------------------------------------

func TestSession_ChainCapsSuspended_ExactHops(t *testing.T) {
	e := newSessionEnv(t)

	// Relay script keyed on the trigger's chain depth:
	//   d0 (charlie, kickoff run) → "@atlas plan the work"
	//   d1 (atlas)                → "@beacon build it"
	//   d2 (beacon)               → "@atlas review this"   (v1: suppressed re-summon)
	//   d3 (atlas, 2nd run)       → "@charlie verify"       (fresh for atlas)
	//   d4+                       → plain (chain ends)
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turn := "turn-" + req.SessionID
		text := "done for now"
		switch req.ChainDepth {
		case 0:
			text = "@atlas plan the work"
		case 1:
			text = "@beacon build it"
		case 2:
			text = "@atlas review this"
		case 3:
			text = "@charlie verify"
		}
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turn, Message: &agents.CompletedMessage{Role: "assistant", Content: text}},
		}
	}

	msg, session := e.kickoff(t, "ship the relay")

	// Five runs minted: charlie(0) → atlas(1) → beacon(2) → atlas(3) →
	// charlie(4). The depth-3 trigger re-summons atlas and the depth-4
	// trigger summons charlie — both dead letters under v1 caps.
	waitFor(t, func() bool { return len(e.submitter.requests()) == 5 }, "five relay runs minted")
	time.Sleep(50 * time.Millisecond) // let a would-be sixth submit surface
	if reqs := e.submitter.requests(); len(reqs) != 5 {
		t.Fatalf("runs = %d, want 5 (caps suspended in-session, plain text ends the chain)", len(reqs))
	}

	depths := map[string][]int{}
	for _, req := range e.submitter.requests() {
		depths[req.AgentID] = append(depths[req.AgentID], req.ChainDepth)
		if req.WorkSessionID != session.ID {
			t.Fatalf("run for %s work_session_id = %q, want %q", req.AgentID, req.WorkSessionID, session.ID)
		}
		if req.RootMessageID != msg.ID {
			t.Fatalf("run for %s root = %q, want the kickoff message", req.AgentID, req.RootMessageID)
		}
	}
	if got := depths[e.charlie.ID]; len(got) != 2 || got[0] != 0 || got[1] != 4 {
		t.Fatalf("charlie run depths = %v, want [0 4]", got)
	}
	if got := depths[e.atlas.ID]; len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("atlas run depths = %v, want [1 3]", got)
	}
	if got := depths[e.beacon.ID]; len(got) != 1 || got[0] != 2 {
		t.Fatalf("beacon run depths = %v, want [2]", got)
	}

	// Hop accounting is exact: 5 runs, 5 hops.
	reloaded, err := e.wsStore.GetWorkSession(context.Background(), e.ws.ID, session.ID)
	if err != nil || reloaded.HopsUsed != 5 {
		t.Fatalf("hops_used = %d (%v), want 5", reloaded.HopsUsed, err)
	}

	// Every in-session feed row carries the session id.
	for _, m := range e.feedMessages(t) {
		if m.WorkSessionID == nil || *m.WorkSessionID != session.ID {
			t.Fatalf("feed row %q = work_session_id %v, want the session", m.ID, m.WorkSessionID)
		}
	}
}

func TestSession_UntaggedGoesToDeciders_ElectedAgentBillsHop(t *testing.T) {
	e := newSessionEnv(t)
	_, session := e.kickoff(t, "watch the flaky payments")
	e.decider.decide = func(agentID string) (Decision, error) {
		if agentID == e.atlas.ID {
			return Decision{Engage: true, Reason: "payments are mine"}, nil
		}
		return Decision{Engage: false, Reason: "not mine"}, nil
	}

	e.mustPost(t, "payments look flaky again")

	if calls := e.decider.calledFor(); len(calls) != 3 {
		t.Fatalf("decider calls = %d, want one per agent member", len(calls))
	}
	reqs := e.submitter.requestsFor(e.atlas.ID)
	if len(reqs) != 1 {
		t.Fatalf("atlas runs = %d, want 1 (elected)", len(reqs))
	}
	if reqs[0].WorkSessionID != session.ID {
		t.Fatalf("elected run work_session_id = %q, want %q", reqs[0].WorkSessionID, session.ID)
	}
	reloaded, _ := e.wsStore.GetWorkSession(context.Background(), e.ws.ID, session.ID)
	if reloaded.HopsUsed != 2 {
		t.Fatalf("hops_used = %d, want 2 (kickoff + elected)", reloaded.HopsUsed)
	}
}

// -------------------------------------------------------------------------
// Budget exhaustion
// -------------------------------------------------------------------------

func TestSession_BudgetExhaustion_Pauses_FreeFacilitatorNotify(t *testing.T) {
	e := newSessionEnv(t)
	_, session := e.kickoff(t, "big goal")

	// Drain the remaining budget directly through the store: hops 2..12.
	ctx := context.Background()
	for i := 2; i <= domain.DefaultWorkSessionBudget; i++ {
		updated, err := e.wsStore.ConsumeWorkSessionHop(ctx, e.ws.ID, session.ID)
		if err != nil || updated.HopsUsed != i {
			t.Fatalf("pre-drain hop %d = %+v, %v", i, updated, err)
		}
	}

	// One more in-session summon hits the wall: the session pauses, the
	// facilitator gets ONE free status notify, and the mentioned agent gets
	// nothing.
	e.mustPost(t, "@atlas keep going")

	if reqs := e.submitter.requestsFor(e.atlas.ID); len(reqs) != 0 {
		t.Fatalf("atlas runs = %d, want 0 (budget exhausted)", len(reqs))
	}
	notify := e.notifyRuns("(budget)")
	if len(notify) != 1 {
		t.Fatalf("facilitator notify runs = %d, want 1 (the free status)", len(notify))
	}
	if !strings.Contains(notify[0].Input, "(budget)") {
		t.Fatalf("notify input = %q, want the budget status note", notify[0].Input)
	}
	if notify[0].WorkSessionID != session.ID {
		t.Fatalf("notify work_session_id = %q", notify[0].WorkSessionID)
	}

	reloaded, _ := e.wsStore.GetWorkSession(ctx, e.ws.ID, session.ID)
	if reloaded.Status != domain.WorkSessionPaused || reloaded.PauseReason != domain.WorkSessionPauseBudgetExhausted || reloaded.HopsUsed != domain.DefaultWorkSessionBudget {
		t.Fatalf("session after exhaustion = %+v", reloaded)
	}

	// session_updated: kickoff open + budget pause.
	waitForEvents(t, e.events, EventSessionUpdated, 2, "session_updated broadcasts")
	sessions := e.sessionUpdatedPayloads(t)
	last := sessions[len(sessions)-1]
	if last.Status != domain.WorkSessionPaused || last.PauseReason != domain.WorkSessionPauseBudgetExhausted {
		t.Fatalf("last session_updated = %+v, want the budget-exhausted pause", last)
	}

	// While paused, an AGENT post summons nothing: its agent mentions drop to
	// plain text. (A human post would resume — covered below.)
	if _, err := e.cp.PostFromAgent(ctx, e.ws.ID, e.channel.ID, e.atlas.ID, "chiming in @charlie anyway"); err != nil {
		t.Fatalf("agent post while paused: %v", err)
	}
	var agentPost domain.ChannelMessage
	for _, m := range e.feedMessages(t) {
		if m.AuthorAgentID == e.atlas.ID {
			agentPost = m
		}
	}
	if len(agentPost.Mentions) != 0 {
		t.Fatalf("paused agent post mentions = %+v, want plain", agentPost.Mentions)
	}
	time.Sleep(50 * time.Millisecond)
	if reqs := e.submitter.requests(); len(reqs) != 2 { // kickoff + notify only
		t.Fatalf("total runs after paused posts = %d, want 2", len(reqs))
	}
}

// -------------------------------------------------------------------------
// Human gate
// -------------------------------------------------------------------------

func TestSession_HumanGate_Pauses_SuppressesSiblingMentions(t *testing.T) {
	e := newSessionEnv(t)

	// The facilitator's kickoff run replies tagging BOTH the human (sign-off)
	// and a sibling agent. The script is set before the kickoff so the run's
	// auto-post uses it.
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turn := "turn-" + req.SessionID
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turn, Message: &agents.CompletedMessage{
				Role: "assistant", Content: "@sarah-chen approve to start? @atlas standby",
			}},
		}
	}
	_, session := e.kickoff(t, "needs a sign-off flow")

	// The kickoff run's auto-post enters the pipeline as an agent post.
	// Expected: pause(awaiting-human), session_updated, the persisted post
	// keeps ONLY the human mention resolved, atlas is NOT summoned.
	waitFor(t, func() bool {
		for _, m := range e.feedMessages(t) {
			if m.AuthorType == domain.ChannelMemberTypeAgent {
				return true
			}
		}
		return false
	}, "auto-post persisted")
	time.Sleep(50 * time.Millisecond)

	if reqs := e.submitter.requestsFor(e.atlas.ID); len(reqs) != 0 {
		t.Fatalf("atlas runs = %d, want 0 (sibling mention suppressed by the gate)", len(reqs))
	}

	var agentPost domain.ChannelMessage
	for _, m := range e.feedMessages(t) {
		if m.AuthorType == domain.ChannelMemberTypeAgent {
			agentPost = m
		}
	}
	if len(agentPost.Mentions) != 1 || agentPost.Mentions[0].Type != domain.ChannelMemberTypeUser || agentPost.Mentions[0].ID != e.sarah.ID {
		t.Fatalf("agent post mentions = %+v, want only the human (badge signal)", agentPost.Mentions)
	}
	if agentPost.WorkSessionID == nil || *agentPost.WorkSessionID != session.ID {
		t.Fatalf("agent post work_session_id = %v", agentPost.WorkSessionID)
	}

	reloaded, _ := e.wsStore.GetWorkSession(context.Background(), e.ws.ID, session.ID)
	if reloaded.Status != domain.WorkSessionPaused || reloaded.PauseReason != domain.WorkSessionPauseAwaitingHuman {
		t.Fatalf("session after gate = %+v, want paused(awaiting-human)", reloaded)
	}

	waitForEvents(t, e.events, EventSessionUpdated, 2, "kickoff + gate session_updated")
	sessions := e.sessionUpdatedPayloads(t)
	last := sessions[len(sessions)-1]
	if last.Status != domain.WorkSessionPaused || last.PauseReason != domain.WorkSessionPauseAwaitingHuman {
		t.Fatalf("last session_updated = %+v, want the awaiting-human pause", last)
	}
}

func TestSession_HumanPostResumes_ThenSummons(t *testing.T) {
	e := newSessionEnv(t)
	_, session := e.kickoff(t, "await the humans")

	// Pause via the store (the gate path is covered above).
	if _, err := e.wsStore.PauseWorkSession(context.Background(), e.ws.ID, session.ID, domain.WorkSessionPauseAwaitingHuman); err != nil {
		t.Fatalf("pause: %v", err)
	}

	// The human posts back with a mention: resume, then ordinary in-session
	// handling (the mention summons).
	msg := e.mustPost(t, "@atlas approved, proceed")
	if msg.WorkSessionID == nil || *msg.WorkSessionID != session.ID {
		t.Fatalf("resuming post work_session_id = %v", msg.WorkSessionID)
	}

	waitFor(t, func() bool { return len(e.submitter.requestsFor(e.atlas.ID)) == 1 }, "atlas summoned after resume")
	req := e.submitter.requestsFor(e.atlas.ID)[0]
	if req.WorkSessionID != session.ID {
		t.Fatalf("atlas run work_session_id = %q", req.WorkSessionID)
	}

	reloaded, _ := e.wsStore.GetWorkSession(context.Background(), e.ws.ID, session.ID)
	if reloaded.Status != domain.WorkSessionOpen || reloaded.PauseReason != "" {
		t.Fatalf("session after resume = %+v", reloaded)
	}
	if reloaded.HopsUsed != 2 {
		t.Fatalf("hops_used = %d, want 2 (kickoff + post-resume summon)", reloaded.HopsUsed)
	}

	// session_updated: kickoff + resume (the store-level pause broadcasts
	// nothing). The collector drains asynchronously — wait for both frames.
	waitForEvents(t, e.events, EventSessionUpdated, 2, "kickoff + resume session_updated")
	sessions := e.sessionUpdatedPayloads(t)
	if len(sessions) != 2 {
		t.Fatalf("session_updated count = %d, want 2", len(sessions))
	}
	if sessions[1].Status != domain.WorkSessionOpen || sessions[1].PauseReason != "" {
		t.Fatalf("resume session_updated = %+v", sessions[1])
	}
}

// -------------------------------------------------------------------------
// Close
// -------------------------------------------------------------------------

func TestSession_Close_FacilitatorOnly_NoFanOut(t *testing.T) {
	e := newSessionEnv(t)
	_, session := e.kickoff(t, "close me when done")

	ctx := context.Background()

	// A non-facilitator agent cannot close; neither can the human.
	if _, err := e.cp.CloseWorkSession(ctx, e.ws.ID, e.channel.ID, e.atlas.ID, "nope"); !errors.Is(err, domain.ErrNotFacilitator) {
		t.Fatalf("atlas close = %v, want ErrNotFacilitator", err)
	}
	if _, err := e.cp.CloseWorkSession(ctx, e.ws.ID, e.channel.ID, e.sarah.ID, "nope"); !errors.Is(err, domain.ErrNotFacilitator) {
		t.Fatalf("human close = %v, want ErrNotFacilitator", err)
	}

	closed, err := e.cp.CloseWorkSession(ctx, e.ws.ID, e.channel.ID, e.charlie.ID, "shipped dark mode @atlas")
	if err != nil {
		t.Fatalf("facilitator close: %v", err)
	}
	if closed.Status != domain.WorkSessionClosed || closed.Summary != "shipped dark mode @atlas" || closed.ClosedAt == nil {
		t.Fatalf("closed session = %+v", closed)
	}

	// The closing message: posted as the facilitator, tagged with the closed
	// session id, mentions plain (no resolved mentions), and NO fan-out —
	// @atlas in the summary mints nothing.
	waitFor(t, func() bool {
		for _, m := range e.feedMessages(t) {
			if m.AuthorAgentID == e.charlie.ID && strings.Contains(m.Body, "shipped") {
				return true
			}
		}
		return false
	}, "closing message persisted")
	var closing domain.ChannelMessage
	for _, m := range e.feedMessages(t) {
		if m.AuthorAgentID == e.charlie.ID && strings.Contains(m.Body, "shipped") {
			closing = m
		}
	}
	if closing.WorkSessionID == nil || *closing.WorkSessionID != session.ID {
		t.Fatalf("closing message work_session_id = %v", closing.WorkSessionID)
	}
	if len(closing.Mentions) != 0 {
		t.Fatalf("closing message mentions = %+v, want plain", closing.Mentions)
	}
	if len(e.submitter.requestsFor(e.atlas.ID)) != 0 {
		t.Fatal("closing message must fan nothing out")
	}

	// session_updated: kickoff + close.
	waitForEvents(t, e.events, EventSessionUpdated, 2, "kickoff + close session_updated")
	sessions := e.sessionUpdatedPayloads(t)
	if last := sessions[len(sessions)-1]; last.Status != domain.WorkSessionClosed || last.Summary != closed.Summary {
		t.Fatalf("close session_updated = %+v", last)
	}

	// After close the channel is session-less: a fresh kickoff works and the
	// closing message's mention traffic follows v1 rules.
	_, session2, err := e.cp.PostKickoff(ctx, e.ws.ID, e.channel.ID, Author{
		Type:   string(domain.ChannelMemberTypeUser),
		UserID: e.sarah.ID,
	}, "next goal")
	if err != nil {
		t.Fatalf("post-close kickoff: %v", err)
	}
	if session2.ID == session.ID {
		t.Fatal("post-close kickoff must mint a NEW session")
	}
}

// -------------------------------------------------------------------------
// Stall watchdog
// -------------------------------------------------------------------------

func TestSession_Watchdog_FiresOncePerIdlePeriod_RearmsOnActivity(t *testing.T) {
	e := newSessionEnvWithIdle(t, 80*time.Millisecond)
	_, session := e.kickoff(t, "idle prone goal")

	ctx := context.Background()
	waitFor(t, func() bool { return len(e.notifyRuns("(watchdog)")) == 1 }, "watchdog fired once")
	time.Sleep(150 * time.Millisecond) // several idle periods' worth
	if got := len(e.notifyRuns("(watchdog)")); got != 1 {
		t.Fatalf("watchdog runs = %d, want exactly 1 per idle period", got)
	}
	req := e.notifyRuns("(watchdog)")[0]
	if !strings.Contains(req.Input, "(watchdog)") || req.WorkSessionID != session.ID {
		t.Fatalf("watchdog input/session = %q/%q", req.Input, req.WorkSessionID)
	}

	// The watchdog run billed a hop (kickoff 1 + watchdog = 2).
	reloaded, _ := e.wsStore.GetWorkSession(ctx, e.ws.ID, session.ID)
	if reloaded.HopsUsed != 2 {
		t.Fatalf("hops_used = %d, want 2 (watchdog bills)", reloaded.HopsUsed)
	}

	// The next session message re-arms the timer: the following idle period
	// fires exactly one more watchdog, then stays quiet until activity.
	e.mustPost(t, "status update from the human")
	waitFor(t, func() bool { return len(e.notifyRuns("(watchdog)")) == 2 }, "watchdog re-armed and fired again")
	time.Sleep(150 * time.Millisecond)
	if got := len(e.notifyRuns("(watchdog)")); got != 2 {
		t.Fatalf("watchdog runs after the second fire = %d, want exactly 2 (no re-arm without activity)", got)
	}

	// Closing cancels the timer: no further watchdogs after close.
	if _, err := e.cp.CloseWorkSession(ctx, e.ws.ID, e.channel.ID, e.charlie.ID, "enough"); err != nil {
		t.Fatalf("close: %v", err)
	}
	runsBefore := len(e.notifyRuns("(watchdog)"))
	time.Sleep(200 * time.Millisecond)
	if got := len(e.notifyRuns("(watchdog)")); got != runsBefore {
		t.Fatalf("watchdog runs after close = %d, want %d (timer cancelled)", got, runsBefore)
	}
}

func TestSession_Watchdog_BootRearmAndCancelOnPause(t *testing.T) {
	e := newSessionEnvWithIdle(t, 80*time.Millisecond)
	_, session := e.kickoff(t, "boot scan goal")

	// StartWatchdog re-arms timers for sessions already open at boot.
	watchCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go e.cp.StartWatchdog(watchCtx)

	waitFor(t, func() bool { return len(e.notifyRuns("(watchdog)")) >= 1 }, "boot watchdog fired")

	// Pause cancels the timer: after pausing, the idle period passes with no
	// new watchdog runs (resume re-arms — covered by the activity test).
	ctx := context.Background()
	if _, err := e.wsStore.PauseWorkSession(ctx, e.ws.ID, session.ID, domain.WorkSessionPauseAwaitingHuman); err != nil {
		t.Fatalf("pause: %v", err)
	}
	runsBefore := len(e.notifyRuns("(watchdog)"))
	time.Sleep(200 * time.Millisecond)
	if got := len(e.notifyRuns("(watchdog)")); got != runsBefore {
		t.Fatalf("watchdog runs while paused = %d, want %d (timer cancelled)", got, runsBefore)
	}
}

// newSessionEnvWithIdle is newSessionEnv with a short injected watchdog idle.
func newSessionEnvWithIdle(t *testing.T, idle time.Duration) *sessionEnv {
	t.Helper()
	e := &sessionEnv{testEnv: newTestEnv(t)}
	roster, err := e.channels.ListChannelMembers(context.Background(), e.ws.ID, e.channel.ID)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	for _, m := range roster {
		if m.MemberType == domain.ChannelMemberTypeAgent && m.AgentID == e.charlie.ID {
			if err := e.channels.UpdateChannelMemberRole(context.Background(), e.ws.ID, e.channel.ID, m.ID, domain.ChannelMemberRoleFacilitator); err != nil {
				t.Fatalf("promote charlie: %v", err)
			}
		}
	}
	e.wsStore = e.st.WorkSessions()
	e.cp = NewChokepoint(e.st.Channels(), e.submitter, e.hub,
		WithSilenceDecider(e.decider),
		WithWorkSessionStore(e.wsStore),
		WithChannelHandles(storeHandles{st: e.st}),
		WithWatchdogIdle(idle),
	)
	e.events = collectEvents(t, e.hub, e.channel.ID)
	return e
}
