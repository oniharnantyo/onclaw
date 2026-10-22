package agents

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The run-origin seam (spec add-scheduler "Scheduled runs are distinguishable
// to hooks", design D6): the origin the dispatch carries is the origin every
// hook event of that run stamps — a scheduler Submit reads as scheduler, the
// chat Send reads as user, and a user follow-up inside a scheduled session is
// still a user run.

// hookEventsFrom parses the concatenated event JSON payloads the command hook
// appended to path (one compact object per delivery, no separators).
func hookEventsFrom(t *testing.T, path string) []hooks.Event {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "the hook must have appended its event payloads")
	var evs []hooks.Event
	dec := json.NewDecoder(bytes.NewReader(b))
	for dec.More() {
		var ev hooks.Event
		require.NoError(t, dec.Decode(&ev))
		evs = append(evs, ev)
	}
	return evs
}

// waitHookPayload waits until the hook has appended a payload containing substr.
func waitHookPayload(t *testing.T, path, substr string) {
	t.Helper()
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(path)
		return err == nil && bytes.Contains(b, []byte(substr))
	}, 5*time.Second, 20*time.Millisecond, "the hook must have received its event")
}

// seedRunStartedHook wires one run_started command hook that appends each
// delivery's stdin JSON to markers — the same marker-file shape the other
// hook tests use, here so the origin field of the delivered payload is
// inspectable.
func seedRunStartedHook(t *testing.T, f *fixture, markers string) {
	t.Helper()
	seedHook(t, f.store, store.Hook{
		Name:        "saw-origin",
		Event:       hooks.EventRunStarted,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"cat >> ` + markers + `"}`,
		Enabled:     true,
	})
}

// TestSubmit_SchedulerOriginThenUserSend_HookOriginsFollow pins design D6 end
// to end through the command handler's stdin JSON: a scheduler-origin Submit
// delivers origin "scheduler", and a plain Send follow-up in the same session
// delivers origin "user" — origin distinguishes who started the run, not the
// session.
func TestSubmit_SchedulerOriginThenUserSend_HookOriginsFollow(t *testing.T) {
	markers := filepath.Join(t.TempDir(), "markers")
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	f.wireRealHooks()
	seedRunStartedHook(t, f, markers)
	sess := f.newSession(t)

	scheduled, err := f.mgr.Submit(t.Context(), SendRequest{
		SessionID: sess.ID,
		Message:   "morning digest",
		Origin:    hooks.OriginScheduler,
	})
	require.NoError(t, err)
	require.NoError(t, scheduled.Wait())
	waitHookPayload(t, markers, `"origin":"scheduler"`)

	// The user's follow-up message in the scheduled session is still a user
	// run (design D6).
	user, err := f.mgr.Send(t.Context(), sess.ID, "and now something else")
	require.NoError(t, err)
	require.NoError(t, user.Wait())
	waitHookPayload(t, markers, `"origin":"user"`)

	evs := hookEventsFrom(t, markers)
	require.Len(t, evs, 2, "one run_started delivery per run")
	require.Equal(t, hooks.OriginScheduler, evs[0].Origin)
	require.Equal(t, hooks.EventRunStarted, evs[0].Event)
	require.Equal(t, sess.ID, evs[0].SessionID)
	require.Equal(t, hooks.OriginUser, evs[1].Origin)
}

// TestSubmit_EmptyOrigin_ReadsAsUser pins the normalization contract: an
// origin-less Submit is a user run's hook events.
func TestSubmit_EmptyOrigin_ReadsAsUser(t *testing.T) {
	markers := filepath.Join(t.TempDir(), "markers")
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	f.wireRealHooks()
	seedRunStartedHook(t, f, markers)
	sess := f.newSession(t)

	turn, err := f.mgr.Submit(t.Context(), SendRequest{SessionID: sess.ID, Message: "hello"})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	waitHookPayload(t, markers, `"origin":"user"`)

	evs := hookEventsFrom(t, markers)
	require.Len(t, evs, 1)
	require.Equal(t, hooks.OriginUser, evs[0].Origin)
}
