package store

import "context"

// Store is the root persistence port: one accessor per aggregate plus
// transaction management. Configuration changes that span aggregates are
// made atomic by running them inside WithTx.
type Store interface {
	Agents() AgentStore
	Sessions() SessionsStore
	Providers() ProviderStore
	MCPServers() MCPServerStore
	MCPOAuthTokens() MCPOAuthTokenStore
	Schedules() ScheduleStore
	Hooks() HookStore
	GatewaySettings() GatewaySettingsStore
	SessionEvents() SessionEventStore
	Checkpoints() CheckpointStore
	Skills() SkillRegistry
	ToolStates() ToolStateStore
	AgentHooks() AgentHookStore
	HookExecutions() HookExecutionStore
	ToolSettings() ToolSettingStore
	Attachments() AttachmentStore
	ProviderModels() ProviderModelStore
	ProviderModelStore
	// WithTx runs fn atomically: every write fn makes through the Store it
	// receives commits together, or — when fn returns an error — rolls
	// back, leaving no partial rows.
	WithTx(ctx context.Context, fn func(Store) error) error
	Close() error
}
