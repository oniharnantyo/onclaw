package groups

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The test harness: a fake store, the real sessions service over it, a
// controllable runtime fake implementing the groups.Manager seam, an events
// recorder, and the service under test. agents.Turn's fields (and finish)
// are unexported, so the harness mints finishable turns reflectively — the
// same pattern internal/cli's schedule adapter tests use.

// fakeSubscription is a hand-written agents.Subscription stand-in the pump
// tests emit through.
type fakeSubscription struct {
	mu     sync.Mutex
	events chan sessions.Event
	closed bool
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{events: make(chan sessions.Event, 64)}
}

func (s *fakeSubscription) Events() <-chan sessions.Event { return s.events }

func (s *fakeSubscription) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.events)
}

// emit delivers one event; a closed subscription drops it.
func (s *fakeSubscription) emit(ev sessions.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.events <- ev
	return true
}

// summarizeCall records one SummarizeText invocation.
type summarizeCall struct {
	AgentID     string
	Instruction string
	Text        string
}

// fakeRuntime is the controllable groups.Manager fake: session creation
// delegates to the real sessions service over the fake store, SubmitGroup
// mints finishable turns the tests resolve, Subscribe hands out fake
// subscriptions, and every call is recorded.
type fakeRuntime struct {
	sessions sessions.Service
	store    *fake.Store

	mu        sync.Mutex
	createErr error
	subErr    error
	submitErr error

	creates         []string                  // agent ids
	sessionsByAgent map[string]string         // agent id -> session id
	submits         []agents.GroupTurnRequest // in dispatch order
	// statesAtSubmit snapshots the group's task states at each SubmitGroup
	// call — the persist-before-dispatch pin (design D8).
	statesAtSubmit [][]string
	pending        []*pendingTurn // finishers in submit order
	cancels        []string       // session ids
	summaries      []summarizeCall
	subs           map[string][]*fakeSubscription // session id -> subscriptions
}

// pendingTurn is one minted dispatch turn and its finisher.
type pendingTurn struct {
	req    agents.GroupTurnRequest
	finish func(error)
}

func newFakeRuntime(st *fake.Store, sess sessions.Service) *fakeRuntime {
	return &fakeRuntime{
		sessions:        sess,
		store:           st,
		sessionsByAgent: make(map[string]string),
		subs:            make(map[string][]*fakeSubscription),
	}
}

func (m *fakeRuntime) CreateSession(_ context.Context, agentID string) (sessions.Session, error) {
	if m.createErr != nil {
		return sessions.Session{}, m.createErr
	}
	sess, err := m.sessions.CreateSession(context.Background(), agentID, "")
	if err != nil {
		return sessions.Session{}, err
	}
	m.mu.Lock()
	m.creates = append(m.creates, agentID)
	m.sessionsByAgent[agentID] = sess.ID
	m.mu.Unlock()
	return sess, nil
}

func (m *fakeRuntime) SubmitGroup(_ context.Context, req agents.GroupTurnRequest) (*agents.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.submitErr != nil {
		return nil, m.submitErr
	}
	turn := &agents.Turn{}
	v := reflect.ValueOf(turn).Elem()
	setTurnField(v, "id", fmt.Sprintf("turn-%d", len(m.submits)+1))
	done := make(chan struct{})
	setTurnField(v, "done", done)
	finisher := func(err error) {
		setTurnField(v, "err", err)
		close(done)
	}
	m.submits = append(m.submits, req)
	m.pending = append(m.pending, &pendingTurn{req: req, finish: finisher})
	if tasks, err := m.store.Groups().ListTasksByGroup(context.Background(), req.Packet.GroupID); err == nil {
		states := make([]string, 0, len(tasks))
		for _, task := range tasks {
			states = append(states, task.State)
		}
		m.statesAtSubmit = append(m.statesAtSubmit, states)
	}
	return turn, nil
}

func (m *fakeRuntime) SummarizeText(_ context.Context, agentID, instruction, text string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summaries = append(m.summaries, summarizeCall{AgentID: agentID, Instruction: instruction, Text: text})
	return "summary of " + text, nil
}

func (m *fakeRuntime) Subscribe(_ context.Context, sessionID string) (agents.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subErr != nil {
		return nil, m.subErr
	}
	sub := newFakeSubscription()
	m.subs[sessionID] = append(m.subs[sessionID], sub)
	return sub, nil
}

func (m *fakeRuntime) Cancel(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancels = append(m.cancels, sessionID)
	return nil
}

// submitCount reports how many dispatches the fake accepted.
func (m *fakeRuntime) submitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.submits)
}

// cancelCount reports how many member-turn interrupts the fake recorded.
func (m *fakeRuntime) cancelCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.cancels)
}

// cancelAt returns the i-th interrupted session id.
func (m *fakeRuntime) cancelAt(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancels[i]
}

// submitAt returns the i-th submitted request.
func (m *fakeRuntime) submitAt(i int) agents.GroupTurnRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.submits[i]
}

// sessionFor maps an agent id to its fake-created session id.
func (m *fakeRuntime) sessionFor(agentID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionsByAgent[agentID]
}

// finishTurn resolves the i-th submitted turn's terminal outcome.
func (m *fakeRuntime) finishTurn(i int, err error) {
	m.mu.Lock()
	p := m.pending[i]
	m.mu.Unlock()
	p.finish(err)
}

// finishOnePending resolves the oldest unfinished dispatch turn; false when
// none is pending.
func (m *fakeRuntime) finishOnePending(err error) bool {
	m.mu.Lock()
	if len(m.pending) == 0 {
		m.mu.Unlock()
		return false
	}
	p := m.pending[0]
	m.pending = m.pending[1:]
	m.mu.Unlock()
	p.finish(err)
	return true
}

// setTurnField writes one unexported agents.Turn field for the finishable
// turns the fake mints; tests never touch live runtime turns.
func setTurnField(v reflect.Value, name string, val any) {
	f := v.FieldByName(name)
	if !f.IsValid() {
		panic("agents.Turn has no field " + name)
	}
	rv := reflect.ValueOf(val)
	if !rv.IsValid() {
		rv = reflect.Zero(f.Type())
	}
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(rv.Convert(f.Type()))
}

// fakeEvents records the UI-push callbacks.
type fakeEvents struct {
	mu           sync.Mutex
	groupChanged int
	events       []GroupEvent
}

func (e *fakeEvents) GroupChanged() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.groupChanged++
}

func (e *fakeEvents) GroupEvent(groupID string, ev GroupEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ev.GroupID = groupID
	e.events = append(e.events, ev)
}

func (e *fakeEvents) changedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.groupChanged
}

func (e *fakeEvents) eventCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

// eventsOf returns the recorded events of one kind.
func (e *fakeEvents) eventsOf(kind GroupEventKind) []GroupEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]GroupEvent, 0)
	for _, ev := range e.events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// harness wires one test's world.
type harness struct {
	t   *testing.T
	ctx context.Context
	st  *fake.Store
	svc *Service
	rt  *fakeRuntime
	ev  *fakeEvents
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	st := fake.New()
	sess := sessions.NewService(st)
	rt := newFakeRuntime(st, sess)
	ev := &fakeEvents{}
	svc := New(Deps{Store: st, Sessions: sess, Manager: rt, Ref: agents.NewGroupTurnRef(), Events: ev})
	return &harness{t: t, ctx: ctx, st: st, svc: svc, rt: rt, ev: ev}
}

// addAgent seeds one agent row and returns it.
func (h *harness) addAgent(name, description string) *store.Agent {
	h.t.Helper()
	a := &store.Agent{Name: name, Description: description, Slug: name}
	require.NoError(h.t, h.st.Agents().Create(h.ctx, a))
	return a
}

// newGroup seeds a group with the given members (first becomes lead).
func (h *harness) newGroup(name string, members ...*store.Agent) *store.AgentGroup {
	h.t.Helper()
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ID)
	}
	g, err := h.svc.CreateGroup(h.ctx, name, name+" title", "be helpful", ids)
	require.NoError(h.t, err)
	return g
}

// groupTasks returns the group's tasks.
func (h *harness) groupTasks(groupID string) []store.AgentGroupTask {
	h.t.Helper()
	tasks, err := h.svc.Tasks(h.ctx, groupID)
	require.NoError(h.t, err)
	return tasks
}

// taskByID returns one task row.
func (h *harness) taskByID(taskID string) *store.AgentGroupTask {
	h.t.Helper()
	t, err := h.st.Groups().TaskByID(h.ctx, taskID)
	require.NoError(h.t, err)
	return t
}

// transcript returns the group's full transcript, oldest first.
func (h *harness) transcript(groupID string) []store.AgentGroupMessage {
	h.t.Helper()
	rows, err := h.svc.Transcript(h.ctx, groupID, 0, 1000)
	require.NoError(h.t, err)
	return rows
}

// messagesOf returns the transcript's rows of one kind.
func (h *harness) messagesOf(groupID, kind string) []store.AgentGroupMessage {
	h.t.Helper()
	rows := h.transcript(groupID)
	out := make([]store.AgentGroupMessage, 0)
	for _, r := range rows {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// restart simulates an app restart: a fresh service (and runtime, and
// events) over the SAME store — all in-memory state (pumps, inflight
// records, checkpoint fingerprints) gone, all persisted state intact.
func (h *harness) restart() {
	h.rt = newFakeRuntime(h.st, sessions.NewService(h.st))
	h.ev = &fakeEvents{}
	h.svc = New(Deps{Store: h.st, Sessions: sessions.NewService(h.st), Manager: h.rt, Ref: agents.NewGroupTurnRef(), Events: h.ev})
}

// emitToPump delivers one session event through the member session's live
// subscription (the copy-back pump's source).
func (h *harness) emitToPump(sessionID string, ev sessions.Event) {
	h.t.Helper()
	h.rt.mu.Lock()
	subs := append([]*fakeSubscription(nil), h.rt.subs[sessionID]...)
	h.rt.mu.Unlock()
	require.NotEmpty(h.t, subs, "no subscription for session %s", sessionID)
	require.True(h.t, subs[len(subs)-1].emit(ev), "subscription closed")
}

// invokeTool runs one in-channel tool with JSON arguments, returning its
// result string.
func (h *harness) invokeTool(t *testing.T, bt tool.BaseTool, args string) string {
	t.Helper()
	it, ok := bt.(tool.InvokableTool)
	require.True(t, ok, "tool is not invokable")
	out, err := it.InvokableRun(h.ctx, args)
	require.NoError(t, err)
	return out
}

// mustTools fetches the group turn's in-channel tools.
func mustTools(t *testing.T, h *harness, groupID, agentID string) []tool.BaseTool {
	t.Helper()
	tools, err := h.svc.GroupTurnTools(h.ctx, groupID, agentID)
	require.NoError(t, err)
	return tools
}

// toInvokable type-asserts a tool set for direct InvokableRun calls.
func toInvokable(t *testing.T, tools []tool.BaseTool) []tool.InvokableTool {
	t.Helper()
	out := make([]tool.InvokableTool, 0, len(tools))
	for _, bt := range tools {
		it, ok := bt.(tool.InvokableTool)
		require.True(t, ok, "tool is not invokable")
		out = append(out, it)
	}
	return out
}

// requireEventually polls cond until it holds or the deadline passes — the
// synchronization point for the service's asynchronous paths (turn-end
// handlers, copy-back pumps, summary builds).
func requireEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 2*time.Millisecond, what)
}

// settleTask waits for one task to reach the given state — the turn-end
// handler applies its outcome asynchronously after finishTurn.
func (h *harness) settleTask(taskID, state string) {
	h.t.Helper()
	requireEventually(h.t, "task "+taskID+" settles to "+state, func() bool {
		return h.taskByID(taskID).State == state
	})
}
