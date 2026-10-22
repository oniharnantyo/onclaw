package sqlite

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type skillRegistry struct{ e executor }

const skillColumns = `name, description, version, source, enabled, created_at, updated_at`

func (r *skillRegistry) Upsert(ctx context.Context, skill *store.Skill) error {
	stampCreated(&skill.CreatedAt, &skill.UpdatedAt)

	_, err := r.e.ExecContext(ctx, `
INSERT INTO skills (`+skillColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO UPDATE SET
description = excluded.description,
version = excluded.version,
source = excluded.source,
enabled = excluded.enabled,
updated_at = excluded.updated_at`,
		skill.Name, skill.Description, skill.Version, skill.Source,
		boolInt(skill.Enabled), formatTime(skill.CreatedAt), formatTime(skill.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert skill %s: %w", skill.Name, err)
	}
	return nil
}

func (r *skillRegistry) GetByName(ctx context.Context, name string) (*store.Skill, error) {
	row := r.e.QueryRowContext(ctx, `SELECT `+skillColumns+` FROM skills WHERE name = ?`, name)
	skill, err := scanSkill(row)
	if err != nil {
		return nil, readErr("get skill", err)
	}
	return skill, nil
}

func (r *skillRegistry) List(ctx context.Context) ([]store.Skill, error) {
	rows, err := r.e.QueryContext(ctx, `SELECT `+skillColumns+` FROM skills ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()

	skills := make([]store.Skill, 0)
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			return nil, fmt.Errorf("list skills: %w", err)
		}
		skills = append(skills, *skill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return skills, nil
}

func (r *skillRegistry) SetEnabled(ctx context.Context, name string, enabled bool) error {
	res, err := r.e.ExecContext(ctx, `
UPDATE skills SET enabled = ?, updated_at = ? WHERE name = ?`,
		boolInt(enabled), formatTime(nowUTC()), name,
	)
	if err != nil {
		return fmt.Errorf("set skill enabled: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("set skill %s enabled: %w", name, store.ErrNotFound)
	}
	return nil
}

func (r *skillRegistry) Delete(ctx context.Context, name string) error {
	res, err := r.e.ExecContext(ctx, `DELETE FROM skills WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete skill %s: %w", name, store.ErrNotFound)
	}
	return nil
}

func scanSkill(row scanner) (*store.Skill, error) {
	var skill store.Skill
	var createdAt, updatedAt string

	err := row.Scan(&skill.Name, &skill.Description, &skill.Version, &skill.Source,
		&skill.Enabled, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if skill.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if skill.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &skill, nil
}
