package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents"
)

// The built-in tool catalog methods on SettingsService: the settings Tools
// pane lists the catalog and toggles tools globally. Tool gating per agent
// lives on the agent definition (store.Agent.Tools) and the runtime's turn
// assembly, not here.

// ToolEntry is one built-in tool as the settings surface renders it.
type ToolEntry struct {
	Name string
	// DisplayName is the Title Case label the row renders; Name stays the
	// gate key the toggles address.
	DisplayName string
	// IconKey names a frontend kit glyph; unknown keys render a neutral
	// plug client-side.
	IconKey     string
	Description string
	// Enabled is the global toggle; a tool with no stored state defaults
	// to enabled.
	Enabled bool
}

// Tools returns the built-in tool catalog with its global enable states,
// in stable catalog order. The result is never nil.
func (s *SettingsService) Tools(ctx context.Context) ([]ToolEntry, error) {
	states, err := s.store.ToolStates().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tool states: %w", err)
	}
	enabled := make(map[string]bool, len(states))
	for _, state := range states {
		enabled[state.Name] = state.Enabled
	}

	catalog := agents.CatalogTools()
	entries := make([]ToolEntry, 0, len(catalog))
	for _, tool := range catalog {
		isEnabled, ok := enabled[tool.Name]
		if !ok {
			isEnabled = true // absence of a stored row is the default-on state
		}
		entries = append(entries, ToolEntry{
			Name:        tool.Name,
			DisplayName: tool.DisplayName,
			IconKey:     tool.IconKey,
			Description: tool.Description,
			Enabled:     isEnabled,
		})
	}
	return entries, nil
}

// SetToolEnabled toggles the global enable state of the named built-in
// tool; an error for a name outside the catalog.
func (s *SettingsService) SetToolEnabled(ctx context.Context, name string, enabled bool) error {
	if _, ok := agents.CatalogToolByName(name); !ok {
		return fmt.Errorf("set tool enabled: unknown tool %q", name)
	}
	if err := s.store.ToolStates().SetEnabled(ctx, name, enabled); err != nil {
		return fmt.Errorf("set tool %s enabled: %w", name, err)
	}
	return nil
}
