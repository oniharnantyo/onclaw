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
