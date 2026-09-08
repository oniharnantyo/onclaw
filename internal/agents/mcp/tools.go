package mcp

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ApplyNames renames each of a server's tools to mcp__<server>__<tool> via
// the pass-scoped [Namer] (design.md D7), preserving the tools' invocation
// capabilities: eino's ToolsNode type-switches on InvokableTool/StreamableTool,
// so the wrapper keeps the promoted methods and only overrides Info.
func ApplyNames(server string, tools []tool.BaseTool, namer *Namer) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, wrapNamed(t, namer.Name(server, rawToolName(t))))
	}
	return out
}

// rawToolName reads the tool's original (server-assigned) name.
func rawToolName(t tool.BaseTool) string {
	info, err := t.Info(context.Background())
	if err != nil || info == nil {
		return ""
	}
	return info.Name
}

// wrapNamed picks the narrowest wrapper matching the tool's capabilities so
// no method is lost in the rename.
func wrapNamed(t tool.BaseTool, name string) tool.BaseTool {
	switch b := t.(type) {
	case tool.StreamableTool:
		return &namedStreamableTool{StreamableTool: b, name: name}
	case tool.InvokableTool:
		return &namedInvokableTool{InvokableTool: b, name: name}
	default:
		return &namedBaseTool{BaseTool: t, name: name}
	}
}

// namedBaseTool renames a bare BaseTool.
type namedBaseTool struct {
	tool.BaseTool
	name string
}

func (t *namedBaseTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.BaseTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	renamed := *info
	renamed.Name = t.name
	return &renamed, nil
}

// namedInvokableTool renames an InvokableTool, keeping InvokableRun promoted.
type namedInvokableTool struct {
	tool.InvokableTool
	name string
}

func (t *namedInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.InvokableTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	renamed := *info
	renamed.Name = t.name
	return &renamed, nil
}

// namedStreamableTool renames a StreamableTool, keeping StreamableRun
// promoted.
type namedStreamableTool struct {
	tool.StreamableTool
	name string
}

func (t *namedStreamableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.StreamableTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	renamed := *info
	renamed.Name = t.name
	return &renamed, nil
}
