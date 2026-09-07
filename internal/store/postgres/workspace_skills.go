package postgres

import (
	"context"
	"encoding/json"
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

const workspaceSkillSelect = `
	SELECT id, workspace_id, name, description, version, source, enabled, dependencies, created_at, updated_at
	FROM workspace_skills
`

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanWorkspaceSkill(scan rowScanner) (*domain.WorkspaceSkill, error) {
	var skill domain.WorkspaceSkill
	var sourceStr string
	var depsJSON []byte
	if err := scan.Scan(
		&skill.ID,
		&skill.WorkspaceID,
		&skill.Name,
		&skill.Description,
		&skill.Version,
		&sourceStr,
		&skill.Enabled,
		&depsJSON,
		&skill.CreatedAt,
		&skill.UpdatedAt,
	); err != nil {
		return nil, convertError(err)
	}
	skill.Source = domain.SkillSource(sourceStr)
	if len(depsJSON) > 0 {
		if err := json.Unmarshal(depsJSON, &skill.Dependencies); err != nil {
			return nil, convertError(err)
		}
	}
	return &skill, nil
}

func (wss *workspaceSkillStore) Create(ctx context.Context, skill *domain.WorkspaceSkill) error {
	if skill == nil {
		return domain.ErrInvalid
	}
	if skill.Version == "" {
		skill.Version = domain.DefaultSkillVersion
	}
	if err := skill.Validate(); err != nil {
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

	depsJSON, err := json.Marshal(skill.Dependencies)
	if err != nil {
		return convertError(err)
	}

	query := `
		INSERT INTO workspace_skills (id, workspace_id, name, description, version, source, enabled, dependencies, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = wss.db.Exec(ctx, query,
		skill.ID,
		skill.WorkspaceID,
		skill.Name,
		skill.Description,
		skill.Version,
		string(skill.Source),
		skill.Enabled,
		depsJSON,
		skill.CreatedAt,
		skill.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (wss *workspaceSkillStore) Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceSkill, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := workspaceSkillSelect + `
		WHERE workspace_id = $1 AND id = $2
	`
	skill, err := scanWorkspaceSkill(wss.db.QueryRow(ctx, query, workspaceID, id))
	if err != nil {
		return nil, err
	}
	return skill, nil
}

func (wss *workspaceSkillStore) GetByName(ctx context.Context, workspaceID, name string) (*domain.WorkspaceSkill, error) {
	if workspaceID == "" || name == "" {
		return nil, domain.ErrNotFound
	}

	query := workspaceSkillSelect + `
		WHERE workspace_id = $1 AND name = $2
	`
	skill, err := scanWorkspaceSkill(wss.db.QueryRow(ctx, query, workspaceID, name))
	if err != nil {
		return nil, err
	}
	return skill, nil
}

func (wss *workspaceSkillStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceSkill, error) {
	if workspaceID == "" {
		return []domain.WorkspaceSkill{}, nil
	}

	query := workspaceSkillSelect + `
		WHERE workspace_id = $1
		ORDER BY name ASC
	`
	rows, err := wss.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	skills := make([]domain.WorkspaceSkill, 0)
	for rows.Next() {
		skill, err := scanWorkspaceSkill(rows)
		if err != nil {
			return nil, err
		}
		skills = append(skills, *skill)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return skills, nil
}

func (wss *workspaceSkillStore) ListEnabled(ctx context.Context, workspaceSlug string) ([]string, error) {
	if workspaceSlug == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT s.name
		FROM workspace_skills s
		JOIN workspaces w ON w.id = s.workspace_id
		WHERE w.slug = $1 AND s.enabled
		ORDER BY s.name ASC
	`
	rows, err := wss.db.Query(ctx, query, workspaceSlug)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, convertError(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	if len(names) == 0 {
		// Distinguish an unknown slug from a workspace with no enabled skills.
		var exists bool
		err = wss.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspaces WHERE slug = $1)`, workspaceSlug).Scan(&exists)
		if err != nil {
			return nil, convertError(err)
		}
		if !exists {
			return nil, domain.ErrNotFound
		}
	}
	return names, nil
}

func (wss *workspaceSkillStore) Update(ctx context.Context, skill *domain.WorkspaceSkill) error {
	if skill == nil || skill.ID == "" || skill.WorkspaceID == "" {
		return domain.ErrInvalid
	}
	if skill.Version == "" {
		skill.Version = domain.DefaultSkillVersion
	}
	if err := skill.Validate(); err != nil {
		return err
	}

	depsJSON, err := json.Marshal(skill.Dependencies)
	if err != nil {
		return convertError(err)
	}

	now := time.Now().UTC()
	query := `
		UPDATE workspace_skills
		SET name = $1,
		    description = $2,
		    version = $3,
		    source = $4,
		    dependencies = $5,
		    updated_at = $6
		WHERE workspace_id = $7 AND id = $8
		RETURNING enabled, created_at
	`
	err = wss.db.QueryRow(ctx, query,
		skill.Name,
		skill.Description,
		skill.Version,
		string(skill.Source),
		depsJSON,
		now,
		skill.WorkspaceID,
		skill.ID,
	).Scan(
		&skill.Enabled,
		&skill.CreatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	skill.UpdatedAt = now
	return nil
}

func (wss *workspaceSkillStore) SetEnabled(ctx context.Context, workspaceID, id string, enabled bool) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		UPDATE workspace_skills
		SET enabled = $3,
		    updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := wss.db.Exec(ctx, query, workspaceID, id, enabled)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (wss *workspaceSkillStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM workspace_skills
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := wss.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
