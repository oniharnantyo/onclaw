package notifications

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNotifier records the payloads the engine hands it; err makes every
// send fail so the best-effort drop path is observable.
type fakeNotifier struct {
	mu   sync.Mutex
	err  error
	sent []Payload
}

func (f *fakeNotifier) Send(p Payload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, p)
	return nil
}

// payloads snapshots the delivered payloads.
func (f *fakeNotifier) payloads() []Payload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Payload(nil), f.sent...)
}

var (
	agentNames    = map[string]string{"agent-1": "Scout"}
	scheduleNames = map[string]string{"sched-1": "Nightly Report"}
	groupNames    = map[string]string{"group-1": "Ops Crew"}
)

// testResolvers resolve the fixture ids to display names.
var testResolvers = NameResolvers{
	Agent:    func(id string) string { return agentNames[id] },
	Schedule: func(id string) string { return scheduleNames[id] },
	Group:    func(id string) string { return groupNames[id] },
}

// newTestEngine builds an engine over a fake notifier with an inspectable
// window-visible switch; all three are returned for assertions.
func newTestEngine(t *testing.T, names NameResolvers) (*Engine, *fakeNotifier, *atomic.Bool) {
	t.Helper()
	n := &fakeNotifier{}
	visible := &atomic.Bool{}
	return NewEngine(n, func() bool { return visible.Load() }, names), n, visible
}

// turnCandidate builds a turn terminal candidate for the matrix.
func turnCandidate(sessionID, turnID string, origin Origin, outcome, detail string) Candidate {
	return Candidate{
		ID:       TurnNotificationID(sessionID, turnID),
		Category: CategoryTurns,
		Origin:   origin,
		NameKey:  "agent-1",
		Outcome:  outcome,
		Detail:   detail,
		Route:    Route{Kind: RouteSession, ID: sessionID},
		ThreadID: sessionID,
	}
}

// parkCandidate builds a needs-attention park candidate.
func parkCandidate(sessionID, interruptID, outcome, detail string) Candidate {
	return Candidate{
		ID:       ParkNotificationID(sessionID, interruptID),
		Category: CategoryAttention,
		NameKey:  "agent-1",
		Outcome:  outcome,
		Detail:   detail,
		Route:    Route{Kind: RouteSession, ID: sessionID},
		ThreadID: sessionID,
	}
}

// scheduleCandidate builds a scheduled-run candidate resolving to its
// session's route.
func scheduleCandidate(scheduleID, runID, sessionID, outcome, detail string) Candidate {
	return Candidate{
		ID:       ScheduleNotificationID(scheduleID, runID),
		Category: CategorySchedules,
		NameKey:  scheduleID,
		Outcome:  outcome,
		Detail:   detail,
		Route:    Route{Kind: RouteSession, ID: sessionID},
		ThreadID: scheduleID,
	}
}

// groupCandidate builds a group task terminal candidate.
func groupCandidate(groupID, taskID, outcome string) Candidate {
	return Candidate{
		ID:       GroupNotificationID(groupID, taskID),
		Category: CategoryGroups,
		NameKey:  groupID,
		Outcome:  outcome,
		Route:    Route{Kind: RouteGroup, ID: groupID},
		ThreadID: groupID,
	}
}

// TestEngineEnqueue_TriggerSuppressionMatrix walks the spec delta's full
// trigger and suppression matrix: the four categories' delivered paths, the
// foreground-suppression rule, the dedup matrix drops, and the settings
// gates.
func TestEngineEnqueue_TriggerSuppressionMatrix(t *testing.T) {
	schedulerTurn := turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced")
	schedulerTurn.Origin = OriginScheduler
	telegramTurn := turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced")
	telegramTurn.Origin = OriginTelegram
	emptyOriginTurn := turnCandidate("sess-1", "turn-1", "", "completed", "All files synced")
	unknownCategory := turnCandidate("sess-1", "turn-1", OriginUser, "completed", "")
	unknownCategory.Category = Category("bogus")

	tests := []struct {
		name      string
		settings  *Values // nil keeps the engine's all-on defaults
		visible   bool    // the window-visible switch
		report    *Route  // nil never reports focus
		candidate Candidate
		wantSent  bool
		wantTitle string
		wantBody  string
	}{
		// — Turns —
		{
			name:      "user turn completed delivered while the window is hidden",
			candidate: turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "completed\nAll files synced",
		},
		{
			name:      "user turn delivered while viewing another session in a visible window",
			visible:   true,
			report:    &Route{Kind: RouteSession, ID: "sess-2"},
			candidate: turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "completed\nAll files synced",
		},
		{
			name:      "user turn suppressed viewing the same session in a visible window",
			visible:   true,
			report:    &Route{Kind: RouteSession, ID: "sess-1"},
			candidate: turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
			wantSent:  false,
		},
		{
			name:      "user turn delivered with the window visible and no focus ever reported",
			visible:   true,
			candidate: turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "completed\nAll files synced",
		},
		{
			name:      "user turn delivered while hidden despite a matching last focus",
			visible:   false,
			report:    &Route{Kind: RouteSession, ID: "sess-1"},
			candidate: turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "completed\nAll files synced",
		},
		{
			name:      "failed turn carries the error detail",
			candidate: turnCandidate("sess-1", "turn-2", OriginUser, "failed", "provider timeout"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "failed\nprovider timeout",
		},
		{
			name:      "cancelled turn without detail bodies the outcome alone",
			candidate: turnCandidate("sess-1", "turn-3", OriginUser, "cancelled", ""),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "cancelled",
		},
		{
			name:      "scheduler-origin turn dropped on the turn path",
			candidate: schedulerTurn,
			wantSent:  false,
		},
		{
			name:      "telegram-origin turn dropped entirely",
			candidate: telegramTurn,
			wantSent:  false,
		},
		{
			name:      "empty origin reads as user",
			candidate: emptyOriginTurn,
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "completed\nAll files synced",
		},
		// — Needs attention —
		{
			name:      "approval park delivered while the window is hidden",
			candidate: parkCandidate("sess-1", "int-1", "approval required", "rm -rf build"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "approval required\nrm -rf build",
		},
		{
			name:      "approval park suppressed viewing the same session",
			visible:   true,
			report:    &Route{Kind: RouteSession, ID: "sess-1"},
			candidate: parkCandidate("sess-1", "int-1", "approval required", "rm -rf build"),
			wantSent:  false,
		},
		{
			name:      "input park delivered while viewing another session",
			visible:   true,
			report:    &Route{Kind: RouteSession, ID: "sess-2"},
			candidate: parkCandidate("sess-1", "int-2", "input required", "Sign in at example.com"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "input required\nSign in at example.com",
		},
		// — Schedules —
		{
			name:      "missed schedule fire delivered naming the schedule",
			candidate: scheduleCandidate("sched-1", "run-5", "sess-9", "missed", ""),
			wantSent:  true,
			wantTitle: "Nightly Report",
			wantBody:  "missed",
		},
		{
			name:      "schedule completion suppressed viewing the schedule's session",
			visible:   true,
			report:    &Route{Kind: RouteSession, ID: "sess-9"},
			candidate: scheduleCandidate("sched-1", "run-5", "sess-9", "completed", "done"),
			wantSent:  false,
		},
		// — Groups —
		{
			name:      "group task terminal delivered naming the group",
			candidate: groupCandidate("group-1", "task-1", "completed"),
			wantSent:  true,
			wantTitle: "Ops Crew",
			wantBody:  "completed",
		},
		{
			name:      "group task terminal suppressed viewing the group",
			visible:   true,
			report:    &Route{Kind: RouteGroup, ID: "group-1"},
			candidate: groupCandidate("group-1", "task-1", "completed"),
			wantSent:  false,
		},
		// — Settings gates —
		{
			name:      "disabled category drops the candidate",
			settings:  &Values{Enabled: true, Turns: true, Attention: true, Schedules: false, Groups: true},
			candidate: scheduleCandidate("sched-1", "run-5", "sess-9", "completed", "done"),
			wantSent:  false,
		},
		{
			name:      "disabled schedules category leaves turns working",
			settings:  &Values{Enabled: true, Turns: true, Attention: true, Schedules: false, Groups: true},
			candidate: turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
			wantSent:  true,
			wantTitle: "Scout",
			wantBody:  "completed\nAll files synced",
		},
		{
			name:      "master off drops attention even with every category on",
			settings:  &Values{Enabled: false, Turns: true, Attention: true, Schedules: true, Groups: true},
			candidate: parkCandidate("sess-1", "int-1", "approval required", "rm -rf build"),
			wantSent:  false,
		},
		{
			name:      "unknown category dropped",
			candidate: unknownCategory,
			wantSent:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, n, visible := newTestEngine(t, testResolvers)
			if tt.settings != nil {
				e.SetSettings(*tt.settings)
			}
			visible.Store(tt.visible)
			if tt.report != nil {
				e.ReportFocus(tt.report.Kind, tt.report.ID)
			}

			e.Enqueue(tt.candidate)

			sent := n.payloads()
			if !tt.wantSent {
				assert.Empty(t, sent, "the candidate must not be delivered")
				return
			}
			require.Len(t, sent, 1, "exactly one notification must be delivered")
			assert.Equal(t, tt.candidate.ID, sent[0].ID)
			assert.Equal(t, tt.candidate.Category, sent[0].Category)
			assert.Equal(t, tt.wantTitle, sent[0].Title)
			assert.Equal(t, tt.wantBody, sent[0].Body)
			assert.Equal(t, tt.candidate.ThreadID, sent[0].ThreadID)
		})
	}
}

// TestEngineSetSettings_AppliesImmediately pins design D4's live-applied
// requirement: flag changes take effect on subsequent enqueues without
// re-constructing the engine, and the master flag off silences every
// category even with all category flags on.
func TestEngineSetSettings_AppliesImmediately(t *testing.T) {
	e, n, _ := newTestEngine(t, testResolvers)

	// The engine starts on DefaultValues: delivered without any SetSettings.
	e.Enqueue(turnCandidate("sess-1", "turn-1", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 1, "all-on defaults must deliver")

	// Turn category off, live.
	e.SetSettings(Values{Enabled: true, Attention: true, Schedules: true, Groups: true})
	e.Enqueue(turnCandidate("sess-1", "turn-2", OriginUser, "completed", ""))
	sent := n.payloads()
	require.Len(t, sent, 1, "the disabled category must drop the candidate")
	e.Enqueue(parkCandidate("sess-1", "int-1", "approval required", ""))
	require.Len(t, n.payloads(), 2, "the still-enabled attention category must deliver")

	// Re-enable turns, live.
	e.SetSettings(DefaultValues())
	e.Enqueue(turnCandidate("sess-1", "turn-3", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 3, "the re-enabled category must deliver")

	// Master off, live: nothing delivers even with every category on.
	e.SetSettings(Values{Enabled: false, Turns: true, Attention: true, Schedules: true, Groups: true})
	e.Enqueue(turnCandidate("sess-1", "turn-4", OriginUser, "completed", ""))
	e.Enqueue(parkCandidate("sess-1", "int-2", "approval required", ""))
	e.Enqueue(scheduleCandidate("sched-1", "run-1", "sess-9", "completed", ""))
	e.Enqueue(groupCandidate("group-1", "task-1", "completed"))
	assert.Len(t, n.payloads(), 3, "the master flag off must silence every category")
}

// TestEngineReportFocus_StaleAndAbsent pins design D3's positive-knowledge
// rule: absent, cleared, and malformed focus reports all deliver —
// suppression only ever comes from a well-formed matching report.
func TestEngineReportFocus_StaleAndAbsent(t *testing.T) {
	e, n, visible := newTestEngine(t, testResolvers)
	visible.Store(true)

	// Absent: focus never reported ⇒ deliver.
	e.Enqueue(turnCandidate("sess-1", "turn-1", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 1, "absent focus must deliver")

	// Cleared by an empty report ⇒ deliver.
	e.ReportFocus(RouteSession, "sess-1")
	e.ReportFocus("", "")
	e.Enqueue(turnCandidate("sess-1", "turn-2", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 2, "a cleared focus must deliver")

	// Cleared by an empty id ⇒ deliver.
	e.ReportFocus(RouteSession, "sess-1")
	e.ReportFocus(RouteSession, "")
	e.Enqueue(turnCandidate("sess-1", "turn-3", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 3, "an empty-id report must clear the mirror and deliver")

	// Cleared by an unknown kind ⇒ deliver.
	e.ReportFocus(RouteSession, "sess-1")
	e.ReportFocus(RouteKind("workspace"), "sess-1")
	e.Enqueue(turnCandidate("sess-1", "turn-4", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 4, "an unknown-kind report must clear the mirror and deliver")

	// Stale: the mirror holds another session ⇒ deliver.
	e.ReportFocus(RouteSession, "sess-2")
	e.Enqueue(turnCandidate("sess-1", "turn-5", OriginUser, "completed", ""))
	require.Len(t, n.payloads(), 5, "a stale mirror for another session must deliver")

	// Only the live matching report suppresses.
	e.ReportFocus(RouteSession, "sess-1")
	e.Enqueue(turnCandidate("sess-1", "turn-6", OriginUser, "completed", ""))
	assert.Len(t, n.payloads(), 5, "the live matching focus must suppress")
}

// TestEngineEnqueue_AnnouncementPath covers tasks.md 4.4: a scheduled-run
// completion candidate lands as a Schedules notification titled with the
// schedule name and carrying the outcome and excerpt; foreground viewing
// that schedule's session suppresses it; a failed run carries the error
// detail.
func TestEngineEnqueue_AnnouncementPath(t *testing.T) {
	e, n, visible := newTestEngine(t, testResolvers)

	completed := scheduleCandidate("sched-1", "run-7", "sess-9", "completed", "Nightly sync finished, 0 errors")
	e.Enqueue(completed)

	sent := n.payloads()
	require.Len(t, sent, 1, "the run completion must announce")
	assert.Equal(t, "Nightly Report", sent[0].Title, "the schedule name must title the notification")
	assert.Equal(t, "completed\nNightly sync finished, 0 errors", sent[0].Body)
	assert.Equal(t, CategorySchedules, sent[0].Category)
	assert.Equal(t, "sched-1", sent[0].ThreadID)

	// Schedule candidates resolve to their session's route for click-to-open.
	route, ok := e.ResolveRoute(completed.ID)
	require.True(t, ok, "the announced run must resolve its click route")
	assert.Equal(t, Route{Kind: RouteSession, ID: "sess-9"}, route)

	// Foreground viewing that schedule's session suppresses the next run.
	visible.Store(true)
	e.ReportFocus(RouteSession, "sess-9")
	e.Enqueue(scheduleCandidate("sched-1", "run-8", "sess-9", "completed", "second run"))
	assert.Len(t, n.payloads(), 1, "the suppressed run must not announce")

	// A failed run announces with the error detail once focus is gone.
	e.ReportFocus("", "")
	visible.Store(false)
	e.Enqueue(scheduleCandidate("sched-1", "run-9", "sess-9", "failed", "exit status 1"))
	sent = n.payloads()
	require.Len(t, sent, 2, "the failed run must announce")
	assert.Equal(t, "failed\nexit status 1", sent[1].Body)
}

// TestEngineEnqueue_SendErrorDropped pins the best-effort delivery contract
// (design D6): a send failure is dropped silently — no error propagates, the
// candidate is not recorded as delivered, and nothing is clickable.
func TestEngineEnqueue_SendErrorDropped(t *testing.T) {
	n := &fakeNotifier{err: errors.New("no notification daemon")}
	e := NewEngine(n, nil, testResolvers)

	c := turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced")
	e.Enqueue(c)

	assert.Empty(t, n.payloads(), "a failed send must not be recorded as delivered")
	_, ok := e.ResolveRoute(c.ID)
	require.False(t, ok, "undelivered notifications must not resolve a click route")
}

// TestEnginePayload_NameFallbacks pins the title fallbacks: unresolvable
// NameKeys and nil resolvers fall back to the route kind's label, and a nil
// windowVisible treats the window as not visible (nothing suppresses).
func TestEnginePayload_NameFallbacks(t *testing.T) {
	t.Run("nil windowVisible treats the window as not visible", func(t *testing.T) {
		n := &fakeNotifier{}
		e := NewEngine(n, nil, testResolvers)
		e.ReportFocus(RouteSession, "sess-1") // would suppress with a visible window
		e.Enqueue(turnCandidate("sess-1", "turn-1", OriginUser, "completed", ""))
		require.Len(t, n.payloads(), 1, "a nil visibility query must deliver")
		assert.Equal(t, "Scout", n.payloads()[0].Title)
	})

	t.Run("unresolvable agent name falls back to the session label", func(t *testing.T) {
		n := &fakeNotifier{}
		e := NewEngine(n, nil, NameResolvers{Agent: func(string) string { return "" }})
		e.Enqueue(turnCandidate("sess-1", "turn-1", OriginUser, "completed", ""))
		require.Len(t, n.payloads(), 1)
		assert.Equal(t, "Session", n.payloads()[0].Title)
	})

	t.Run("nil resolvers fall back to the route labels", func(t *testing.T) {
		n := &fakeNotifier{}
		e := NewEngine(n, nil, NameResolvers{})
		e.Enqueue(turnCandidate("sess-1", "turn-1", OriginUser, "completed", ""))
		e.Enqueue(groupCandidate("group-1", "task-1", "completed"))
		sent := n.payloads()
		require.Len(t, sent, 2)
		assert.Equal(t, "Session", sent[0].Title, "session routes fall back to the session label")
		assert.Equal(t, "Group", sent[1].Title, "group routes fall back to the group label")
	})

	t.Run("empty outcome degrades to the detail alone", func(t *testing.T) {
		n := &fakeNotifier{}
		e := NewEngine(n, nil, testResolvers)
		e.Enqueue(parkCandidate("sess-1", "int-1", "", "browser sign-in pending"))
		require.Len(t, n.payloads(), 1)
		assert.Equal(t, "browser sign-in pending", n.payloads()[0].Body)
	})
}

// TestEngine_ConcurrentAccessRaceSafe exercises the engine under concurrent
// settings writes, focus reports, visibility flips, enqueues, and route
// lookups; run with -race, it validates the concurrency contract. Delivery
// outcomes are intentionally unasserted — goroutines race by design here.
func TestEngine_ConcurrentAccessRaceSafe(t *testing.T) {
	e, n, visible := newTestEngine(t, testResolvers)

	const goroutines = 32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := fmt.Sprintf("s-%d", i)
			e.SetSettings(DefaultValues())
			e.ReportFocus(RouteSession, fmt.Sprintf("s-%d", i%4))
			visible.Store(i%2 == 0)
			e.Enqueue(turnCandidate(session, fmt.Sprintf("t-%d", i), OriginUser, "completed", ""))
			_, _ = e.ResolveRoute(TurnNotificationID(session, fmt.Sprintf("t-%d", i)))
		}(i)
	}
	wg.Wait()

	for _, p := range n.payloads() {
		assert.NotEmpty(t, p.ID)
		assert.NotEmpty(t, p.Title)
		assert.NotEmpty(t, p.Body)
	}
}
