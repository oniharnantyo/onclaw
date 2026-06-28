package store

// Profile represents a provider profile configuration in the DB.
type Profile struct {
	Name         string
	ProviderType string
	APIBase      string
	Model        string
	Settings     string // JSON string
	Enabled      int    // 1 (enabled) or 0 (disabled)
	CreatedAt    string
	UpdatedAt    string
}

// Agent represents an agent configuration in the DB.
type Agent struct {
	Name            string
	Provider        string
	Model           string
	ReasoningEffort string // "low", "medium", "high", or empty
	SystemPrompt    string
	Workspace       string
	Tools           string // Comma-separated list of enabled tools
	MaxIterations   int
	CreatedAt       string
	UpdatedAt       string
}
