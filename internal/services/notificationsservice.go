package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/notifications"
	"github.com/oniharnantyo/buddies/internal/store"
)

// NotificationEngine is the narrow seam over the notifications package's
// decision core (add-desktop-notifications design D1): the settings save
// path pushes refreshed values through SetSettings so preference changes
// apply immediately, and the frontend's focus reports forward through
// ReportFocus. *notifications.Engine satisfies it; tests fake it.
type NotificationEngine interface {
	// SetSettings replaces the engine's cached settings snapshot.
	SetSettings(v notifications.Values)
	// ReportFocus mirrors the frontend's currently viewed context; an empty
	// id or an unknown kind clears the mirror — nothing on screen
	// (add-desktop-notifications design D3).
	ReportFocus(kind notifications.RouteKind, id string)
}

// NotificationsService is the bound service for desktop-notification
// preferences (add-desktop-notifications design D5): the notification
// settings singleton's get/save with the engine cache refresh, plus the
// frontend's focus reports. Delivery never crosses this service — trigger
// sources and the click path reach the engine at the composition root.
type NotificationsService struct {
	settings store.NotificationSettingsStore
	engine   NotificationEngine
}

// NewNotificationsService returns the notifications service over the
// notification settings singleton store and the notifications engine.
func NewNotificationsService(settings store.NotificationSettingsStore, engine NotificationEngine) *NotificationsService {
	return &NotificationsService{settings: settings, engine: engine}
}

// GetNotificationSettings returns the desktop-notification settings
// singleton. When nothing was ever saved the defaults are returned:
// notifications on, every category on.
func (s *NotificationsService) GetNotificationSettings(ctx context.Context) (store.NotificationSettings, error) {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return store.NotificationSettings{}, fmt.Errorf("get notification settings: %w", err)
	}
	if settings == nil {
		return store.DefaultNotificationSettings(), nil
	}
	return *settings, nil
}

// SaveNotificationSettings create-or-replaces the desktop-notification
// settings singleton and pushes the saved values into the engine's settings
// cache so changes apply immediately without a restart
// (add-desktop-notifications design D4). The store stamps UpdatedAt; the
// service does not.
func (s *NotificationsService) SaveNotificationSettings(ctx context.Context, settings store.NotificationSettings) error {
	if err := s.settings.Save(ctx, &settings); err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}
	s.engine.SetSettings(notifications.Values{
		Enabled:   settings.Enabled,
		Turns:     settings.Turns,
		Attention: settings.Attention,
		Schedules: settings.Schedules,
		Groups:    settings.Groups,
	})
	return nil
}

// ReportFocus mirrors the frontend's currently viewed context into the
// engine's focus mirror (add-desktop-notifications design D3): kind is
// "session" or "group" and ID names the conversation on screen. An empty id
// or an unknown kind clears the mirror — nothing on screen — and never
// errors: suppression requires positive knowledge, so an uninformative
// report just delivers.
func (s *NotificationsService) ReportFocus(kind, id string) error {
	s.engine.ReportFocus(notifications.RouteKind(kind), id)
	return nil
}
