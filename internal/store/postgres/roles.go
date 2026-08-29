package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// roleStore implements storeport.RoleStore for PostgreSQL.
type roleStore struct {
	db Executor
}

// NewRoleStore creates a new RoleStore with the given database executor.
func NewRoleStore(db Executor) storeport.RoleStore {
	return &roleStore{db: db}
}

func (rs *roleStore) Create(ctx context.Context, r *domain.Role) error {
	if r == nil || r.WorkspaceID == "" || r.Name == "" {
		return domain.ErrInvalid
	}

	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}

	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}

	query := `
		INSERT INTO roles (id, workspace_id, name, is_owner, permissions, built_in, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := rs.db.Exec(ctx, query,
		r.ID,
		r.WorkspaceID,
		r.Name,
		r.IsOwner,
		r.Permissions,
		r.BuiltIn,
		r.CreatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (rs *roleStore) ByID(ctx context.Context, id string) (*domain.Role, error) {
	if id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, name, is_owner, permissions, built_in, created_at
		FROM roles
		WHERE id = $1
	`
	var r domain.Role
	err := rs.db.QueryRow(ctx, query, id).Scan(
		&r.ID,
		&r.WorkspaceID,
		&r.Name,
		&r.IsOwner,
		&r.Permissions,
		&r.BuiltIn,
		&r.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	return &r, nil
}

func (rs *roleStore) FindByName(ctx context.Context, workspaceID, name string) (*domain.Role, error) {
	if workspaceID == "" || name == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, name, is_owner, permissions, built_in, created_at
		FROM roles
		WHERE workspace_id = $1 AND name = $2
	`
	var r domain.Role
	err := rs.db.QueryRow(ctx, query, workspaceID, name).Scan(
		&r.ID,
		&r.WorkspaceID,
		&r.Name,
		&r.IsOwner,
		&r.Permissions,
		&r.BuiltIn,
		&r.CreatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	return &r, nil
}

func (rs *roleStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Role, error) {
	if workspaceID == "" {
		return []domain.Role{}, nil
	}

	query := `
		SELECT id, workspace_id, name, is_owner, permissions, built_in, created_at
		FROM roles
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := rs.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	roles := make([]domain.Role, 0)
	for rows.Next() {
		var r domain.Role
		if err := rows.Scan(
			&r.ID,
			&r.WorkspaceID,
			&r.Name,
			&r.IsOwner,
			&r.Permissions,
			&r.BuiltIn,
			&r.CreatedAt,
		); err != nil {
			return nil, convertError(err)
		}
		if r.Permissions == nil {
			r.Permissions = []string{}
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return roles, nil
}

func (rs *roleStore) CountMembers(ctx context.Context, roleID string) (int, error) {
	if roleID == "" {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM workspace_members
		WHERE role_id = $1
	`
	var count int
	err := rs.db.QueryRow(ctx, query, roleID).Scan(&count)
	if err != nil {
		return 0, convertError(err)
	}
	return count, nil
}
