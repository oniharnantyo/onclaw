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
	Documents() DocumentStore
	DocumentPresets() DocumentPresetStore
	// DocumentTemplates returns the on-disk template store holding the
	// uploaded skeletons of template-document presets
	// (add-document-editing design D6). It is filesystem-backed — it needs
	// the data directory, not the database — so implementations root it at
	// the aggregate's data directory and it takes no part in WithTx.
	DocumentTemplates() DocumentTemplateStore
	ProviderModels() ProviderModelStore
	ProviderModelStore
	Memory() MemoryStore
	// WithTx runs fn atomically: every write fn makes through the Store it
	// receives commits together, or — when fn returns an error — rolls
	// back, leaving no partial rows.
	WithTx(ctx context.Context, fn func(Store) error) error
	Close() error
}
