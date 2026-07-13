package tools

import (
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
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

// ToolMeta describes a tool that is not assembled through the tool factory but
// still needs to appear in the management API/UI (e.g. the filesystem-middleware
// tools injected directly by the Eino filesystem middleware).
type ToolMeta struct {
	Name     string
	Desc     string
	Category string
}

// FSToolMetadata returns the seven filesystem-middleware tools for registry
// seeding: the six Filesystem tools and the Shell `execute` tool. Descriptions
// reuse the Eino filesystem middleware's own tool descriptions.
func FSToolMetadata() []ToolMeta {
	return []ToolMeta{
		{Name: "ls", Desc: fsmw.ListFilesToolDesc, Category: "Filesystem"},
		{Name: "read_file", Desc: fsmw.ReadFileToolDesc, Category: "Filesystem"},
		{Name: "write_file", Desc: fsmw.WriteFileToolDesc, Category: "Filesystem"},
		{Name: "edit_file", Desc: fsmw.EditFileToolDesc, Category: "Filesystem"},
		{Name: "glob", Desc: fsmw.GlobToolDesc, Category: "Filesystem"},
		{Name: "grep", Desc: fsmw.GrepToolDesc, Category: "Filesystem"},
		{Name: "execute", Desc: fsmw.ExecuteToolDesc, Category: "Shell"},
	}
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
