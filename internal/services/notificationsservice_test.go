package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// fakeEngine records what the service pushes through the NotificationEngine
// seam. The focus fields mirror the real engine's documented semantics
// (design D3): an empty id or an unknown kind clears the mirror — nothing on
// screen — because suppression requires positive knowledge.
type fakeEngine struct {
	setSettings []notifications.Values
	focus       *notifications.Route
}

func (f *fakeEngine) SetSettings(v notifications.Values) {
	f.setSettings = append(f.setSettings, v)
}

func (f *fakeEngine) ReportFocus(kind notifications.RouteKind, id string) {
	if id == "" || (kind != notifications.RouteSession && kind != notifications.RouteGroup) {
		f.focus = nil
		return
	}
	f.focus = &notifications.Route{Kind: kind, ID: id}
}

func TestNotificationsService_Get_DefaultsWhenUnsaved(t *testing.T) {
	svc := NewNotificationsService(fake.New().NotificationSettings(), &fakeEngine{})

	settings, err := svc.GetNotificationSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, store.DefaultNotificationSettings(), settings)
	require.True(t, settings.Enabled)
	require.True(t, settings.Turns)
	require.True(t, settings.Attention)
	require.True(t, settings.Schedules)
	require.True(t, settings.Groups)
}

func TestNotificationsService_SaveAndReload(t *testing.T) {
	svc := NewNotificationsService(fake.New().NotificationSettings(), &fakeEngine{})
	ctx := context.Background()

	require.NoError(t, svc.SaveNotificationSettings(ctx, store.NotificationSettings{
		Enabled: true,
		Turns:   false,
		Groups:  true,
	}))

	settings, err := svc.GetNotificationSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.False(t, settings.Turns)
	require.False(t, settings.Attention)
	require.False(t, settings.Schedules)
	require.True(t, settings.Groups)
	require.False(t, settings.UpdatedAt.IsZero(), "the store stamps UpdatedAt on save")
}

func TestNotificationsService_Save_PushesValuesIntoEngine(t *testing.T) {
	eng := &fakeEngine{}
	svc := NewNotificationsService(fake.New().NotificationSettings(), eng)
	ctx := context.Background()

	saved := store.NotificationSettings{Enabled: true, Attention: true}
	require.NoError(t, svc.SaveNotificationSettings(ctx, saved))

	require.Len(t, eng.setSettings, 1)
	require.Equal(t, notifications.Values{
		Enabled:   true,
		Turns:     false,
		Attention: true,
		Schedules: false,
		Groups:    false,
	}, eng.setSettings[0])
}

func TestNotificationsService_ReportFocus_ForwardsToEngine(t *testing.T) {
	eng := &fakeEngine{}
	svc := NewNotificationsService(fake.New().NotificationSettings(), eng)

	require.NoError(t, svc.ReportFocus("session", "sess1"))
	require.NotNil(t, eng.focus)
	require.Equal(t, notifications.Route{Kind: notifications.RouteSession, ID: "sess1"}, *eng.focus)

	require.NoError(t, svc.ReportFocus("group", "grp1"))
	require.NotNil(t, eng.focus)
	require.Equal(t, notifications.Route{Kind: notifications.RouteGroup, ID: "grp1"}, *eng.focus)
}

func TestNotificationsService_ReportFocus_ClearsOnNormalInput(t *testing.T) {
	eng := &fakeEngine{}
	svc := NewNotificationsService(fake.New().NotificationSettings(), eng)

	require.NoError(t, svc.ReportFocus("session", "sess1"))
	require.NotNil(t, eng.focus)

	// An empty id is the engine's "nothing on screen" semantics: it clears
	// the mirror and never errors.
	require.NoError(t, svc.ReportFocus("session", ""))
	require.Nil(t, eng.focus)

	// An unknown kind clears too, per the engine's documented semantics.
	require.NoError(t, svc.ReportFocus("bogus", "sess1"))
	require.Nil(t, eng.focus)
}
