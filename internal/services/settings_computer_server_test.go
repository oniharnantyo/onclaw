//go:build server

package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// TestSettingsComputerServer_LaneUnavailable proves the server seam answers
// the Desktop-only availability contract (add-computer-use design D9; spec
// computer-use "Desktop-only availability"): the lane reports unavailable,
// the managing calls refuse with the readable sentinel, and nothing touches
// the hooks — which the server composition never wires.
func TestSettingsComputerServer_LaneUnavailable(t *testing.T) {
	ctx := context.Background()
	require.False(t, computerLaneAvailable())
	require.Nil(t, computerPermissions())
	require.ErrorIs(t, promptComputerPermissions(), ErrComputerUnavailable)

	svc := NewSettingsService(fake.New()) // no hooks: server mode never wires them

	st, err := svc.ComputerStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, ComputerStatus{
		Available: false,
		Status:    ComputerStatusUnavailable,
	}, st)

	st, err = svc.CheckComputerPermissions(ctx)
	require.NoError(t, err)
	require.Equal(t, ComputerStatus{
		Available: false,
		Status:    ComputerStatusUnavailable,
	}, st)

	require.ErrorIs(t, svc.SaveComputerConfig(ctx, ComputerConfigView{Enabled: true}), ErrComputerUnavailable)
	require.ErrorIs(t, svc.PromptComputerPermissions(ctx), ErrComputerUnavailable)

	// The config read stays store-backed in server mode: the stored document
	// outlives the build that wrote it.
	cfg, err := svc.ComputerConfig(ctx)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
}
