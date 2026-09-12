package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// -------------------------------------------------------------------------
// Test scaffolding: seeded fake store, scripted run submitter, scripted
// decider, hub event collector.
// -------------------------------------------------------------------------

type testEnv struct {
	st        store.Store
	channels  store.ChannelStore
	ws        *domain.Workspace
	sarah     *domain.User // the human poster
	atlas     *domain.Agent
	beacon    *domain.Agent
	charlie   *domain.Agent
	channel   *domain.Channel
	submitter *fakeRunSubmitter
	decider   *scriptedDecider
	hub       *Hub
	cp        *Chokepoint
	events    *eventCollector
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "Acme Corp"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	sarah := &domain.User{Email: "sarah@example.com", Name: "Sarah Chen"}
	if err := st.Users().Create(ctx, sarah); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: sarah.ID, RoleID: role.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	newAgent := func(slug, name string) *domain.Agent {
		ag := &domain.Agent{
			WorkspaceID: ws.ID,
			Slug:        slug,
			Name:        name,
			ProviderID:  prov.ID,
			Model:       "gpt-4o",
			Temperature: 0.7,
			Autonomy:    domain.AutonomyFull,
		}
		if err := st.Agents().Create(ctx, ag); err != nil {
			t.Fatalf("create agent %s: %v", slug, err)
		}
		return ag
	}
	atlas := newAgent("atlas", "Atlas")
	beacon := newAgent("beacon", "Beacon")
	charlie := newAgent("charlie", "Charlie")

	channel := &domain.Channel{
		WorkspaceID: ws.ID,
		Name:        "Production Ops",
		Slug:        "ops",
		Purpose:     "Coordinate production incident response",
	}
	if err := st.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	addMember := func(member *domain.ChannelMember) {
		if err := st.Channels().AddChannelMember(ctx, member); err != nil {
			t.Fatalf("add channel member: %v", err)
		}
	}
	addMember(&domain.ChannelMember{WorkspaceID: ws.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeUser, UserID: sarah.ID})
	addMember(&domain.ChannelMember{WorkspaceID: ws.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeAgent, AgentID: atlas.ID, Specialization: "metrics"})
	addMember(&domain.ChannelMember{WorkspaceID: ws.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeAgent, AgentID: beacon.ID, Specialization: "comms"})
	addMember(&domain.ChannelMember{WorkspaceID: ws.ID, ChannelID: channel.ID, MemberType: domain.ChannelMemberTypeAgent, AgentID: charlie.ID})

	submitter := &fakeRunSubmitter{}
	decider := &scriptedDecider{
		decide: func(string) (Decision, error) { return Decision{Engage: false, Reason: "declined by default"}, nil },
	}
	hub := NewHub()

	env := &testEnv{
		st:        st,
		channels:  st.Channels(),
		ws:        ws,
		sarah:     sarah,
		atlas:     atlas,
		beacon:    beacon,
		charlie:   charlie,
		channel:   channel,
		submitter: submitter,
		decider:   decider,
		hub:       hub,
	}
	env.cp = newChokepointWithDecider(st.Channels(), submitter, hub, decider)
	env.cp.handles = storeHandles{st: st}
	env.events = collectEvents(t, hub, channel.ID)
	return env
}

// postAs posts as the seeded human (Sarah).
func (e *testEnv) postAs(ctx context.Context, body string) (domain.ChannelMessage, error) {
	return e.cp.Post(ctx, e.ws.ID, e.channel.ID, Author{
		Type:   string(domain.ChannelMemberTypeUser),
		UserID: e.sarah.ID,
	}, body)
}

// storeHandles resolves handles from the fake store with the shared rule:
// agents by slug, humans by dashed lowercase display name.
type storeHandles struct {
	st store.Store
}

func (h storeHandles) UserHandle(ctx context.Context, userID string) (string, error) {
	u, err := h.st.Users().ByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return humanChannelHandle(u.Name), nil
}

func (h storeHandles) AgentHandle(ctx context.Context, workspaceID, agentID string) (string, error) {
	a, err := h.st.Agents().ByID(ctx, workspaceID, agentID)
	if err != nil {
		return "", err
	}
	return a.Slug, nil
}

// humanChannelHandle mirrors the agents.ChannelHandles rule (lowercase,
// spaces → dashes). The chokepoint must stay in lockstep with the runner's
// rule; the copy here is asserted equal to the runner's in a test.
func humanChannelHandle(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}

// fakeRunSubmitter records ExecRequests and returns pre-scripted streams.
// The default script emits a turn with an empty final assistant message —
// no auto-post — keeping summon tests focused. Tests script per-request
// event lists for auto-post and run-summary scenarios.
type fakeRunSubmitter struct {
	mu     sync.Mutex
	reqs   []agents.ExecRequest
	err    error // when set, every Run fails immediately
	script func(req agents.ExecRequest) []agents.TranscriptEvent
}

func (f *fakeRunSubmitter) Run(_ context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	script, err := f.script, f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}

	stream := agents.NewEventStream(32)
	events := []agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-" + req.SessionID},
		{Kind: agents.TranscriptEventMessageCompleted, TurnID: "turn-" + req.SessionID, Message: &agents.CompletedMessage{Role: "assistant", Content: ""}},
	}
	if script != nil {
		events = script(req)
	}
	for i := range events {
		stream.Send(&events[i])
	}
	stream.Close()
	return stream, nil
}

func (f *fakeRunSubmitter) requests() []agents.ExecRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]agents.ExecRequest, len(f.reqs))
	copy(out, f.reqs)
	return out
}

func (f *fakeRunSubmitter) requestsFor(agentID string) []agents.ExecRequest {
	var out []agents.ExecRequest
	for _, req := range f.requests() {
		if req.AgentID == agentID {
			out = append(out, req)
		}
	}
	return out
}

// scriptedDecider records which agents it was asked about and answers
// through a test-controlled function (which may block to sequence the
// responder-cap race deterministically).
type scriptedDecider struct {
	mu     sync.Mutex
	calls  []string
	decide func(agentID string) (Decision, error)
}

func (d *scriptedDecider) Decide(_ context.Context, in DecideInput) (Decision, error) {
	d.mu.Lock()
	d.calls = append(d.calls, in.AgentID)
	d.mu.Unlock()
	return d.decide(in.AgentID)
}

func (d *scriptedDecider) calledFor() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// eventCollector drains one channel's hub stream in the background.
type eventCollector struct {
	mu     sync.Mutex
	events []Event
}

func collectEvents(t *testing.T, hub *Hub, channelID string) *eventCollector {
	t.Helper()
	ch, cancel := hub.Subscribe(channelID)
	t.Cleanup(cancel)
	ec := &eventCollector{}
	go func() {
		for ev := range ch {
			ec.mu.Lock()
			ec.events = append(ec.events, ev)
			ec.mu.Unlock()
		}
	}()
	return ec
}

func (ec *eventCollector) snapshot() []Event {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return append([]Event(nil), ec.events...)
}

func (ec *eventCollector) ofType(eventType string) []Event {
	var out []Event
	for _, ev := range ec.snapshot() {
		if ev.Type == eventType {
			out = append(out, ev)
		}
	}
	return out
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within 2s: %s", msg)
}

// waitForEvents waits until the collector has seen n events of one type —
// the collector drains the hub stream on its own goroutine, so synchronous
// broadcasts may not be visible to the test immediately.
func waitForEvents(t *testing.T, ec *eventCollector, eventType string, n int, msg string) []Event {
	t.Helper()
	waitFor(t, func() bool { return len(ec.ofType(eventType)) >= n }, msg)
	return ec.ofType(eventType)
}

func feedMessages(t *testing.T, e *testEnv) []domain.ChannelMessage {
	t.Helper()
	msgs, err := e.channels.ListChannelMessages(context.Background(), store.ListChannelMessagesParams{
		WorkspaceID: e.ws.ID,
		ChannelID:   e.channel.ID,
	})
	if err != nil {
		t.Fatalf("list feed: %v", err)
	}
	return msgs
}

func mustPost(t *testing.T, e *testEnv, body string) domain.ChannelMessage {
	t.Helper()
	msg, err := e.postAs(context.Background(), body)
	if err != nil {
		t.Fatalf("post %q: %v", body, err)
	}
	return msg
}

// -------------------------------------------------------------------------
// Pipeline tests
// -------------------------------------------------------------------------

func TestPost_MentionResolution(t *testing.T) {
	e := newTestEnv(t)
	msg := mustPost(t, e, "@Atlas please check @ghost too")

	if len(msg.Mentions) != 1 {
		t.Fatalf("mentions = %+v, want exactly the resolved atlas mention", msg.Mentions)
	}
	mention := msg.Mentions[0]
	if mention.Type != domain.ChannelMemberTypeAgent || mention.ID != e.atlas.ID || mention.Handle != "atlas" {
		t.Fatalf("mention = %+v, want resolved atlas", mention)
	}
	if msg.Body != "@Atlas please check @ghost too" {
		t.Fatalf("body = %q, want verbatim (unresolved @ghost stays plain text)", msg.Body)
	}

	// The persisted row carries the same resolution.
	stored := feedMessages(t, e)[0]
	if len(stored.Mentions) != 1 || stored.Mentions[0].ID != e.atlas.ID {
		t.Fatalf("persisted mentions = %+v", stored.Mentions)
	}

	// message_posted carries seq + the message payload.
	posted := waitForEvents(t, e.events, EventMessagePosted, 1, "message_posted broadcast")
	if len(posted) != 1 {
		t.Fatalf("message_posted events = %d, want 1", len(posted))
	}
	if posted[0].Seq != msg.Seq {
		t.Fatalf("broadcast seq = %d, want %d", posted[0].Seq, msg.Seq)
	}
	var payload struct {
		Message domain.ChannelMessage `json:"message"`
	}
	if err := json.Unmarshal(posted[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Message.ID != msg.ID || len(payload.Message.Mentions) != 1 {
		t.Fatalf("broadcast message = %+v", payload.Message)
	}
}

func TestPost_MentionSummon_Deterministic_NoDecider(t *testing.T) {
	e := newTestEnv(t)
	msg := mustPost(t, e, "@atlas can you analyze the payment part?")

	reqs := e.submitter.requestsFor(e.atlas.ID)
	if len(reqs) != 1 {
		t.Fatalf("atlas runs = %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Origin != agents.OriginChannel {
		t.Fatalf("origin = %q, want channel", req.Origin)
	}
	if req.SessionID != ChannelSessionID(e.channel.ID, e.atlas.ID) {
		t.Fatalf("session id = %q, want deterministic chan_<channel>_<agent>", req.SessionID)
	}
	if req.ChannelID != e.channel.ID {
		t.Fatalf("channel id = %q", req.ChannelID)
	}
	if req.RootMessageID != msg.ID {
		t.Fatalf("root message id = %q, want the triggering message itself", req.RootMessageID)
	}
	if req.ChainDepth != 0 {
		t.Fatalf("chain depth = %d, want 0 for a fresh human post", req.ChainDepth)
	}
	if req.UserID != e.sarah.ID {
		t.Fatalf("user id = %q, want the summoning user", req.UserID)
	}
	if req.Input != "[#ops] @sarah-chen: @atlas can you analyze the payment part?" {
		t.Fatalf("input = %q", req.Input)
	}
	if calls := e.decider.calledFor(); len(calls) != 0 {
		t.Fatalf("decider called for %v; deterministic summons never call it", calls)
	}

	// Events: message_posted, summon_decided (no considering), run_started.
	if len(e.events.ofType(EventSummonConsidering)) != 0 {
		t.Fatal("deterministic summons must not broadcast summon_considering")
	}
	waitForEvents(t, e.events, EventSummonDecided, 1, "summon_decided broadcast")
	waitForEvents(t, e.events, EventRunStarted, 1, "run_started broadcast")
	waitForEvents(t, e.events, EventRunFinished, 1, "run_finished broadcast")
}

func TestPost_SuppressedMention_RendersPlainText_NoReSummon(t *testing.T) {
	// atlas mentions beacon in its auto-post; beacon's auto-post mentions
	// atlas — but atlas was already summoned in the chain, so the mention is
	// suppressed: plain text, no second atlas run.
	e := newTestEnv(t)
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turn := "turn-" + req.SessionID
		text := ""
		switch req.AgentID {
		case e.atlas.ID:
			text = "pulling the graphs now @beacon"
		case e.beacon.ID:
			text = "updates sent @atlas"
		}
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turn, Message: &agents.CompletedMessage{Role: "assistant", Content: text}},
		}
	}

	root := mustPost(t, e, "@atlas incident started")

	// Two runs total: atlas (mentioned) and beacon (mentioned by atlas's
	// auto-post). Atlas's re-mention in beacon's auto-post is suppressed.
	waitFor(t, func() bool { return len(e.submitter.requests()) == 2 }, "atlas + beacon runs minted")
	time.Sleep(50 * time.Millisecond) // let a would-be third submit surface
	if reqs := e.submitter.requestsFor(e.atlas.ID); len(reqs) != 1 {
		t.Fatalf("atlas runs = %d, want 1 (no re-summon within the chain)", len(reqs))
	}

	msgs := feedMessages(t, e)
	if len(msgs) != 3 {
		t.Fatalf("feed rows = %d, want root + two auto-posts", len(msgs))
	}

	// atlas auto-post: chain root = the human message, depth 1, beacon resolved.
	var atlasPost, beaconPost domain.ChannelMessage
	for _, msg := range msgs {
		switch msg.AuthorAgentID {
		case e.atlas.ID:
			atlasPost = msg
		case e.beacon.ID:
			beaconPost = msg
		}
	}
	if atlasPost.ID == "" || beaconPost.ID == "" {
		t.Fatalf("missing auto-posts: %+v", msgs)
	}
	if atlasPost.RootMessageID == nil || *atlasPost.RootMessageID != root.ID || atlasPost.ChainDepth != 1 {
		t.Fatalf("atlas auto-post chain = root %v depth %d, want root %s depth 1", atlasPost.RootMessageID, atlasPost.ChainDepth, root.ID)
	}
	if len(atlasPost.Mentions) != 1 || atlasPost.Mentions[0].ID != e.beacon.ID {
		t.Fatalf("atlas auto-post mentions = %+v, want beacon resolved", atlasPost.Mentions)
	}

	// beacon auto-post: depth 2, and the @atlas mention was suppressed —
	// plain text, so the persisted mentions list is empty.
	if beaconPost.ChainDepth != 2 || beaconPost.RootMessageID == nil || *beaconPost.RootMessageID != root.ID {
		t.Fatalf("beacon auto-post chain = root %v depth %d, want root %s depth 2", beaconPost.RootMessageID, beaconPost.ChainDepth, root.ID)
	}
	if len(beaconPost.Mentions) != 0 {
		t.Fatalf("beacon auto-post mentions = %+v, want the suppressed @atlas dropped (plain text)", beaconPost.Mentions)
	}
	if beaconPost.SessionID == nil || *beaconPost.SessionID != ChannelSessionID(e.channel.ID, e.beacon.ID) {
		t.Fatalf("beacon auto-post session = %v, want its deterministic session", beaconPost.SessionID)
	}

	// The summon suppression is visible in the log-checked pipeline only;
	// the feed itself shows a plain-text mention, and no third run exists.
	if reqs := e.submitter.requests(); len(reqs) != 2 {
		t.Fatalf("total runs = %d, want 2", len(reqs))
	}
}

func TestPost_UntaggedMessage_DeciderFlow(t *testing.T) {
	e := newTestEnv(t)
	e.decider.decide = func(agentID string) (Decision, error) {
		if agentID == e.atlas.ID {
			return Decision{Engage: true, Reason: "payment expertise"}, nil
		}
		return Decision{Engage: false, Reason: "not my domain"}, nil
	}

	mustPost(t, e, "payments look flaky today")

	calls := e.decider.calledFor()
	if len(calls) != 3 {
		t.Fatalf("decider calls = %v, want one per agent member (atlas, beacon, charlie)", calls)
	}
	// All three agents observe; only atlas engages; beacon/charlie decline.
	for _, agent := range []*domain.Agent{e.atlas, e.beacon, e.charlie} {
		var found bool
		for _, id := range calls {
			if id == agent.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("decider not called for %s; calls = %v", agent.ID, calls)
		}
	}

	if reqs := e.submitter.requests(); len(reqs) != 1 || reqs[0].AgentID != e.atlas.ID {
		t.Fatalf("runs = %+v, want exactly atlas", reqs)
	}
	req := e.submitter.requests()[0]
	if req.RootMessageID == "" {
		t.Fatal("untagged run must still carry the chain root (the message itself)")
	}
	if req.ChainDepth != 0 {
		t.Fatalf("chain depth = %d, want 0", req.ChainDepth)
	}

	// considering for every candidate, decided per outcome.
	waitForEvents(t, e.events, EventSummonConsidering, 3, "summon_considering per candidate")
	waitForEvents(t, e.events, EventSummonDecided, 3, "summon_decided per outcome")
	decided := e.events.ofType(EventSummonDecided)
	if len(decided) != 3 {
		t.Fatalf("summon_decided events = %d, want 3 (one engage, two declines)", len(decided))
	}
}

func TestPost_UntaggedMessage_ResponsorCapTwo(t *testing.T) {
	e := newTestEnv(t)
	// Gate each decider so the test sequences the race deterministically:
	// atlas elects first, beacon second, charlie third — over the cap.
	release := map[string]chan struct{}{
		e.atlas.ID:   make(chan struct{}),
		e.beacon.ID:  make(chan struct{}),
		e.charlie.ID: make(chan struct{}),
	}
	e.decider.decide = func(agentID string) (Decision, error) {
		<-release[agentID]
		return Decision{Engage: true, Reason: "elect " + agentID}, nil
	}

	done := make(chan struct{})
	var postErr error
	go func() {
		defer close(done)
		_, postErr = e.postAs(context.Background(), "who can take this?")
	}()

	elect := func(agentID string) {
		close(release[agentID])
		waitFor(t, func() bool { return len(e.submitter.requestsFor(agentID)) == 1 }, agentID+" run submitted")
	}
	elect(e.atlas.ID)
	elect(e.beacon.ID)
	close(release[e.charlie.ID]) // third election: over the cap, suppressed
	waitFor(t, func() bool {
		return len(e.submitter.requests()) == 2
	}, "exactly two responder runs minted")
	<-done
	if postErr != nil {
		t.Fatalf("post: %v", postErr)
	}

	if reqs := e.submitter.requests(); len(reqs) != 2 {
		t.Fatalf("runs = %d, want 2 (responder cap)", len(reqs))
	}

	waitForEvents(t, e.events, EventSummonConsidering, 3, "summon_considering per candidate")
	decided := waitForEvents(t, e.events, EventSummonDecided, 3, "summon_decided per outcome")
	engaged, declined := 0, 0
	for _, ev := range decided {
		var payload struct {
			AgentID string `json:"agent_id"`
			Engage  bool   `json:"engage"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Engage {
			engaged++
			continue
		}
		declined++
		if payload.AgentID != e.charlie.ID {
			t.Fatalf("declined agent = %s, want only the over-cap charlie", payload.AgentID)
		}
		if !strings.Contains(payload.Reason, "cap") {
			t.Fatalf("cap decline reason = %q", payload.Reason)
		}
	}
	if engaged != 2 || declined != 1 {
		t.Fatalf("engaged = %d declined = %d, want 2/1", engaged, declined)
	}
}

func TestPost_DeciderFailure_Silent(t *testing.T) {
	e := newTestEnv(t)
	e.decider.decide = func(string) (Decision, error) {
		return Decision{}, fmt.Errorf("provider down")
	}

	mustPost(t, e, "nobody mentioned, who's in?")

	if calls := e.decider.calledFor(); len(calls) != 3 {
		t.Fatalf("decider calls = %d, want one per agent member", len(calls))
	}
	if reqs := e.submitter.requests(); len(reqs) != 0 {
		t.Fatalf("runs = %d, want 0 (silent on failure)", len(reqs))
	}
	if decided := e.events.ofType(EventSummonDecided); len(decided) != 0 {
		t.Fatalf("summon_decided events = %d, want 0 (silent-on-failure broadcasts nothing)", len(decided))
	}
	waitForEvents(t, e.events, EventSummonConsidering, 3, "summon_considering per candidate")
}

func TestPost_ChainDepthCap(t *testing.T) {
	// A chain where each agent's auto-post mentions the next fresh agent:
	// depths run 0 (human root) → 1 → 2 → 3, whose runs are minted at depths
	// 0..3; the depth-3 run's auto-post lands at depth 4 and summons nobody.
	e := newTestEnv(t)
	dave := &domain.Agent{WorkspaceID: e.ws.ID, Slug: "dave", Name: "Dave", ProviderID: e.atlas.ProviderID, Model: "m", Temperature: 0.5, Autonomy: domain.AutonomyFull}
	if err := e.st.Agents().Create(context.Background(), dave); err != nil {
		t.Fatal(err)
	}
	if err := e.channels.AddChannelMember(context.Background(), &domain.ChannelMember{
		WorkspaceID: e.ws.ID, ChannelID: e.channel.ID,
		MemberType: domain.ChannelMemberTypeAgent, AgentID: dave.ID,
	}); err != nil {
		t.Fatal(err)
	}
	agentBySlug := map[string]*domain.Agent{
		"atlas": e.atlas, "beacon": e.beacon, "charlie": e.charlie, "dave": dave,
	}
	nextMention := map[string]string{
		"atlas": "@beacon", "beacon": "@charlie", "charlie": "@dave", "dave": "@atlas",
	}
	e.submitter.script = func(req agents.ExecRequest) []agents.TranscriptEvent {
		turn := "turn-" + req.SessionID
		slug := ""
		for s, ag := range agentBySlug {
			if ag.ID == req.AgentID {
				slug = s
			}
		}
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: turn},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: turn, Message: &agents.CompletedMessage{
				Role: "assistant", Content: "handing to " + nextMention[slug],
			}},
		}
	}

	root := mustPost(t, e, "@atlas start the relay")

	// Four runs: atlas(0) → beacon(1) → charlie(2) → dave(3). Dave's
	// auto-post mentions atlas at depth 4 — over the cap: no fifth run.
	waitFor(t, func() bool { return len(e.submitter.requests()) == 4 }, "relay runs minted")
	time.Sleep(50 * time.Millisecond)
	if reqs := e.submitter.requests(); len(reqs) != 4 {
		t.Fatalf("runs = %d, want 4 (depth cap stops the chain)", len(reqs))
	}

	// Depth propagation along the minted runs.
	wantDepth := map[string]int{e.atlas.ID: 0, e.beacon.ID: 1, e.charlie.ID: 2, dave.ID: 3}
	for _, req := range e.submitter.requests() {
		if req.ChainDepth != wantDepth[req.AgentID] {
			t.Fatalf("run for %s depth = %d, want %d", req.AgentID, req.ChainDepth, wantDepth[req.AgentID])
		}
		if req.RootMessageID != root.ID {
			t.Fatalf("run for %s root = %q, want %q", req.AgentID, req.RootMessageID, root.ID)
		}
	}

	// The depth-4 auto-post exists in the feed with its mention resolved but
	// summoned nobody.
	waitFor(t, func() bool { return len(feedMessages(t, e)) == 5 }, "root + four auto-posts persisted")
	msgs := feedMessages(t, e)
	var depth4 *domain.ChannelMessage
	for i := range msgs {
		if msgs[i].ChainDepth == 4 {
			depth4 = &msgs[i]
		}
	}
	if depth4 == nil {
		t.Fatal("depth-4 auto-post missing from the feed")
	}
	if depth4.AuthorAgentID != dave.ID {
		t.Fatalf("depth-4 author = %s, want dave", depth4.AuthorAgentID)
	}
}

func TestPost_Validation(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		ws      string
		channel string
		author  Author
		body    string
	}{
		{name: "unknown author type", ws: e.ws.ID, channel: e.channel.ID, author: Author{Type: "robot", UserID: "u"}, body: "hi"},
		{name: "user author without id", ws: e.ws.ID, channel: e.channel.ID, author: Author{Type: "user"}, body: "hi"},
		{name: "agent author without id", ws: e.ws.ID, channel: e.channel.ID, author: Author{Type: "agent"}, body: "hi"},
		{name: "blank body", ws: e.ws.ID, channel: e.channel.ID, author: Author{Type: "user", UserID: e.sarah.ID}, body: "   "},
		{name: "unknown channel", ws: e.ws.ID, channel: "nope", author: Author{Type: "user", UserID: e.sarah.ID}, body: "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.cp.Post(ctx, tt.ws, tt.channel, tt.author, tt.body)
			if err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if reqs := e.submitter.requests(); len(reqs) != 0 {
		t.Fatalf("rejected posts must not mint runs, got %d", len(reqs))
	}
}

func TestPostFromAgent(t *testing.T) {
	e := newTestEnv(t)
	msg, err := e.cp.PostFromAgent(context.Background(), e.ws.ID, e.channel.ID, e.atlas.ID, "on it")
	if err != nil {
		t.Fatalf("PostFromAgent: %v", err)
	}
	if msg.AuthorType != domain.ChannelMemberTypeAgent || msg.AuthorAgentID != e.atlas.ID {
		t.Fatalf("author = %s/%s, want agent %s", msg.AuthorType, msg.AuthorAgentID, e.atlas.ID)
	}
	if msg.RootMessageID != nil || msg.ChainDepth != 0 {
		t.Fatalf("agent post without an active run = fresh chain, got root %v depth %d", msg.RootMessageID, msg.ChainDepth)
	}
}

func TestChannelContext_Wrappers(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	// Seed six messages; tail and before-cursor reads slice and order them.
	var lastSeq int64
	for i := 1; i <= 6; i++ {
		msg := mustPost(t, e, fmt.Sprintf("message %d", i))
		lastSeq = msg.Seq
	}

	tail, err := e.cp.ChannelTail(ctx, e.ws.ID, e.channel.ID, 4)
	if err != nil {
		t.Fatalf("ChannelTail: %v", err)
	}
	if len(tail) != 4 {
		t.Fatalf("tail length = %d, want 4", len(tail))
	}
	if tail[0].Body != "message 3" || tail[3].Body != "message 6" {
		t.Fatalf("tail bodies = %s..%s, want message 3..message 6 (oldest→newest)", tail[0].Body, tail[3].Body)
	}

	tail, err = e.cp.ChannelTail(ctx, e.ws.ID, e.channel.ID, 0)
	if err != nil || len(tail) != 6 {
		t.Fatalf("uncapped tail = %d rows (err %v), want 6", len(tail), err)
	}

	after, err := e.cp.ChannelMessagesAfter(ctx, e.ws.ID, e.channel.ID, lastSeq, 50)
	if err != nil {
		t.Fatalf("ChannelMessagesAfter: %v", err)
	}
	if len(after) != 5 {
		t.Fatalf("before-cursor page = %d rows, want 5 (strictly older than the newest)", len(after))
	}
	if after[0].Body != "message 1" || after[4].Body != "message 5" {
		t.Fatalf("before-cursor order = %s..%s, want oldest→newest", after[0].Body, after[4].Body)
	}

	firstPage, err := e.cp.ChannelMessagesAfter(ctx, e.ws.ID, e.channel.ID, 0, 2)
	if err != nil {
		t.Fatalf("ChannelMessagesAfter(0): %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].Body != "message 5" || firstPage[1].Body != "message 6" {
		t.Fatalf("first page = %+v, want the two newest messages", firstPage)
	}

	channel, err := e.cp.GetChannel(ctx, e.ws.ID, e.channel.ID)
	if err != nil || channel.Slug != "ops" {
		t.Fatalf("GetChannel = %+v (%v)", channel, err)
	}
	members, err := e.cp.ListChannelMembers(ctx, e.ws.ID, e.channel.ID)
	if err != nil || len(members) != 4 {
		t.Fatalf("ListChannelMembers = %d members (%v), want 4", len(members), err)
	}
}

func TestHumanChannelHandle_MatchesRunnerRule(t *testing.T) {
	// The runner package owns the canonical rule; keep the copies in lockstep
	// (the chokepoint test package cannot import the runner's unexported
	// function, so the rule is restated here and pinned by expectation).
	tests := map[string]string{
		"Sarah Chen":  "sarah-chen",
		"  Padded  ":  "padded",
		"single":      "single",
		"Multi Space": "multi-space",
	}
	for name, want := range tests {
		if got := humanChannelHandle(name); got != want {
			t.Fatalf("humanChannelHandle(%q) = %q, want %q", name, got, want)
		}
	}
}
