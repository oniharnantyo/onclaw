package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

// ListTools returns all tools grouped by category with configuration schemas.
func (s *Service) ListTools(ctx context.Context) ([]*ToolCategoryView, error) {
	toolsList, err := s.toolRegistryStore.ListTools(ctx)
	if err != nil {
		return nil, classify(err)
	}

	// Group tools by category and retrieve descriptions from global registry
	descMap := make(map[string]string)
	for _, t := range tools.GetRegistry() {
		descMap[t.Name()] = t.Desc()
	}
	// Filesystem-middleware tools are injected by the Eino middleware and are
	// not in the tool factory registry, so seed their descriptions here.
	for _, meta := range tools.FSToolMetadata() {
		if _, ok := descMap[meta.Name]; !ok {
			descMap[meta.Name] = meta.Desc
		}
	}

	catMap := make(map[string][]*ToolView)
	for _, t := range toolsList {
		view := &ToolView{
			Name:        t.Name,
			Category:    t.Category,
			Enabled:     t.Enabled == 1,
			Description: descMap[t.Name],
		}
		catMap[t.Category] = append(catMap[t.Category], view)
	}

	// Build and sort categories
	var categories []string
	for cat := range catMap {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	var resp []*ToolCategoryView
	for _, cat := range categories {
		configurable := tools.IsConfigurable(cat)
		schema := ""
		if configurable {
			if entry, ok := tools.GetConfigEntry(cat); ok {
				schema = entry.JSONSchema
			}
		}

		catTools := catMap[cat]
		sort.Slice(catTools, func(i, j int) bool {
			return catTools[i].Name < catTools[j].Name
		})

		resp = append(resp, &ToolCategoryView{
			Category:     cat,
			Configurable: configurable,
			Schema:       schema,
			Tools:        catTools,
		})
	}

	return resp, nil
}

// ToggleTool updates the enabled state of a tool globally.
func (s *Service) ToggleTool(ctx context.Context, name string, enabled bool) error {
	if name == "*" {
		toolsList, err := s.toolRegistryStore.ListTools(ctx)
		if err != nil {
			return classify(err)
		}
		for _, t := range toolsList {
			if err := s.toolRegistryStore.ToggleTool(ctx, t.Name, enabled); err != nil {
				return classify(err)
			}
		}
		return nil
	}

	err := s.toolRegistryStore.ToggleTool(ctx, name, enabled)
	if err != nil {
		return classify(err)
	}
	return nil
}

// GetCategoryConfig retrieves the configuration JSON for a configurable category.
func (s *Service) GetCategoryConfig(ctx context.Context, category string) (*CategoryConfigView, error) {
	if !tools.IsConfigurable(category) {
		return nil, fmt.Errorf("category %s is not configurable: %w", category, ErrNotFound)
	}

	cfg, err := s.toolGroupConfigStore.GetConfig(ctx, category)
	if err != nil {
		if strings.Contains(err.Error(), "no rows in result set") {
			return &CategoryConfigView{
				Category: category,
				Config:   "{}",
			}, nil
		}
		return nil, classify(err)
	}

	return &CategoryConfigView{
		Category: cfg.Category,
		Config:   cfg.Config,
	}, nil
}

// PutCategoryConfig validates and stores category configuration JSON.
func (s *Service) PutCategoryConfig(ctx context.Context, category string, configJSON string) error {
	if !tools.IsConfigurable(category) {
		return fmt.Errorf("category %s is not configurable: %w", category, ErrNotFound)
	}

	entry, ok := tools.GetConfigEntry(category)
	if !ok {
		return fmt.Errorf("category config entry %s not found: %w", category, ErrNotFound)
	}

	if err := entry.Load(ctx, configJSON); err != nil {
		return fmt.Errorf("invalid config for category %s: %v: %w", category, err, ErrInvalidInput)
	}

	err := s.toolGroupConfigStore.PutConfig(ctx, category, configJSON)
	if err != nil {
		return classify(err)
	}
	return nil
}
