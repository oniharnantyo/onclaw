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

// Builtin returns the set of standard agent tools wrapped with the redaction decorator.
func Builtin(scope *Scope) []tool.BaseTool {
	var tools []tool.BaseTool
	for _, t := range registry {
		invokable := t.Build(scope)
		// Decorate it with WrapRedacted
		tools = append(tools, WrapRedacted(invokable))
	}
	return tools
}
