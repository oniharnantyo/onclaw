package service

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/agent"
)

// AssembledAgent defines the interface for running the assembled agent.
type AssembledAgent interface {
	Run(ctx context.Context, userInput string) agent.EventIterator
}

// ResolveAndAssembleFunc resolves agent settings and assembles an agent instance.
type ResolveAndAssembleFunc func(ctx context.Context, agentName, providerName, modelName, reasoning, workspace string, convID int64) (AssembledAgent, string, error)

type ProviderView struct {
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
	APIBase      string `json:"api_base"`
	Settings     string `json:"settings"`
	Enabled      bool   `json:"enabled"`
	IsDefault    bool   `json:"is_default"`
	SecretSet    bool   `json:"secret_set"`
}

type ProfileInput struct {
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
	APIBase      string `json:"api_base"`
	Settings     string `json:"settings"`
	Enabled      bool   `json:"enabled"`
}

type SecretStatus struct {
	Set  bool   `json:"set"`
	Hint string `json:"hint"`
}

type SetSecretInput struct {
	APIKey string `json:"api_key"`
}

type AgentView struct {
	Name                  string `json:"name"`
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	ModelMetadata         string `json:"model_metadata"`
	ReasoningEffort       string `json:"reasoning_effort"`
	ReasoningBudgetTokens int    `json:"reasoning_budget_tokens"`
	SystemPrompt          string `json:"system_prompt"`
	Workspace             string `json:"workspace"`
	Tools                 string `json:"tools"`
	MaxIterations         int    `json:"max_iterations"`
	IsDefault             bool   `json:"is_default"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

type AgentInput struct {
	Name                  string `json:"name"`
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	ModelMetadata         string `json:"model_metadata"`
	ReasoningEffort       string `json:"reasoning_effort"`
	ReasoningBudgetTokens int    `json:"reasoning_budget_tokens"`
	SystemPrompt          string `json:"system_prompt"`
	Workspace             string `json:"workspace"`
	Tools                 string `json:"tools"`
	MaxIterations         int    `json:"max_iterations"`
	IsDefault             bool   `json:"is_default"`
}

type ChatInput struct {
	Prompt         string `json:"prompt"`
	Agent          string `json:"agent"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Reasoning      string `json:"reasoning"`
	Workspace      string `json:"workspace"`
	ConversationID int64  `json:"conversation_id"`
}

type SkillView struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	SourceType  string `json:"source_type"`
	Source      string `json:"source"`
	SkillPath   string `json:"skill_path"`
	Version     string `json:"version"`
	Hash        string `json:"hash"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	InstalledAt string `json:"installed_at"`
	UpdatedAt   string `json:"updated_at"`
}

type DiscoverInput struct {
	Source string `json:"source"`
	Branch string `json:"branch,omitempty"`
}

type DiscoveredSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type DiscoverResult struct {
	PackageName string            `json:"package_name"`
	IsPlugin    bool              `json:"is_plugin"`
	Skills      []DiscoveredSkill `json:"skills"`
}

type InstallSkillInput struct {
	Source        string   `json:"source"`
	SelectedNames []string `json:"selected_names,omitempty"`
	Scope         string   `json:"scope"`
	Branch        string   `json:"branch,omitempty"`
	AsName        string   `json:"as_name,omitempty"`
	Force         bool     `json:"force,omitempty"`
}

type MCPServerView struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Command   string `json:"command"`
	Args      string `json:"args"`
	Env       string `json:"env"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type MCPServerInput struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Command   string `json:"command"`
	Args      string `json:"args"`
	Env       string `json:"env"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
}

type ToggleMCPServerInput struct {
	Enabled bool `json:"enabled"`
}
