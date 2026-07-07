package store

// Profile represents a provider profile configuration in the DB.
type Profile struct {
	Name         string
	ProviderType string
	APIBase      string
	Settings     string // JSON string
	Enabled      int    // 1 (enabled) or 0 (disabled)
	CreatedAt    string
	UpdatedAt    string
}

// ReasoningOption represents a reasoning option capability stored in ModelMetadata.
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Min    int      `json:"min,omitempty"`
	Max    int      `json:"max,omitempty"`
}

// ModelMetadata represents discovered metadata for a model.
type ModelMetadata struct {
	ContextWindow    int               `json:"context_window"`
	Thinking         bool              `json:"thinking"`
	InputModalities  []string          `json:"input_modalities"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
}

// Agent represents an agent configuration in the DB.
type Agent struct {
	Name                  string
	Provider              string
	Model                 string
	ModelMetadata         string // JSON string representing ModelMetadata
	ReasoningEffort       string // reasoning effort level (e.g. low, medium, high, minimal, xhigh, max, none, or toggle: on/off)
	ReasoningBudgetTokens int
	SystemPrompt          string
	Workspace             string
	Tools                 string // Comma-separated list of enabled tools
	MaxIterations         int
	CreatedAt             string
	UpdatedAt             string
}

// Conversation represents a conversation configuration/metadata in the DB.
type Conversation struct {
	ID               int64
	AgentName        string
	SummaryUntilSeq  int64
	SummaryMessageID int64
	CreatedAt        string
	UpdatedAt        string
}

// TurnRow represents a persisted conversation turn in the DB.
type TurnRow struct {
	ID                 int64  `json:"id"`
	ConversationID     int64  `json:"conversation_id"`
	SequenceNum        int64  `json:"sequence_num"`
	ResponseID         string `json:"response_id"`
	PreviousResponseID string `json:"previous_response_id"`
	Message            string `json:"message"` // JSON array of the turn's AgenticMessage deltas
	Model              string `json:"model"`
	PromptTokens       int64  `json:"prompt_tokens"`
	CompletionTokens   int64  `json:"completion_tokens"`
	TotalTokens        int64  `json:"total_tokens"`
	Question           string `json:"question"`
	Answer             string `json:"answer"`
	CreatedAt          string `json:"created_at"`
}

// TurnMeta represents metadata for a committed turn.
type TurnMeta struct {
	ConversationID     int64  `json:"conversation_id"`
	SequenceNum        int64  `json:"sequence_num"`
	ResponseID         string `json:"response_id"`
	PreviousResponseID string `json:"previous_response_id"`
	Model              string `json:"model"`
	Tokens             int64  `json:"tokens"`
}


// ConversationRow represents a summarized conversation for listing in the web UI.
type ConversationRow struct {
	ID           int64  `json:"id"`
	AgentName    string `json:"agent_name"`
	MessageCount int    `json:"message_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	Preview      string `json:"preview"`
}

// Skill represents an installed skill metadata ledger entry in the DB.
type Skill struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`       // e.g. "global" or agent name
	SourceType  string `json:"source_type"` // e.g. "github", "http", "local", "plugin"
	Source      string `json:"source"`      // original source identifier/URL
	SkillPath   string `json:"skill_path"`  // relative/absolute directory path on disk
	Version     string `json:"version"`
	Hash        string `json:"hash"`
	Description string `json:"description"`
	Enabled     int    `json:"enabled"` // 1 = enabled, 0 = disabled
	InstalledAt string `json:"installed_at"`
	UpdatedAt   string `json:"updated_at"`
}

// MCPServer represents a managed Model Context Protocol server configuration in the DB.
type MCPServer struct {
	Name      string `json:"name"`
	Transport string `json:"transport"` // "stdio", "http", or "sse"
	Command   string `json:"command"`   // Command to run for stdio
	Args      string `json:"args"`      // JSON array of command arguments
	Env       string `json:"env"`       // JSON object of environment variables
	URL       string `json:"url"`       // Server URL for http or sse
	Enabled   int    `json:"enabled"`   // 1 = enabled, 0 = disabled
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Hook represents a user-configured lifecycle hook definition.
type Hook struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Scope       string `json:"scope"`        // "global" or agent name
	Event       string `json:"event"`        // e.g. "session_start", "user_prompt_submit", "pre_tool_use", "post_tool_use", "stop"
	HandlerType string `json:"handler_type"` // "command" or "script"
	Config      string `json:"config"`       // JSON string representing handler-specific config
	Matcher     string `json:"matcher"`      // Regex matching tool_name (optional)
	TimeoutMS   int    `json:"timeout_ms"`
	OnTimeout   string `json:"on_timeout"` // "block" or "allow"
	Priority    int    `json:"priority"`
	Enabled     int    `json:"enabled"` // 1 = enabled, 0 = disabled
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// HookExecution represents an audit log entry for a hook execution.
type HookExecution struct {
	ID          int64  `json:"id"`
	HookID      string `json:"hook_id"` // references Hook.ID, can be empty/NULL if hook was deleted
	Event       string `json:"event"`
	HandlerType string `json:"handler_type"`
	Decision    string `json:"decision"` // "allow" or "block" or "observe"
	DurationMS  int64  `json:"duration_ms"`
	Error       string `json:"error"`
	CreatedAt   string `json:"created_at"`
}

// ToolRegistry represents a registered tool with enable state.
type ToolRegistry struct {
	Name      string `json:"name"`
	Category  string `json:"category"`
	Enabled   int    `json:"enabled"` // 1 = enabled, 0 = disabled
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ToolGroupConfig represents category-level configuration.
type ToolGroupConfig struct {
	Category  string `json:"category"`
	Config    string `json:"config"` // JSON string
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}
