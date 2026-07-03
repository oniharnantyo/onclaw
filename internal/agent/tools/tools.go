package tools

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ToolGroupCfg defines the interface to retrieve tool configuration for a category.
type ToolGroupCfg interface {
	GetConfig(ctx context.Context, category string) (string, error)
}

// Scope defines the workspace and security configurations for tools.
type Scope struct {
	Workspace      string
	ShellPolicy    string
	ShellAllowlist []string
	ToolGroupCfg   ToolGroupCfg
	KVStore        store.KVStore
	SecretResolver secrets.SecretResolver
}

// Tool defines the interface that extensible tools must implement to register with the system.
type Tool interface {
	Name() string
	Desc() string
	Category() string
	Build(scope *Scope) tool.InvokableTool
}
