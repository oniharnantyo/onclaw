package tools

import (
	"context"
	"database/sql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ToolGroupCfg defines the interface to retrieve tool configuration for a category.
type ToolGroupCfg interface {
	GetConfig(ctx context.Context, category string) (string, error)
}

// Scope defines the workspace and security configurations for tools.
type Scope struct {
	Workspace        string
	ShellPolicy      string
	ShellAllowlist   []string
	ShellDenylist    []string
	ToolGroupCfg     ToolGroupCfg
	KVStore          store.KVStore
	SecretResolver   secrets.SecretResolver
	AgentName        string
	Db               *sql.DB
	MemoryStore      memory.MemoryStore
	Embedder         *memory.Embedder
	StagedWriteStore memory.StagedWriteStore
	CharLimit        int
	KGStore          memory.KGStore
	KGTraversalDepth int
}

// Tool defines the interface that extensible tools must implement to register with the system.
type Tool interface {
	Name() string
	Desc() string
	Category() string
	Build(scope *Scope) tool.InvokableTool
}
