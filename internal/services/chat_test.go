package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// fakeSubscription is a hand-written agents.Subscription stand-in backed by
// an unbuffered channel.
type fakeSubscription struct {
	mu     sync.Mutex
	events chan sessions.Event
	closed bool
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{events: make(chan sessions.Event)}
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

func (s *fakeSubscription) emit(ev sessions.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.events <- ev
}

// fakeAgentManager is a hand-written agents.Manager fake for the
// session-registry and subscription paths. Send and Submit support only the
// failure path: a successful send needs a real *agents.Turn, which is
// exercised in TestChatService_Send_ReturnsTurnID against the real runtime.
type fakeAgentManager struct {
	mu   sync.Mutex
	subs map[string][]*fakeSubscription

	createSessionErr error
	subscribeErr     error
	sendErr          error
	regenerateErr    error
	cancelErr        error
	resolveErr       error

	contextUsage    agents.ContextUsage
	contextUsageErr error

	cancelled       []string
	resolvedApprove []string
	resolvedDeny    []string
	resolvedIDs     []string
	regenerated     []string
	busy            map[string]bool
}

func newFakeAgentManager() *fakeAgentManager {
	return &fakeAgentManager{subs: make(map[string][]*fakeSubscription), busy: make(map[string]bool)}
}

func (m *fakeAgentManager) CreateSession(_ context.Context, agentID string) (sessions.Session, error) {
	if m.createSessionErr != nil {
		return sessions.Session{}, m.createSessionErr
	}
	return sessions.Session{ID: "session-" + agentID, AgentID: agentID}, nil
}

func (m *fakeAgentManager) Send(_ context.Context, _, _ string, _ ...string) (*agents.Turn, error) {
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	return nil, errors.New("fakeAgentManager: successful Send not supported; use the real runtime")
}

func (m *fakeAgentManager) Submit(_ context.Context, _ agents.SendRequest) (*agents.Turn, error) {
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	return nil, errors.New("fakeAgentManager: successful Submit not supported; use the real runtime")
}

func (m *fakeAgentManager) Regenerate(_ context.Context, sessionID, turnID string) (*agents.Turn, error) {
	if m.regenerateErr != nil {
		return nil, m.regenerateErr
	}
	m.mu.Lock()
	m.regenerated = append(m.regenerated, sessionID+"|"+turnID)
	m.mu.Unlock()
	return nil, errors.New("fakeAgentManager: successful Regenerate not supported; use the real runtime")
}

func (m *fakeAgentManager) Busy(_ context.Context, sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[sessionID]
}

func (m *fakeAgentManager) TurnState(_ context.Context, sessionID string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[sessionID] {
		return true, "turn-" + sessionID
	}
	return false, ""
}

func (m *fakeAgentManager) Subscribe(_ context.Context, sessionID string) (agents.Subscription, error) {
	if m.subscribeErr != nil {
		return nil, m.subscribeErr
	}
	sub := newFakeSubscription()
	m.mu.Lock()
	m.subs[sessionID] = append(m.subs[sessionID], sub)
	m.mu.Unlock()
	return sub, nil
}

func (m *fakeAgentManager) ContextUsage(_ context.Context, _ string) (agents.ContextUsage, error) {
	return m.contextUsage, m.contextUsageErr
}

func (m *fakeAgentManager) Cancel(_ context.Context, sessionID string) error {
	if m.cancelErr != nil {
		return m.cancelErr
	}
	m.mu.Lock()
	m.cancelled = append(m.cancelled, sessionID)
	m.mu.Unlock()
	return nil
}

func (m *fakeAgentManager) ResolveApproval(_ context.Context, sessionID, interruptID string, approved bool) error {
	if m.resolveErr != nil {
		return m.resolveErr
	}
	m.mu.Lock()
	m.resolvedIDs = append(m.resolvedIDs, sessionID+"|"+interruptID)
	if approved {
		m.resolvedApprove = append(m.resolvedApprove, interruptID)
	} else {
		m.resolvedDeny = append(m.resolvedDeny, interruptID)
	}
	m.mu.Unlock()
	return nil
}

// emit delivers ev to every subscription of the session.
func (m *fakeAgentManager) emit(sessionID string, ev sessions.Event) {
	m.mu.Lock()
	subs := append([]*fakeSubscription(nil), m.subs[sessionID]...)
	m.mu.Unlock()
	for _, sub := range subs {
		sub.emit(ev)
	}
}

// subscriptionAt returns the i-th subscription created for the session.
func (m *fakeAgentManager) subscriptionAt(t *testing.T, sessionID string, i int) *fakeSubscription {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	require.Greater(t, len(m.subs[sessionID]), i)
	return m.subs[sessionID][i]
}

// fakeSessionsService is a hand-written sessions.Service fake covering the
// chat service's registry and transcript paths; registry reads delegate to
// the store fake.
type fakeSessionsService struct {
	store store.Store
}

func (s *fakeSessionsService) CreateSession(ctx context.Context, agentID, title string) (sessions.Session, error) {
	session := &store.Session{AgentID: agentID, Title: title}
	if err := s.store.Sessions().Create(ctx, session); err != nil {
		return sessions.Session{}, err
	}
	return *session, nil
}

func (s *fakeSessionsService) Session(ctx context.Context, id string) (sessions.Session, error) {
	row, err := s.store.Sessions().ByID(ctx, id)
	if err != nil {
		return sessions.Session{}, err
	}
	return *row, nil
}

func (s *fakeSessionsService) SessionsByAgent(ctx context.Context, agentID string) ([]sessions.Session, error) {
	return s.store.Sessions().ListByAgent(ctx, agentID)
}

func (s *fakeSessionsService) SetTitle(ctx context.Context, id, title string) error {
	return s.store.Sessions().SetTitle(ctx, id, title)
}

func (s *fakeSessionsService) SetOverrides(ctx context.Context, id, model, effort string) error {
	return s.store.Sessions().SetOverrides(ctx, id, model, effort)
}

func (s *fakeSessionsService) Append(_ context.Context, _ string, _ []sessions.Event) error {
	return nil
}

func (s *fakeSessionsService) Events(_ context.Context, sessionID string) ([]sessions.Event, error) {
	return []sessions.Event{{
		ID:         "ev1",
		TurnID:     "turn1",
		Kind:       sessions.KindUserMessage,
		OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Text:       "hello from " + sessionID,
	}}, nil
}

func (s *fakeSessionsService) EventsPage(_ context.Context, sessionID string, _ int64, _ int) ([]sessions.Event, error) {
	return s.Events(nil, sessionID)
}

func (s *fakeSessionsService) Delete(_ context.Context, _ string) error { return nil }

func (s *fakeSessionsService) SaveCheckpoint(_ context.Context, _ string, _ sessions.Checkpoint) error {
	return nil
}

func (s *fakeSessionsService) Checkpoint(_ context.Context, _ string) (sessions.Checkpoint, bool, error) {
	return sessions.Checkpoint{}, false, nil
}

// newTestChatService wires a ChatService over the given manager and session
// service with throwaway attachment and workspace roots; the attachment and
// workspace-preview surfaces are exercised by their own tests against real
// stores, these cases only need the delegation paths.
func newTestChatService(t *testing.T, turns agents.Manager, fakes sessions.Service, emitter Emitter) *ChatService {
	t.Helper()
	st := fake.New()
	return NewChatService(turns, fakes, emitter, st,
		attachments.NewStore(t.TempDir(), st.Attachments()),
		workspaces.NewManager(t.TempDir()))
}

func TestChatService_CreateSession_Success(t *testing.T) {
	turns := newFakeAgentManager()
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	session, err := svc.CreateSession(context.Background(), "agent1")
	require.NoError(t, err)
	require.Equal(t, "session-agent1", session.ID)
	require.Equal(t, "agent1", session.AgentID)
}

func TestChatService_CreateSession_DelegatesError(t *testing.T) {
	turns := newFakeAgentManager()
	turns.createSessionErr = store.ErrNotFound
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	_, err := svc.CreateSession(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestChatService_Sessions_ReturnsAgentSessions(t *testing.T) {
	st := fake.New()
	session := &store.Session{AgentID: "agent1"}
	require.NoError(t, st.Sessions().Create(context.Background(), session))

	svc := newTestChatService(t, newFakeAgentManager(), &fakeSessionsService{store: st}, &recordingEmitter{})

	list, err := svc.Sessions(context.Background(), "agent1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, session.ID, list[0].ID)
}

func TestChatService_Transcript_ReplaysEvents(t *testing.T) {
	svc := newTestChatService(t, newFakeAgentManager(), &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	events, err := svc.Transcript(context.Background(), "s1")
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, sessions.KindUserMessage, events[0].Kind)
	require.Equal(t, "hello from s1", events[0].Text)
}

// TestChatService_Send_ReturnsTurnID drives Send against the real runtime:
// provider resolution happens asynchronously, so Send returns a turn id
// promptly even without a configured provider.
func TestChatService_Send_ReturnsTurnID(t *testing.T) {
	st := fake.New()
	agent := &store.Agent{Name: "Scout"}
	require.NoError(t, st.Agents().Create(context.Background(), agent))

	runtime := agents.NewManager(st)
	svc := newTestChatService(t, runtime, &fakeSessionsService{store: st}, &recordingEmitter{})

	session, err := svc.CreateSession(context.Background(), agent.ID)
	require.NoError(t, err)

	turnID, err := svc.Send(context.Background(), session.ID, "hi", nil)
	require.NoError(t, err)
	require.NotEmpty(t, turnID)
}

func TestChatService_Send_PropagatesBusy(t *testing.T) {
	turns := newFakeAgentManager()
	turns.sendErr = agents.ErrTurnInProgress
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	_, err := svc.Send(context.Background(), "s1", "hi", nil)
	require.ErrorIs(t, err, agents.ErrTurnInProgress)
}

func TestChatService_Subscribe_ForwardsSessionEvents(t *testing.T) {
	turns := newFakeAgentManager()
	emitter := &recordingEmitter{}
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, emitter)

	require.NoError(t, svc.Subscribe(context.Background(), "s1"))

	want := sessions.Event{TurnID: "turn1", Kind: sessions.KindTextDelta, Text: "he"}
	turns.emit("s1", want)

	var got sessions.Event
	require.Eventually(t, func() bool {
		recorded := emitter.recorded()
		if len(recorded) == 0 {
			return false
		}
		ev, ok := recorded[len(recorded)-1].Data[0].(sessions.Event)
		require.True(t, ok)
		got = ev
		return true
	}, time.Second, time.Millisecond)

	recorded := emitter.recorded()
	require.Equal(t, SessionEventName("s1"), recorded[0].Name)
	require.Equal(t, want, got)
}

func TestChatService_Subscribe_ReplacesPrevious(t *testing.T) {
	turns := newFakeAgentManager()
	emitter := &recordingEmitter{}
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, emitter)

	require.NoError(t, svc.Subscribe(context.Background(), "s1"))
	first := turns.subscriptionAt(t, "s1", 0)

	require.NoError(t, svc.Subscribe(context.Background(), "s1"))
	turns.subscriptionAt(t, "s1", 1)

	// The replaced subscription must be closed so its forwarder stops.
	select {
	case _, open := <-first.Events():
		require.False(t, open, "replaced subscription should be closed")
	case <-time.After(time.Second):
		t.Fatal("replaced subscription was not closed")
	}

	// Only the live subscription's forwarder sees the event: exactly one
	// emission reaches the emitter.
	turns.emit("s1", sessions.Event{Kind: sessions.KindTextDelta})
	require.Eventually(t, func() bool {
		return len(emitter.recorded()) == 1
	}, time.Second, time.Millisecond)
	// The replaced subscription's channel must not have been fed.
	require.Empty(t, first.Events())
}

func TestChatService_Unsubscribe_ClosesAndIsIdempotent(t *testing.T) {
	turns := newFakeAgentManager()
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	require.NoError(t, svc.Subscribe(context.Background(), "s1"))
	sub := turns.subscriptionAt(t, "s1", 0)

	ctx := context.Background()
	require.NoError(t, svc.Unsubscribe(ctx, "s1"))
	require.NoError(t, svc.Unsubscribe(ctx, "s1")) // no-op the second time

	select {
	case _, open := <-sub.Events():
		require.False(t, open, "unsubscribed subscription should be closed")
	case <-time.After(time.Second):
		t.Fatal("unsubscribed subscription was not closed")
	}
}

func TestChatService_Subscribe_NotFound(t *testing.T) {
	turns := newFakeAgentManager()
	turns.subscribeErr = store.ErrNotFound
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	err := svc.Subscribe(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestChatService_ContextUsage_Passthrough(t *testing.T) {
	turns := newFakeAgentManager()
	turns.contextUsage = agents.ContextUsage{Used: 4200, Limit: 200000}
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	usage, err := svc.ContextUsage(context.Background(), "s1")
	require.NoError(t, err)
	require.Equal(t, agents.ContextUsage{Used: 4200, Limit: 200000}, usage)
}

func TestChatService_ContextUsage_PropagatesError(t *testing.T) {
	turns := newFakeAgentManager()
	turns.contextUsageErr = store.ErrNotFound
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	_, err := svc.ContextUsage(context.Background(), "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChatService_Cancel_DelegatesToRuntime covers the bound cancel: the
// session id reaches the runtime and runtime errors propagate.
func TestChatService_Cancel_DelegatesToRuntime(t *testing.T) {
	turns := newFakeAgentManager()
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	require.NoError(t, svc.Cancel(context.Background(), "s1"))
	require.Equal(t, []string{"s1"}, turns.cancelled)
}

// TestChatService_ResolveApproval_DelegatesToRuntime covers the bound
// resolve: session, interrupt id, and decision reach the runtime; conflicts
// propagate.
func TestChatService_ResolveApproval_DelegatesToRuntime(t *testing.T) {
	turns := newFakeAgentManager()
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	require.NoError(t, svc.ResolveApproval(context.Background(), "s1", "int-1", true))
	require.NoError(t, svc.ResolveApproval(context.Background(), "s1", "int-2", false))
	require.Equal(t, []string{"s1|int-1", "s1|int-2"}, turns.resolvedIDs)
	require.Equal(t, []string{"int-1"}, turns.resolvedApprove)
	require.Equal(t, []string{"int-2"}, turns.resolvedDeny)

	turns.resolveErr = agents.ErrApprovalNotPending
	err := svc.ResolveApproval(context.Background(), "s1", "int-1", true)
	require.ErrorIs(t, err, agents.ErrApprovalNotPending, "the stale-id conflict propagates to the caller")
}

func TestChatService_TurnState_RunningAndIdle(t *testing.T) {
	turns := newFakeAgentManager()
	turns.busy["s1"] = true
	svc := newTestChatService(t, turns, &fakeSessionsService{store: fake.New()}, &recordingEmitter{})

	res, err := svc.TurnState(context.Background(), "s1")
	require.NoError(t, err)
	require.True(t, res.Busy)
	require.Equal(t, "turn-s1", res.TurnID)

	idleRes, err := svc.TurnState(context.Background(), "s2")
	require.NoError(t, err)
	require.False(t, idleRes.Busy)
	require.Empty(t, idleRes.TurnID)
}
