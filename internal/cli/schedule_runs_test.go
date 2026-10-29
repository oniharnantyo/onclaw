package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

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

	// turn, when armed, is what Submit returns instead of the inert zero
	// turn (whose done channel is nil and never resolves); announcer tests
	// arm one to drive the turn's terminal outcome.
	turn *agents.Turn
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
	if m.turn != nil {
		return m.turn, nil
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
func (m *fakeAgentsManager) TurnState(context.Context, string) (bool, string) {
	panicUnexpected()
	return false, ""
}
func (m *fakeAgentsManager) RunningSessions(context.Context) []string {
	panicUnexpected()
	return nil
}

func panicUnexpected() error {
	panic("unexpected call")
}

// armTurn makes Submit return a real, finishable Turn carrying id. The
// returned func resolves it with the given terminal outcome — the same
// writes the runtime's unexported finish performs, done reflectively here
// because agents.Turn's fields (and finish) are unexported.
func (m *fakeAgentsManager) armTurn(t *testing.T, id string) func(error) {
	t.Helper()
	turn := &agents.Turn{}
	v := reflect.ValueOf(turn).Elem()
	setTurnField(t, v, "id", id)
	done := make(chan struct{})
	setTurnField(t, v, "done", done)
	m.turn = turn
	return func(err error) {
		setTurnField(t, v, "err", err)
		close(done)
	}
}

// setTurnField writes one unexported agents.Turn field for the controllable
// turn armTurn builds; tests never touch live runtime turns. A nil val (the
// completed outcome) writes the field's zero value.
func setTurnField(t *testing.T, v reflect.Value, name string, val any) {
	t.Helper()
	f := v.FieldByName(name)
	require.True(t, f.IsValid(), "agents.Turn has no field %q", name)
	rv := reflect.ValueOf(val)
	if !rv.IsValid() {
		rv = reflect.Zero(f.Type())
	}
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(rv.Convert(f.Type()))
}

// syncBuffer is a race-safe buffer for capturing the default logger's
// output: the adapter's announce goroutine writes it while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// announceCall is one AnnounceRunCompletion invocation's recorded shape.
type announceCall struct {
	scheduleName string
	outcome      string
	excerpt      string
}

// fakeAnnouncer records announcements on a channel so tests wait for the
// adapter's goroutine deterministically instead of polling.
type fakeAnnouncer struct {
	err   error
	calls chan announceCall
}

func newFakeAnnouncer() *fakeAnnouncer {
	return &fakeAnnouncer{calls: make(chan announceCall, 8)}
}

func (a *fakeAnnouncer) AnnounceRunCompletion(_ context.Context, scheduleName, outcome, excerpt string) error {
	a.calls <- announceCall{scheduleName: scheduleName, outcome: outcome, excerpt: excerpt}
	return a.err
}

// waitCall returns the next recorded announcement, failing the test when the
// announcer goroutine never calls.
func (a *fakeAnnouncer) waitCall(t *testing.T) announceCall {
	t.Helper()
	select {
	case c := <-a.calls:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("completion announce never arrived")
		return announceCall{}
	}
}

// newScheduleRuns wires the adapter over the fake manager and the real
// sessions service over the fake store.
func newScheduleRuns(t *testing.T) (*scheduleRuns, *fakeAgentsManager) {
	t.Helper()
	svc := sessions.NewService(fake.New())
	mgr := &fakeAgentsManager{sessions: svc}
	return &scheduleRuns{runtime: mgr, sessions: svc}, mgr
}

// newAnnouncingScheduleRuns binds a fake announcer and creates the titled
// scheduled session the fire path's Create produces, returning its id.
func newAnnouncingScheduleRuns(t *testing.T) (*scheduleRuns, *fakeAgentsManager, *fakeAnnouncer, string) {
	t.Helper()
	runs, mgr := newScheduleRuns(t)
	ann := newFakeAnnouncer()
	runs.announcer = ann
	id, err := runs.Create(context.Background(), "agt-1", "Standup — 09:00")
	require.NoError(t, err)
	return runs, mgr, ann, id
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

func TestScheduleRuns_Submit_NilAnnouncerIsInert(t *testing.T) {
	runs, mgr := newScheduleRuns(t)

	id, err := runs.Create(context.Background(), "agt-1", "Standup — 09:00")
	require.NoError(t, err)
	finish := mgr.armTurn(t, "turn-1")

	require.NoError(t, runs.Submit(context.Background(), id, "prompt"))
	finish(nil) // no announcer bound: resolving the turn must not announce or panic
}

func TestScheduleRuns_Submit_AnnouncesCompletionAfterDone(t *testing.T) {
	runs, mgr, ann, id := newAnnouncingScheduleRuns(t)
	finish := mgr.armTurn(t, "turn-1")
	require.NoError(t, runs.sessions.Append(context.Background(), id, []sessions.Event{
		{TurnID: "turn-0", Kind: sessions.KindAssistantMessage, Text: "an earlier turn's reply"},
		{TurnID: "turn-1", Kind: sessions.KindAssistantMessage, Text: "standup posted"},
	}))

	require.NoError(t, runs.Submit(context.Background(), id, "post the standup"))

	// The turn has not resolved yet, so the goroutine must still be waiting
	// on Done (deterministic: done only closes at finish).
	select {
	case c := <-ann.calls:
		t.Fatalf("announced before Done: %+v", c)
	case <-time.After(25 * time.Millisecond):
	}

	finish(nil)

	c := ann.waitCall(t)
	require.Equal(t, "Standup — 09:00", c.scheduleName)
	require.Equal(t, "completed", c.outcome)
	require.Equal(t, "standup posted", c.excerpt) // this turn's reply, not turn-0's
}

func TestScheduleRuns_Submit_AnnounceExcerptTruncatedTo400Runes(t *testing.T) {
	runs, mgr, ann, id := newAnnouncingScheduleRuns(t)
	finish := mgr.armTurn(t, "turn-1")
	require.NoError(t, runs.sessions.Append(context.Background(), id, []sessions.Event{
		{TurnID: "turn-1", Kind: sessions.KindAssistantMessage, Text: strings.Repeat("日", maxExcerptRunes+50)},
	}))

	require.NoError(t, runs.Submit(context.Background(), id, "prompt"))
	finish(nil)

	c := ann.waitCall(t)
	require.Len(t, []rune(c.excerpt), maxExcerptRunes)
}

func TestTruncateRunes_NeverSplitsARune(t *testing.T) {
	t.Run("shorter strings pass through", func(t *testing.T) {
		require.Equal(t, "done", truncateRunes("done", maxExcerptRunes))
	})

	t.Run("exactly max is unchanged", func(t *testing.T) {
		s := strings.Repeat("x", maxExcerptRunes)
		require.Equal(t, s, truncateRunes(s, maxExcerptRunes))
	})

	t.Run("ascii overflow clips to max", func(t *testing.T) {
		got := truncateRunes(strings.Repeat("x", maxExcerptRunes+1), maxExcerptRunes)
		require.Len(t, got, maxExcerptRunes)
	})

	t.Run("multibyte overflow clips to max runes", func(t *testing.T) {
		got := truncateRunes(strings.Repeat("日", maxExcerptRunes+1), maxExcerptRunes)
		require.Len(t, []rune(got), maxExcerptRunes)
	})
}

func TestScheduleRuns_Submit_AnnouncerErrorIsLoggedAndSwallowed(t *testing.T) {
	runs, mgr, ann, id := newAnnouncingScheduleRuns(t)
	ann.err = errInject

	// Capture the default logger: the failure must surface in the log, not
	// vanish. Nothing in this package runs parallel, so the swap is safe.
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	finish := mgr.armTurn(t, "turn-1")
	require.NoError(t, runs.Submit(context.Background(), id, "prompt"))
	finish(nil)

	c := ann.waitCall(t)
	require.Equal(t, "completed", c.outcome)
	// The log lands after AnnounceRunCompletion returns, which is
	// asynchronous to the recorded call received above.
	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "completion announce failed") &&
			strings.Contains(buf.String(), errInject.Error())
	}, 2*time.Second, 5*time.Millisecond, "the announcer failure was logged")
}

func TestScheduleRuns_Submit_AnnouncesFailedAndCancelledOutcomes(t *testing.T) {
	t.Run("failed turn announces the failure text as excerpt", func(t *testing.T) {
		runs, mgr, ann, id := newAnnouncingScheduleRuns(t)
		finish := mgr.armTurn(t, "turn-1")

		require.NoError(t, runs.Submit(context.Background(), id, "prompt"))
		finish(errInject)

		c := ann.waitCall(t)
		require.Equal(t, "failed", c.outcome)
		// No assistant message exists; the terminal error is the only
		// content a failed run can announce.
		require.Equal(t, errInject.Error(), c.excerpt)
	})

	t.Run("cancelled turn announces cancelled", func(t *testing.T) {
		runs, mgr, ann, id := newAnnouncingScheduleRuns(t)
		finish := mgr.armTurn(t, "turn-1")

		require.NoError(t, runs.Submit(context.Background(), id, "prompt"))
		finish(agents.ErrTurnCancelled)

		c := ann.waitCall(t)
		require.Equal(t, "cancelled", c.outcome)
		require.Empty(t, c.excerpt)
	})
}

func TestScheduleRuns_Submit_AnnounceSkippedWhenSessionUnreadable(t *testing.T) {
	runs, mgr := newScheduleRuns(t)
	ann := newFakeAnnouncer()
	runs.announcer = ann
	finish := mgr.armTurn(t, "turn-1")

	// The session id was never registered with the sessions service (the
	// skipPersist path): best-effort means log and drop, never call.
	require.NoError(t, runs.Submit(context.Background(), "ghost", "prompt"))
	finish(nil)

	select {
	case c := <-ann.calls:
		t.Fatalf("announced for an unreadable session: %+v", c)
	case <-time.After(25 * time.Millisecond):
	}
}
