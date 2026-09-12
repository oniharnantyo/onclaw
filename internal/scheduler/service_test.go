package scheduler

import (
	"context"
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
// mirroring the channels package's fake runner. The default script is a
// successful turn replying "All done." with 42 tokens of usage.
type fakeSubmitter struct {
	mu     sync.Mutex
	reqs   []agents.ExecRequest
	err    error
	script func(req agents.ExecRequest) []agents.TranscriptEvent
	// hold, when non-nil, keeps every stream open until one token is
	// consumed — in-flight and concurrency tests gate on it.
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

// harness seeds the full scheduler fixture (creator, workspace, provider,
// agent) and a service over the fake store with an injected clock.
type harness struct {
	st        store.Store
	submitter *fakeSubmitter
	poster    *fakePoster
	clock     *fakeClock
	svc       *Service
	wsID      string
	agentID   string
	creatorID string
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
		clock:     newClock(baseTime),
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
	h.svc = NewService(h.st.Schedulers(), h.st.Users(), agentsStore, h.submitter, h.poster, discardLogger(), opts...)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedOnce materializes a due enabled one-shot at runAt (which may lie in
// the past relative to the injected clock): the store recomputes schedule
// fields from real now on every write, so the row is created disabled with a
// future instant and then updated with the backdated one — the store's own
// test-fixture trick.
func (h *harness) seedOnce(t *testing.T, name string, runAt time.Time, delivery domain.SchedulerDelivery) *domain.Scheduler {
	t.Helper()
	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)
	s := &domain.Scheduler{
		WorkspaceID: h.wsID,
		AgentID:     h.agentID,
		CreatedBy:   &h.creatorID,
		Name:        name,
		Prompt:      "Do the thing",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &future,
		Enabled:     false,
		Delivery:    delivery,
	}
	if err := h.st.Schedulers().CreateScheduler(ctx, h.wsID, s); err != nil {
		t.Fatalf("create once %q: %v", name, err)
	}
	s.Enabled = true
	s.RunAt = &runAt
	if err := h.st.Schedulers().UpdateScheduler(ctx, h.wsID, s); err != nil {
		t.Fatalf("backdate once %q: %v", name, err)
	}
	return s
}

// seedRecurring materializes an enabled recurring scheduler. next_run_at is
// computed from real now; tests make it due by advancing the injected clock
// past it (also the overdue-restart shape).
func (h *harness) seedRecurring(t *testing.T, name, expr string, delivery domain.SchedulerDelivery) *domain.Scheduler {
	t.Helper()
	ctx := context.Background()
	s := &domain.Scheduler{
		WorkspaceID: h.wsID,
		AgentID:     h.agentID,
		CreatedBy:   &h.creatorID,
		Name:        name,
		Prompt:      "Do the thing",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        expr,
		Enabled:     true,
		Delivery:    delivery,
	}
	if err := h.st.Schedulers().CreateScheduler(ctx, h.wsID, s); err != nil {
		t.Fatalf("create recurring %q: %v", name, err)
	}
	return s
}

// seedPausedOnce creates a disabled one-shot (valid future instant) and
// leaves it paused — the run-now-while-paused fixture.
func (h *harness) seedPausedOnce(t *testing.T, name string, delivery domain.SchedulerDelivery) *domain.Scheduler {
	t.Helper()
	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)
	s := &domain.Scheduler{
		WorkspaceID: h.wsID,
		AgentID:     h.agentID,
		CreatedBy:   &h.creatorID,
		Name:        name,
		Prompt:      "Do the thing",
		Kind:        domain.SchedulerKindOnce,
		RunAt:       &future,
		Enabled:     false,
		Delivery:    delivery,
	}
	if err := h.st.Schedulers().CreateScheduler(ctx, h.wsID, s); err != nil {
		t.Fatalf("create paused once %q: %v", name, err)
	}
	return s
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

func (h *harness) runsFor(t *testing.T, schedulerID string) []domain.SchedulerRun {
	t.Helper()
	runs, _, err := h.st.Schedulers().ListSchedulerRuns(context.Background(), h.wsID, schedulerID, 100, 0)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	return runs
}

func channelDelivery() domain.SchedulerDelivery {
	return domain.SchedulerDelivery{Type: domain.SchedulerDeliveryChannel, ChannelID: "chan-ops"}
}

// TestDueDispatchFiresThreadTarget covers the claim → ExecRequest contract:
// origin, session id shape, acting identity, prompt, and the per-target
// SchedulerNoReply selection — then the recorded outcome.
func TestDueDispatchFiresThreadTarget(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedOnce(t, "remind", baseTime.Add(-time.Minute), domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})

	h.svc.tickOnce(ctx)
	h.svc.Stop() // join all fires before asserting

	reqs := h.submitter.requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	req := reqs[0]
	wantSession := fmt.Sprintf("sched_%s_%d", sched.ID, baseTime.Unix())
	if req.SessionID != wantSession {
		t.Fatalf("session id: got %q, want %q", req.SessionID, wantSession)
	}
	if req.Origin != agents.OriginScheduler {
		t.Fatalf("origin: got %q, want %q", req.Origin, agents.OriginScheduler)
	}
	if req.WorkspaceID != h.wsID || req.AgentID != h.agentID {
		t.Fatalf("scoping: workspace %q agent %q", req.WorkspaceID, req.AgentID)
	}
	if req.UserID != h.creatorID {
		t.Fatalf("acting identity: got %q, want creator %q", req.UserID, h.creatorID)
	}
	if req.Input != "Do the thing" {
		t.Fatalf("input: got %q", req.Input)
	}
	if req.SchedulerNoReply != "" {
		t.Fatalf("thread target must not carry the token, got %q", req.SchedulerNoReply)
	}

	runs := h.runsFor(t, sched.ID)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run row, got %d", len(runs))
	}
	run := runs[0]
	if run.Status != domain.SchedulerRunStatusCompleted {
		t.Fatalf("run status: got %q, want completed", run.Status)
	}
	if run.Trigger != domain.SchedulerTriggerScheduled {
		t.Fatalf("trigger: got %q, want scheduler", run.Trigger)
	}
	if run.SessionID != wantSession {
		t.Fatalf("run session: got %q, want %q", run.SessionID, wantSession)
	}
	if run.TokensUsed != 42 {
		t.Fatalf("tokens: got %d, want 42", run.TokensUsed)
	}
	if run.DurationMS < 0 {
		t.Fatalf("duration must be non-negative, got %d", run.DurationMS)
	}
	if run.DeliveryStatus != "" {
		t.Fatalf("thread target delivery status: got %q, want empty", run.DeliveryStatus)
	}

	got, err := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if err != nil || got == nil {
		t.Fatalf("get scheduler: %v, %v", got, err)
	}
	if got.LastRun == nil || got.LastRun.Status != domain.SchedulerRunStatusCompleted || got.LastRun.SessionID != wantSession {
		t.Fatalf("last_run not mirrored: %+v", got.LastRun)
	}
}

// TestPausedRowsNeverClaimed: the loop claims nothing when only paused rows
// exist — no submits, no run rows.
func TestPausedRowsNeverClaimed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedOnce(t, "paused", baseTime.Add(-time.Minute), domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	sched.Enabled = false
	if err := h.st.Schedulers().UpdateScheduler(ctx, h.wsID, sched); err != nil {
		t.Fatalf("pause scheduler: %v", err)
	}

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("paused row submitted %d runs", len(got))
	}
	if runs := h.runsFor(t, sched.ID); len(runs) != 0 {
		t.Fatalf("paused row recorded %d runs", len(runs))
	}
}

// TestOnceArchivesAfterFire: after the fire the one-shot is archived and the
// next tick never resubmits it.
func TestOnceArchivesAfterFire(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedOnce(t, "once", baseTime.Add(-time.Minute), domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})

	h.svc.tickOnce(ctx)
	h.svc.Stop()
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("expected 1 submit after first tick, got %d", got)
	}

	h.svc.tickOnce(ctx)
	h.svc.Stop()
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("archived once resubmitted on second tick (%d submits)", got)
	}

	got, err := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if err != nil || got == nil {
		t.Fatalf("get scheduler: %v, %v", got, err)
	}
	if got.Enabled || got.NextRunAt != nil {
		t.Fatalf("once not archived after fire: enabled=%v next=%v", got.Enabled, got.NextRunAt)
	}
}

// TestMissedGraceRecordsMissedRun: a once scheduler overdue beyond the store
// grace window is claimed with Missed=true — no execution, but a missed run
// row so the runs listing shows the outcome.
func TestMissedGraceRecordsMissedRun(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedOnce(t, "stale", baseTime.Add(-2*time.Hour), domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("missed claim submitted %d runs", len(got))
	}
	runs := h.runsFor(t, sched.ID)
	if len(runs) != 1 {
		t.Fatalf("expected 1 missed run row, got %d", len(runs))
	}
	run := runs[0]
	if run.Status != domain.SchedulerRunStatusMissed {
		t.Fatalf("status: got %q, want missed", run.Status)
	}
	if run.Trigger != domain.SchedulerTriggerScheduled {
		t.Fatalf("trigger: got %q, want scheduler", run.Trigger)
	}
	if run.SessionID != missedSessionID(sched.ID) {
		t.Fatalf("session: got %q, want %q", run.SessionID, missedSessionID(sched.ID))
	}
	if run.DurationMS != 0 || run.TokensUsed != 0 {
		t.Fatalf("missed run must be free: duration=%d tokens=%d", run.DurationMS, run.TokensUsed)
	}
	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if got == nil || got.Enabled || got.NextRunAt != nil || got.LastRun == nil || got.LastRun.Status != domain.SchedulerRunStatusMissed {
		t.Fatalf("missed scheduler not archived: %+v", got)
	}
}

// TestRestartCatchUpFiresOnce: an overdue recurring row (server down past
// its fire time) fires exactly once and its next occurrence lands in the
// future — no replay storm on subsequent ticks.
func TestRestartCatchUpFiresOnce(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedRecurring(t, "nightly", "* * * * *", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	// The server was "down" for 3 hours: the injected clock jumps past the
	// stored next_run_at.
	h.clock.Set(time.Now().UTC().Add(3 * time.Hour))

	h.svc.tickOnce(ctx)
	h.svc.Stop()
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("catch-up must fire once, got %d submits", got)
	}

	// The claim already advanced next_run_at: the same clock reading claims
	// nothing more.
	h.svc.tickOnce(ctx)
	h.svc.Stop()
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("overdue recurring replayed: %d submits", got)
	}
	if runs := h.runsFor(t, sched.ID); len(runs) != 1 {
		t.Fatalf("expected exactly 1 run, got %d", len(runs))
	}
	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if got == nil || got.NextRunAt == nil || !got.NextRunAt.After(h.clock.Now()) {
		t.Fatalf("next occurrence not in the future: %+v", got)
	}
}

// TestNoReplyMatchingTable: whole-reply, case-insensitive NO_REPLY suppresses
// channel delivery; anything else posts. Thread targets never post.
func TestNoReplyMatchingTable(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		delivery  domain.SchedulerDelivery
		want      string
		wantPosts int
	}{
		{"exact", "NO_REPLY", channelDelivery(), domain.SchedulerDeliverySuppressed, 0},
		{"whitespace-wrapped", " no_reply ", channelDelivery(), domain.SchedulerDeliverySuppressed, 0},
		{"mixed-case", "No_RePly", channelDelivery(), domain.SchedulerDeliverySuppressed, 0},
		{"with-punctuation", "NO_REPLY!", channelDelivery(), domain.SchedulerDeliveryDelivered, 1},
		{"prose", "nothing to report", channelDelivery(), domain.SchedulerDeliveryDelivered, 1},
		{"token-prefixed-prose", "NO_REPLY now", channelDelivery(), domain.SchedulerDeliveryDelivered, 1},
		{"thread-target-token", "NO_REPLY", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread}, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			h.seedOnce(t, "digest", baseTime.Add(-time.Minute), tc.delivery)
			reply := tc.reply
			h.submitter.script = func(agents.ExecRequest) []agents.TranscriptEvent {
				return []agents.TranscriptEvent{
					{Kind: agents.TranscriptEventTurnStarted, TurnID: "turn-1"},
					{Kind: agents.TranscriptEventMessageCompleted, TurnID: "turn-1",
						Message: &agents.CompletedMessage{Role: "assistant", Content: reply}},
					{Kind: agents.TranscriptEventTurnCompleted, TurnID: "turn-1"},
				}
			}

			h.svc.tickOnce(ctx)
			h.svc.Stop()

			runs := h.runsFor(t, "digest-id-unused")
			_ = runs // listing is by scheduler id; the per-id checks happen below

			scheds, err := h.st.Schedulers().ListSchedulers(ctx, h.wsID)
			if err != nil || len(scheds) != 1 {
				t.Fatalf("list schedulers: %v (%d)", err, len(scheds))
			}
			run := h.runsFor(t, scheds[0].ID)[0]
			if run.Status != domain.SchedulerRunStatusCompleted {
				t.Fatalf("run status: got %q, want completed", run.Status)
			}
			if run.DeliveryStatus != tc.want {
				t.Fatalf("delivery status: got %q, want %q", run.DeliveryStatus, tc.want)
			}
			if got := len(h.poster.posted()); got != tc.wantPosts {
				t.Fatalf("posts: got %d, want %d (%v)", got, tc.wantPosts, h.poster.posted())
			}
			// Channel targets teach the run the suppression token; thread
			// targets leave it empty.
			wantToken := ""
			if tc.delivery.Type == domain.SchedulerDeliveryChannel {
				wantToken = noReplyToken
			}
			if got := h.submitter.requests()[0].SchedulerNoReply; got != wantToken {
				t.Fatalf("SchedulerNoReply: got %q, want %q", got, wantToken)
			}
		})
	}
}

// TestDeliveryFailureNotRunFailure: a failing channel post records a failed
// delivery on an otherwise completed run (design D8's split).
func TestDeliveryFailureNotRunFailure(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.poster.err = errors.New("agent is no longer a channel member")
	h.seedOnce(t, "digest", baseTime.Add(-time.Minute), channelDelivery())

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	scheds, _ := h.st.Schedulers().ListSchedulers(ctx, h.wsID)
	run := h.runsFor(t, scheds[0].ID)[0]
	if run.Status != domain.SchedulerRunStatusCompleted {
		t.Fatalf("delivery failure must not fail the run, got %q", run.Status)
	}
	if run.DeliveryStatus != domain.SchedulerDeliveryFailed {
		t.Fatalf("delivery status: got %q, want failed", run.DeliveryStatus)
	}
	if !strings.Contains(run.Error, "channel delivery failed") {
		t.Fatalf("run error should name the delivery failure, got %q", run.Error)
	}
	if got := len(h.poster.posted()); got != 1 {
		t.Fatalf("poster should have been attempted once, got %d", got)
	}
}

// TestBlockedCreatorDisabledAutoPauses: a disabled creator blocks the run
// with no model call and auto-pauses the scheduler (design D5). Uses a
// recurring row so the pause is attributable to the service — a once row
// archives at claim time regardless.
func TestBlockedCreatorDisabledAutoPauses(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedRecurring(t, "digest", "* * * * *", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	h.clock.Set(time.Now().UTC().Add(3 * time.Hour))
	disabledAt := baseTime
	if err := h.st.Users().SetDisabled(ctx, h.creatorID, &disabledAt); err != nil {
		t.Fatalf("disable creator: %v", err)
	}

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("disabled creator must not reach the model, got %d submits", len(got))
	}
	run := h.runsFor(t, sched.ID)[0]
	if run.Status != domain.SchedulerRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if got == nil || got.Enabled || got.NextRunAt != nil {
		t.Fatalf("scheduler not auto-paused: enabled=%v next=%v", got.Enabled, got.NextRunAt)
	}
	if got.LastRun == nil || got.LastRun.Status != domain.SchedulerRunStatusBlocked {
		t.Fatalf("last_run not blocked: %+v", got.LastRun)
	}
}

// TestBlockedNilCreatorAutoPauses: created_by is ON DELETE SET NULL, so a
// scheduler whose creator is gone resolves to no acting identity — blocked
// and auto-paused, never silently privileged (design D5).
func TestBlockedNilCreatorAutoPauses(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	s := &domain.Scheduler{
		WorkspaceID: h.wsID,
		AgentID:     h.agentID,
		Name:        "orphaned",
		Prompt:      "Do the thing",
		Kind:        domain.SchedulerKindRecurring,
		Expr:        "* * * * *",
		Enabled:     true,
	}
	if err := h.st.Schedulers().CreateScheduler(ctx, h.wsID, s); err != nil {
		t.Fatalf("create nil-creator scheduler: %v", err)
	}
	h.clock.Set(time.Now().UTC().Add(3 * time.Hour))

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("nil creator must not reach the model, got %d submits", len(got))
	}
	run := h.runsFor(t, s.ID)[0]
	if run.Status != domain.SchedulerRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, s.ID)
	if got == nil || got.Enabled || got.NextRunAt != nil {
		t.Fatalf("nil-creator scheduler not auto-paused: enabled=%v next=%v", got.Enabled, got.NextRunAt)
	}
}

// agentGoneStore hides every agent behind ErrNotFound. Agent deletion
// cascades schedulers in both store ports, so the claim-then-fire race — a
// claimable row whose bound agent cannot be resolved — is simulated at the
// preflight seam.
type agentGoneStore struct {
	store.AgentStore
}

func (a agentGoneStore) ByID(ctx context.Context, workspaceID, id string) (*domain.Agent, error) {
	return nil, fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
}

// TestBlockedMissingAgentDoesNotPause: the bound agent vanished — the run
// blocks cheaply but the scheduler is NOT paused (agent grief is not creator
// grief), and the next occurrence stays scheduled.
func TestBlockedMissingAgentDoesNotPause(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedRecurring(t, "nightly", "* * * * *", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	h.clock.Set(time.Now().UTC().Add(3 * time.Hour))
	h.rewire(t, agentGoneStore{h.st.Agents()})

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := h.submitter.requests(); len(got) != 0 {
		t.Fatalf("missing agent must not reach the model, got %d submits", len(got))
	}
	run := h.runsFor(t, sched.ID)[0]
	if run.Status != domain.SchedulerRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if got == nil || !got.Enabled {
		t.Fatalf("missing agent must not pause the scheduler: %+v", got)
	}
	if got.NextRunAt == nil || !got.NextRunAt.After(h.clock.Now()) {
		t.Fatalf("next occurrence should still be scheduled: %+v", got.NextRunAt)
	}
}

// TestSubmitErrorBlockedNoPause: a synchronous submit failure (pre-model
// config grief) records a blocked run without pausing the scheduler.
func TestSubmitErrorBlockedNoPause(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.submitter.err = errors.New("provider unreachable")
	sched := h.seedRecurring(t, "nightly", "* * * * *", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	h.clock.Set(time.Now().UTC().Add(3 * time.Hour))

	h.svc.tickOnce(ctx)
	h.svc.Stop()

	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("expected the submit attempt to be recorded, got %d", got)
	}
	run := h.runsFor(t, sched.ID)[0]
	if run.Status != domain.SchedulerRunStatusBlocked {
		t.Fatalf("run status: got %q, want blocked", run.Status)
	}
	if !strings.Contains(run.Error, "submit failed") {
		t.Fatalf("run error should carry the submit failure, got %q", run.Error)
	}
	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if got == nil || !got.Enabled {
		t.Fatalf("submit failure must not pause the scheduler: %+v", got)
	}
}

// TestRunNowManual: run-now works on a paused scheduler without re-enabling
// it, records trigger manual, and never touches next_run_at (design D9).
func TestRunNowManual(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedPausedOnce(t, "paused", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})

	run, err := h.svc.RunNow(ctx, h.wsID, sched.ID)
	if err != nil {
		t.Fatalf("run now: %v", err)
	}
	if run.Status != domain.SchedulerRunStatusRunning {
		t.Fatalf("returned run status: got %q, want running", run.Status)
	}
	if run.Trigger != domain.SchedulerTriggerManual {
		t.Fatalf("trigger: got %q, want manual", run.Trigger)
	}
	if run.SessionID != fmt.Sprintf("sched_%s_%d", sched.ID, baseTime.Unix()) {
		t.Fatalf("session id shape: %q", run.SessionID)
	}

	waitFor(t, "run to complete", func() bool {
		runs := h.runsFor(t, sched.ID)
		return len(runs) == 1 && runs[0].Status == domain.SchedulerRunStatusCompleted
	})

	got, _ := h.st.Schedulers().GetScheduler(ctx, h.wsID, sched.ID)
	if got == nil || got.Enabled {
		t.Fatalf("run-now must not re-enable a paused scheduler: %+v", got)
	}
	if got.NextRunAt != nil {
		t.Fatalf("run-now must not touch next_run_at, got %v", got.NextRunAt)
	}
	if got.LastRun == nil || got.LastRun.Trigger != domain.SchedulerTriggerManual {
		t.Fatalf("last_run trigger: %+v", got.LastRun)
	}
}

// TestRunNowInFlightConflict: a second run-now while the first is live
// conflicts; after the terminal outcome a new run is accepted.
func TestRunNowInFlightConflict(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sched := h.seedPausedOnce(t, "busy", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})

	hold := make(chan struct{})
	h.submitter.setHold(hold)

	if _, err := h.svc.RunNow(ctx, h.wsID, sched.ID); err != nil {
		t.Fatalf("first run now: %v", err)
	}
	_, err := h.svc.RunNow(ctx, h.wsID, sched.ID)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("in-flight run now: got %v, want ErrConflict", err)
	}

	close(hold)
	h.submitter.setHold(nil)
	waitFor(t, "run to complete", func() bool {
		runs := h.runsFor(t, sched.ID)
		return len(runs) == 1 && runs[0].Status == domain.SchedulerRunStatusCompleted
	})

	if _, err := h.svc.RunNow(ctx, h.wsID, sched.ID); err != nil {
		t.Fatalf("run now after terminal outcome: %v", err)
	}
	waitFor(t, "second run to complete", func() bool {
		return len(h.runsFor(t, sched.ID)) == 2
	})
	h.svc.Stop()
}

// TestRunNowUnknownScheduler: an unknown id — including a foreign
// workspace's id — is ErrNotFound wrapping the domain sentinel.
func TestRunNowUnknownScheduler(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	_, err := h.svc.RunNow(ctx, h.wsID, "nope")
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown id: got %v, want ErrNotFound wrapping domain.ErrNotFound", err)
	}
	sched := h.seedPausedOnce(t, "mine", domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	_, err = h.svc.RunNow(ctx, "ws-other", sched.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign workspace: got %v, want ErrNotFound", err)
	}
}

// TestConcurrencySemaphoreSerializesAndDropsNothing: with one slot, the
// second and third claims wait for the live fire; releasing the hold lets
// all three complete.
func TestConcurrencySemaphoreSerializesAndDropsNothing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithConcurrency(1))
	hold := make(chan struct{})
	h.submitter.setHold(hold)
	for i := 0; i < 3; i++ {
		h.seedOnce(t, fmt.Sprintf("job-%d", i), baseTime.Add(-time.Minute), domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})
	}

	h.svc.tickOnce(ctx)
	waitFor(t, "first submit", func() bool { return len(h.submitter.requests()) == 1 })

	// The held stream cannot finish, so the single slot stays occupied: no
	// further submit may land while the hold is out.
	time.Sleep(50 * time.Millisecond)
	if got := len(h.submitter.requests()); got != 1 {
		t.Fatalf("semaphore leaked: %d submits with concurrency 1 and a held run", got)
	}

	close(hold)
	h.submitter.setHold(nil)
	h.svc.Stop() // joins every fire

	if got := len(h.submitter.requests()); got != 3 {
		t.Fatalf("expected all 3 claims to fire, got %d", got)
	}
	scheds, _ := h.st.Schedulers().ListSchedulers(ctx, h.wsID)
	for _, sched := range scheds {
		runs := h.runsFor(t, sched.ID)
		if len(runs) != 1 || runs[0].Status != domain.SchedulerRunStatusCompleted {
			t.Fatalf("scheduler %s: runs %+v", sched.ID, runs)
		}
	}
}

// TestTickerLoopDispatches: the real Start/Stop lifecycle claims and fires
// due rows without a manual tick.
func TestTickerLoopDispatches(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithTick(2*time.Millisecond))
	sched := h.seedOnce(t, "looped", baseTime.Add(-time.Minute), domain.SchedulerDelivery{Type: domain.SchedulerDeliveryThread})

	h.svc.Start(ctx)
	waitFor(t, "the loop to fire the due row", func() bool {
		return len(h.submitter.requests()) == 1
	})
	h.svc.Stop()

	runs := h.runsFor(t, sched.ID)
	if len(runs) != 1 || runs[0].Status != domain.SchedulerRunStatusCompleted {
		t.Fatalf("loop fire did not complete a run: %+v", runs)
	}
}
