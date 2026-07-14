package store

import "context"

// ProfileStore defines LLM provider profile operations.
type ProfileStore interface {
	AddProfile(ctx context.Context, p *Profile) error
	GetProfile(ctx context.Context, name string) (*Profile, error)
	ListProfiles(ctx context.Context) ([]*Profile, error)
	RemoveProfile(ctx context.Context, name string) error
}

// SecretStore defines opaque encrypted config key-value operations.
type SecretStore interface {
	SetSecret(ctx context.Context, key string, encryptedValue string) error
	GetSecret(ctx context.Context, key string) (string, error)
	DeleteSecret(ctx context.Context, key string) error
}

// KVStore defines application preference operations.
type KVStore interface {
	Set(ctx context.Context, key string, value string) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

// AgentStore defines agent configuration operations.
type AgentStore interface {
	AddAgent(ctx context.Context, a *Agent) error
	GetAgent(ctx context.Context, name string) (*Agent, error)
	ListAgents(ctx context.Context) ([]*Agent, error)
	UpdateAgent(ctx context.Context, a *Agent) error
	RemoveAgent(ctx context.Context, name string) error
	UpdateAgentTools(ctx context.Context, name string, tools string) error
}

// ConversationStore defines operations for persisting and retrieving conversation history.
type ConversationStore interface {
	CreateConversation(ctx context.Context, agentName string) (int64, error)
	AppendTurn(ctx context.Context, convID int64, msgArrayJSON string, responseID string, previousResponseID string, model string, prompt int64, completion int64, total int64, question string, answer string) (seq int64, err error)
	LoadHistory(ctx context.Context, conversationID int64) (summary *TurnRow, tail []*TurnRow, err error)
	ListTurns(ctx context.Context, conversationID int64) ([]*TurnRow, error)
	SaveSummary(ctx context.Context, conversationID int64, summaryMessageJSON string, coveredUntilSeq int64) error
	// GetCompactionMeta returns conversation-level compaction metadata: the
	// number of summary (is_summary) rows and the created_at of the most
	// recent one. Used by the API to surface compaction without scanning content.
	GetCompactionMeta(ctx context.Context, conversationID int64) (count int, lastAt string, err error)
	// Transcript returns a readable dump of turns with sequence_num <= upToSeq,
	// i.e. the compacted range, so the agent can re-read exact prior detail.
	Transcript(ctx context.Context, conversationID int64, upToSeq int64) (string, error)
	ListConversations(ctx context.Context) ([]*ConversationRow, error)
}

// SkillStore defines skill metadata ledger operations.
type SkillStore interface {
	AddSkill(ctx context.Context, s *Skill) error
	GetSkill(ctx context.Context, name string, scope string) (*Skill, error)
	ListSkills(ctx context.Context) ([]*Skill, error)
	ListSkillsByScope(ctx context.Context, scope string) ([]*Skill, error)
	UpdateSkill(ctx context.Context, s *Skill) error
	RemoveSkill(ctx context.Context, name string, scope string) error
}

// MCPServerStore defines MCP server configuration operations.
type MCPServerStore interface {
	AddServer(ctx context.Context, s *MCPServer) error
	GetServer(ctx context.Context, name string) (*MCPServer, error)
	ListServers(ctx context.Context) ([]*MCPServer, error)
	UpdateServer(ctx context.Context, s *MCPServer) error
	RemoveServer(ctx context.Context, name string) error
	ListAgentServers(ctx context.Context, agentName string) ([]*MCPServer, error)
	SetAgentServerEnabled(ctx context.Context, agentName string, serverName string, enabled bool) error
}

// HookStore defines operations for managing agent hooks.
type HookStore interface {
	AddHook(ctx context.Context, h *Hook) error
	GetHook(ctx context.Context, id string) (*Hook, error)
	ListHooks(ctx context.Context) ([]*Hook, error)
	ListHooksByScopeAndEvent(ctx context.Context, scope string, event string) ([]*Hook, error)
	UpdateHook(ctx context.Context, h *Hook) error
	RemoveHook(ctx context.Context, id string) error
	ToggleHook(ctx context.Context, id string, enabled bool) error
}

// HookExecutionStore defines operations for logging hook executions.
type HookExecutionStore interface {
	AppendExecution(ctx context.Context, exec *HookExecution) error
	ListExecutions(ctx context.Context) ([]*HookExecution, error)
}

// ToolRegistryStore defines operations for listing, toggling, and retrieving tools.
type ToolRegistryStore interface {
	ListTools(ctx context.Context) ([]*ToolRegistry, error)
	GetTool(ctx context.Context, name string) (*ToolRegistry, error)
	UpsertTool(ctx context.Context, t *ToolRegistry) error
	ToggleTool(ctx context.Context, name string, enabled bool) error
}

// ToolGroupConfigStore defines operations for retrieving and updating category configuration.
type ToolGroupConfigStore interface {
	GetConfig(ctx context.Context, category string) (*ToolGroupConfig, error)
	PutConfig(ctx context.Context, category string, config string) error
}
