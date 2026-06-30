package service

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// ListAgents lists all agents and flags the default one.
func (s *Service) ListAgents(ctx context.Context) ([]AgentView, error) {
	agents, err := s.mgr.ListAgents(ctx)
	if err != nil {
		return nil, classify(err)
	}

	defaultAgent, err := s.kv.Get(ctx, "default_agent")
	if err != nil {
		s.log.Debug("Failed to get default agent preference", "error", err)
	}

	resp := make([]AgentView, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, AgentView{
			Name:                  a.Name,
			Provider:              a.Provider,
			Model:                 a.Model,
			ModelMetadata:         a.ModelMetadata,
			ReasoningEffort:       a.ReasoningEffort,
			ReasoningBudgetTokens: a.ReasoningBudgetTokens,
			SystemPrompt:          a.SystemPrompt,
			Workspace:             a.Workspace,
			Tools:                 a.Tools,
			MaxIterations:         a.MaxIterations,
			IsDefault:             a.Name == defaultAgent,
			CreatedAt:             a.CreatedAt,
			UpdatedAt:             a.UpdatedAt,
		})
	}

	return resp, nil
}

// CreateAgent adds a new agent profile and updates the default preference if requested.
func (s *Service) CreateAgent(ctx context.Context, input AgentInput) (*store.Agent, error) {
	a := &store.Agent{
		Name:                  input.Name,
		Provider:              input.Provider,
		Model:                 input.Model,
		ModelMetadata:         input.ModelMetadata,
		ReasoningEffort:       input.ReasoningEffort,
		ReasoningBudgetTokens: input.ReasoningBudgetTokens,
		SystemPrompt:          input.SystemPrompt,
		Workspace:             input.Workspace,
		Tools:                 input.Tools,
		MaxIterations:         input.MaxIterations,
	}

	if err := s.mgr.AddAgent(ctx, a); err != nil {
		return nil, classify(err)
	}

	if input.IsDefault {
		if err := s.kv.Set(ctx, "default_agent", input.Name); err != nil {
			return nil, classify(err)
		}
	}

	return a, nil
}

// GetAgent retrieves a single agent by name.
func (s *Service) GetAgent(ctx context.Context, name string) (AgentView, error) {
	a, err := s.mgr.GetAgent(ctx, name)
	if err != nil {
		return AgentView{}, classify(err)
	}

	defaultAgent, _ := s.kv.Get(ctx, "default_agent")

	return AgentView{
		Name:                  a.Name,
		Provider:              a.Provider,
		Model:                 a.Model,
		ModelMetadata:         a.ModelMetadata,
		ReasoningEffort:       a.ReasoningEffort,
		ReasoningBudgetTokens: a.ReasoningBudgetTokens,
		SystemPrompt:          a.SystemPrompt,
		Workspace:             a.Workspace,
		Tools:                 a.Tools,
		MaxIterations:         a.MaxIterations,
		IsDefault:             a.Name == defaultAgent,
		CreatedAt:             a.CreatedAt,
		UpdatedAt:             a.UpdatedAt,
	}, nil
}

// UpdateAgent updates agent settings and default preference.
func (s *Service) UpdateAgent(ctx context.Context, name string, input AgentInput) (*store.Agent, error) {
	if _, err := s.mgr.GetAgent(ctx, name); err != nil {
		return nil, classify(err)
	}

	a := &store.Agent{
		Name:                  name,
		Provider:              input.Provider,
		Model:                 input.Model,
		ModelMetadata:         input.ModelMetadata,
		ReasoningEffort:       input.ReasoningEffort,
		ReasoningBudgetTokens: input.ReasoningBudgetTokens,
		SystemPrompt:          input.SystemPrompt,
		Workspace:             input.Workspace,
		Tools:                 input.Tools,
		MaxIterations:         input.MaxIterations,
	}

	if err := s.mgr.UpdateAgent(ctx, a); err != nil {
		return nil, classify(err)
	}

	if input.IsDefault {
		if err := s.kv.Set(ctx, "default_agent", name); err != nil {
			return nil, classify(err)
		}
	}

	return a, nil
}

// DeleteAgent deletes an agent profile and cleans up default preference.
func (s *Service) DeleteAgent(ctx context.Context, name string) error {
	if _, err := s.mgr.GetAgent(ctx, name); err != nil {
		return classify(err)
	}

	if err := s.mgr.RemoveAgent(ctx, name); err != nil {
		return classify(err)
	}

	defaultAgent, _ := s.kv.Get(ctx, "default_agent")
	if defaultAgent == name {
		_ = s.kv.Delete(ctx, "default_agent")
	}

	return nil
}
