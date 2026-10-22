package store

import "context"

// SkillRegistry indexes the on-disk skill directories under the data
// directory: one row per skill, keyed by name. The skill bodies themselves
// live on disk; the registry carries their metadata and enable state.
type SkillRegistry interface {
	// Upsert create-or-replaces the registry row for skill.Name, stamping
	// timestamps when empty.
	Upsert(ctx context.Context, skill *Skill) error
	// GetByName returns the registry row for the named skill; ErrNotFound
	// when absent.
	GetByName(ctx context.Context, name string) (*Skill, error)
	// List returns all registry rows ordered by name. The result is never
	// nil.
	List(ctx context.Context) ([]Skill, error)
	// SetEnabled toggles the enable flag of the named skill; ErrNotFound
	// when absent.
	SetEnabled(ctx context.Context, name string, enabled bool) error
	// Delete removes the registry row of the named skill; ErrNotFound when
	// absent.
	Delete(ctx context.Context, name string) error
}
