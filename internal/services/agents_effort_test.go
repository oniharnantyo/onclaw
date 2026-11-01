package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The agent default-effort tests (addendum, spec agent-management "Edit
// agent configuration"): the create/update inputs carry the default
// reasoning effort, validated against the selected model's supported set.

// TestAgents_CreateEffortRoundTripsAndValidates covers create: a supported
// effort is stored, the empty effort is the inherit default, and an
// unsupported effort is a validation error that leaves no row.
func TestAgents_CreateEffortRoundTripsAndValidates(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	ctx := context.Background()

	input := f.validCreateInput()
	input.Effort = "high"
	agent, err := f.svc.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "high", agent.Effort, "the default effort round-trips")

	// The empty effort is the cleared/inherit state.
	minimal := f.validCreateInput()
	minimal.Slug = "minimal"
	minimal.Effort = ""
	bare, err := f.svc.Create(ctx, minimal)
	require.NoError(t, err)
	require.Empty(t, bare.Effort)

	// An unsupported effort is rejected before any row is written.
	invalid := f.validCreateInput()
	invalid.Slug = "invalid"
	invalid.Effort = "turbo"
	_, err = f.svc.Create(ctx, invalid)
	require.Error(t, err)
	require.Contains(t, err.Error(), "turbo", "the rejection names the effort")

	list, err := f.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2, "the rejected create left no row")
}

// TestAgents_UpdateEffortRoundTripsAndValidates covers update: the effort
// follows the (possibly changed) provider and model, clearing to empty is
// the inherit state, and an unsupported combination is rejected with the row
// untouched.
func TestAgents_UpdateEffortRoundTripsAndValidates(t *testing.T) {
	f := newAgentsFixture(t)
	f.addProvider(t, "p1")
	ctx := context.Background()

	seeded := f.seedAgent(t, "scout", store.PromptsStatusReady)
	seeded.Effort = "medium"
	updated, err := f.svc.Update(ctx, seeded)
	require.NoError(t, err)
	require.Equal(t, "medium", updated.Effort, "a supported effort saves")

	// Clearing to empty (the config modal clears the select on a model
	// without support) saves.
	updated.Effort = ""
	cleared, err := f.svc.Update(ctx, updated)
	require.NoError(t, err)
	require.Empty(t, cleared.Effort)

	// An unsupported effort for the agent's model is rejected.
	seeded.Effort = "minimal" // claude models have no minimal tier
	_, err = f.svc.Update(ctx, seeded)
	require.Error(t, err)
	require.Contains(t, err.Error(), "minimal")

	stored, err := f.svc.Get(ctx, seeded.ID)
	require.NoError(t, err)
	require.Empty(t, stored.Effort, "the rejected update left the stored effort untouched")
}

// TestAgents_EffortResolvesStoredRow pins the stored-first resolution
// (design D3) for the agent-default validation: the provider's stored model
// row is authoritative exactly as it is for the turn-time check. A glm model
// on an openai-compatible provider therefore accepts the low tier its row
// declares, though the static catalog has no glm entry and its heuristic
// yields nothing. An undeclared row degrades to high; an unlisted model on a
// compatible provider still rejects everything.
func TestAgents_EffortResolvesStoredRow(t *testing.T) {
	f := newAgentsFixture(t)
	ctx := context.Background()

	provider := store.Provider{ID: "zai", Type: "openai-compatible", Name: "Z.AI", APIKey: "sk-test-key"}
	require.NoError(t, f.st.Providers().Create(ctx, &provider))
	require.NoError(t, f.st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID: "zai",
		ModelID:    "glm-5.3-flash",
		Efforts:    []string{"low", "high", "max"},
		Enabled:    true,
	}))

	input := f.validCreateInput()
	input.ProviderID = "zai"
	input.Model = "glm-5.3-flash"
	input.Effort = "low"
	agent, err := f.svc.Create(ctx, input)
	require.NoError(t, err, "the stored row's declared set is authoritative")
	require.Equal(t, "low", agent.Effort)

	// Update validates against the same stored set.
	agent.Effort = "max"
	updated, err := f.svc.Update(ctx, agent)
	require.NoError(t, err)
	require.Equal(t, "max", updated.Effort)

	// An effort outside the row's declared set is still rejected.
	agent.Effort = "medium"
	_, err = f.svc.Update(ctx, agent)
	require.Error(t, err)
	require.Contains(t, err.Error(), "medium")

	// An undeclared row (no efforts) degrades to the high tier — the same
	// resolution the capability surfaces apply.
	require.NoError(t, f.st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID: "zai",
		ModelID:    "glm-5.3-air",
		Enabled:    true,
	}))
	undeclared := f.validCreateInput()
	undeclared.Name = "Air"
	undeclared.Slug = "air"
	undeclared.ProviderID = "zai"
	undeclared.Model = "glm-5.3-air"
	undeclared.Effort = "low"
	_, err = f.svc.Create(ctx, undeclared)
	require.Error(t, err, "an undeclared row offers only the high tier")
	require.Contains(t, err.Error(), "low")

	undeclared.Effort = "high"
	_, err = f.svc.Create(ctx, undeclared)
	require.NoError(t, err)

	// No stored row at all: the static catalog governs, and glm matches no
	// heuristic — every effort is rejected, leaving no row.
	unlisted := f.validCreateInput()
	unlisted.Name = "Ghost"
	unlisted.Slug = "ghost"
	unlisted.ProviderID = "zai"
	unlisted.Model = "glm-5.3-not-listed"
	unlisted.Effort = "low"
	_, err = f.svc.Create(ctx, unlisted)
	require.Error(t, err)
	list, err := f.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2, "the rejected create left no row")
}
