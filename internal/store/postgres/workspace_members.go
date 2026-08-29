package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// memberStore implements storeport.MemberStore for PostgreSQL.
type memberStore struct {
	db Executor
}

// NewMemberStore creates a new MemberStore with the given database executor.
func NewMemberStore(db Executor) storeport.MemberStore {
	return &memberStore{db: db}
}

func (ms *memberStore) Add(ctx context.Context, m *domain.Member) error {
	if m == nil || m.WorkspaceID == "" || m.UserID == "" || m.RoleID == "" {
		return domain.ErrInvalid
	}

	// Validate that role exists and belongs to the workspace
	var roleWorkspaceID string
	err := ms.db.QueryRow(ctx, `SELECT workspace_id FROM roles WHERE id = $1`, m.RoleID).Scan(&roleWorkspaceID)
	if err != nil {
		return convertError(err)
	}
	if roleWorkspaceID != m.WorkspaceID {
		return fmt.Errorf("%w: role does not belong to workspace", domain.ErrInvalid)
	}

	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	query := `
		INSERT INTO workspace_members (workspace_id, user_id, role_id, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err = ms.db.Exec(ctx, query,
		m.WorkspaceID,
		m.UserID,
		m.RoleID,
		m.CreatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ms *memberStore) Get(ctx context.Context, workspaceID, userID string) (*domain.Member, error) {
	if workspaceID == "" || userID == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT wm.workspace_id, wm.user_id, wm.role_id, wm.created_at,
		       r.id, r.workspace_id, r.name, r.is_owner, r.permissions, r.built_in, r.created_at
		FROM workspace_members wm
		JOIN roles r ON wm.role_id = r.id
		WHERE wm.workspace_id = $1 AND wm.user_id = $2
	`
	var m domain.Member
	var r domain.Role
	err := ms.db.QueryRow(ctx, query, workspaceID, userID).Scan(
		&m.WorkspaceID,
		&m.UserID,
		&m.RoleID,
		&m.CreatedAt,
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
	m.Role = &r
	return &m, nil
}

func (ms *memberStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Member, error) {
	if workspaceID == "" {
		return []domain.Member{}, nil
	}

	query := `
		SELECT wm.workspace_id, wm.user_id, wm.role_id, wm.created_at,
		       r.id, r.workspace_id, r.name, r.is_owner, r.permissions, r.built_in, r.created_at
		FROM workspace_members wm
		JOIN roles r ON wm.role_id = r.id
		WHERE wm.workspace_id = $1
		ORDER BY wm.created_at ASC, wm.user_id ASC
	`
	rows, err := ms.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	members := make([]domain.Member, 0)
	for rows.Next() {
		var m domain.Member
		var r domain.Role
		if err := rows.Scan(
			&m.WorkspaceID,
			&m.UserID,
			&m.RoleID,
			&m.CreatedAt,
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
		m.Role = &r
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return members, nil
}

func (ms *memberStore) ListForUser(ctx context.Context, userID string) ([]domain.MemberView, error) {
	if userID == "" {
		return []domain.MemberView{}, nil
	}

	query := `
		SELECT wm.workspace_id, w.slug, w.name,
		       wm.user_id, u.email, u.name, u.avatar_key, u.avatar_url,
		       wm.role_id, r.name,
		       r.id, r.workspace_id, r.name, r.is_owner, r.permissions, r.built_in, r.created_at,
		       w.id, w.slug, w.name, w.timezone, w.is_master, w.disabled_at, w.created_at, w.updated_at,
		       (u.password_hash IS NULL OR u.password_hash = '') AS invited,
		       wm.created_at
		FROM workspace_members wm
		JOIN workspaces w ON wm.workspace_id = w.id
		JOIN users u ON wm.user_id = u.id
		JOIN roles r ON wm.role_id = r.id
		WHERE wm.user_id = $1
		ORDER BY wm.created_at ASC, wm.workspace_id ASC
	`
	rows, err := ms.db.Query(ctx, query, userID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	views := make([]domain.MemberView, 0)
	for rows.Next() {
		var mv domain.MemberView
		var r domain.Role
		var ws domain.Workspace
		if err := rows.Scan(
			&mv.WorkspaceID,
			&mv.WorkspaceSlug,
			&mv.WorkspaceName,
			&mv.UserID,
			&mv.Email,
			&mv.Name,
			&mv.AvatarKey,
			&mv.AvatarURL,
			&mv.RoleID,
			&mv.RoleName,
			&r.ID,
			&r.WorkspaceID,
			&r.Name,
			&r.IsOwner,
			&r.Permissions,
			&r.BuiltIn,
			&r.CreatedAt,
			&ws.ID,
			&ws.Slug,
			&ws.Name,
			&ws.Timezone,
			&ws.IsMaster,
			&ws.DisabledAt,
			&ws.CreatedAt,
			&ws.UpdatedAt,
			&mv.Invited,
			&mv.JoinedAt,
		); err != nil {
			return nil, convertError(err)
		}
		if r.Permissions == nil {
			r.Permissions = []string{}
		}
		mv.Role = &r
		mv.Workspace = &ws
		views = append(views, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return views, nil
}

func (ms *memberStore) UpdateRole(ctx context.Context, workspaceID, userID, roleID string) error {
	if workspaceID == "" || userID == "" || roleID == "" {
		return domain.ErrNotFound
	}

	// Validate that role exists and belongs to the workspace
	var roleWorkspaceID string
	err := ms.db.QueryRow(ctx, `SELECT workspace_id FROM roles WHERE id = $1`, roleID).Scan(&roleWorkspaceID)
	if err != nil {
		return convertError(err)
	}
	if roleWorkspaceID != workspaceID {
		return fmt.Errorf("%w: role does not belong to workspace", domain.ErrInvalid)
	}

	query := `
		UPDATE workspace_members
		SET role_id = $1
		WHERE workspace_id = $2 AND user_id = $3
	`
	tag, err := ms.db.Exec(ctx, query, roleID, workspaceID, userID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (ms *memberStore) Remove(ctx context.Context, workspaceID, userID string) error {
	if workspaceID == "" || userID == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM workspace_members
		WHERE workspace_id = $1 AND user_id = $2
	`
	tag, err := ms.db.Exec(ctx, query, workspaceID, userID)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
