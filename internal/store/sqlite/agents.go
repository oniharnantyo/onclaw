package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type agentStore struct{ e executor }

const agentColumns = `id, name, slug, description, brief, avatar, prompts_status, prompts_error,
provider_id, model, effort, temperature, max_tokens, context_window,
tools, skills, mcp_servers, subagents_enabled, created_at, updated_at`

func (a *agentStore) Create(ctx context.Context, agent *store.Agent) error {
	if agent.ID == "" {
		id, err := newID()
		if err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
		agent.ID = id
	}
	if agent.PromptsStatus == "" {
		// A new agent's document lifecycle starts at ready unless the
		// caller says otherwise (mirrors the app-managed timestamps).
		agent.PromptsStatus = store.PromptsStatusReady
	}
	stampCreated(&agent.CreatedAt, &agent.UpdatedAt)
	// The avatar config persists verbatim as a JSON object; no stored config
	// is the column's '' default, never a JSON null.
	avatar := ""
	if len(agent.Avatar) > 0 {
		var err error

		avatar, err = jsonMarshal(agent.Avatar)
		if err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
	}
	tools, err := jsonMarshal(agent.Tools)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	skills, err := jsonMarshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	mcps, err := jsonMarshal(agent.MCPServers)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	_, err = a.e.ExecContext(ctx, `
INSERT INTO agents (`+agentColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.Name, nullString(agent.Slug), agent.Description, agent.Brief, avatar,
		agent.PromptsStatus, agent.PromptsError,
		agent.ProviderID, agent.Model, agent.Effort, agent.Temperature, agent.MaxTokens, agent.ContextWindow,
		tools, skills, mcps, boolInt(agent.SubagentsEnabled),
		formatTime(agent.CreatedAt), formatTime(agent.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (a *agentStore) ByID(ctx context.Context, id string) (*store.Agent, error) {
	row := a.e.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	agent, err := scanAgent(row)
	if err != nil {
		return nil, readErr("get agent", err)
	}
	return agent, nil
}

func (a *agentStore) List(ctx context.Context) ([]store.Agent, error) {
	rows, err := a.e.QueryContext(ctx,
		`SELECT `+agentColumns+` FROM agents ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	agents := make([]store.Agent, 0)
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, fmt.Errorf("list agents: %w", err)
		}
		agents = append(agents, *agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return agents, nil
}

func (a *agentStore) Update(ctx context.Context, agent *store.Agent) error {
	if agent.PromptsStatus == "" {
		agent.PromptsStatus = store.PromptsStatusReady
	}
	agent.UpdatedAt = nowUTC()
	avatar := ""
	if len(agent.Avatar) > 0 {
		var err error

		avatar, err = jsonMarshal(agent.Avatar)
		if err != nil {
			return fmt.Errorf("update agent: %w", err)
		}
	}
	tools, err := jsonMarshal(agent.Tools)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	skills, err := jsonMarshal(agent.Skills)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	mcps, err := jsonMarshal(agent.MCPServers)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}

	res, err := a.e.ExecContext(ctx, `
UPDATE agents SET name = ?, slug = ?, description = ?, brief = ?, avatar = ?, prompts_status = ?,
prompts_error = ?, provider_id = ?, model = ?, effort = ?, temperature = ?, max_tokens = ?,
context_window = ?, tools = ?, skills = ?, mcp_servers = ?, subagents_enabled = ?, updated_at = ?
WHERE id = ?`,
		agent.Name, nullString(agent.Slug), agent.Description, agent.Brief, avatar,
		agent.PromptsStatus, agent.PromptsError,
		agent.ProviderID, agent.Model, agent.Effort, agent.Temperature, agent.MaxTokens, agent.ContextWindow,
		tools, skills, mcps, boolInt(agent.SubagentsEnabled), formatTime(agent.UpdatedAt), agent.ID,
	)
	if err != nil {
		return fmt.Errorf("update agent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update agent %s: %w", agent.ID, store.ErrNotFound)
	}
	return nil
}

func (a *agentStore) Delete(ctx context.Context, id string) error {
	res, err := a.e.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("delete agent %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// scanAgent scans one agents row from a single- or multi-row result.
func scanAgent(row scanner) (*store.Agent, error) {
	var agent store.Agent
	var slug sql.NullString
	var avatar sql.NullString
	var createdAt, updatedAt, tools, skills, mcps string

	err := row.Scan(&agent.ID, &agent.Name, &slug, &agent.Description, &agent.Brief,
		&avatar, &agent.PromptsStatus, &agent.PromptsError,
		&agent.ProviderID, &agent.Model, &agent.Effort, &agent.Temperature, &agent.MaxTokens, &agent.ContextWindow,
		&tools, &skills, &mcps, &agent.SubagentsEnabled, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	agent.Slug = slug.String
	if avatar.Valid && avatar.String != "" {
		if err := jsonUnmarshal(avatar.String, &agent.Avatar); err != nil {
			return nil, err
		}
	}
	if agent.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if agent.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(tools, &agent.Tools); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(skills, &agent.Skills); err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(mcps, &agent.MCPServers); err != nil {
		return nil, err
	}
	return &agent, nil
}
