package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotificationIDSchemes pins the deterministic ID scheme (design D5):
// each trigger source's ID is stable and prefixed by its kind, so the click
// response's ID is a lookup key, not a heuristic.
func TestNotificationIDSchemes(t *testing.T) {
	tests := []struct {
		name string
		want string
		got  string
	}{
		{"turn", "turn:sess-1:turn-9", TurnNotificationID("sess-1", "turn-9")},
		{"park", "park:sess-1:int-3", ParkNotificationID("sess-1", "int-3")},
		{"schedule", "sched:sched-1:run-5", ScheduleNotificationID("sched-1", "run-5")},
		{"group", "group:group-1:task-2", GroupNotificationID("group-1", "task-2")},
		{"webhook", "webhook:sess-1:turn-9", WebhookNotificationID("sess-1", "turn-9")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}

// TestEngineResolveRoute_RoundTrip walks each ID scheme through a delivered
// candidate: the engine remembers the candidate's route under its
// deterministic ID, unknown and empty ids resolve nothing, and suppressed
// candidates never become clickable.
func TestEngineResolveRoute_RoundTrip(t *testing.T) {
	e, n, visible := newTestEngine(t, testResolvers)

	schemes := []Candidate{
		turnCandidate("sess-1", "turn-1", OriginUser, "completed", "All files synced"),
		parkCandidate("sess-2", "int-1", "approval required", "git push"),
		scheduleCandidate("sched-1", "run-2", "sess-9", "completed", "done"),
		groupCandidate("group-1", "task-1", "completed"),
	}
	for _, c := range schemes {
		e.Enqueue(c)
		route, ok := e.ResolveRoute(c.ID)
		require.True(t, ok, "the delivered candidate %s must resolve its route", c.ID)
		assert.Equal(t, c.Route, route)
	}
	require.Len(t, n.payloads(), 4, "every scheme's candidate must deliver")

	// Unknown and empty ids resolve nothing.
	_, ok := e.ResolveRoute(TurnNotificationID("nope", "nope"))
	require.False(t, ok, "an unknown id must not resolve")
	_, ok = e.ResolveRoute("")
	require.False(t, ok, "an empty id must not resolve")

	// Suppressed candidates are never delivered, so never clickable.
	visible.Store(true)
	e.ReportFocus(RouteSession, "sess-1")
	suppressed := turnCandidate("sess-1", "turn-2", OriginUser, "completed", "")
	e.Enqueue(suppressed)
	assert.Len(t, n.payloads(), 4, "the suppressed candidate must not deliver")
	_, ok = e.ResolveRoute(suppressed.ID)
	require.False(t, ok, "a suppressed candidate must not resolve a click route")
}
