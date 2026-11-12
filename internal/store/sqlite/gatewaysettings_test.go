package sqlite

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestGatewaySettings_WebhookAddrRoundTrip covers the webhook listener
// address riding the gateway's singleton trigger-config row
// (add-webhook-triggers design D7): an absent row is still (nil, nil) — the
// defaults-when-absent convention — and the address round-trips through
// Get/Save alongside the existing columns, with ” as the unset value.
func TestGatewaySettings_WebhookAddrRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	t.Run("absent read returns nil", func(t *testing.T) {
		settings, err := s.GatewaySettings().Get(ctx)
		require.NoError(t, err)
		require.Nil(t, settings, "no row saved yet — the caller applies defaults")
	})

	t.Run("webhook addr round-trips with the other columns", func(t *testing.T) {
		want := store.GatewaySettings{
			Enabled:        true,
			DefaultAgentID: "lead",
			WebhookAddr:    "127.0.0.1:8787",
		}
		require.NoError(t, s.GatewaySettings().Save(ctx, &want))
		require.False(t, want.UpdatedAt.IsZero(), "the store stamps UpdatedAt")

		got, err := s.GatewaySettings().Get(ctx)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.True(t, got.Enabled)
		require.Equal(t, "lead", got.DefaultAgentID)
		require.Equal(t, "127.0.0.1:8787", got.WebhookAddr)
		require.True(t, got.UpdatedAt.Equal(want.UpdatedAt))
	})

	t.Run("empty addr round-trips as unset", func(t *testing.T) {
		next := store.GatewaySettings{Enabled: false, DefaultAgentID: ""}
		require.NoError(t, s.GatewaySettings().Save(ctx, &next))

		got, err := s.GatewaySettings().Get(ctx)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.False(t, got.Enabled)
		require.Equal(t, "", got.WebhookAddr, "'' means unset — callers apply the loopback default")
	})
}
