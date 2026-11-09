package sqlite

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestNotificationSettings_RoundTrip covers the singleton contract the
// settings pane and the notifications engine rely on: an absent read is
// (nil, nil) — the defaults-when-absent convention, applied by callers — and
// a save is a create-or-replace of the single row that stamps UpdatedAt.
func TestNotificationSettings_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	t.Run("absent read returns nil", func(t *testing.T) {
		settings, err := s.NotificationSettings().Get(ctx)
		require.NoError(t, err)
		require.Nil(t, settings, "no row saved yet — the caller applies defaults")
	})

	t.Run("save then get round-trips the toggles", func(t *testing.T) {
		want := store.NotificationSettings{
			Enabled:   true,
			Turns:     false,
			Attention: true,
			Schedules: false,
			Groups:    true,
		}
		require.NoError(t, s.NotificationSettings().Save(ctx, &want))
		require.False(t, want.UpdatedAt.IsZero(), "the store stamps UpdatedAt")

		got, err := s.NotificationSettings().Get(ctx)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, want.Enabled, got.Enabled)
		require.Equal(t, want.Turns, got.Turns)
		require.Equal(t, want.Attention, got.Attention)
		require.Equal(t, want.Schedules, got.Schedules)
		require.Equal(t, want.Groups, got.Groups)
		require.True(t, got.UpdatedAt.Equal(want.UpdatedAt))
	})

	t.Run("second save replaces the singleton row", func(t *testing.T) {
		previous := store.NotificationSettings{Enabled: true, Turns: true}
		require.NoError(t, s.NotificationSettings().Save(ctx, &previous))

		next := store.NotificationSettings{Enabled: false, Attention: true}
		require.NoError(t, s.NotificationSettings().Save(ctx, &next))

		got, err := s.NotificationSettings().Get(ctx)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.False(t, got.Enabled)
		require.False(t, got.Turns, "unmentioned toggles reset to the zero value, never merge")
		require.True(t, got.Attention)
		require.True(t, got.UpdatedAt.After(previous.UpdatedAt), "each save re-stamps UpdatedAt")
	})
}
