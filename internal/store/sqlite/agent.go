package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// sqliteAgentStore implements store.AgentStore.
type sqliteAgentStore struct {
	db *sql.DB
}

// NewAgentStore creates a new AgentStore backed by SQLite.
func NewAgentStore(db *sql.DB) store.AgentStore {
	return &sqliteAgentStore{db: db}
}

func (s *sqliteAgentStore) AddAgent(ctx context.Context, a *store.Agent) error {
	if a.Name == "" || a.Provider == "" {
		return fmt.Errorf("agent name and provider must not be empty")
	}

	if a.CreatedAt == "" {
		a.CreatedAt = now()
	}
	a.UpdatedAt = now()

	if a.ModelMetadata == "" {
		a.ModelMetadata = "{}"
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO agents (name, provider, model, model_metadata, reasoning_effort, reasoning_budget_tokens, system_prompt, workspace, tools, max_iterations, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		a.Name, a.Provider, a.Model, a.ModelMetadata, a.ReasoningEffort, a.ReasoningBudgetTokens, a.SystemPrompt, a.Workspace, a.Tools, a.MaxIterations, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *sqliteAgentStore) GetAgent(ctx context.Context, name string) (*store.Agent, error) {
	var a store.Agent
	err := s.db.QueryRowContext(ctx,
		"SELECT name, provider, model, model_metadata, reasoning_effort, reasoning_budget_tokens, system_prompt, workspace, tools, max_iterations, created_at, updated_at FROM agents WHERE name = ?",
		name,
	).Scan(&a.Name, &a.Provider, &a.Model, &a.ModelMetadata, &a.ReasoningEffort, &a.ReasoningBudgetTokens, &a.SystemPrompt, &a.Workspace, &a.Tools, &a.MaxIterations, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *sqliteAgentStore) ListAgents(ctx context.Context) ([]*store.Agent, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, provider, model, model_metadata, reasoning_effort, reasoning_budget_tokens, system_prompt, workspace, tools, max_iterations, created_at, updated_at FROM agents ORDER BY name ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*store.Agent
	for rows.Next() {
		var a store.Agent
		err := rows.Scan(&a.Name, &a.Provider, &a.Model, &a.ModelMetadata, &a.ReasoningEffort, &a.ReasoningBudgetTokens, &a.SystemPrompt, &a.Workspace, &a.Tools, &a.MaxIterations, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			return nil, err
		}
		agents = append(agents, &a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *sqliteAgentStore) UpdateAgent(ctx context.Context, a *store.Agent) error {
	if a.Name == "" || a.Provider == "" {
		return fmt.Errorf("agent name and provider must not be empty")
	}

	a.UpdatedAt = now()

	if a.ModelMetadata == "" {
		a.ModelMetadata = "{}"
	}

	_, err := s.db.ExecContext(ctx,
		"UPDATE agents SET provider = ?, model = ?, model_metadata = ?, reasoning_effort = ?, reasoning_budget_tokens = ?, system_prompt = ?, workspace = ?, tools = ?, max_iterations = ?, updated_at = ? WHERE name = ?",
		a.Provider, a.Model, a.ModelMetadata, a.ReasoningEffort, a.ReasoningBudgetTokens, a.SystemPrompt, a.Workspace, a.Tools, a.MaxIterations, a.UpdatedAt, a.Name,
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *sqliteAgentStore) RemoveAgent(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM agents WHERE name = ?", name)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
