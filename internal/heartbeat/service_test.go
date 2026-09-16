package heartbeat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// baseTime anchors the injected clock; every test computes expectations from
// it, so no due-math depends on the real wall clock.
var baseTime = time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

// fakeClock is the WithNow-injected test clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// fakeSubmitter records ExecRequests and returns pre-scripted streams,
// mirroring the scheduler test's stream fake. The default script is a
// successful turn replying "All done." (a report) with 42 tokens of usage.
type fakeSubmitter struct {
	mu     sync.Mutex
	reqs   []agents.ExecRequest
	err    error
	script func(req agents.ExecRequest) []agents.TranscriptEvent
	// hold, when non-nil, keeps every stream open until one token is
	// consumed — in-flight and conflict tests gate on it.
	hold chan struct{}
}

func (f *fakeSubmitter) Run(_ context.Context, req agents.ExecRequest) (*agents.EventStream, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	err, script, hold := f.err, f.script, f.hold
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	events := []agents.TranscriptEvent{
		{Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
		{Kind: agents.TranscriptEventMessageCompleted, TurnID: "turn-1",
			Message: &agents.CompletedMessage{Role: "assistant", Content: "All done."}},
		{Kind: agents.TranscriptEventTurnCompleted, TurnID: "turn-1",
			Usage: &agents.UsagePayload{TotalTokens: 42}},
	}
	if script != nil {
		events = script(req)
	}
	stream := agents.NewEventStream(32)
	go func() {
		defer stream.Close()
		for i := range events {
			stream.Send(&events[i])
		}
		if hold != nil {
			<-hold
		}
	}()
	return stream, nil
}

func (f *fakeSubmitter) requests() []agents.ExecRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agents.ExecRequest(nil), f.reqs...)
}

func (f *fakeSubmitter) setHold(h chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hold = h
}

func (f *fakeSubmitter) reply(content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = func(agents.ExecRequest) []agents.TranscriptEvent {
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
			{Kind: agents.TranscriptEventMessageCompleted, TurnID: "turn-1",
				Message: &agents.CompletedMessage{Role: "assistant", Content: content}},
			{Kind: agents.TranscriptEventTurnCompleted, TurnID: "turn-1"},
		}
	}
}

func (f *fakeSubmitter) failTurn(message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = func(agents.ExecRequest) []agents.TranscriptEvent {
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
			{Kind: agents.TranscriptEventError, Error: message},
		}
	}
}

// fakePoster records PostFromAgent calls.
type fakePoster struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakePoster) PostFromAgent(_ context.Context, workspaceID, channelID, agentID, body string) (domain.ChannelMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, workspaceID+"|"+channelID+"|"+agentID+"|"+body)
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return domain.ChannelMessage{}, err
	}
	return domain.ChannelMessage{ID: "posted"}, nil
}

func (f *fakePoster) posted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeBusy stands in for the runner's AgentBusy method — the service
// consumes the narrow interface, never the concrete runner.
type fakeBusy struct {
	mu   sync.Mutex
	busy map[string]bool
}

func (f *fakeBusy) AgentBusy(workspaceID, agentID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy[workspaceID+"|"+agentID]
}

func (f *fakeBusy) set(workspaceID, agentID string, busy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy == nil {
		f.busy = make(map[string]bool)
	}
	f.busy[workspaceID+"|"+agentID] = busy
}

// outboxErrStore fails every enqueue: the outbox write-failure seam.
type outboxErrStore struct {
	store.GatewayOutbox
}

func (o outboxErrStore) Enqueue(ctx context.Context, e *domain.OutboxEntry) error {
	return errors.New("outbox write failed")
}

// agentGoneStore hides every agent behind ErrNotFound. Agent deletion
// cascades the heartbeat in the store ports, so the claim-then-fire race is
// simulated at the preflight seam (the scheduler test's same trick).
type agentGoneStore struct {
	store.AgentStore
}

func (a agentGoneStore) ByID(ctx context.Context, workspaceID, id string) (*domain.Agent, error) {
	return nil, fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
}

// harness seeds the full heartbeat fixture (creator, workspace, provider,
// agent) and a service over the fake store with an injected clock.
type harness struct {
	st        store.Store
	submitter *fakeSubmitter
	poster    *fakePoster
	busy      *fakeBusy
	clock     *fakeClock
	svc       *Service
	ws        *domain.Workspace
	wsID      string
	agentID   string
	creatorID string
	// outboxPort optionally replaces the store's outbox (failure seams).
	outboxPort store.GatewayOutbox
}

func newHarness(t *testing.T, opts ...Option) *harness {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	u := &domain.User{Email: "creator@example.com", Name: "Creator"}
	if err := st.Users().Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Name: "main", Type: "openai"}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt"}
	if err := st.Agents().Create(ctx, a); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	h := &harness{
		st:        st,
		submitter: &fakeSubmitter{},
		poster:    &fakePoster{},
		busy:      &fakeBusy{},
		clock:     newClock(baseTime),
		ws:        ws,
		wsID:      ws.ID,
		agentID:   a.ID,
		creatorID: u.ID,
	}
	h.rewire(t, st.Agents(), opts...)
	return h
}

// rewire constructs (or reconstructs) the service over the fixture, with an
// optionally wrapped store port.
func (h *harness) rewire(t *testing.T, agentsStore store.AgentStore, opts ...Option) {
	t.Helper()
	opts = append([]Option{
		WithNow(h.clock.Now),
		WithRunTimeout(2 * time.Second), // real-time watchdog bound for held streams
	}, opts...)
	outbox := h.outboxPort
	if outbox == nil {
		outbox = h.st.GatewayOutbox()
	}
	h.svc = NewService(
		h.st.Heartbeats(), h.st.Users(), agentsStore, h.st.Workspaces(),
		h.st.Channels(), h.st.Schedulers(),
		h.submitter, h.busy, h.poster,
		h.st.Gateways(), h.st.GatewayLinks(), outbox,
		discardLogger(), opts...,
	)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setWorkspaceTimezone rewrites the fixture workspace's timezone (the
// active-hours tests evaluate the window in it).
func (h *harness) setWorkspaceTimezone(t *testing.T, tz string) {
	t.Helper()
	h.ws.Timezone = tz
	if err := h.st.Workspaces().Update(context.Background(), h.ws); err != nil {
		t.Fatalf("set workspace timezone: %v", err)
	}
}

// seedHeartbeatDue materializes an enabled heartbeat due at the given
// instant (the fake store persists next_tick_at as given — the caller-side
// derivation contract the service layer implements).
func (h *harness) seedHeartbeatDue(t *testing.T, mutate func(*domain.Heartbeat), due time.Time) *domain.Heartbeat {
	t.Helper()
	hb := &domain.Heartbeat{
		WorkspaceID: h.wsID,
		AgentID:     h.agentID,
		CreatedBy:   &h.creatorID,
		Prompt:      "Check the overnight deploy",
		Expr:        "*/15 * * * *",
		Delivery:    domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryCreatorDM},
		Enabled:     true,
	}
	if mutate != nil {
		mutate(hb)
	}
	if err := domain.ValidateHeartbeat(hb, baseTime); err != nil {
		t.Fatalf("validate heartbeat: %v", err)
	}
	hb.NextTickAt = &due
	if err := h.st.Heartbeats().PutHeartbeat(context.Background(), h.wsID, h.agentID, hb); err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
	return hb
}

// seedHeartbeat seeds a heartbeat due one minute before the injected clock.
func (h *harness) seedHeartbeat(t *testing.T, mutate func(*domain.Heartbeat)) *domain.Heartbeat {
	t.Helper()
	return h.seedHeartbeatDue(t, mutate, h.clock.Now().Add(-time.Minute))
}

// pauseSeed clears next_tick_at on a seeded heartbeat (the paused-banner
// shape: disabled with no next tick).
func (h *harness) pauseSeed(t *testing.T, hb *domain.Heartbeat) {
	t.Helper()
	hb.Enabled = false
	hb.NextTickAt = nil
	if err := h.st.Heartbeats().PutHeartbeat(context.Background(), h.wsID, h.agentID, hb); err != nil {
		t.Fatalf("pause seeded heartbeat: %v", err)
	}
}

func (h *harness) getHeartbeat(t *testing.T) *domain.Heartbeat {
	t.Helper()
	hb, err := h.st.Heartbeats().GetHeartbeat(context.Background(), h.wsID, h.agentID)
	if err != nil {
		t.Fatalf("get heartbeat: %v", err)
	}
	return hb
}

func (h *harness) runsFor(t *testing.T, heartbeatID string) []domain.HeartbeatRun {
	t.Helper()
	runs, _, err := h.st.Heartbeats().ListHeartbeatRuns(context.Background(), h.wsID, heartbeatID, 100, 0)
	if err != nil {
		t.Fatalf("list heartbeat runs: %v", err)
	}
	return runs
}

func (h *harness) pendingOutbox(t *testing.T) []domain.OutboxEntry {
	t.Helper()
	entries, err := h.st.GatewayOutbox().ClaimDue(context.Background(), baseTime, 50)
	if err != nil {
		t.Fatalf("claim outbox: %v", err)
	}
	return entries
}

func (h *harness) seedGateway(t *testing.T, platform string, enabled bool) domain.GatewayConfig {
	t.Helper()
		g := &domain.GatewayConfig{
			WorkspaceID: h.wsID,
			Platform:    platform,
			Identity:    "bot_" + platform,
			AgentID:     h.agentID,
			Enabled:     enabled,
			Transport:   domain.GatewayTransportLongPolling,
		}
		if platform == domain.GatewayPlatformTelegram {
			g.BotTokenCiphertext = "v1:nonce:ciphertext"
		}
		if platform == domain.GatewayPlatformWhatsApp {
			g.Lane = domain.GatewayLaneMultiDevice
		}
		if err := h.st.Gateways().CreateGateway(context.Background(), h.wsID, g); err != nil {
			t.Fatalf("seed gateway %s: %v", platform, err)
		}
	return *g
}

func (h *harness) seedUserLink(t *testing.T, platform, platformUserID string) {
	t.Helper()
	l := &domain.UserLink{
		Platform:       platform,
		PlatformUserID: platformUserID,
		WorkspaceID:    h.wsID,
		UserID:         h.creatorID,
	}
	if err := h.st.GatewayLinks().CreateUserLink(context.Background(), h.wsID, l); err != nil {
		t.Fatalf("seed user link %s/%s: %v", platform, platformUserID, err)
	}
}

func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func outboxPayload(t *testing.T, e domain.OutboxEntry) gateways.OutboxPayload {
	t.Helper()
	var p gateways.OutboxPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("decode outbox payload: %v", err)
	}
	return p
}

// TestDueTickFiresPersistentSession covers the claim → ExecRequest contract
// (add-agent-heartbeat D2/D11/D14): the tick fires into the agent's ONE
// persistent hb_<agentID> session with the heartbeat origin, the fixed turn
// prompt, the checklist, and the (here empty) digest — then the recorded
// outcome and the claim-time cadence advance.
func TestDueTickFiresPersistentSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop() // join all fires before asserting

	reqs := h.submitter.requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	req := reqs[0]
	if want := hbSessionID(h.agentID); req.SessionID != want {
		t.Fatalf("session id: got %q, want %q", req.SessionID, want)
	}
	if req.Origin != agents.OriginHeartbeat {
		t.Fatalf("origin: got %q, want %q", req.Origin, agents.OriginHeartbeat)
	}
	if req.WorkspaceID != h.wsID || req.AgentID != h.agentID {
		t.Fatalf("scoping: workspace %q agent %q", req.WorkspaceID, req.AgentID)
	}
	if req.UserID != h.creatorID {
		t.Fatalf("acting identity: got %q, want creator %q", req.UserID, h.creatorID)
	}
	if req.Input != heartbeatTurnPrompt {
		t.Fatalf("input: got %q, want the fixed tick prompt", req.Input)
	}
	if req.HeartbeatChecklist != hb.Prompt {
		t.Fatalf("checklist: got %q, want %q", req.HeartbeatChecklist, hb.Prompt)
	}
	if req.HeartbeatDigest != "" {
		t.Fatalf("digest with no workspace activity must be empty, got %q", req.HeartbeatDigest)
	}

	runs := h.runsFor(t, hb.ID)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run row, got %d", len(runs))
	}
	run := runs[0]
	if run.Status != domain.HeartbeatRunStatusCompleted {
		t.Fatalf("run status: got %q, want completed", run.Status)
	}
	if run.Trigger != domain.HeartbeatTriggerTick {
		t.Fatalf("trigger: got %q, want tick", run.Trigger)
	}
	if run.TokensUsed != 42 {
		t.Fatalf("tokens: got %d, want 42", run.TokensUsed)
	}
	if run.DurationMS < 0 {
		t.Fatalf("duration must be non-negative, got %d", run.DurationMS)
	}
	// Creator paired on nothing: transcript-only delivery, "" not an error.
	if run.DeliveryStatus != "" {
		t.Fatalf("unpaired creator delivery status: got %q, want empty", run.DeliveryStatus)
	}

	// Claim-time cadence advance (D14): the store moved next_tick_at; the
	// last_tick snapshot mirrored the outcome.
	stored := h.getHeartbeat(t)
	if stored.NextTickAt == nil || !stored.NextTickAt.After(h.clock.Now()) {
		t.Fatalf("next tick not advanced by the claim: %+v", stored.NextTickAt)
	}
	if stored.LastTick == nil || stored.LastTick.Status != domain.HeartbeatRunStatusCompleted ||
		stored.LastTick.SessionID != hbSessionID(h.agentID) {
		t.Fatalf("last_tick not mirrored: %+v", stored.LastTick)
	}
}

// TestSilentReplySuppressed: the whole-reply NO_REPLY contract (D7) — the
// tick completes as suppressed with zero delivery calls.
func TestSilentReplySuppressed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.seedGateway(t, domain.GatewayPlatformTelegram, true)
	h.seedUserLink(t, domain.GatewayPlatformTelegram, "12345")
	hb := h.seedHeartbeat(t, nil)
	h.submitter.reply(" no_RePly ") // whitespace-wrapped, mixed case

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusCompleted {
		t.Fatalf("run status: got %q, want completed", run.Status)
	}
	if run.DeliveryStatus != domain.HeartbeatDeliveryStatusSuppressed {
		t.Fatalf("delivery status: got %q, want suppressed", run.DeliveryStatus)
	}
	if entries := h.pendingOutbox(t); len(entries) != 0 {
		t.Fatalf("silent tick enqueued %d outbox entries", len(entries))
	}
	if posts := h.poster.posted(); len(posts) != 0 {
		t.Fatalf("silent tick posted %d channel messages", len(posts))
	}
}

// TestAReportIsNotSilence: prose containing the token is still a report —
// delivered, not suppressed (spec "A report is not silence").
func TestAReportIsNotSilence(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.seedGateway(t, domain.GatewayPlatformTelegram, true)
	h.seedUserLink(t, domain.GatewayPlatformTelegram, "12345")
	hb := h.seedHeartbeat(t, nil)
	h.submitter.reply("No reply needed, but note: disk 90% full")

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	run := h.runsFor(t, hb.ID)[0]
	if run.DeliveryStatus != domain.HeartbeatDeliveryStatusDelivered {
		t.Fatalf("delivery status: got %q, want delivered", run.DeliveryStatus)
	}
	if entries := h.pendingOutbox(t); len(entries) != 1 {
		t.Fatalf("report must enqueue exactly one entry, got %d", len(entries))
	}
}

// TestReportDeliveredToPairedGateways (D8): a creator paired on Telegram and
// WhatsApp gets one outbox entry per paired chat with the right gateway id,
// chat id, flavor, preview flag, body, and shared session.
func TestReportDeliveredToPairedGateways(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	tg := h.seedGateway(t, domain.GatewayPlatformTelegram, true)
	wa := h.seedGateway(t, domain.GatewayPlatformWhatsApp, true)
	h.seedUserLink(t, domain.GatewayPlatformTelegram, "12345")
	h.seedUserLink(t, domain.GatewayPlatformWhatsApp, "67890")
	// A pairing on a platform with no enabled gateway must be ignored.
	h.seedUserLink(t, "slack", "99999")
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusCompleted || run.DeliveryStatus != domain.HeartbeatDeliveryStatusDelivered {
		t.Fatalf("run = (%s, %s), want (completed, delivered)", run.Status, run.DeliveryStatus)
	}
	entries := h.pendingOutbox(t)
	if len(entries) != 2 {
		t.Fatalf("expected one entry per paired chat (2), got %d", len(entries))
	}
	byChat := make(map[string]gateways.OutboxPayload, len(entries))
	for _, e := range entries {
		if e.WorkspaceID != h.wsID {
			t.Fatalf("entry workspace: %q", e.WorkspaceID)
		}
		if e.SessionID != hbSessionID(h.agentID) {
			t.Fatalf("entry session: got %q, want the shared hb_ session", e.SessionID)
		}
		p := outboxPayload(t, e)
		if p.Body != "All done." || !p.DisablePreview {
			t.Fatalf("payload body/preview: %+v", p)
		}
		byChat[p.ChatID] = p
	}
	tgPayload, ok := byChat["12345"]
	if !ok || tgPayload.GatewayID != tg.ID || tgPayload.Flavor != gateways.FlavorTelegramHTML {
		t.Fatalf("telegram entry wrong: %+v (have %v)", tgPayload, byChat)
	}
	waPayload, ok := byChat["67890"]
	if !ok || waPayload.GatewayID != wa.ID || waPayload.Flavor != gateways.FlavorWhatsAppMD {
		t.Fatalf("whatsapp entry wrong: %+v (have %v)", waPayload, byChat)
	}
}

// TestDisabledGatewayExcludesItsChats: a paired chat on a disabled gateway
// is not a delivery target (D8 "enabled gateways").
func TestDisabledGatewayExcludesItsChats(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.seedGateway(t, domain.GatewayPlatformTelegram, true)
	h.seedGateway(t, domain.GatewayPlatformWhatsApp, false)
	h.seedUserLink(t, domain.GatewayPlatformTelegram, "12345")
	h.seedUserLink(t, domain.GatewayPlatformWhatsApp, "67890")
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	run := h.runsFor(t, hb.ID)[0]
	if run.DeliveryStatus != domain.HeartbeatDeliveryStatusDelivered {
		t.Fatalf("delivery status: got %q, want delivered", run.DeliveryStatus)
	}
	entries := h.pendingOutbox(t)
	if len(entries) != 1 || outboxPayload(t, entries[0]).ChatID != "12345" {
		t.Fatalf("expected only the telegram chat, got %d entries", len(entries))
	}
}

// TestOutboxEnqueueFailureKeepsTickCompleted: a failing enqueue marks the
// delivery failed with the error recorded; the tick itself stays completed
// (D8: delivery failure never fails the run).
func TestOutboxEnqueueFailureKeepsTickCompleted(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.seedGateway(t, domain.GatewayPlatformTelegram, true)
	h.seedUserLink(t, domain.GatewayPlatformTelegram, "12345")
	h.outboxPort = outboxErrStore{}
	h.rewire(t, h.st.Agents())
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusCompleted {
		t.Fatalf("delivery failure must not fail the tick, got %q", run.Status)
	}
	if run.DeliveryStatus != domain.HeartbeatDeliveryStatusFailed {
		t.Fatalf("delivery status: got %q, want failed", run.DeliveryStatus)
	}
	if !strings.Contains(run.Error, "outbox write failed") {
		t.Fatalf("run error should carry the delivery failure, got %q", run.Error)
	}
}

// TestChannelDeliveryAndFailureKeepsTickCompleted: channel delivery posts
// through the chokepoint exactly like scheduler channel delivery, and a
// failing post stays completed with delivery_status failed (D8).
func TestChannelDeliveryAndFailureKeepsTickCompleted(t *testing.T) {
	cases := []struct {
		name        string
		posterFails bool
	}{
		{"delivered", false},
		{"delivery failed", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			if tc.posterFails {
				h.poster.err = errors.New("agent is no longer a channel member")
			}
			hb := h.seedHeartbeat(t, func(hb *domain.Heartbeat) {
				hb.Delivery = domain.HeartbeatDelivery{Type: domain.HeartbeatDeliveryChannel, ChannelID: "chan-ops"}
			})

			h.svc.tickOnce(ctx)
			h.svc.Stop()

			run := h.runsFor(t, hb.ID)[0]
			if run.Status != domain.HeartbeatRunStatusCompleted {
				t.Fatalf("run status: got %q, want completed", run.Status)
			}
			posts := h.poster.posted()
			if len(posts) != 1 {
				t.Fatalf("poster attempts: got %d, want 1", len(posts))
			}
			if tc.posterFails {
				if run.DeliveryStatus != domain.HeartbeatDeliveryStatusFailed {
					t.Fatalf("delivery status: got %q, want failed", run.DeliveryStatus)
				}
				if !strings.Contains(run.Error, "channel delivery failed") {
					t.Fatalf("run error should name the failure, got %q", run.Error)
				}
			} else {
				if run.DeliveryStatus != domain.HeartbeatDeliveryStatusDelivered {
					t.Fatalf("delivery status: got %q, want delivered", run.DeliveryStatus)
				}
			}
		})
	}
}

// TestEmptyChecklistSkipsFree (D3): a whitespace checklist skips with zero
// model calls, a skipped row carrying the reason, and the cadence advanced
// by the claim — the streak untouched.
func TestEmptyChecklistSkipsFree(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, func(hb *domain.Heartbeat) { hb.Prompt = "   \n\t " })

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("empty checklist must not reach the model, got %d submits", len(got))
	}
	runs := h.runsFor(t, hb.ID)
	if len(runs) != 1 {
		t.Fatalf("expected 1 skipped row, got %d", len(runs))
	}
	run := runs[0]
	if run.Status != domain.HeartbeatRunStatusSkipped || run.Error != skipReasonEmptyChecklist {
		t.Fatalf("run = (%s, %q), want (skipped, %q)", run.Status, run.Error, skipReasonEmptyChecklist)
	}
	if run.TokensUsed != 0 || run.DeliveryStatus != "" {
		t.Fatalf("skipped row must be free: tokens=%d delivery=%q", run.TokensUsed, run.DeliveryStatus)
	}
	stored := h.getHeartbeat(t)
	if stored.NextTickAt == nil || !stored.NextTickAt.After(h.clock.Now()) {
		t.Fatalf("skipped tick must advance the cadence, next=%v", stored.NextTickAt)
	}
	if stored.FailureStreak != 0 {
		t.Fatalf("skipped tick must not touch the streak, got %d", stored.FailureStreak)
	}
}

// TestBusyAgentTickDefers (D12): an agent-level busy signal — any live run,
// faked here through the narrow interface — records a skipped busy row with
// no model call and leaves the cadence to the claim's advance.
func TestBusyAgentTickDefers(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.busy.set(h.wsID, h.agentID, true)
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("busy tick must not reach the model, got %d submits", len(got))
	}
	runs := h.runsFor(t, hb.ID)
	if len(runs) != 1 || runs[0].Status != domain.HeartbeatRunStatusSkipped || runs[0].Error != skipReasonBusy {
		t.Fatalf("expected one skipped-busy row, got %+v", runs)
	}
	if stored := h.getHeartbeat(t); stored.NextTickAt == nil || !stored.NextTickAt.After(h.clock.Now()) {
		t.Fatalf("busy defer must keep the cadence, next=%v", stored.NextTickAt)
	}
}

// TestInFlightSecondClaimSkipsBusy: while a fire is still draining, the next
// due claim is skipped busy — the occurrence is not queued and not lost
// beyond this firing (the claim already advanced next_tick_at).
func TestInFlightSecondClaimSkipsBusy(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)

	hold := make(chan struct{})
	h.submitter.setHold(hold)
	h.svc.tickOnce(ctx)
	waitFor(t, "first submit", func() bool { return len(h.submitter.requests()) == 1 })

	// The next occurrence comes due while the first fire is still live.
	h.clock.Set(baseTime.Add(16 * time.Minute))
	h.svc.tickOnce(ctx)
	waitFor(t, "the busy skip row", func() bool {
		runs := h.runsFor(t, hb.ID)
		return len(runs) == 2 && runs[0].Status == domain.HeartbeatRunStatusSkipped &&
			runs[0].Error == skipReasonBusy
	})

	close(hold)
	h.submitter.setHold(nil)
	h.svc.Stop()

	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("in-flight claim must not re-fire, got %d submits", got)
	}
	runs := h.runsFor(t, hb.ID)
	if len(runs) != 2 || runs[1].Status != domain.HeartbeatRunStatusCompleted {
		t.Fatalf("expected one completed + one skipped row, got %+v", runs)
	}
}

// TestActiveHoursWindow (D5): the window is evaluated in the workspace
// timezone; start is inclusive, end exclusive, and a start>end window wraps
// midnight. Skipped ticks burn no tokens and still advance the cadence.
func TestActiveHoursWindow(t *testing.T) {
	cases := []struct {
		name      string
		tz        string
		start     string
		end       string
		at        time.Time
		wantFires bool
	}{
		{"no window is 24/7", "", "", "", baseTime, true},
		{"inside window at start", "", "09:00", "17:00", baseTime, true},
		{"inside window midday", "", "09:00", "17:00", time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), true},
		{"outside window at night", "", "09:00", "17:00", time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC), false},
		{"outside window at end boundary", "", "09:00", "17:00", time.Date(2026, 9, 10, 17, 0, 0, 0, time.UTC), false},
		{"wrap active late evening", "", "22:00", "06:00", time.Date(2026, 9, 10, 23, 0, 0, 0, time.UTC), true},
		{"wrap active early morning", "", "22:00", "06:00", time.Date(2026, 9, 10, 5, 0, 0, 0, time.UTC), true},
		{"wrap outside midday", "", "22:00", "06:00", baseTime, false},
		{"workspace tz inside", "Asia/Tokyo", "09:00", "17:00", time.Date(2026, 9, 10, 1, 30, 0, 0, time.UTC), true},
		{"workspace tz outside", "Asia/Tokyo", "09:00", "17:00", time.Date(2026, 9, 9, 23, 30, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			if tc.tz != "" {
				h.setWorkspaceTimezone(t, tc.tz)
			}
			h.clock.Set(tc.at)
			hb := h.seedHeartbeatDue(t, func(hb *domain.Heartbeat) {
				if tc.start != "" {
					start, end := tc.start, tc.end
					hb.ActiveStart = &start
					hb.ActiveEnd = &end
				}
			}, tc.at.Add(-time.Minute))

			h.svc.tickOnce(ctx)
			h.svc.Stop()

			runs := h.runsFor(t, hb.ID)
			if tc.wantFires {
				if got := len(h.submitter.requests()); got != 1 {
					t.Fatalf("expected the tick to fire, got %d submits (%+v)", got, runs)
				}
				return
			}
			if got := h.submitter.requests(); len(got) != 0 {
				t.Fatalf("outside-active-hours tick must not reach the model, got %d submits", len(got))
			}
			if len(runs) != 1 || runs[0].Status != domain.HeartbeatRunStatusSkipped || runs[0].Error != skipReasonOutsideHours {
				t.Fatalf("expected one skipped outside-active-hours row, got %+v", runs)
			}
			if stored := h.getHeartbeat(t); stored.NextTickAt == nil || !stored.NextTickAt.After(tc.at) {
				t.Fatalf("skipped tick must advance the cadence, next=%v", stored.NextTickAt)
			}
		})
	}
}

// TestDisabledCreatorBlocksAndAutoPauses (D6): a disabled creator blocks the
// tick with no model call and auto-pauses the heartbeat.
func TestDisabledCreatorBlocksAndAutoPauses(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)
	disabledAt := baseTime
	if err := h.st.Users().SetDisabled(ctx, h.creatorID, &disabledAt); err != nil {
		t.Fatalf("disable creator: %v", err)
	}

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("disabled creator must not reach the model, got %d submits", len(got))
	}
	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	stored := h.getHeartbeat(t)
	if stored.Enabled || stored.NextTickAt != nil {
		t.Fatalf("heartbeat not auto-paused: enabled=%v next=%v", stored.Enabled, stored.NextTickAt)
	}
	if stored.FailureStreak != 0 {
		t.Fatalf("blocked tick must not touch the streak, got %d", stored.FailureStreak)
	}
}

// TestNilCreatorBlocksAndAutoPauses: created_by is ON DELETE SET NULL, so a
// heartbeat whose creator is gone resolves to no acting identity — blocked
// and auto-paused, never silently privileged (D6).
func TestNilCreatorBlocksAndAutoPauses(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, func(hb *domain.Heartbeat) { hb.CreatedBy = nil })

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("nil creator must not reach the model, got %d submits", len(got))
	}
	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	if stored := h.getHeartbeat(t); stored.Enabled || stored.NextTickAt != nil {
		t.Fatalf("heartbeat not auto-paused: enabled=%v next=%v", stored.Enabled, stored.NextTickAt)
	}
}

// TestMissingAgentBlocksNoPause: the bound agent vanished — the tick blocks
// cheaply but the heartbeat is NOT paused (agent grief is not creator
// grief), and the cadence stays scheduled.
func TestMissingAgentBlocksNoPause(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)
	h.rewire(t, agentGoneStore{h.st.Agents()})

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("missing agent must not reach the model, got %d submits", len(got))
	}
	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	stored := h.getHeartbeat(t)
	if !stored.Enabled || stored.NextTickAt == nil || !stored.NextTickAt.After(h.clock.Now()) {
		t.Fatalf("missing agent must not pause the heartbeat: %+v", stored)
	}
}

// TestSubmitErrorBlockedNoPause: a synchronous submit failure (pre-model
// config grief) records a blocked run without pausing the heartbeat.
func TestSubmitErrorBlockedNoPause(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.submitter.err = errors.New("provider unreachable")
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("expected the submit attempt to be recorded, got %d", got)
	}
	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	if !strings.Contains(run.Error, "submit failed") {
		t.Fatalf("run error should carry the submit failure, got %q", run.Error)
	}
	stored := h.getHeartbeat(t)
	if !stored.Enabled || stored.NextTickAt == nil {
		t.Fatalf("submit failure must not pause the heartbeat: %+v", stored)
	}
}

// TestHookPromptBlockedNotFailed (D11): a user_prompt_submit block ends the
// tick as blocked — never failed — so the streak must not move.
func TestHookPromptBlockedNotFailed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.submitter.script = func(agents.ExecRequest) []agents.TranscriptEvent {
		return []agents.TranscriptEvent{
			{Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
			{Kind: agents.TranscriptEventPromptBlocked, TurnID: "turn-1",
				PromptBlocked: &agents.PromptBlockedPayload{Hook: "gatekeeper", Reason: "ambient ticks are off-policy"}},
			{Kind: agents.TranscriptEventTurnCompleted, TurnID: "turn-1"},
		}
	}
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	run := h.runsFor(t, hb.ID)[0]
	if run.Status != domain.HeartbeatRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	if !strings.Contains(run.Error, "gatekeeper") || !strings.Contains(run.Error, "ambient ticks are off-policy") {
		t.Fatalf("run error should carry hook and reason, got %q", run.Error)
	}
	stored := h.getHeartbeat(t)
	if !stored.Enabled {
		t.Fatalf("hook block must not pause the heartbeat: %+v", stored)
	}
	if stored.FailureStreak != 0 {
		t.Fatalf("hook block must not increment the streak, got %d", stored.FailureStreak)
	}
}

// TestFailureStreakAutoPausesAtFive (D12): five consecutive failed ticks
// disable the heartbeat and clear next_tick_at.
func TestFailureStreakAutoPausesAtFive(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.submitter.failTurn("provider exploded")
	hb := h.seedHeartbeat(t, nil)

	for i := 0; i < 5; i++ {
		h.svc.tickOnce(ctx)
		h.svc.Stop()
		if i < 4 {
			h.clock.Set(h.clock.Now().Add(16 * time.Minute))
		}
	}

	stored := h.getHeartbeat(t)
	if stored.Enabled || stored.NextTickAt != nil {
		t.Fatalf("five failures must auto-pause: enabled=%v next=%v", stored.Enabled, stored.NextTickAt)
	}
	if stored.FailureStreak != 5 {
		t.Fatalf("streak: got %d, want 5", stored.FailureStreak)
	}
	if runs := h.runsFor(t, hb.ID); len(runs) != 5 || runs[0].Status != domain.HeartbeatRunStatusFailed {
		t.Fatalf("expected five failed rows, got %+v", runs)
	}
}

// TestBlockedAndSkippedLeaveStreakUntouched (D12): only failed ticks move
// the streak — skipped guards and blocked policy grief do not.
func TestBlockedAndSkippedLeaveStreakUntouched(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.submitter.failTurn("provider exploded")
	hb := h.seedHeartbeat(t, nil)

	// Two failed ticks: streak 2.
	for i := 0; i < 2; i++ {
		h.svc.tickOnce(ctx)
		h.svc.Stop()
		h.clock.Set(h.clock.Now().Add(16 * time.Minute))
	}

	// A busy skip: streak stays 2.
	h.busy.set(h.wsID, h.agentID, true)
	h.svc.tickOnce(ctx)
	h.svc.Stop()
	h.busy.set(h.wsID, h.agentID, false)
	h.clock.Set(h.clock.Now().Add(16 * time.Minute))

	// A disabled creator blocks AND auto-pauses: streak stays 2.
	if err := h.st.Users().SetDisabled(ctx, h.creatorID, &baseTime); err != nil {
		t.Fatalf("disable creator: %v", err)
	}
	h.svc.tickOnce(ctx)
	h.svc.Stop()

	stored := h.getHeartbeat(t)
	if stored.FailureStreak != 2 {
		t.Fatalf("streak: got %d, want 2 (blocked and skipped must not move it)", stored.FailureStreak)
	}
	if stored.Enabled || stored.NextTickAt != nil {
		t.Fatalf("the blocked tick must have auto-paused: %+v", stored)
	}
	if runs := h.runsFor(t, hb.ID); len(runs) != 4 {
		t.Fatalf("expected four run rows, got %d", len(runs))
	}
}

// TestRunNowOnPausedHeartbeat: run-now works while disabled without
// re-enabling or advancing the cadence, records trigger manual, and rides
// the shared hb_ session (spec "Run-now on a paused heartbeat").
func TestRunNowOnPausedHeartbeat(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)
	h.pauseSeed(t, hb)

	run, err := h.svc.RunNow(ctx, h.wsID, h.agentID)
	if err != nil {
		t.Fatalf("run now: %v", err)
	}
	if run == nil || run.Status != domain.HeartbeatRunStatusRunning {
		t.Fatalf("returned run: %+v, want running", run)
	}
	if run.Trigger != domain.HeartbeatTriggerManual {
		t.Fatalf("trigger: got %q, want manual", run.Trigger)
	}
	if run.SessionID != hbSessionID(h.agentID) {
		t.Fatalf("session id shape: %q", run.SessionID)
	}

	waitFor(t, "run to complete", func() bool {
		runs := h.runsFor(t, hb.ID)
		return len(runs) == 1 && runs[0].Status == domain.HeartbeatRunStatusCompleted
	})

	stored := h.getHeartbeat(t)
	if stored.Enabled {
		t.Fatalf("run-now must not re-enable a paused heartbeat: %+v", stored)
	}
	if stored.NextTickAt != nil {
		t.Fatalf("run-now must not touch next_tick_at, got %v", stored.NextTickAt)
	}
	if stored.LastTick == nil || stored.LastTick.Trigger != domain.HeartbeatTriggerManual {
		t.Fatalf("last_tick trigger: %+v", stored.LastTick)
	}
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("expected one manual submit, got %d", got)
	}
}

// TestRunNowGuardSkipReturnsFinishedRow: a manual tick through a cheap guard
// returns the already-finished skipped row instead of a stream (the
// scheduler's "already-finished row" return shape), and never claims.
func TestRunNowGuardSkipReturnsFinishedRow(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	due := baseTime.Add(-time.Minute)
	hb := h.seedHeartbeatDue(t, func(hb *domain.Heartbeat) { hb.Prompt = " \n " }, due)
	_ = hb

	run, err := h.svc.RunNow(ctx, h.wsID, h.agentID)
	if err != nil {
		t.Fatalf("run now: %v", err)
	}
	if run == nil || run.Status != domain.HeartbeatRunStatusSkipped || run.Error != skipReasonEmptyChecklist {
		t.Fatalf("run = %+v, want the finished skipped row", run)
	}
	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("guard skip must not reach the model, got %d submits", len(got))
	}
	if stored := h.getHeartbeat(t); stored.NextTickAt == nil || !stored.NextTickAt.Equal(due) {
		t.Fatalf("run-now must not advance the cadence, next=%v", stored.NextTickAt)
	}
}

// TestRunNowConflictInFlight: a second run-now while the first is live
// conflicts; after the terminal outcome a new run is accepted.
func TestRunNowConflictInFlight(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)
	h.pauseSeed(t, hb)

	hold := make(chan struct{})
	h.submitter.setHold(hold)
	if _, err := h.svc.RunNow(ctx, h.wsID, h.agentID); err != nil {
		t.Fatalf("first run now: %v", err)
	}
	_, err := h.svc.RunNow(ctx, h.wsID, h.agentID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("in-flight run now: got %v, want ErrConflict", err)
	}

	close(hold)
	h.submitter.setHold(nil)
	waitFor(t, "run to complete", func() bool {
		runs := h.runsFor(t, hb.ID)
		return len(runs) == 1 && runs[0].Status == domain.HeartbeatRunStatusCompleted
	})

	if _, err := h.svc.RunNow(ctx, h.wsID, h.agentID); err != nil {
		t.Fatalf("run now after terminal outcome: %v", err)
	}
	waitFor(t, "second run to complete", func() bool {
		return len(h.runsFor(t, hb.ID)) == 2
	})
	h.svc.Stop()
}

// TestRunNowUnknownHeartbeat: an agent without a heartbeat — including in a
// foreign workspace — is ErrNotFound wrapping the domain sentinel.
func TestRunNowUnknownHeartbeat(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	_, err := h.svc.RunNow(ctx, h.wsID, h.agentID)
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown heartbeat: got %v, want ErrNotFound wrapping domain.ErrNotFound", err)
	}
	hb := h.seedHeartbeat(t, nil)
	_, err = h.svc.RunNow(ctx, "ws-other", h.agentID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign workspace: got %v, want ErrNotFound", err)
	}
	_ = hb
}

// TestResumeRecomputesAndResetsStreak (D12): the paused banner's recovery —
// re-enabled, next_tick_at recomputed in the workspace timezone from now,
// streak reset.
func TestResumeRecomputesAndResetsStreak(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, func(hb *domain.Heartbeat) {
		hb.Enabled = false
		hb.FailureStreak = 3
	})
	h.pauseSeed(t, hb)
	h.clock.Set(baseTime.Add(time.Hour))

	got, err := h.svc.Resume(ctx, h.wsID, h.agentID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !got.Enabled {
		t.Fatal("resume must enable the heartbeat")
	}
	if got.FailureStreak != 0 {
		t.Fatalf("resume must reset the streak, got %d", got.FailureStreak)
	}
	if got.NextTickAt == nil || !got.NextTickAt.After(h.clock.Now()) {
		t.Fatalf("resume must recompute next_tick_at, got %v", got.NextTickAt)
	}
	stored := h.getHeartbeat(t)
	if !stored.Enabled || stored.FailureStreak != 0 || stored.NextTickAt == nil {
		t.Fatalf("resumed state not persisted: %+v", stored)
	}
}

// TestResumeOnEnabledIsHarmless: resume on an already-enabled heartbeat is
// idempotent-friendly — recompute + reset changes nothing that matters.
func TestResumeOnEnabledIsHarmless(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)

	got, err := h.svc.Resume(ctx, h.wsID, h.agentID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !got.Enabled || got.FailureStreak != 0 || got.NextTickAt == nil || !got.NextTickAt.After(h.clock.Now()) {
		t.Fatalf("resume on enabled heartbeat must be harmless, got %+v", got)
	}
	_ = hb
}

// TestResumeUnknownHeartbeat: resume addresses an agent's heartbeat; absence
// is ErrNotFound wrapping the domain sentinel.
func TestResumeUnknownHeartbeat(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	_, err := h.svc.Resume(ctx, h.wsID, h.agentID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("resume without heartbeat: got %v, want ErrNotFound", err)
	}
}

// TestClaimAdvanceStaysStoreSide: the service never re-advances the cadence
// — the claim's atomic advance means a second pass at the same clock claims
// nothing.
func TestClaimAdvanceStaysStoreSide(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	hb := h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop()
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("expected 1 submit after first tick, got %d", got)
	}

	h.svc.tickOnce(ctx)
	h.svc.Stop()
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("second pass replayed the tick: %d submits", got)
	}
	if runs := h.runsFor(t, hb.ID); len(runs) != 1 {
		t.Fatalf("expected exactly 1 run, got %d", len(runs))
	}
}

// TestDigestComposedSinceLastTick (D9): the first tick digests since
// creation (empty here); the second digests workspace activity that happened
// after the previous tick started.
func TestDigestComposedSinceLastTick(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.seedHeartbeat(t, nil)

	h.svc.tickOnce(ctx)
	h.svc.Stop() // tick 1 completed; last_tick.start = baseTime

	// Workspace activity after the first tick started (CreatedAt after the
	// heartbeat's real-now creation, hence after baseTime too).
	ch := &domain.Channel{WorkspaceID: h.wsID, Name: "Ops", Slug: "ops"}
	if err := h.st.Channels().CreateChannel(ctx, ch); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	msg := &domain.ChannelMessage{
		WorkspaceID:   h.wsID,
		ChannelID:     ch.ID,
		AuthorType:    domain.ChannelMemberTypeAgent,
		AuthorAgentID: h.agentID,
		Body:          "DIGEST-MARKER disk alarm cleared",
		CreatedAt:     time.Now().Add(time.Minute),
	}
	if err := h.st.Channels().InsertChannelMessage(ctx, msg); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	h.clock.Set(baseTime.Add(16 * time.Minute))
	h.svc.tickOnce(ctx)
	h.svc.Stop()

	reqs := h.submitter.requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 submits, got %d", len(reqs))
	}
	if reqs[0].HeartbeatDigest != "" {
		t.Fatalf("first tick digest must be empty, got %q", reqs[0].HeartbeatDigest)
	}
	digest := reqs[1].HeartbeatDigest
	if !strings.Contains(digest, "#ops") || !strings.Contains(digest, "Atlas (agent)") ||
		!strings.Contains(digest, "DIGEST-MARKER disk alarm cleared") {
		t.Fatalf("second tick digest must carry the channel preview, got %q", digest)
	}
}

// TestTickerLoopDispatches: the real Start/Stop lifecycle claims and fires
// due heartbeats without a manual tick.
func TestTickerLoopDispatches(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithTick(2*time.Millisecond))
	hb := h.seedHeartbeat(t, nil)

	h.svc.Start(ctx)
	waitFor(t, "the loop to fire the due heartbeat", func() bool {
		return len(h.submitter.requests()) == 1
	})
	h.svc.Stop()

	runs := h.runsFor(t, hb.ID)
	if len(runs) != 1 || runs[0].Status != domain.HeartbeatRunStatusCompleted {
		t.Fatalf("loop fire did not complete a tick: %+v", runs)
	}
}
