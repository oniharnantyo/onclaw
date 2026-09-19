package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceStore implements storeport.WorkspaceStore for PostgreSQL.
type workspaceStore struct {
	db Executor
}

// NewWorkspaceStore creates a new WorkspaceStore with the given database executor.
func NewWorkspaceStore(db Executor) storeport.WorkspaceStore {
	return &workspaceStore{db: db}
}

// defaultModelPairColumns is the workspace default-model pair's select list;
// keep it aligned with scanDefaultModelPair.
const defaultModelPairColumns = `w.default_provider_id, w.default_model`

// scanDefaultModelPair scans the pair selected by defaultModelPairColumns into
// the workspace: both NULL (or both set) only — the store refuses half pairs
// on write, so a half-read degrades to unset rather than failing the query.
func scanDefaultModelPair(w *domain.Workspace, providerID, model *string) {
	if providerID == nil || model == nil || *providerID == "" || *model == "" {
		return
	}
	w.DefaultModel = &domain.DefaultModelPair{ProviderID: *providerID, Model: *model}
}

func (ws *workspaceStore) Create(ctx context.Context, w *domain.Workspace) error {
	if w == nil {
		return domain.ErrInvalid
	}
	if w.Slug == "" {
		return fmt.Errorf("%w: workspace slug cannot be empty", domain.ErrInvalid)
	}
	if !w.IsMaster {
		if err := domain.ValidateSlug(w.Slug); err != nil {
			return err
		}
	}

	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	if w.Timezone == "" {
		w.Timezone = "UTC"
	}

	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	if w.UpdatedAt.IsZero() {
		w.UpdatedAt = now
	}

	query := `
		INSERT INTO workspaces (id, slug, name, description, timezone, is_master, disabled_at, default_provider_id, default_model, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	var defaultProviderID, defaultModel *string
	if w.DefaultModel != nil {
		defaultProviderID = &w.DefaultModel.ProviderID
		defaultModel = &w.DefaultModel.Model
	}
	_, err := ws.db.Exec(ctx, query,
		w.ID,
		w.Slug,
		w.Name,
		w.Description,
		w.Timezone,
		w.IsMaster,
		w.DisabledAt,
		defaultProviderID,
		defaultModel,
		w.CreatedAt,
		w.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ws *workspaceStore) BySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	if slug == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT w.id, w.slug, w.name, COALESCE(w.description, ''), w.timezone, w.is_master, w.disabled_at, w.created_at, w.updated_at,
		       w.default_provider_id, w.default_model
		FROM workspaces w
		WHERE w.slug = $1
	`
	var w domain.Workspace
	var defaultProviderID, defaultModel *string
	err := ws.db.QueryRow(ctx, query, slug).Scan(
		&w.ID,
		&w.Slug,
		&w.Name,
		&w.Description,
		&w.Timezone,
		&w.IsMaster,
		&w.DisabledAt,
		&w.CreatedAt,
		&w.UpdatedAt,
		&defaultProviderID,
		&defaultModel,
	)
	if err != nil {
		return nil, convertError(err)
	}
	scanDefaultModelPair(&w, defaultProviderID, defaultModel)
	return &w, nil
}

func (ws *workspaceStore) ByID(ctx context.Context, id string) (*domain.Workspace, error) {
	if id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT w.id, w.slug, w.name, COALESCE(w.description, ''), w.timezone, w.is_master, w.disabled_at, w.created_at, w.updated_at,
		       w.default_provider_id, w.default_model
		FROM workspaces w
		WHERE w.id = $1
	`
	var w domain.Workspace
	var defaultProviderID, defaultModel *string
	err := ws.db.QueryRow(ctx, query, id).Scan(
		&w.ID,
		&w.Slug,
		&w.Name,
		&w.Description,
		&w.Timezone,
		&w.IsMaster,
		&w.DisabledAt,
		&w.CreatedAt,
		&w.UpdatedAt,
		&defaultProviderID,
		&defaultModel,
	)
	if err != nil {
		return nil, convertError(err)
	}
	scanDefaultModelPair(&w, defaultProviderID, defaultModel)
	return &w, nil
}

func (ws *workspaceStore) Update(ctx context.Context, w *domain.Workspace) error {
	if w == nil || w.ID == "" {
		return domain.ErrInvalid
	}

	now := time.Now().UTC()
	var defaultProviderID, defaultModel *string
	if w.DefaultModel != nil {
		defaultProviderID = &w.DefaultModel.ProviderID
		defaultModel = &w.DefaultModel.Model
	}
	query := `
		UPDATE workspaces
		SET name = CASE WHEN $1 <> '' THEN $1 ELSE name END,
		    description = $2,
		    timezone = CASE WHEN $3 <> '' THEN $3 ELSE timezone END,
		    is_master = $4,
		    disabled_at = $5,
		    default_provider_id = $6,
		    default_model = $7,
		    updated_at = $8
		WHERE id = $9
		RETURNING slug, name, COALESCE(description, ''), timezone, is_master, disabled_at, created_at, updated_at,
		          default_provider_id, default_model
	`
	err := ws.db.QueryRow(ctx, query,
		w.Name,
		w.Description,
		w.Timezone,
		w.IsMaster,
		w.DisabledAt,
		defaultProviderID,
		defaultModel,
		now,
		w.ID,
	).Scan(
		&w.Slug,
		&w.Name,
		&w.Description,
		&w.Timezone,
		&w.IsMaster,
		&w.DisabledAt,
		&w.CreatedAt,
		&w.UpdatedAt,
		&defaultProviderID,
		&defaultModel,
	)
	if err != nil {
		return convertError(err)
	}
	scanDefaultModelPair(w, defaultProviderID, defaultModel)
	return nil
}

func (ws *workspaceStore) ListForUser(ctx context.Context, userID string) ([]domain.Workspace, error) {
	if userID == "" {
		return []domain.Workspace{}, nil
	}

	query := `
		SELECT w.id, w.slug, w.name, COALESCE(w.description, ''), w.timezone, w.is_master, w.disabled_at, w.created_at, w.updated_at,
		       w.default_provider_id, w.default_model
		FROM workspaces w
		JOIN workspace_members wm ON w.id = wm.workspace_id
		WHERE wm.user_id = $1
		ORDER BY w.created_at ASC, w.id ASC
	`
	rows, err := ws.db.Query(ctx, query, userID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	workspaces := make([]domain.Workspace, 0)
	for rows.Next() {
		var w domain.Workspace
		var defaultProviderID, defaultModel *string
		if err := rows.Scan(
			&w.ID,
			&w.Slug,
			&w.Name,
			&w.Description,
			&w.Timezone,
			&w.IsMaster,
			&w.DisabledAt,
			&w.CreatedAt,
			&w.UpdatedAt,
			&defaultProviderID,
			&defaultModel,
		); err != nil {
			return nil, convertError(err)
		}
		scanDefaultModelPair(&w, defaultProviderID, defaultModel)
		workspaces = append(workspaces, w)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return workspaces, nil
}

func (ws *workspaceStore) ListAll(ctx context.Context) ([]domain.Workspace, error) {
	query := `
		SELECT w.id, w.slug, w.name, COALESCE(w.description, ''), w.timezone, w.is_master, w.disabled_at, w.created_at, w.updated_at,
		       w.default_provider_id, w.default_model
		FROM workspaces w
		ORDER BY w.created_at ASC, w.id ASC
	`
	rows, err := ws.db.Query(ctx, query)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	workspaces := make([]domain.Workspace, 0)
	for rows.Next() {
		var w domain.Workspace
		var defaultProviderID, defaultModel *string
		if err := rows.Scan(
			&w.ID,
			&w.Slug,
			&w.Name,
			&w.Description,
			&w.Timezone,
			&w.IsMaster,
			&w.DisabledAt,
			&w.CreatedAt,
			&w.UpdatedAt,
			&defaultProviderID,
			&defaultModel,
		); err != nil {
			return nil, convertError(err)
		}
		scanDefaultModelPair(&w, defaultProviderID, defaultModel)
		workspaces = append(workspaces, w)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return workspaces, nil
}
