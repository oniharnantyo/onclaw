package tools

import (
	"github.com/cloudwego/eino/components/tool"
)

var (
	registry []Tool
)

// Register registers a tool definition to the global registry.
func Register(t Tool) {
	registry = append(registry, t)
}

// GetRegistry returns the list of registered tools.
func GetRegistry() []Tool {
	return registry
}

// EnabledChecker defines an interface to check if a tool is enabled.
type EnabledChecker interface {
	Enabled(name string) bool
}

// Builtin returns the set of standard agent tools wrapped with the redaction decorator.
func Builtin(scope *Scope, enabled EnabledChecker) []tool.BaseTool {
	var tools []tool.BaseTool
	for _, t := range registry {
		if enabled != nil && !enabled.Enabled(t.Name()) {
			continue
		}
		invokable := t.Build(scope)
		// Decorate it with WrapRedacted
		tools = append(tools, WrapRedacted(invokable))
	}
	return tools
}
