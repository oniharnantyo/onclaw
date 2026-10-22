package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// The fake must satisfy the same ports the sqlite adapter satisfies.
var _ store.Store = (*fake.Store)(nil)

func newStore(t *testing.T) *fake.Store {
	t.Helper()
	return fake.New()
}

// TestAgents_CRUD round-trips the agent aggregate including the not-found
// paths of every operation.
func TestAgents_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	agent := &store.Agent{Name: "atlas", ProviderID: "p1", Model: "claude-x", Tools: []string{"web"}}
	require.NoError(t, s.Agents().Create(ctx, agent))
	require.NotEmpty(t, agent.ID)
	require.False(t, agent.CreatedAt.IsZero())

	got, err := s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "atlas", got.Name)
	require.Equal(t, []string{"web"}, got.Tools)

	got.Tools[0] = "mutated" // must not alias internal state
	again, err := s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"web"}, again.Tools)

	agent.Name = "atlas-2"
	require.NoError(t, s.Agents().Update(ctx, agent))

	list, err := s.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "atlas-2", list[0].Name)

	require.NoError(t, s.Agents().Delete(ctx, agent.ID))

	_, err = s.Agents().ByID(ctx, agent.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, s.Agents().Update(ctx, agent), store.ErrNotFound)
	require.ErrorIs(t, s.Agents().Delete(ctx, agent.ID), store.ErrNotFound)
}

// TestProvidersAndServers_CRUD covers the remaining id-keyed configuration
// aggregates (providers, MCP servers, hooks) with create/list/update/delete.
func TestProvidersAndServers_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	provider := &store.Provider{Name: "anthropic", Type: "anthropic", APIKey: "sk-1"}
	require.NoError(t, s.Providers().Create(ctx, provider))
	server := &store.MCPServer{Name: "fs", Transport: "stdio", Command: "npx", Env: map[string]string{"A": "b"}}
	require.NoError(t, s.MCPServers().Create(ctx, server))
	hook := &store.Hook{Name: "redact", Event: "before_turn", HandlerType: "http"}
	require.NoError(t, s.Hooks().Create(ctx, hook))

	require.NoError(t, s.Providers().Update(ctx, &store.Provider{ID: provider.ID, Name: "anthropic-2"}))
	require.NoError(t, s.MCPServers().Update(ctx, &store.MCPServer{ID: server.ID, Name: "fs-2"}))
	require.NoError(t, s.Hooks().Update(ctx, &store.Hook{ID: hook.ID, Name: "redact-2"}))

	providers, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Len(t, providers, 1)
	require.Equal(t, "anthropic-2", providers[0].Name)

	servers, err := s.MCPServers().List(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	require.Equal(t, "fs-2", servers[0].Name)

	hooks, err := s.Hooks().List(ctx)
	require.NoError(t, err)
	require.Len(t, hooks, 1)
	require.Equal(t, "redact-2", hooks[0].Name)

	require.NoError(t, s.Providers().Delete(ctx, provider.ID))
	require.NoError(t, s.MCPServers().Delete(ctx, server.ID))
	require.NoError(t, s.Hooks().Delete(ctx, hook.ID))
	_, err = s.Providers().ByID(ctx, provider.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.MCPServers().ByID(ctx, server.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, s.Hooks().Delete(ctx, hook.ID), store.ErrNotFound)
}

// TestSessionEvents_AppendReplayOrdered covers the session event contract:
// the store assigns monotonically increasing sequences per session, replay
// (LoadEvents) returns them in order, and re-appending a known EventID is a
// no-op.
func TestSessionEvents_AppendReplayOrdered(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	batch := func(ids ...string) []store.SessionEvent {
		out := make([]store.SessionEvent, 0, len(ids))
		for _, id := range ids {
			out = append(out, store.SessionEvent{EventID: id, Kind: "message", Payload: []byte(`{"x":1}`)})
		}
		return out
	}

	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1", batch("e1", "e2")))
	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1", batch("e3")))
	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1", batch("e2", "e4"))) // e2 is a retry

	events, err := s.SessionEvents().LoadEvents(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, events, 4, "a retried EventID must not duplicate")
	for i, ev := range events {
		require.Equal(t, int64(i+1), ev.Seq, "sequences must be 1..n in order")
		require.Equal(t, "sess-1", ev.SessionID)
	}
	require.Equal(t, "e4", events[3].EventID)

	// Session isolation and unknown sessions.
	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-2", batch("e1")))
	other, err := s.SessionEvents().LoadEvents(ctx, "sess-2")
	require.NoError(t, err)
	require.Len(t, other, 1)

	unknown, err := s.SessionEvents().LoadEvents(ctx, "nope")
	require.NoError(t, err)
	require.Empty(t, unknown)
	require.NoError(t, s.SessionEvents().AppendEvents(ctx, "sess-1", nil))
}

// TestCheckpoints_SetGetDelete covers checkpoint storage including the
// absent-read and absent-delete conventions.
func TestCheckpoints_SetGetDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	data, ok, err := s.Checkpoints().Get(ctx, "cp-1")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, data)

	require.NoError(t, s.Checkpoints().Set(ctx, "cp-1", []byte("snapshot-1")))
	require.NoError(t, s.Checkpoints().Set(ctx, "cp-1", []byte("snapshot-2")))

	data, ok, err = s.Checkpoints().Get(ctx, "cp-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("snapshot-2"), data)

	require.NoError(t, s.Checkpoints().Delete(ctx, "cp-1"))
	require.ErrorIs(t, s.Checkpoints().Delete(ctx, "cp-1"), store.ErrNotFound)
}

// TestGatewaySettings_AbsentThenSaved covers the singleton convention: Get
// before any Save is (nil, nil); Save then Get returns the saved values.
func TestGatewaySettings_AbsentThenSaved(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	settings, err := s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.Nil(t, settings)

	require.NoError(t, s.GatewaySettings().Save(ctx, &store.GatewaySettings{Enabled: true, DefaultAgentID: "a1"}))

	settings, err = s.GatewaySettings().Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.True(t, settings.Enabled)
	require.Equal(t, "a1", settings.DefaultAgentID)
}

// TestSkills_RegistryLifecycle covers the skill registry: upsert, ordered
// list, enable toggle, delete, and the not-found paths.
func TestSkills_RegistryLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	require.NoError(t, s.Skills().Upsert(ctx, &store.Skill{Name: "summarize", Source: "authored"}))
	require.NoError(t, s.Skills().Upsert(ctx, &store.Skill{Name: "translate", Source: "authored"}))

	list, err := s.Skills().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "summarize", list[0].Name, "list must be ordered by name")

	require.NoError(t, s.Skills().SetEnabled(ctx, "summarize", false))
	skill, err := s.Skills().GetByName(ctx, "summarize")
	require.NoError(t, err)
	require.False(t, skill.Enabled)

	require.ErrorIs(t, s.Skills().SetEnabled(ctx, "nope", true), store.ErrNotFound)

	require.NoError(t, s.Skills().Delete(ctx, "translate"))
	_, err = s.Skills().GetByName(ctx, "translate")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestStore_WithTxRunsFn covers the fake's transaction passthrough: fn runs
// once against the same store and its error propagates.
func TestStore_WithTxRunsFn(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	inside := false
	err := s.WithTx(ctx, func(tx store.Store) error {
		inside = true
		return tx.Providers().Create(ctx, &store.Provider{Name: "p"})
	})
	require.NoError(t, err)
	require.True(t, inside)

	providers, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Len(t, providers, 1)

	sentinel := errors.New("boom")
	err = s.WithTx(ctx, func(tx store.Store) error { return sentinel })
	require.ErrorIs(t, err, sentinel)
}

// TestToolStates_GlobalToggleRoundTrip covers the fake's tool toggle store:
// absence of a row is the default-on state, upserted rows round-trip, and
// the list is ordered by name.
func TestToolStates_GlobalToggleRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	enabled, err := s.ToolStates().Enabled(ctx, "http_get")
	require.NoError(t, err)
	require.True(t, enabled, "a tool with no stored row is enabled")

	require.NoError(t, s.ToolStates().SetEnabled(ctx, "http_get", false))
	require.NoError(t, s.ToolStates().SetEnabled(ctx, "current_time", true))

	enabled, err = s.ToolStates().Enabled(ctx, "http_get")
	require.NoError(t, err)
	require.False(t, enabled)

	list, err := s.ToolStates().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "current_time", list[0].Name, "list must be ordered by name")
	require.True(t, list[0].Enabled)
	require.Equal(t, "http_get", list[1].Name)
	require.False(t, list[1].Enabled)

	require.NoError(t, s.ToolStates().SetEnabled(ctx, "http_get", true))
	enabled, err = s.ToolStates().Enabled(ctx, "http_get")
	require.NoError(t, err)
	require.True(t, enabled)
}
