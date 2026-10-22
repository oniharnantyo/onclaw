package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type skillRegistry struct{ s *Store }

func (r skillRegistry) Upsert(_ context.Context, skill *store.Skill) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	stored, exists := r.s.skills[skill.Name]
	stamp(&skill.CreatedAt, &skill.UpdatedAt)
	if exists {
		skill.CreatedAt = stored.CreatedAt
	}
	r.s.skills[skill.Name] = *skill
	return nil
}

func (r skillRegistry) GetByName(_ context.Context, name string) (*store.Skill, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	skill, ok := r.s.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill %s: %w", name, store.ErrNotFound)
	}
	out := skill
	return &out, nil
}

func (r skillRegistry) List(_ context.Context) ([]store.Skill, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	out := make([]store.Skill, 0, len(r.s.skills))
	for _, skill := range r.s.skills {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r skillRegistry) SetEnabled(_ context.Context, name string, enabled bool) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	skill, ok := r.s.skills[name]
	if !ok {
		return fmt.Errorf("skill %s: %w", name, store.ErrNotFound)
	}
	skill.Enabled = enabled
	skill.UpdatedAt = nowUTC()
	r.s.skills[name] = skill
	return nil
}

func (r skillRegistry) Delete(_ context.Context, name string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	if _, ok := r.s.skills[name]; !ok {
		return fmt.Errorf("skill %s: %w", name, store.ErrNotFound)
	}
	delete(r.s.skills, name)
	return nil
}
