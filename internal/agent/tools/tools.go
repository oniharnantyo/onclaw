package tools

import (
	"github.com/cloudwego/eino/components/tool"
)

// Scope defines the workspace and security configurations for tools.
type Scope struct {
	Workspace      string
	ShellPolicy    string
	ShellAllowlist []string
}

// Tool defines the interface that extensible tools must implement to register with the system.
type Tool interface {
	Name() string
	Desc() string
	Build(scope *Scope) tool.InvokableTool
}
