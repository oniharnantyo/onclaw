package sessions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The ToolActivity payload contract (tasks.md 1.1, design D5): the terminal
// record carries the observed duration; records persisted before durations
// were recorded round-trip without one and re-marshal without the field.

// TestToolActivityDurationRoundTrip pins the duration field's wire shape: a
// stamped record marshals duration_ms and reads it back unchanged.
func TestToolActivityDurationRoundTrip(t *testing.T) {
	stamped := ToolActivity{
		CallID:     "call-1",
		Name:       "current_time",
		Status:     ToolStatusCompleted,
		DurationMs: 1500,
	}
	data, err := json.Marshal(stamped)
	require.NoError(t, err)
	require.Contains(t, string(data), `"duration_ms":1500`)

	var got ToolActivity
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, 1500, got.DurationMs)
}

// TestToolActivityLegacyRecordCarriesNoDuration pins the migration rule: a
// record persisted before durations existed (no duration_ms in its JSON)
// unmarshals to zero and re-marshals without the field — omitempty keeps the
// runtime from fabricating a latency for it.
func TestToolActivityLegacyRecordCarriesNoDuration(t *testing.T) {
	legacy := `{"call_id":"call-1","name":"current_time","status":"completed","result":"ok"}`
	var got ToolActivity
	require.NoError(t, json.Unmarshal([]byte(legacy), &got))
	require.Zero(t, got.DurationMs)

	remarshaled, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(remarshaled), "duration_ms")
}

// TestToolActivityTerminalWithStampedDuration guards the D10 invariant the
// field edit must not disturb: the running form stays live-only while a
// stamped terminal record is the persistable form.
func TestToolActivityTerminalWithStampedDuration(t *testing.T) {
	running := ToolActivity{CallID: "call-1", Status: ToolStatusRunning}
	require.False(t, running.Terminal())

	stamped := ToolActivity{CallID: "call-1", Status: ToolStatusCompleted, DurationMs: 42}
	require.True(t, stamped.Terminal())
}
