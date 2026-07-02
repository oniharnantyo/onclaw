package service

import (
	"context"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/hooks"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// AddHook creates a new hook and persists it.
func (s *Service) AddHook(ctx context.Context, h *store.Hook) (*store.Hook, error) {
	if h.ID == "" {
		h.ID = fmt.Sprintf("hook-%d", time.Now().UnixNano())
	}
	h.Enabled = 1
	if err := s.hookStore.AddHook(ctx, h); err != nil {
		return nil, classify(err)
	}
	return h, nil
}

// GetHook retrieves a single hook by ID.
func (s *Service) GetHook(ctx context.Context, id string) (*store.Hook, error) {
	h, err := s.hookStore.GetHook(ctx, id)
	if err != nil {
		return nil, classify(err)
	}
	return h, nil
}

// ListHooks retrieves all hooks.
func (s *Service) ListHooks(ctx context.Context) ([]*store.Hook, error) {
	list, err := s.hookStore.ListHooks(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return list, nil
}

// UpdateHook updates an existing hook.
func (s *Service) UpdateHook(ctx context.Context, h *store.Hook) (*store.Hook, error) {
	if err := s.hookStore.UpdateHook(ctx, h); err != nil {
		return nil, classify(err)
	}
	return h, nil
}

// RemoveHook deletes a hook by ID.
func (s *Service) RemoveHook(ctx context.Context, id string) error {
	if err := s.hookStore.RemoveHook(ctx, id); err != nil {
		return classify(err)
	}
	return nil
}

// ToggleHook enables or disables a hook.
func (s *Service) ToggleHook(ctx context.Context, id string, enabled bool) error {
	if err := s.hookStore.ToggleHook(ctx, id, enabled); err != nil {
		return classify(err)
	}
	return nil
}

// ListHookExecutions lists all hook audit logs.
func (s *Service) ListHookExecutions(ctx context.Context) ([]*store.HookExecution, error) {
	list, err := s.execStore.ListExecutions(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return list, nil
}

// TestHook executes a dry run of a hook against a sample payload.
func (s *Service) TestHook(ctx context.Context, h *store.Hook) (hooks.Decision, error) {
	// Build mock payload
	payload := hooks.Payload{
		Event:     hooks.Event(h.Event),
		Agent:     "test-agent",
		Channel:   "web",
		SessionID: "test-session-id",
		Prompt:    "List files in the active workspace directory",
		ToolName:  "list_dir",
		ToolArgs:  map[string]interface{}{"DirectoryPath": "."},
	}

	dispatcher := hooks.NewDispatcher(s.hookStore, s.execStore)
	return dispatcher.TestHook(ctx, h, payload)
}
