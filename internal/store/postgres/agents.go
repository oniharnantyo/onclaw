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
	if a == nil || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	// Provider binding is a both-set-or-both-empty pair: the empty pair means
	// inherit-the-workspace-default (NULL columns sit outside the composite FK).
	if err := domain.ValidateDefaultModelPair(a.ProviderID, a.Model); err != nil {
		return err
	}
	// The empty pair persists as SQL NULL; an empty string would fail the uuid
	// column on provider_id.
	var providerIDPtr, modelPtr *string
	if a.ProviderID != "" {
		providerIDPtr = &a.ProviderID
	}
	if a.Model != "" {
		modelPtr = &a.Model
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
	if a.DisabledTools == nil {
		a.DisabledTools = []string{}
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
			context_window, disabled_tools, enabled_mcps,
			memory_sidecall_provider_id, memory_sidecall_model,
			skill_curation_provider_id, skill_curation_model,
			avatar, prompts_status, prompts_error, created_by, updated_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18,
			$19, $20,
			$21, $22, $23, $24, $25, $26, $27
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
		providerIDPtr,
		modelPtr,
		a.Temperature,
		a.MaxTokens,
		a.Effort,
		string(a.Autonomy),
		a.ContextWindow,
		a.DisabledTools,
		a.EnabledMCPS,
		a.MemorySidecallProviderID,
		a.MemorySidecallModel,
		a.SkillCurationProviderID,
		a.SkillCurationModel,
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
		       COALESCE(provider_id::text, ''), COALESCE(model, ''), temperature, max_tokens, effort, autonomy,
		       context_window, disabled_tools, enabled_mcps,
		       memory_sidecall_provider_id, memory_sidecall_model,
		       skill_curation_provider_id, skill_curation_model,
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
		&a.DisabledTools,
		&a.EnabledMCPS,
		&a.MemorySidecallProviderID,
		&a.MemorySidecallModel,
		&a.SkillCurationProviderID,
		&a.SkillCurationModel,
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
	if a.DisabledTools == nil {
		a.DisabledTools = []string{}
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
		       COALESCE(provider_id::text, ''), COALESCE(model, ''), temperature, max_tokens, effort, autonomy,
		       context_window, disabled_tools, enabled_mcps,
		       memory_sidecall_provider_id, memory_sidecall_model,
		       skill_curation_provider_id, skill_curation_model,
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
		&a.DisabledTools,
		&a.EnabledMCPS,
		&a.MemorySidecallProviderID,
		&a.MemorySidecallModel,
		&a.SkillCurationProviderID,
		&a.SkillCurationModel,
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
	if a.DisabledTools == nil {
		a.DisabledTools = []string{}
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
		       COALESCE(provider_id::text, ''), COALESCE(model, ''), temperature, max_tokens, effort, autonomy,
		       context_window, disabled_tools, enabled_mcps,
		       memory_sidecall_provider_id, memory_sidecall_model,
		       skill_curation_provider_id, skill_curation_model,
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
			&a.DisabledTools,
			&a.EnabledMCPS,
			&a.MemorySidecallProviderID,
			&a.MemorySidecallModel,
			&a.SkillCurationProviderID,
			&a.SkillCurationModel,
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
		if a.DisabledTools == nil {
			a.DisabledTools = []string{}
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
	if a == nil || a.ID == "" || a.WorkspaceID == "" || a.Name == "" || a.Slug == "" {
		return fmt.Errorf("%w: missing required agent fields", domain.ErrInvalid)
	}
	// Same pair rule as Create: fully pinned or fully empty, never half.
	if err := domain.ValidateDefaultModelPair(a.ProviderID, a.Model); err != nil {
		return err
	}
	// The empty pair persists as SQL NULL (see Create).
	var providerIDPtr, modelPtr *string
	if a.ProviderID != "" {
		providerIDPtr = &a.ProviderID
	}
	if a.Model != "" {
		modelPtr = &a.Model
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
	if a.DisabledTools == nil {
		a.DisabledTools = []string{}
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
		    disabled_tools = $13,
		    enabled_mcps = $14,
		    memory_sidecall_provider_id = $15,
		    memory_sidecall_model = $16,
		    skill_curation_provider_id = $17,
		    skill_curation_model = $18,
		    avatar = $19,
		    updated_by = $20,
		    updated_at = $21
		WHERE workspace_id = $22 AND id = $23
		RETURNING prompts_status, prompts_error, created_by, created_at
	`
	var promptsStatusStr string
	err := as.db.QueryRow(ctx, query,
		a.Slug,
		a.Name,
		a.Role,
		a.Description,
		a.Brief,
		providerIDPtr,
		modelPtr,
		a.Temperature,
		a.MaxTokens,
		a.Effort,
		string(a.Autonomy),
		a.ContextWindow,
		a.DisabledTools,
		a.EnabledMCPS,
		a.MemorySidecallProviderID,
		a.MemorySidecallModel,
		a.SkillCurationProviderID,
		a.SkillCurationModel,
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

// CountInheriting counts the workspace's inherit agents — provider_id IS NULL
// (the model column follows the pair rule, so the provider column alone
// identifies the empty pair).
func (as *agentStore) CountInheriting(ctx context.Context, workspaceID string) (int, error) {
	if workspaceID == "" {
		return 0, nil
	}

	query := `
		SELECT COUNT(*)
		FROM agents
		WHERE workspace_id = $1 AND provider_id IS NULL
	`
	var count int
	err := as.db.QueryRow(ctx, query, workspaceID).Scan(&count)
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
