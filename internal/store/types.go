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
