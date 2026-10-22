package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// errInject is the marker error the fakes return.
var errInject = errors.New("injected failure")

// fakeAgentsManager is a tiny agents.Manager fake: only CreateSession and
// Submit matter for the adapter; every other method is unexpected here.
type fakeAgentsManager struct {
	sessions sessions.Service

	createErr error
	submitErr error
	// skipPersist makes CreateSession return a session id that was never
	// registered with the sessions service, so the adapter's SetTitle fails.
	skipPersist bool

	created []string
	submits []agents.SendRequest
}

func (m *fakeAgentsManager) CreateSession(ctx context.Context, agentID string) (sessions.Session, error) {
	if m.createErr != nil {
		return sessions.Session{}, m.createErr
	}
	if m.skipPersist {
		return sessions.Session{ID: "ghost", AgentID: agentID}, nil
	}
	sess, err := m.sessions.CreateSession(ctx, agentID, "")
	if err != nil {
		return sessions.Session{}, err
	}
	m.created = append(m.created, sess.ID)
	return sess, nil
}

func (m *fakeAgentsManager) Submit(_ context.Context, req agents.SendRequest) (*agents.Turn, error) {
	m.submits = append(m.submits, req)
	if m.submitErr != nil {
		return nil, m.submitErr
	}
	return &agents.Turn{}, nil
}

func (m *fakeAgentsManager) Send(context.Context, string, string, ...string) (*agents.Turn, error) {
	return nil, panicUnexpected()
}
func (m *fakeAgentsManager) Regenerate(context.Context, string, string) (*agents.Turn, error) {
	return nil, panicUnexpected()
}
func (m *fakeAgentsManager) Subscribe(context.Context, string) (agents.Subscription, error) {
	return nil, panicUnexpected()
}
func (m *fakeAgentsManager) ContextUsage(context.Context, string) (agents.ContextUsage, error) {
	return agents.ContextUsage{}, panicUnexpected()
}
func (m *fakeAgentsManager) Cancel(context.Context, string) error { return panicUnexpected() }
func (m *fakeAgentsManager) ResolveApproval(context.Context, string, string, bool) error {
	return panicUnexpected()
}
func (m *fakeAgentsManager) Busy(context.Context, string) bool { panicUnexpected(); return false }

func panicUnexpected() error {
	panic("unexpected call")
}

// newScheduleRuns wires the adapter over the fake manager and the real
// sessions service over the fake store.
func newScheduleRuns(t *testing.T) (*scheduleRuns, *fakeAgentsManager) {
	t.Helper()
	svc := sessions.NewService(fake.New())
	mgr := &fakeAgentsManager{sessions: svc}
	return &scheduleRuns{runtime: mgr, sessions: svc}, mgr
}

func TestScheduleRuns_Create_TitlesSessionAndReturnsID(t *testing.T) {
	runs, mgr := newScheduleRuns(t)

	id, err := runs.Create(context.Background(), "agt-1", "Standup — 2026-10-21 09:00")
	require.NoError(t, err)
	require.Equal(t, id, mgr.created[0])

	// The title was set BEFORE returning (the runner only auto-titles
	// untitled sessions), so the persisted session carries it.
	sess, err := runs.sessions.Session(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "agt-1", sess.AgentID)
	require.Equal(t, "Standup — 2026-10-21 09:00", sess.Title)
}

func TestScheduleRuns_Create_WrapsErrors(t *testing.T) {
	t.Run("create session", func(t *testing.T) {
		runs, mgr := newScheduleRuns(t)
		mgr.createErr = errInject

		id, err := runs.Create(context.Background(), "agt-1", "title")
		require.Empty(t, id)
		require.ErrorIs(t, err, errInject)
		require.Contains(t, err.Error(), "create scheduled session")
	})

	t.Run("set title", func(t *testing.T) {
		runs, mgr := newScheduleRuns(t)
		mgr.skipPersist = true // the returned session does not exist downstream

		id, err := runs.Create(context.Background(), "agt-1", "title")
		require.Empty(t, id)
		require.Contains(t, err.Error(), "title scheduled session")
	})
}

func TestScheduleRuns_Submit_SendsSchedulerOrigin(t *testing.T) {
	runs, mgr := newScheduleRuns(t)

	require.NoError(t, runs.Submit(context.Background(), "sess-1", "summarize the standup"))
	require.Len(t, mgr.submits, 1)
	req := mgr.submits[0]
	require.Equal(t, "sess-1", req.SessionID)
	require.Equal(t, "summarize the standup", req.Message)
	require.Empty(t, req.AttachmentKeys)
	require.Equal(t, hooks.OriginScheduler, req.Origin)
}

func TestScheduleRuns_Submit_WrapsErrors(t *testing.T) {
	runs, mgr := newScheduleRuns(t)
	mgr.submitErr = errInject

	err := runs.Submit(context.Background(), "sess-1", "prompt")
	require.ErrorIs(t, err, errInject)
	require.Contains(t, err.Error(), fmt.Sprintf("submit scheduled run for session %s", "sess-1"))
}
