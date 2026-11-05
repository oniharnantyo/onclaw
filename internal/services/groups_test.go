package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/groups"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// fakeGroupsManager is a hand-written groups.Manager fake: member-session
// creation and subscription go through the same paths the chat tests fake,
// and SubmitGroup supports only the failure path — a successful group turn
// needs a real *agents.Turn, which the groups package's own harness covers.
type fakeGroupsManager struct {
	mu              sync.Mutex
	createdSessions []string
	submits         []agents.GroupTurnRequest
	cancelled       []string
}

var _ groups.Manager = (*fakeGroupsManager)(nil)

func (m *fakeGroupsManager) CreateSession(_ context.Context, agentID string) (sessions.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdSessions = append(m.createdSessions, agentID)
	return sessions.Session{ID: "session-" + agentID, AgentID: agentID}, nil
}

func (m *fakeGroupsManager) SubmitGroup(_ context.Context, req agents.GroupTurnRequest) (*agents.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.submits = append(m.submits, req)
	return nil, errors.New("fakeGroupsManager: successful SubmitGroup not supported; use the groups harness")
}

func (m *fakeGroupsManager) SummarizeText(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}

func (m *fakeGroupsManager) Subscribe(_ context.Context, _ string) (agents.Subscription, error) {
	return newFakeSubscription(), nil
}

func (m *fakeGroupsManager) Cancel(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelled = append(m.cancelled, sessionID)
	return nil
}

// groupPayloadText decodes a transcript row's display text (the groups
// package's payload schema is opaque JSON the services layer reads but never
// edits).
func groupPayloadText(m store.AgentGroupMessage) string {
	var p struct {
		Text string `json:"text"`
	}
	if m.Payload == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(m.Payload), &p); err != nil {
		return ""
	}
	return p.Text
}

// newTestGroupsService wires a GroupsService over the fake store and the
// fake groups manager with the events forwarder carrying the given emitter —
// the same wiring Assemble builds, minus the real runtime. The store comes
// back so tests can seed agents and groups directly.
func newTestGroupsService(t *testing.T, emitter *recordingEmitter) (*GroupsService, *fakeGroupsManager, *fake.Store) {
	t.Helper()
	st := fake.New()
	mgr := &fakeGroupsManager{}
	svc := groups.New(groups.Deps{
		Store:    st,
		Sessions: sessions.NewService(st),
		Manager:  mgr,
		Ref:      agents.NewGroupTurnRef(),
		Events:   NewGroupsEvents(emitter),
	})
	return NewGroupsService(svc), mgr, st
}

// TestGroupsService_CreateGroup_EmitsGroupsChanged drives a roster-level
// mutation through the events forwarder: creating a group announces
// groups:changed so the rail refetches (design D9).
func TestGroupsService_CreateGroup_EmitsGroupsChanged(t *testing.T) {
	emitter := &recordingEmitter{}
	svc, _, _ := newTestGroupsService(t, emitter)

	g, err := svc.CreateGroup(context.Background(), "scouts", "The Scouts", "", nil)
	require.NoError(t, err)
	require.NotEmpty(t, g.ID)
	require.Equal(t, "scouts", g.Name)

	recorded := emitter.recorded()
	require.Len(t, recorded, 1)
	require.Equal(t, EventGroupsChanged, recorded[0].Name)
	require.Equal(t, GroupsChangedEvent{}, recorded[0].Data[0])
}

// TestGroupsService_Send_AppendedMessageEmittedUnderGroupEventName drives a
// transcript mutation arriving via the events implementation: a user send to
// a member-less group appends a row and the update is emitted under
// GroupEventName(groupID) carrying the appended row (design D9).
func TestGroupsService_Send_AppendedMessageEmittedUnderGroupEventName(t *testing.T) {
	emitter := &recordingEmitter{}
	svc, _, _ := newTestGroupsService(t, emitter)
	ctx := context.Background()

	g, err := svc.CreateGroup(ctx, "scouts", "", "", nil)
	require.NoError(t, err)

	require.NoError(t, svc.Send(ctx, groups.SendRequest{GroupID: g.ID, Text: "hello group"}))

	recorded := emitter.recorded()
	require.Len(t, recorded, 2) // groups:changed on create, then the append
	last := recorded[len(recorded)-1]
	require.Equal(t, GroupEventName(g.ID), last.Name)

	ev, ok := last.Data[0].(GroupEvent)
	require.True(t, ok)
	require.Equal(t, GroupEventKindMessageAppended, ev.Kind)
	require.NotEmpty(t, ev.MessageID)
	require.Positive(t, ev.Seq)
	require.NotNil(t, ev.Message)
	require.Equal(t, store.GroupMessageKindMessage, ev.Message.Kind)
	require.Equal(t, store.GroupAuthorKindUser, ev.Message.AuthorKind)
	require.Equal(t, "hello group", groupPayloadText(*ev.Message))
	require.Empty(t, ev.TaskID)

	// The payload serializes with the expected json tags.
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	require.Contains(t, string(b), `"kind":"message-appended"`)
	require.Contains(t, string(b), `"messageId":`)
	require.Contains(t, string(b), `"message":`)
	require.NotContains(t, string(b), `"taskId":`)
}

// TestGroupsService_Send_TaskUpdatedEmittedUnderGroupEventName covers the
// task lifecycle push: routing a single-member group's message creates the
// owner's root task and the update is emitted under the group's event name
// with the task kind (design D9). The fake manager rejects the dispatch, so
// the task reverts to queued — both transitions emit.
func TestGroupsService_Send_TaskUpdatedEmittedUnderGroupEventName(t *testing.T) {
	emitter := &recordingEmitter{}
	svc, mgr, st := newTestGroupsService(t, emitter)
	ctx := context.Background()

	agent := &store.Agent{Name: "Scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))

	g, err := svc.CreateGroup(ctx, "scouts", "", "", []string{agent.ID})
	require.NoError(t, err)
	emitter.recorded() // drain the create-time events

	require.NoError(t, svc.Send(ctx, groups.SendRequest{GroupID: g.ID, Text: "please investigate"}))

	var taskEvents []GroupEvent
	for _, rec := range emitter.recorded() {
		if rec.Name != GroupEventName(g.ID) {
			continue
		}
		ev, ok := rec.Data[0].(GroupEvent)
		require.True(t, ok)
		if ev.Kind == GroupEventKindTaskUpdated {
			taskEvents = append(taskEvents, ev)
		}
	}
	require.NotEmpty(t, taskEvents, "the task creation emits under the group's event name")
	for _, ev := range taskEvents {
		require.NotEmpty(t, ev.TaskID)
		require.Nil(t, ev.Message)
	}

	// The dispatch never started (the fake manager refuses), so the task
	// reverted to queued for the next drain.
	tasks, err := svc.Tasks(ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, store.GroupTaskStateQueued, tasks[0].State)
	require.Equal(t, tasks[0].TaskID, taskEvents[0].TaskID)
	require.Len(t, mgr.submits, 1, "the dispatch reached the runtime once before reverting")
}

// TestGroupsService_TranscriptPage_ReturnsWindow covers the transcript read
// the group surface pages through.
func TestGroupsService_TranscriptPage_ReturnsWindow(t *testing.T) {
	emitter := &recordingEmitter{}
	svc, _, _ := newTestGroupsService(t, emitter)
	ctx := context.Background()

	g, err := svc.CreateGroup(ctx, "scouts", "", "", nil)
	require.NoError(t, err)
	require.NoError(t, svc.Send(ctx, groups.SendRequest{GroupID: g.ID, Text: "first"}))
	require.NoError(t, svc.Send(ctx, groups.SendRequest{GroupID: g.ID, Text: "second"}))

	rows, err := svc.TranscriptPage(ctx, g.ID, 0, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "second", groupPayloadText(rows[0]))
}

// TestGroupEventName_ComposesLikeSessionEventName pins the composition: the
// per-group event name is derived from the group id exactly the way the
// per-session name is (a literal string in two places is how frontend
// subscriptions silently break).
func TestGroupEventName_ComposesLikeSessionEventName(t *testing.T) {
	require.Equal(t, "group:g1:event", GroupEventName("g1"))
	require.Equal(t, "session:g1:event", SessionEventName("g1"))
}

// fakeGroupCleaner is the GroupCleaner test double: it records the agents
// Delete cleaned up, fails on demand, and — when given the store — records
// whether the agent row still existed at call time (the cleanup must run
// before the row delete: a group's lead column references the agent row).
type fakeGroupCleaner struct {
	mu     sync.Mutex
	st     store.Store
	called []string
	exists []bool
	err    error
}

func (f *fakeGroupCleaner) CleanupAgent(ctx context.Context, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = append(f.called, agentID)
	if f.st != nil {
		_, err := f.st.Agents().ByID(ctx, agentID)
		f.exists = append(f.exists, err == nil)
	}
	return f.err
}

// TestAgentsService_Delete_InvokesGroupCleanup covers the delete-time group
// cleanup (add-agent-groups tasks 4.3): deleting a member agent invokes the
// cleanup while the row still exists, and the row delete proceeds after.
func TestAgentsService_Delete_InvokesGroupCleanup(t *testing.T) {
	st := fake.New()
	agent := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(context.Background(), agent))

	cleaner := &fakeGroupCleaner{st: st}
	svc := NewAgentsService(st, &fakeGenerator{}, workspaces.NewManager(t.TempDir()),
		knowledge.NewStore(t.TempDir(), st.Documents()), t.TempDir(), cleaner)

	require.NoError(t, svc.Delete(context.Background(), agent.ID))
	require.Equal(t, []string{agent.ID}, cleaner.called)
	require.Equal(t, []bool{true}, cleaner.exists, "cleanup runs before the row delete")

	_, err := st.Agents().ByID(context.Background(), agent.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestAgentsService_Delete_CleanupFailureAbortsDelete pins the ordering
// contract's failure path: a failed group cleanup aborts the delete so the
// agent row never disappears with its group state unrepaired.
func TestAgentsService_Delete_CleanupFailureAbortsDelete(t *testing.T) {
	st := fake.New()
	agent := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(context.Background(), agent))

	cleaner := &fakeGroupCleaner{err: errors.New("group store unavailable")}
	svc := NewAgentsService(st, &fakeGenerator{}, workspaces.NewManager(t.TempDir()),
		knowledge.NewStore(t.TempDir(), st.Documents()), t.TempDir(), cleaner)

	err := svc.Delete(context.Background(), agent.ID)
	require.ErrorContains(t, err, "group cleanup")
	require.ErrorContains(t, err, "group store unavailable")

	_, byErr := st.Agents().ByID(context.Background(), agent.ID)
	require.NoError(t, byErr, "the row delete is aborted")
}
