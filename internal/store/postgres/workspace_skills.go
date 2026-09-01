package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceSkillStore implements storeport.WorkspaceSkillStore for PostgreSQL.
type workspaceSkillStore struct {
	db Executor
}

// NewWorkspaceSkillStore creates a new WorkspaceSkillStore with the given database executor.
func NewWorkspaceSkillStore(db Executor) storeport.WorkspaceSkillStore {
	return &workspaceSkillStore{db: db}
}

func (ss *workspaceSkillStore) Create(ctx context.Context, skill *domain.WorkspaceSkill) error {
	if skill == nil || skill.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateSkillName(skill.Name); err != nil {
		return err
	}

	if skill.ID == "" {
		skill.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if skill.CreatedAt.IsZero() {
		skill.CreatedAt = now
	}
	if skill.UpdatedAt.IsZero() {
		skill.UpdatedAt = now
	}

	query := `
		INSERT INTO workspace_skills (id, workspace_id, name, description, body, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := ss.db.Exec(ctx, query,
		skill.ID,
		skill.WorkspaceID,
		skill.Name,
		skill.Description,
		skill.Body,
		skill.Enabled,
		skill.CreatedAt,
		skill.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ss *workspaceSkillStore) ByID(ctx context.Context, workspaceID, id string) (*domain.WorkspaceSkill, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, name, description, body, enabled, created_at, updated_at
		FROM workspace_skills
		WHERE workspace_id = $1 AND id = $2
	`
	var skill domain.WorkspaceSkill
	err := ss.db.QueryRow(ctx, query, workspaceID, id).Scan(
		&skill.ID,
		&skill.WorkspaceID,
		&skill.Name,
		&skill.Description,
		&skill.Body,
		&skill.Enabled,
		&skill.CreatedAt,
		&skill.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &skill, nil
}

func (ss *workspaceSkillStore) FindByName(ctx context.Context, workspaceID, name string) (*domain.WorkspaceSkill, error) {
	if workspaceID == "" || name == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, name, description, body, enabled, created_at, updated_at
		FROM workspace_skills
		WHERE workspace_id = $1 AND name = $2
	`
	var skill domain.WorkspaceSkill
	err := ss.db.QueryRow(ctx, query, workspaceID, name).Scan(
		&skill.ID,
		&skill.WorkspaceID,
		&skill.Name,
		&skill.Description,
		&skill.Body,
		&skill.Enabled,
		&skill.CreatedAt,
		&skill.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &skill, nil
}

func (ss *workspaceSkillStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.WorkspaceSkill, error) {
	if workspaceID == "" {
		return []domain.WorkspaceSkill{}, nil
	}

	// Progressive disclosure: List omits body
	query := `
		SELECT id, workspace_id, name, description, enabled, created_at, updated_at
		FROM workspace_skills
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := ss.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	skills := make([]domain.WorkspaceSkill, 0)
	for rows.Next() {
		var skill domain.WorkspaceSkill
		if err := rows.Scan(
			&skill.ID,
			&skill.WorkspaceID,
			&skill.Name,
			&skill.Description,
			&skill.Enabled,
			&skill.CreatedAt,
			&skill.UpdatedAt,
		); err != nil {
			return nil, convertError(err)
		}
		skills = append(skills, skill)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return skills, nil
}

func (ss *workspaceSkillStore) Update(ctx context.Context, skill *domain.WorkspaceSkill) error {
	if skill == nil || skill.ID == "" || skill.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if err := domain.ValidateSkillName(skill.Name); err != nil {
		return err
	}

	now := time.Now().UTC()
	query := `
		UPDATE workspace_skills
		SET name = $1,
		    description = $2,
		    body = $3,
		    enabled = $4,
		    updated_at = $5
		WHERE workspace_id = $6 AND id = $7
		RETURNING created_at
	`
	err := ss.db.QueryRow(ctx, query,
		skill.Name,
		skill.Description,
		skill.Body,
		skill.Enabled,
		now,
		skill.WorkspaceID,
		skill.ID,
	).Scan(&skill.CreatedAt)
	if err != nil {
		return convertError(err)
	}
	skill.UpdatedAt = now
	return nil
}

func (ss *workspaceSkillStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM workspace_skills
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := ss.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
