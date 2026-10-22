package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestAgents_CRUDAndNotFound round-trips the agent aggregate: JSON list
// columns survive, app-managed timestamps survive update, and every
// not-found path returns the store sentinel.
func TestAgents_CRUDAndNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{
		Name:          "atlas",
		Slug:          "atlas",
		Description:   "The navigator.",
		Brief:         "A terse scout who maps unknown terrain.",
		Avatar:        map[string]any{"sex": "woman", "bgColor": "#74D153"},
		PromptsStatus: store.PromptsStatusReady,
		PromptsError:  "",
		ProviderID:    "prov-1",
		Model:         "claude-x",
		Temperature:   0.5,
		MaxTokens:     1024,
		ContextWindow: 200000,
		Tools:         []string{"web", "calc"},
		Skills:        []string{"summarize"},
		MCPServers:    []string{"fs"},
	}
	require.NoError(t, s.Agents().Create(ctx, agent))
	require.NotEmpty(t, agent.ID)

	got, err := s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, *agent, *got, "agent must round-trip exactly")

	agent.Model = "claude-y"
	agent.Description = "The navigator, updated."
	agent.ContextWindow = 100000
	agent.PromptsStatus = store.PromptsStatusFailed
	agent.PromptsError = "generation failed"
	agent.Tools = []string{"web"}
	agent.Avatar = map[string]any{"sex": "man"}
	require.NoError(t, s.Agents().Update(ctx, agent))

	got, err = s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "claude-y", got.Model)
	require.Equal(t, "The navigator, updated.", got.Description)
	require.Equal(t, 100000, got.ContextWindow)
	require.Equal(t, store.PromptsStatusFailed, got.PromptsStatus)
	require.Equal(t, "generation failed", got.PromptsError)
	require.Equal(t, []string{"web"}, got.Tools)
	require.Equal(t, map[string]any{"sex": "man"}, got.Avatar)
	require.Equal(t, agent.CreatedAt, got.CreatedAt, "created_at is immutable on update")

	list, err := s.Agents().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, s.Agents().Delete(ctx, agent.ID))
	_, err = s.Agents().ByID(ctx, agent.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, s.Agents().Update(ctx, agent), store.ErrNotFound)
	require.ErrorIs(t, s.Agents().Delete(ctx, agent.ID), store.ErrNotFound)

	_, err = s.Agents().ByID(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestAgents_SlugUniqueAndEmptyCoexist covers the slug column's semantics:
// a duplicate slug is rejected by the UNIQUE index, while rows without a
// slug (NULL, the pre-backfill state) coexist freely.
func TestAgents_SlugUniqueAndEmptyCoexist(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	first := &store.Agent{Name: "Scout", Slug: "scout"}
	require.NoError(t, s.Agents().Create(ctx, first))

	err := s.Agents().Create(ctx, &store.Agent{Name: "Other", Slug: "scout"})
	require.Error(t, err, "duplicate slugs must be rejected")

	// Empty slugs store as NULL: any number of slug-less rows may coexist.
	nameless1 := &store.Agent{Name: "No Slug One"}
	nameless2 := &store.Agent{Name: "No Slug Two"}
	require.NoError(t, s.Agents().Create(ctx, nameless1))
	require.NoError(t, s.Agents().Create(ctx, nameless2))

	got, err := s.Agents().ByID(ctx, nameless2.ID)
	require.NoError(t, err)
	require.Empty(t, got.Slug)
	// No stored config: the avatar column reads back nil so surfaces derive
	// the face from the id.
	require.Nil(t, got.Avatar)

	// An agent may take a slug another row has vacated.
	require.NoError(t, s.Agents().Delete(ctx, first.ID))
	freed := &store.Agent{Name: "Scout II", Slug: "scout"}
	require.NoError(t, s.Agents().Create(ctx, freed))
}

// TestAgents_EmptyPromptsStatusStampsReady pins the Create normalization:
// an agent created without an explicit prompt status reads ready, so no row
// ever carries an empty lifecycle state.
func TestAgents_EmptyPromptsStatusStampsReady(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	agent := &store.Agent{Name: "atlas", Slug: "atlas"}
	require.NoError(t, s.Agents().Create(ctx, agent))
	require.Equal(t, store.PromptsStatusReady, agent.PromptsStatus)

	got, err := s.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, store.PromptsStatusReady, got.PromptsStatus)
}

// TestProviders_CRUDAndDuplicateName covers provider storage plus the unique
// name constraint (duplicate names are rejected as plain errors).
func TestProviders_CRUDAndDuplicateName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	provider := &store.Provider{Name: "anthropic", Type: "anthropic", APIKey: "sk-1", Enabled: true}
	require.NoError(t, s.Providers().Create(ctx, provider))
	require.NotEmpty(t, provider.ID)

	err := s.Providers().Create(ctx, &store.Provider{Name: "anthropic"})
	require.Error(t, err, "duplicate provider names must be rejected")

	got, err := s.Providers().ByID(ctx, provider.ID)
	require.NoError(t, err)
	require.Equal(t, *provider, *got)

	provider.APIKey = "sk-2"
	require.NoError(t, s.Providers().Update(ctx, provider))
	got, err = s.Providers().ByID(ctx, provider.ID)
	require.NoError(t, err)
	require.Equal(t, "sk-2", got.APIKey)

	list, err := s.Providers().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, s.Providers().Delete(ctx, provider.ID))
	_, err = s.Providers().ByID(ctx, provider.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestMCPServers_JSONColumnsRoundTrip covers the list and map columns (args,
// env, headers) surviving a write/read cycle exactly.
func TestMCPServers_JSONColumnsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	server := &store.MCPServer{
		Name:        "filesystem",
		Transport:   "stdio",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-fs"},
		Env:         map[string]string{"NODE_ENV": "production"},
		URL:         "",
		Headers:     map[string]string{"Authorization": "Bearer t"},
		Enabled:     true,
		Status:      "connected",
		StatusError: "",
		ToolCount:   11,
	}
	require.NoError(t, s.MCPServers().Create(ctx, server))

	got, err := s.MCPServers().ByID(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, *server, *got)

	list, err := s.MCPServers().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, s.MCPServers().Delete(ctx, server.ID))
	require.ErrorIs(t, s.MCPServers().Delete(ctx, server.ID), store.ErrNotFound)
}

// TestHooks_CRUDOrderedByPosition covers hook storage and the position/name
// list order.
func TestHooks_CRUDOrderedByPosition(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	late := &store.Hook{Name: "z-late", Event: "after_turn", Matcher: `{}`, HandlerType: "http", Position: 2}
	early := &store.Hook{Name: "a-early", Event: "before_turn", Matcher: `{}`, HandlerType: "http", Position: 1}
	require.NoError(t, s.Hooks().Create(ctx, late))
	require.NoError(t, s.Hooks().Create(ctx, early))

	hooks, err := s.Hooks().List(ctx)
	require.NoError(t, err)
	require.Len(t, hooks, 2)
	require.Equal(t, "a-early", hooks[0].Name)
	require.Equal(t, "z-late", hooks[1].Name)

	early.Config = `{"url":"http://x"}`
	require.NoError(t, s.Hooks().Update(ctx, early))
	hooks, err = s.Hooks().List(ctx)
	require.NoError(t, err)
	require.Equal(t, `{"url":"http://x"}`, hooks[0].Config)

	require.NoError(t, s.Hooks().Delete(ctx, late.ID))
	require.ErrorIs(t, s.Hooks().Delete(ctx, late.ID), store.ErrNotFound)
}

// TestCheckpoints_SetGetDelete covers checkpoint storage: upsert semantics,
// the absent-read convention, and the not-found delete.
func TestCheckpoints_SetGetDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

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

// TestSkills_RegistryLifecycle covers the skill registry: upsert keeps the
// original created_at, toggle, ordered list, and delete.
func TestSkills_RegistryLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.Skills().Upsert(ctx, &store.Skill{Name: "summarize", Source: "authored", Version: "0.1.0"}))
	first, err := s.Skills().GetByName(ctx, "summarize")
	require.NoError(t, err)

	time.Sleep(2 * time.Millisecond) // timestamps are RFC3339Nano; nudge the clock for update
	require.NoError(t, s.Skills().Upsert(ctx, &store.Skill{Name: "summarize", Description: "updated", Source: "authored"}))
	second, err := s.Skills().GetByName(ctx, "summarize")
	require.NoError(t, err)
	require.Equal(t, first.CreatedAt, second.CreatedAt, "upsert must keep the original created_at")
	require.Equal(t, "updated", second.Description)
	require.True(t, second.UpdatedAt.After(first.UpdatedAt))

	require.NoError(t, s.Skills().Upsert(ctx, &store.Skill{Name: "translate", Source: "upload"}))

	skills, err := s.Skills().List(ctx)
	require.NoError(t, err)
	require.Len(t, skills, 2)
	require.Equal(t, "summarize", skills[0].Name, "list must be ordered by name")

	require.NoError(t, s.Skills().SetEnabled(ctx, "summarize", false))
	skill, err := s.Skills().GetByName(ctx, "summarize")
	require.NoError(t, err)
	require.False(t, skill.Enabled)
	require.ErrorIs(t, s.Skills().SetEnabled(ctx, "missing", true), store.ErrNotFound)

	require.NoError(t, s.Skills().Delete(ctx, "translate"))
	require.ErrorIs(t, s.Skills().Delete(ctx, "translate"), store.ErrNotFound)
}

// TestToolStates_GlobalToggleRoundTrip covers the global tool toggle store:
// absence of a row is the default-on state, an upserted row round-trips,
// and the list is ordered by name.
func TestToolStates_GlobalToggleRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// No row yet: default-on.
	enabled, err := s.ToolStates().Enabled(ctx, "http_get")
	require.NoError(t, err)
	require.True(t, enabled)

	require.NoError(t, s.ToolStates().SetEnabled(ctx, "http_get", false))
	require.NoError(t, s.ToolStates().SetEnabled(ctx, "current_time", true))
	states, err := s.ToolStates().List(ctx)
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Equal(t, "current_time", states[0].Name, "list must be ordered by name")
	require.True(t, states[0].Enabled)
	require.Equal(t, "http_get", states[1].Name)
	require.False(t, states[1].Enabled)

	// Upsert flips the stored state back on.
	time.Sleep(2 * time.Millisecond) // nudge the clock for the updated_at stamp
	require.NoError(t, s.ToolStates().SetEnabled(ctx, "http_get", true))
	enabled, err = s.ToolStates().Enabled(ctx, "http_get")
	require.NoError(t, err)
	require.True(t, enabled)
}
