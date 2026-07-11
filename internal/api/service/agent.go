package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/workspace"
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

	// Fetch skills and MCP servers for all agents
	allSkills, err := s.ListSkills(ctx)
	if err != nil {
		s.log.Debug("Failed to fetch skills for agents list", "error", err)
		allSkills = []SkillView{}
	}

	allMCP, err := s.ListMCP(ctx)
	if err != nil {
		s.log.Debug("Failed to fetch MCP servers for agents list", "error", err)
		allMCP = []MCPServerView{}
	}

	resp := make([]AgentView, 0, len(agents))
	for _, a := range agents {
		// Count skills for this agent (scope matches agent name)
		skillsCount := 0
		for _, skill := range allSkills {
			if skill.Scope == a.Name && skill.Enabled {
				skillsCount++
			}
		}

		// Count MCP servers for this agent
		mcpCount := 0
		for _, mcp := range allMCP {
			if mcp.Enabled {
				mcpCount++
			}
		}

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
			MaxContextTokens:      a.MaxContextTokens,
			MemoryConfig:          a.MemoryConfig,
			IsDefault:             a.Name == defaultAgent,
			CreatedAt:             a.CreatedAt,
			UpdatedAt:             a.UpdatedAt,
			SkillsCount:           skillsCount,
			MCPCount:              mcpCount,
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
		MaxContextTokens:      input.MaxContextTokens,
		MemoryConfig:          input.MemoryConfig,
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
		MaxContextTokens:      a.MaxContextTokens,
		MemoryConfig:          a.MemoryConfig,
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
		MaxContextTokens:      input.MaxContextTokens,
		MemoryConfig:          input.MemoryConfig,
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

var whitelistedPersonaFiles = map[string]bool{
	"BOOTSTRAP.md":    true,
	"IDENTITY.md":     true,
	"SOUL.md":         true,
	"CAPABILITIES.md": true,
	"USER.md":         true,
	"AGENTS.md":       true,
	"MEMORY.md":       true,
}

// GetAgentPersona retrieves the content of a specific whitelisted persona file for the agent.
func (s *Service) GetAgentPersona(ctx context.Context, name string, file string) (string, error) {
	if !whitelistedPersonaFiles[file] {
		return "", fmt.Errorf("%w: file %q is not a whitelisted persona file", ErrInvalidInput, file)
	}

	ws, err := s.resolveAgentWorkspace(ctx, name)
	if err != nil {
		return "", err
	}

	path := filepath.Join(ws, filepath.Base(file))
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read persona file %s: %w", file, err)
	}

	return string(content), nil
}

// SetAgentPersona updates the content of a specific whitelisted persona file for the agent.
func (s *Service) SetAgentPersona(ctx context.Context, name string, file string, content string) error {
	if !whitelistedPersonaFiles[file] {
		return fmt.Errorf("%w: file %q is not a whitelisted persona file", ErrInvalidInput, file)
	}

	if err := memory.ScanContent(content); err != nil {
		return fmt.Errorf("%w: security threat detected in persona content: %v", ErrInvalidInput, err)
	}

	ws, err := s.resolveAgentWorkspace(ctx, name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(ws, 0755); err != nil {
		return fmt.Errorf("create agent workspace: %w", err)
	}

	path := filepath.Join(ws, filepath.Base(file))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write persona file %s: %w", file, err)
	}

	return nil
}

func (s *Service) resolveAgentWorkspace(ctx context.Context, name string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if name == "global" {
		return workspace.ResolveWorkspace("", "", s.workspacePath, cwd)
	}
	agent, err := s.mgr.GetAgent(ctx, name)
	if err != nil {
		return "", classify(err)
	}
	return workspace.ResolveWorkspace("", agent.Workspace, s.workspacePath, cwd)
}

// SetAgentTools updates the list of enabled tools for the agent.
func (s *Service) SetAgentTools(ctx context.Context, name string, tool string, enabled bool) error {
	agent, err := s.mgr.GetAgent(ctx, name)
	if err != nil {
		return classify(err)
	}

	if tool == "*" {
		var newTools string
		if enabled {
			list, err := s.toolRegistryStore.ListTools(ctx)
			if err != nil {
				return classify(err)
			}
			var allNames []string
			for _, t := range list {
				allNames = append(allNames, t.Name)
			}
			newTools = strings.Join(allNames, ",")
		} else {
			newTools = ""
		}
		if err := s.mgr.UpdateAgentTools(ctx, name, newTools); err != nil {
			return classify(err)
		}
		return nil
	}

	// An empty allowlist means "all globally-enabled tools" (matches assembly). Toggling a
	// single tool from that state must be symmetric: disabling one tool stores the explicit
	// list of every other registry tool; enabling one is a no-op (stays empty).
	if agent.Tools == "" {
		var newTools string
		if enabled {
			newTools = ""
		} else {
			list, err := s.toolRegistryStore.ListTools(ctx)
			if err != nil {
				return classify(err)
			}
			var allNames []string
			for _, t := range list {
				if t.Name != tool {
					allNames = append(allNames, t.Name)
				}
			}
			newTools = strings.Join(allNames, ",")
		}
		if err := s.mgr.UpdateAgentTools(ctx, name, newTools); err != nil {
			return classify(err)
		}
		return nil
	}

	var tools []string
	if agent.Tools != "" {
		for _, t := range strings.Split(agent.Tools, ",") {
			trimmed := strings.TrimSpace(t)
			if trimmed != "" {
				tools = append(tools, trimmed)
			}
		}
	}

	found := false
	var updated []string
	for _, t := range tools {
		if t == tool {
			found = true
			if enabled {
				updated = append(updated, t)
			}
		} else {
			updated = append(updated, t)
		}
	}
	if enabled && !found {
		updated = append(updated, tool)
	}

	newTools := strings.Join(updated, ",")

	if err := s.mgr.UpdateAgentTools(ctx, name, newTools); err != nil {
		return classify(err)
	}
	return nil
}
