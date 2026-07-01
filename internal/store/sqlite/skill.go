package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type sqliteSkillStore struct {
	db *sql.DB
}

// NewSkillStore creates a new SkillStore backed by SQLite.
func NewSkillStore(db *sql.DB) store.SkillStore {
	return &sqliteSkillStore{db: db}
}

func (s *sqliteSkillStore) AddSkill(ctx context.Context, sk *store.Skill) error {
	if sk.Name == "" {
		return fmt.Errorf("skill name must not be empty")
	}

	if sk.InstalledAt == "" {
		sk.InstalledAt = now()
	}
	sk.UpdatedAt = now()

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO skills (name, scope, source_type, source, skill_path, version, hash, description, enabled, installed_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		sk.Name, sk.Scope, sk.SourceType, sk.Source, sk.SkillPath, sk.Version, sk.Hash, sk.Description, sk.Enabled, sk.InstalledAt, sk.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *sqliteSkillStore) GetSkill(ctx context.Context, name string, scope string) (*store.Skill, error) {
	var sk store.Skill
	err := s.db.QueryRowContext(ctx,
		"SELECT name, scope, source_type, source, skill_path, version, hash, description, enabled, installed_at, updated_at FROM skills WHERE name = ? AND scope = ?",
		name, scope,
	).Scan(&sk.Name, &sk.Scope, &sk.SourceType, &sk.Source, &sk.SkillPath, &sk.Version, &sk.Hash, &sk.Description, &sk.Enabled, &sk.InstalledAt, &sk.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sk, nil
}

func (s *sqliteSkillStore) ListSkills(ctx context.Context) ([]*store.Skill, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, scope, source_type, source, skill_path, version, hash, description, enabled, installed_at, updated_at FROM skills ORDER BY name ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []*store.Skill
	for rows.Next() {
		var sk store.Skill
		err := rows.Scan(&sk.Name, &sk.Scope, &sk.SourceType, &sk.Source, &sk.SkillPath, &sk.Version, &sk.Hash, &sk.Description, &sk.Enabled, &sk.InstalledAt, &sk.UpdatedAt)
		if err != nil {
			return nil, err
		}
		skills = append(skills, &sk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return skills, nil
}

func (s *sqliteSkillStore) ListSkillsByScope(ctx context.Context, scope string) ([]*store.Skill, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, scope, source_type, source, skill_path, version, hash, description, enabled, installed_at, updated_at FROM skills WHERE scope = ? ORDER BY name ASC",
		scope,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []*store.Skill
	for rows.Next() {
		var sk store.Skill
		err := rows.Scan(&sk.Name, &sk.Scope, &sk.SourceType, &sk.Source, &sk.SkillPath, &sk.Version, &sk.Hash, &sk.Description, &sk.Enabled, &sk.InstalledAt, &sk.UpdatedAt)
		if err != nil {
			return nil, err
		}
		skills = append(skills, &sk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return skills, nil
}

func (s *sqliteSkillStore) UpdateSkill(ctx context.Context, sk *store.Skill) error {
	if sk.Name == "" {
		return fmt.Errorf("skill name must not be empty")
	}
	sk.UpdatedAt = now()

	res, err := s.db.ExecContext(ctx,
		"UPDATE skills SET source_type = ?, source = ?, skill_path = ?, version = ?, hash = ?, description = ?, enabled = ?, updated_at = ? WHERE name = ? AND scope = ?",
		sk.SourceType, sk.Source, sk.SkillPath, sk.Version, sk.Hash, sk.Description, sk.Enabled, sk.UpdatedAt, sk.Name, sk.Scope,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqliteSkillStore) RemoveSkill(ctx context.Context, name string, scope string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM skills WHERE name = ? AND scope = ?", name, scope)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
