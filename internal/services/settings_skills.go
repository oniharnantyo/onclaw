package services

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Skills returns the skill registry: one row per on-disk skill directory,
// ordered by name. The result is never nil.
func (s *SettingsService) Skills(ctx context.Context) ([]store.Skill, error) {
	skills, err := s.store.Skills().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return skills, nil
}

// SetSkillEnabled toggles the global enable flag of the named skill;
// store.ErrNotFound when absent.
func (s *SettingsService) SetSkillEnabled(ctx context.Context, name string, enabled bool) error {
	if err := s.store.Skills().SetEnabled(ctx, name, enabled); err != nil {
		return fmt.Errorf("set skill enabled: %w", err)
	}
	return nil
}
