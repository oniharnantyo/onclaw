package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// agentStore implements storeport.AgentStore for PostgreSQL.
type agentStore struct {
	db Executor
}

// NewAgentStore creates a new AgentStore with the given database executor.
func NewAgentStore(db Executor) storeport.AgentStore {
	return &agentStore{db: db}
}

func (as *agentStore) Create(ctx context.Context, a *domain.Agent) error {
	if a == nil || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" || a.ProviderID == "" || a.Model == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentSlug(a.Slug); err != nil {
		return err
	}
	if a.Autonomy != "" {
		if err := domain.ValidateAgentAutonomy(a.Autonomy); err != nil {
			return err
		}
	} else {
		a.Autonomy = domain.AutonomyApproval
	}
	if err := domain.ValidateAgentTemperature(a.Temperature); err != nil {
		return err
	}
	if a.MaxTokens != nil && *a.MaxTokens <= 0 {
		return fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentContextWindow(a.ContextWindow); err != nil {
		return err
	}
	if len(a.Avatar) > 0 {
		if err := domain.ValidateAgentAvatar(a.Avatar); err != nil {
			return err
		}
	} else {
		a.Avatar = json.RawMessage("{}")
	}
	if a.PromptsStatus == "" {
		a.PromptsStatus = domain.PromptsStatusGenerating
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if a.EnabledMCPS == nil {
		a.EnabledMCPS = []string{}
	}

	if a.ID == "" {
		a.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}

	query := `
		INSERT INTO agents (
			id, workspace_id, slug, name, role, description, brief,
			provider_id, model, temperature, max_tokens, effort, autonomy,
			context_window, tools, enabled_mcps,
			memory_sidecall_provider_id, memory_sidecall_model,
			avatar, prompts_status, prompts_error, created_by, updated_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18,
			$19, $20, $21, $22, $23, $24, $25
		)
	`
	_, err := as.db.Exec(ctx, query,
		a.ID,
		a.WorkspaceID,
		a.Slug,
		a.Name,
		a.Role,
		a.Description,
		a.Brief,
		a.ProviderID,
		a.Model,
		a.Temperature,
		a.MaxTokens,
		a.Effort,
		string(a.Autonomy),
		a.ContextWindow,
		a.Tools,
		a.EnabledMCPS,
		a.MemorySidecallProviderID,
		a.MemorySidecallModel,
		[]byte(a.Avatar),
		string(a.PromptsStatus),
		a.PromptsError,
		a.CreatedBy,
		a.UpdatedBy,
		a.CreatedAt,
		a.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (as *agentStore) ByID(ctx context.Context, workspaceID, id string) (*domain.Agent, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, slug, name, role, description, brief,
		       provider_id, model, temperature, max_tokens, effort, autonomy,
		       context_window, tools, enabled_mcps,
		       memory_sidecall_provider_id, memory_sidecall_model,
		       avatar, prompts_status, prompts_error, created_by, updated_by, created_at, updated_at
		FROM agents
		WHERE workspace_id = $1 AND id = $2
	`
	var a domain.Agent
	var autonomyStr, promptsStatusStr string
	var avatarBytes []byte
	err := as.db.QueryRow(ctx, query, workspaceID, id).Scan(
		&a.ID,
		&a.WorkspaceID,
		&a.Slug,
		&a.Name,
		&a.Role,
		&a.Description,
		&a.Brief,
		&a.ProviderID,
		&a.Model,
		&a.Temperature,
		&a.MaxTokens,
		&a.Effort,
		&autonomyStr,
		&a.ContextWindow,
		&a.Tools,
		&a.EnabledMCPS,
		&a.MemorySidecallProviderID,
		&a.MemorySidecallModel,
		&avatarBytes,
		&promptsStatusStr,
		&a.PromptsError,
		&a.CreatedBy,
		&a.UpdatedBy,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	a.Autonomy = domain.AgentAutonomy(autonomyStr)
	a.PromptsStatus = domain.PromptsStatus(promptsStatusStr)
	if len(avatarBytes) > 0 {
		a.Avatar = json.RawMessage(avatarBytes)
	} else {
		a.Avatar = json.RawMessage("{}")
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if a.EnabledMCPS == nil {
		a.EnabledMCPS = []string{}
	}
	return &a, nil
}

func (as *agentStore) BySlug(ctx context.Context, workspaceID, slug string) (*domain.Agent, error) {
	if workspaceID == "" || slug == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, slug, name, role, description, brief,
		       provider_id, model, temperature, max_tokens, effort, autonomy,
		       context_window, tools, enabled_mcps,
		       memory_sidecall_provider_id, memory_sidecall_model,
		       avatar, prompts_status, prompts_error, created_by, updated_by, created_at, updated_at
		FROM agents
		WHERE workspace_id = $1 AND slug = $2
	`
	var a domain.Agent
	var autonomyStr, promptsStatusStr string
	var avatarBytes []byte
	err := as.db.QueryRow(ctx, query, workspaceID, slug).Scan(
		&a.ID,
		&a.WorkspaceID,
		&a.Slug,
		&a.Name,
		&a.Role,
		&a.Description,
		&a.Brief,
		&a.ProviderID,
		&a.Model,
		&a.Temperature,
		&a.MaxTokens,
		&a.Effort,
		&autonomyStr,
		&a.ContextWindow,
		&a.Tools,
		&a.EnabledMCPS,
		&a.MemorySidecallProviderID,
		&a.MemorySidecallModel,
		&avatarBytes,
		&promptsStatusStr,
		&a.PromptsError,
		&a.CreatedBy,
		&a.UpdatedBy,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	a.Autonomy = domain.AgentAutonomy(autonomyStr)
	a.PromptsStatus = domain.PromptsStatus(promptsStatusStr)
	if len(avatarBytes) > 0 {
		a.Avatar = json.RawMessage(avatarBytes)
	} else {
		a.Avatar = json.RawMessage("{}")
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if a.EnabledMCPS == nil {
		a.EnabledMCPS = []string{}
	}
	return &a, nil
}

func (as *agentStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.Agent, error) {
	if workspaceID == "" {
		return []domain.Agent{}, nil
	}

	query := `
		SELECT id, workspace_id, slug, name, role, description, brief,
		       provider_id, model, temperature, max_tokens, effort, autonomy,
		       context_window, tools, enabled_mcps,
		       memory_sidecall_provider_id, memory_sidecall_model,
		       avatar, prompts_status, prompts_error, created_by, updated_by, created_at, updated_at
		FROM agents
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
	`
	rows, err := as.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	agents := make([]domain.Agent, 0)
	for rows.Next() {
		var a domain.Agent
		var autonomyStr, promptsStatusStr string
		var avatarBytes []byte
		if err := rows.Scan(
			&a.ID,
			&a.WorkspaceID,
			&a.Slug,
			&a.Name,
			&a.Role,
			&a.Description,
			&a.Brief,
			&a.ProviderID,
			&a.Model,
			&a.Temperature,
			&a.MaxTokens,
			&a.Effort,
			&autonomyStr,
			&a.ContextWindow,
			&a.Tools,
			&a.EnabledMCPS,
		&a.MemorySidecallProviderID,
		&a.MemorySidecallModel,
			&avatarBytes,
			&promptsStatusStr,
			&a.PromptsError,
			&a.CreatedBy,
			&a.UpdatedBy,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, convertError(err)
		}
		a.Autonomy = domain.AgentAutonomy(autonomyStr)
		a.PromptsStatus = domain.PromptsStatus(promptsStatusStr)
		if len(avatarBytes) > 0 {
			a.Avatar = json.RawMessage(avatarBytes)
		} else {
			a.Avatar = json.RawMessage("{}")
		}
		if a.Tools == nil {
			a.Tools = []string{}
		}
		if a.EnabledMCPS == nil {
			a.EnabledMCPS = []string{}
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return agents, nil
}

func (as *agentStore) Update(ctx context.Context, a *domain.Agent) error {
	if a == nil || a.ID == "" || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" || a.ProviderID == "" || a.Model == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentSlug(a.Slug); err != nil {
		return err
	}
	if a.Autonomy != "" {
		if err := domain.ValidateAgentAutonomy(a.Autonomy); err != nil {
			return err
		}
	} else {
		a.Autonomy = domain.AutonomyApproval
	}
	if err := domain.ValidateAgentTemperature(a.Temperature); err != nil {
		return err
	}
	if a.MaxTokens != nil && *a.MaxTokens <= 0 {
		return fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentContextWindow(a.ContextWindow); err != nil {
		return err
	}
	if len(a.Avatar) > 0 {
		if err := domain.ValidateAgentAvatar(a.Avatar); err != nil {
			return err
		}
	} else {
		a.Avatar = json.RawMessage("{}")
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	if a.EnabledMCPS == nil {
		a.EnabledMCPS = []string{}
	}

	now := time.Now().UTC()
	a.UpdatedAt = now

	query := `
		UPDATE agents
		SET slug = $1,
		    name = $2,
		    role = $3,
		    description = $4,
		    brief = $5,
		    provider_id = $6,
		    model = $7,
		    temperature = $8,
		    max_tokens = $9,
		    effort = $10,
		    autonomy = $11,
		    context_window = $12,
		    tools = $13,
		    enabled_mcps = $14,
		    memory_sidecall_provider_id = $15,
		    memory_sidecall_model = $16,
		    avatar = $17,
		    updated_by = $18,
		    updated_at = $19
			WHERE workspace_id = $20 AND id = $21
		RETURNING prompts_status, prompts_error, created_by, created_at
	`
	var promptsStatusStr string
	err := as.db.QueryRow(ctx, query,
		a.Slug,
		a.Name,
		a.Role,
		a.Description,
		a.Brief,
		a.ProviderID,
		a.Model,
		a.Temperature,
		a.MaxTokens,
		a.Effort,
		string(a.Autonomy),
		a.ContextWindow,
		a.Tools,
		a.EnabledMCPS,
		a.MemorySidecallProviderID,
		a.MemorySidecallModel,
		[]byte(a.Avatar),
		a.UpdatedBy,
		now,
		a.WorkspaceID,
		a.ID,
	).Scan(
		&promptsStatusStr,
		&a.PromptsError,
		&a.CreatedBy,
		&a.CreatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	a.PromptsStatus = domain.PromptsStatus(promptsStatusStr)
	return nil
}

func (as *agentStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM agents
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := as.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (as *agentStore) CountByProvider(ctx context.Context, workspaceID, providerID string) (int, error) {
	if workspaceID == "" || providerID == "" {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM agents
		WHERE workspace_id = $1 AND provider_id = $2
	`
	var count int
	err := as.db.QueryRow(ctx, query, workspaceID, providerID).Scan(&count)
	if err != nil {
		return 0, convertError(err)
	}
	return count, nil
}

func (as *agentStore) SetPromptState(ctx context.Context, workspaceID, id string, status domain.PromptsStatus, promptsErr *string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	now := time.Now().UTC()
	query := `
		UPDATE agents
		SET prompts_status = $1,
		    prompts_error = $2,
		    updated_at = $3
		WHERE workspace_id = $4 AND id = $5
	`
	tag, err := as.db.Exec(ctx, query,
		string(status),
		promptsErr,
		now,
		workspaceID,
		id,
	)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (as *agentStore) SweepGenerating(ctx context.Context, errMsg string) (int64, error) {
	now := time.Now().UTC()
	query := `
		UPDATE agents
		SET prompts_status = 'failed',
		    prompts_error = $1,
		    updated_at = $2
		WHERE prompts_status = 'generating'
	`
	tag, err := as.db.Exec(ctx, query, errMsg, now)
	if err != nil {
		return 0, convertError(err)
	}
	return tag.RowsAffected(), nil
}
