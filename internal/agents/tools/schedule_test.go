package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// fakeSchedulerStore is a minimal SchedulerStore for tool tests: a map of
// seeded rows keyed by id plus a per-(workspace, agent, name) uniqueness set
// mirroring the real stores' UNIQUE (workspace_id, agent_id, name) conflict
// sentinel. Create assigns ids and computes next_run_at exactly like the
// in-memory store does, so tool results carry a truthful next fire.
type fakeSchedulerStore struct {
	mu      sync.Mutex
	seq     int
	rows    map[string]*domain.Scheduler // key: id
	names   map[string]bool              // key: workspace|agent|name
	created []*domain.Scheduler
	updated []*domain.Scheduler
	deleted []string
	listErr error
}

func newFakeSchedulerStore() *fakeSchedulerStore {
	return &fakeSchedulerStore{
		rows:  make(map[string]*domain.Scheduler),
		names: make(map[string]bool),
	}
}

func (f *fakeSchedulerStore) nameKey(workspaceID string, s *domain.Scheduler) string {
	return workspaceID + "|" + s.AgentID + "|" + s.Name
}

func (f *fakeSchedulerStore) CreateScheduler(_ context.Context, workspaceID string, s *domain.Scheduler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.names[f.nameKey(workspaceID, s)] {
		return fmt.Errorf("%w: scheduler name %q already taken in this workspace for this agent", domain.ErrConflict, s.Name)
	}
	f.seq++
	if s.ID == "" {
		s.ID = fmt.Sprintf("sch-%d", f.seq)
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	s.NextRunAt = computeNextRunAt(s, now, time.UTC)
	clone := *s
	f.rows[s.ID] = &clone
	f.names[f.nameKey(workspaceID, s)] = true
	created := clone
	f.created = append(f.created, &created)
	return nil
}

func (f *fakeSchedulerStore) GetScheduler(_ context.Context, workspaceID, id string) (*domain.Scheduler, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.rows[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, nil
	}
	clone := *s
	return &clone, nil
}

func (f *fakeSchedulerStore) ListSchedulers(_ context.Context, workspaceID string) ([]domain.Scheduler, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]domain.Scheduler, 0)
	for _, s := range f.rows {
		if s.WorkspaceID == workspaceID {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (f *fakeSchedulerStore) UpdateScheduler(_ context.Context, workspaceID string, s *domain.Scheduler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.rows[s.ID]
	if !ok || existing.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	if other := f.names[f.nameKey(workspaceID, s)]; other && existing.Name != s.Name {
		return fmt.Errorf("%w: scheduler name %q already taken in this workspace for this agent", domain.ErrConflict, s.Name)
	}
	delete(f.names, f.nameKey(workspaceID, existing))
	s.UpdatedAt = time.Now().UTC()
	clone := *s
	f.rows[s.ID] = &clone
	f.names[f.nameKey(workspaceID, s)] = true
	updated := clone
	f.updated = append(f.updated, &updated)
	return nil
}

func (f *fakeSchedulerStore) DeleteScheduler(_ context.Context, workspaceID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.rows[id]
	if !ok || s.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	delete(f.rows, id)
	delete(f.names, f.nameKey(workspaceID, s))
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeSchedulerStore) ClaimDueSchedulers(context.Context, time.Time, int) ([]store.SchedulerClaim, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeSchedulerStore) StartSchedulerRun(context.Context, *domain.SchedulerRun) error {
	return errors.New("not implemented")
}

func (f *fakeSchedulerStore) FinishSchedulerRun(context.Context, string, string, string, int64, int, string, string, string) error {
	return errors.New("not implemented")
}

func (f *fakeSchedulerStore) ListSchedulerRuns(context.Context, string, string, int, int) ([]domain.SchedulerRun, int, error) {
	return nil, 0, errors.New("not implemented")
}

// seedScheduler plants a row directly, bypassing create, so update/delete and
// listing tests control the full entity shape.
func (f *fakeSchedulerStore) seedScheduler(s *domain.Scheduler) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if s.ID == "" {
		s.ID = fmt.Sprintf("sch-seed-%d", f.seq)
	}
	clone := *s
	f.rows[s.ID] = &clone
	f.names[s.WorkspaceID+"|"+s.AgentID+"|"+s.Name] = true
	return s.ID
}

// fakeScheduleMembers resolves the roster per channel id.
type fakeScheduleMembers struct {
	members map[string][]domain.ChannelMember // key: channelID
	listErr error
}

func (f *fakeScheduleMembers) ListChannelMembers(_ context.Context, _ string, channelID string) ([]domain.ChannelMember, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.members[channelID], nil
}

func channelAgentMember(id, agentID string) domain.ChannelMember {
	return domain.ChannelMember{ID: id, MemberType: domain.ChannelMemberTypeAgent, AgentID: agentID}
}

// newScheduleToolForTest constructs the schedule tool bound to a fixed
// identity (ws-1 / agent-1 / user-1, UTC workspace clock).
func newScheduleToolForTest(t *testing.T, schedulers *fakeSchedulerStore, members *fakeScheduleMembers) *scheduleTool {
	t.Helper()
	tl, err := NewSchedule(schedulers, members, "ws-1", "agent-1", "user-1", time.UTC)
	if err != nil {
		t.Fatalf("NewSchedule: %v", err)
	}
	return tl.(*scheduleTool)
}

// TestScheduleTool_CreateRecurringHappyPath covers the create contract: the
// stored entity is bound to the acting agent/workspace/user with a thread
// delivery default and enabled, and the result names the schedule, its human
// label, and the next fire time.
func TestScheduleTool_CreateRecurringHappyPath(t *testing.T) {
	st := newFakeSchedulerStore()
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	out, err := tl.InvokableRun(context.Background(),
		`{"action":"create","name":"morning-digest","prompt":"Summarize overnight alerts.","kind":"recurring","expression":"0 9 * * 1-5"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if len(st.created) != 1 {
		t.Fatalf("expected exactly one created scheduler, got %d", len(st.created))
	}
	s := st.created[0]
	if s.WorkspaceID != "ws-1" || s.AgentID != "agent-1" {
		t.Errorf("scheduler must bind the acting workspace and agent, got ws=%q agent=%q", s.WorkspaceID, s.AgentID)
	}
	if s.CreatedBy == nil || *s.CreatedBy != "user-1" {
		t.Errorf("created_by = %v, want the acting user", s.CreatedBy)
	}
	if s.Kind != domain.SchedulerKindRecurring || s.Expr != "0 9 * * 1-5" {
		t.Errorf("kind/expr = %q/%q, want recurring/0 9 * * 1-5", s.Kind, s.Expr)
	}
	if !s.Enabled {
		t.Error("new schedules must start enabled")
	}
	if s.Delivery.Type != domain.SchedulerDeliveryThread || s.Delivery.ChannelID != "" {
		t.Errorf("delivery = %+v, want the thread default", s.Delivery)
	}
	if s.NextRunAt == nil {
		t.Fatal("store must compute next_run_at for an enabled recurring scheduler")
	}
	if !strings.Contains(out, "morning-digest") {
		t.Errorf("result must name the schedule: %s", out)
	}
	if !strings.Contains(out, "09:00 · Mon–Fri") {
		t.Errorf("result must carry the human label, got %s", out)
	}
	if !strings.Contains(out, "T09:00:00Z") {
		t.Errorf("result must carry the next fire time (a weekday 09:00 UTC), got %s", out)
	}
}

// TestScheduleTool_CreateDuplicateNameRejected pins the conflict mapping: the
// store's ErrConflict-wrapped uniqueness error surfaces as a tool error naming
// the taken name.
func TestScheduleTool_CreateDuplicateNameRejected(t *testing.T) {
	st := newFakeSchedulerStore()
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	args := `{"action":"create","name":"morning-digest","prompt":"First.","kind":"recurring","expression":"0 9 * * *"}`
	if _, err := tl.InvokableRun(context.Background(), args); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := tl.InvokableRun(context.Background(), args)
	if err == nil {
		t.Fatal("duplicate name must be rejected")
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("error must wrap the domain conflict sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), "morning-digest") {
		t.Errorf("error must name the taken name, got %v", err)
	}
	if len(st.created) != 1 {
		t.Errorf("the rejected create must not persist, got %d created", len(st.created))
	}
}

// TestScheduleTool_CreateChannelTargetMembership covers the tool-layer rule
// (integrate-scheduler 6.2): channel delivery passes when the acting agent is
// on the roster and is rejected naming the channel otherwise — with nothing
// created on rejection.
func TestScheduleTool_CreateChannelTargetMembership(t *testing.T) {
	st := newFakeSchedulerStore()
	members := &fakeScheduleMembers{members: map[string][]domain.ChannelMember{
		"ch-mine":  {channelAgentMember("m-1", "agent-1")},
		"ch-other": {channelAgentMember("m-2", "agent-2")},
	}}
	tl := newScheduleToolForTest(t, st, members)

	out, err := tl.InvokableRun(context.Background(),
		`{"action":"create","name":"ops-watch","prompt":"Watch the feed.","kind":"recurring","expression":"*/15 * * * *","delivery":"channel","channel_id":"ch-mine"}`)
	if err != nil {
		t.Fatalf("member channel target must pass: %v", err)
	}
	if !strings.Contains(out, "channel") {
		t.Errorf("result should carry the delivery, got %s", out)
	}
	if len(st.created) != 1 || st.created[0].Delivery.ChannelID != "ch-mine" {
		t.Fatalf("expected the channel-targeted scheduler stored, got %+v", st.created)
	}

	_, err = tl.InvokableRun(context.Background(),
		`{"action":"create","name":"other-watch","prompt":"Watch the other feed.","kind":"recurring","expression":"*/15 * * * *","delivery":"channel","channel_id":"ch-other"}`)
	if err == nil {
		t.Fatal("non-member channel target must be rejected")
	}
	if !strings.Contains(err.Error(), "ch-other") {
		t.Errorf("rejection must name the channel, got %v", err)
	}
	if len(st.created) != 1 {
		t.Errorf("the rejected create must not persist, got %d created", len(st.created))
	}
}

// TestScheduleTool_ListWorkspaceScoped covers workspace isolation: the listing
// returns the acting workspace's schedules only, each with id, name, bound
// agent, label, next fire, enabled, and last status.
func TestScheduleTool_ListWorkspaceScoped(t *testing.T) {
	st := newFakeSchedulerStore()
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-1", Name: "mine", Prompt: "p", Kind: domain.SchedulerKindRecurring, Expr: "0 9 * * *", Enabled: true})
	st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-2", Name: "theirs", Prompt: "p", Kind: domain.SchedulerKindOnce, RunAt: &future, Enabled: true,
		LastRun: &domain.SchedulerLastRun{Status: domain.SchedulerRunStatusCompleted}})
	st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-2", AgentID: "agent-9", Name: "foreign", Prompt: "p", Kind: domain.SchedulerKindRecurring, Expr: "0 9 * * *", Enabled: true})
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	out, err := tl.InvokableRun(context.Background(), `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(out, "foreign") {
		t.Errorf("another workspace's schedules must never appear: %s", out)
	}
	for _, want := range []string{"mine", "theirs", "agent-2", "09:00 · Daily", "completed"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing must carry %q, got %s", want, out)
		}
	}
}

// TestScheduleTool_UpdateAndDeleteRejectCrossAgent pins the anti-runaway
// guard: schedules bound to another agent reject update and delete naming the
// schedule, and nothing is mutated.
func TestScheduleTool_UpdateAndDeleteRejectCrossAgent(t *testing.T) {
	st := newFakeSchedulerStore()
	id := st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-2", Name: "theirs", Prompt: "p", Kind: domain.SchedulerKindRecurring, Expr: "0 9 * * *", Enabled: true})
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	_, err := tl.InvokableRun(context.Background(), `{"action":"update","id":"`+id+`","prompt":"hijack"}`)
	if err == nil || !strings.Contains(err.Error(), "theirs") {
		t.Errorf("cross-agent update must be rejected naming the schedule, got %v", err)
	}
	_, err = tl.InvokableRun(context.Background(), `{"action":"delete","id":"`+id+`"}`)
	if err == nil || !strings.Contains(err.Error(), "theirs") {
		t.Errorf("cross-agent delete must be rejected naming the schedule, got %v", err)
	}
	if len(st.updated) != 0 || len(st.deleted) != 0 {
		t.Errorf("rejected update/delete must not touch the store (updated=%d deleted=%d)", len(st.updated), len(st.deleted))
	}
}

// TestScheduleTool_UpdateAppliesFieldsAndRecomputesNext covers the edit
// contract: provided fields apply, the new schedule recomputes the next fire
// time from now, and disabling clears it.
func TestScheduleTool_UpdateAppliesFieldsAndRecomputesNext(t *testing.T) {
	st := newFakeSchedulerStore()
	id := st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-1", Name: "digest", Prompt: "old", Kind: domain.SchedulerKindRecurring, Expr: "0 9 * * *", Enabled: true})
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	out, err := tl.InvokableRun(context.Background(),
		`{"action":"update","id":"`+id+`","expression":"0 10 * * *","prompt":"new prompt","enabled":false}`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(st.updated) != 1 {
		t.Fatalf("expected one persisted update, got %d", len(st.updated))
	}
	s := st.updated[0]
	if s.Expr != "0 10 * * *" || s.Prompt != "new prompt" {
		t.Errorf("provided fields must apply, got expr=%q prompt=%q", s.Expr, s.Prompt)
	}
	if s.Enabled {
		t.Error("enabled=false must apply")
	}
	if s.NextRunAt != nil {
		t.Errorf("a paused schedule must carry no next fire, got %v", s.NextRunAt)
	}
	if !strings.Contains(out, "digest") {
		t.Errorf("result must name the schedule, got %s", out)
	}

	// Re-enabling recomputes from now under the current expression.
	if _, err := tl.InvokableRun(context.Background(), `{"action":"update","id":"`+id+`","enabled":true}`); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if len(st.updated) != 2 || st.updated[1].NextRunAt == nil || !strings.Contains(st.updated[1].NextRunAt.Format(time.RFC3339), "T10:00:00Z") {
		t.Errorf("re-enable must recompute the next 10:00 fire, got %+v", st.updated[len(st.updated)-1].NextRunAt)
	}
}

// TestScheduleTool_UpdateChannelTargetMembership covers the update side of the
// channel rule: switching delivery to a channel the agent belongs to passes;
// one it does not belong to is rejected naming the channel.
func TestScheduleTool_UpdateChannelTargetMembership(t *testing.T) {
	st := newFakeSchedulerStore()
	members := &fakeScheduleMembers{members: map[string][]domain.ChannelMember{
		"ch-other": {channelAgentMember("m-2", "agent-2")},
	}}
	id := st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-1", Name: "digest", Prompt: "p", Kind: domain.SchedulerKindRecurring, Expr: "0 9 * * *", Enabled: true})
	tl := newScheduleToolForTest(t, st, members)

	_, err := tl.InvokableRun(context.Background(),
		`{"action":"update","id":"`+id+`","delivery":"channel","channel_id":"ch-other"}`)
	if err == nil || !strings.Contains(err.Error(), "ch-other") {
		t.Errorf("non-member channel update must be rejected naming the channel, got %v", err)
	}
	if len(st.updated) != 0 {
		t.Errorf("the rejected update must not persist, got %d", len(st.updated))
	}

	members.members["ch-mine"] = []domain.ChannelMember{channelAgentMember("m-1", "agent-1")}
	if _, err := tl.InvokableRun(context.Background(),
		`{"action":"update","id":"`+id+`","delivery":"channel","channel_id":"ch-mine"}`); err != nil {
		t.Fatalf("member channel update must pass: %v", err)
	}
	if len(st.updated) != 1 || st.updated[0].Delivery.ChannelID != "ch-mine" {
		t.Errorf("the accepted update must persist the channel delivery, got %+v", st.updated)
	}
}

// TestScheduleTool_OnceInstants covers the one-shot rule: a past instant is
// rejected at create, a future instant passes naming run_at as the next fire,
// and an update leaving a carried-past run_at unchanged still passes
// (creating=false).
func TestScheduleTool_OnceInstants(t *testing.T) {
	st := newFakeSchedulerStore()
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	_, err := tl.InvokableRun(context.Background(),
		`{"action":"create","name":"late","prompt":"p","kind":"once","run_at":"2020-01-01T00:00:00Z"}`)
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Errorf("a past one-shot instant must be rejected, got %v", err)
	}
	if len(st.created) != 0 {
		t.Errorf("the rejected create must not persist, got %d", len(st.created))
	}

	out, err := tl.InvokableRun(context.Background(),
		`{"action":"create","name":"reminder","prompt":"p","kind":"once","run_at":"2099-01-01T09:30:00Z"}`)
	if err != nil {
		t.Fatalf("a future one-shot instant must pass: %v", err)
	}
	if len(st.created) != 1 {
		t.Fatalf("expected the one-shot stored, got %d", len(st.created))
	}
	if !strings.Contains(out, "2099-01-01T09:30:00Z") {
		t.Errorf("the result must name the one-shot instant, got %s", out)
	}

	// The stored row's instant is now past; an update that does not touch the
	// schedule (rename only) must still pass.
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	id := st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-1", Name: "expired", Prompt: "p", Kind: domain.SchedulerKindOnce, RunAt: &past, Enabled: true})
	if _, err := tl.InvokableRun(context.Background(), `{"action":"update","id":"`+id+`","prompt":"reworded"}`); err != nil {
		t.Errorf("an unchanged past run_at must pass on update (creating=false), got %v", err)
	}
}

// TestScheduleTool_KindSwitchValidation covers kind/expression change
// validation on update: switching to once without run_at, and switching to
// recurring without an expression, are rejected by the shared validator.
func TestScheduleTool_KindSwitchValidation(t *testing.T) {
	st := newFakeSchedulerStore()
	recurring := st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-1", Name: "recurring", Prompt: "p", Kind: domain.SchedulerKindRecurring, Expr: "0 9 * * *", Enabled: true})
	once := st.seedScheduler(&domain.Scheduler{WorkspaceID: "ws-1", AgentID: "agent-1", Name: "once", Prompt: "p", Kind: domain.SchedulerKindOnce, RunAt: ptrTime(time.Now().UTC().Add(time.Hour)), Enabled: true})
	tl := newScheduleToolForTest(t, st, &fakeScheduleMembers{})

	_, err := tl.InvokableRun(context.Background(), `{"action":"update","id":"`+recurring+`","kind":"once"}`)
	if err == nil || !strings.Contains(err.Error(), "run_at") {
		t.Errorf("once switch without run_at must be rejected, got %v", err)
	}
	_, err = tl.InvokableRun(context.Background(), `{"action":"update","id":"`+once+`","kind":"recurring"}`)
	if err == nil || !strings.Contains(err.Error(), "cron expression") {
		t.Errorf("recurring switch without an expression must be rejected, got %v", err)
	}
	if len(st.updated) != 0 {
		t.Errorf("rejected updates must not persist, got %d", len(st.updated))
	}

	// A fully-specified switch passes and recomputes the next fire.
	if _, err := tl.InvokableRun(context.Background(),
		`{"action":"update","id":"`+once+`","kind":"recurring","expression":"30 9 * * *"}`); err != nil {
		t.Fatalf("fully-specified switch must pass: %v", err)
	}
	if len(st.updated) != 1 || st.updated[0].Kind != domain.SchedulerKindRecurring || st.updated[0].RunAt != nil {
		t.Errorf("switch must persist kind=once→recurring and clear run_at, got %+v", st.updated)
	}
}

// TestScheduleTool_UnknownActionAndMissingFields pins the argument contract.
func TestScheduleTool_UnknownActionAndMissingFields(t *testing.T) {
	tl := newScheduleToolForTest(t, newFakeSchedulerStore(), &fakeScheduleMembers{})

	for _, tc := range []struct {
		args string
		want string
	}{
		{`{"action":"run"}`, `action must be`},
		{`{"action":"create","prompt":"p","kind":"recurring","expression":"0 9 * * *"}`, "name is required"},
		{`{"action":"create","name":"x","kind":"recurring","expression":"0 9 * * *"}`, "prompt is required"},
		{`{"action":"create","name":"x","prompt":"p","expression":"0 9 * * *"}`, "kind is required"},
		{`{"action":"create","name":"x","prompt":"p","kind":"once"}`, "run_at is required"},
		{`{"action":"create","name":"x","prompt":"p","kind":"once","run_at":"not-a-time"}`, "RFC3339"},
		{`{"action":"update"}`, "id is required"},
		{`{"action":"delete"}`, "id is required"},
		{`{"action":"update","id":"nope","prompt":"p"}`, "no schedule nope"},
		{`{"action":"delete","id":"nope"}`, "no schedule nope"},
	} {
		_, err := tl.InvokableRun(context.Background(), tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected error containing %q, got %v", tc.args, tc.want, err)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
