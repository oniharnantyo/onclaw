package agents

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// usageReply grows its prompt accounting with the size of the input it is
// handed: more context in, more prompt tokens reported.
func usageReply(input []*schema.Message) fakeReply {
	prompt := 0
	for _, msg := range input {
		prompt += 100 * (len(msg.Content)/10 + 1)
	}
	return fakeReply{
		text:             "noted",
		promptTokens:     prompt,
		completionTokens: 10,
	}
}

// TestManager_ContextUsageGrowsAcrossTurns is 4.4's acceptance test: usage
// is tracked per session from the model's reported token counts, grows as
// turns accumulate, is zero before any turn, and the limit comes from the
// model catalog's declared window for the agent's model.
func TestManager_ContextUsageGrowsAcrossTurns(t *testing.T) {
	f := newFixture(t, usageReply)
	sess := f.newSession(t)

	usage, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, 0, usage.Used, "a fresh session has used nothing")
	require.Equal(t, 200000, usage.Limit, "the limit is the catalog window of claude-sonnet-4-5")

	turn1, err := f.mgr.Send(t.Context(), sess.ID, "hello there")
	require.NoError(t, err)
	require.NoError(t, turn1.Wait())

	afterTurn1, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Positive(t, afterTurn1.Used)

	turn2, err := f.mgr.Send(t.Context(), sess.ID, "and now for something completely different, please continue")
	require.NoError(t, err)
	require.NoError(t, turn2.Wait())

	afterTurn2, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Greater(t, afterTurn2.Used, afterTurn1.Used,
		"usage must grow as the context accumulates turns")
	require.Equal(t, 200000, afterTurn2.Limit)
}

// TestManager_ContextUsageIsUsageDerived pins the accounting rule: Used is
// the last completed turn's final model input plus its output — derived from
// persisted turn_completed events, so it survives restarts.
func TestManager_ContextUsageIsUsageDerived(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 300, completionTokens: 20}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hi")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	usage, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, 300+20, usage.Used, "final input + output of the last completed turn")
}

// TestManager_ContextUsageLastTurnUsage is 1.2's acceptance test (design
// D7): the exposed usage carries the last completed turn's input and output
// token counts beside the used total, and both are zero before any turn.
func TestManager_ContextUsageLastTurnUsage(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 300, completionTokens: 20}
	})
	sess := f.newSession(t)

	before, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Zero(t, before.LastInput, "no completed turn yet: no last input")
	require.Zero(t, before.LastOutput, "no completed turn yet: no last output")

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hi")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	usage, err := f.mgr.ContextUsage(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, 300, usage.LastInput, "the completed turn's final input")
	require.Equal(t, 20, usage.LastOutput, "the completed turn's output")
	require.Equal(t, 300+20, usage.Used, "the total still reads the same block")
}

// TestManager_ContextUsageBelowCompactionHasNoMarker guards the boundary
// between 4.4 and 4.5: ordinary growing usage must not compact anything.
func TestManager_ContextUsageBelowCompactionHasNoMarker(t *testing.T) {
	f := newFixture(t, usageReply)
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, eventsOfKind(events, sessions.KindCompaction))

	_, ok, err := f.sessions.Checkpoint(t.Context(), sess.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestManager_ContextWindowOverride is the context-window override's
// acceptance test (design D8): an agent's explicit override wins over the
// catalog, an unset override resolves the model's catalog window, and a
// model outside the catalog resolves the conservative default.
func TestManager_ContextWindowOverride(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	})
	ctx := t.Context()

	// A provider whose catalog windows differ from the default, so the
	// catalog-value case is distinguishable from the default fallback.
	openai := &store.Provider{Name: "openai", Type: ProviderOpenAI, APIKey: "key-test", Enabled: true}
	require.NoError(t, f.store.Providers().Create(ctx, openai))

	sessionLimit := func(agent *store.Agent) int {
		t.Helper()
		require.NoError(t, f.store.Agents().Create(ctx, agent))
		sess, err := f.mgr.CreateSession(ctx, agent.ID)
		require.NoError(t, err)
		usage, err := f.mgr.ContextUsage(ctx, sess.ID)
		require.NoError(t, err)
		return usage.Limit
	}

	// Unset override (0): the catalog window of the agent's model.
	require.Equal(t, 128000, sessionLimit(&store.Agent{
		Name: "cataloged", Slug: "cataloged", ProviderID: openai.ID, Model: "gpt-4o",
	}), "an unset override resolves the catalog window")

	// Catalog miss: the conservative default window.
	require.Equal(t, DefaultContextWindow, sessionLimit(&store.Agent{
		Name: "unknown", Slug: "unknown-model", ProviderID: openai.ID, Model: "mystery-model",
	}), "a model outside the catalog resolves the default window")

	// Set override: it wins over the catalog value.
	require.Equal(t, 12345, sessionLimit(&store.Agent{
		Name: "overridden", Slug: "overridden", ProviderID: openai.ID, Model: "gpt-4o", ContextWindow: 12345,
	}), "the agent's explicit override wins over the catalog")
}
