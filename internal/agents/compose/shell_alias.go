package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// shellAliasArgs mirrors the filesystem middleware's execute tool input.
type shellAliasArgs struct {
	Command string `json:"command" jsonschema:"required" jsonschema_description:"The command to execute"`
}

// NewShellAliasTool registers the jailed shell a second time under
// ToolShellAlias. It behaves like the middleware's plain execute tool (same
// description, same request, same output framing) and exists because an
// unknown tool name fails the whole run while models primed by prompts that
// say "shell" keep reaching for one.
func NewShellAliasTool(sh einofs.Shell) (tool.BaseTool, error) {
	if sh == nil {
		return nil, errors.New("shell alias: shell is required")
	}
	return utils.InferTool(ToolShellAlias, fsmw.ExecuteToolDesc, func(ctx context.Context, in shellAliasArgs) (string, error) {
		res, err := sh.Execute(ctx, &einofs.ExecuteRequest{Command: in.Command})
		if err != nil {
			return "", err
		}
		return formatExecuteResponse(res), nil
	})
}

// formatExecuteResponse frames a shell response the way the middleware's
// execute tool does: output first, then the exit or timeout note, then the
// truncation note.
func formatExecuteResponse(res *einofs.ExecuteResponse) string {
	if res == nil {
		return ""
	}
	var parts []string
	if res.Output != "" {
		parts = append(parts, res.Output)
	}
	if res.TimedOut {
		parts = append(parts, "[command timed out]")
	} else if res.ExitCode != nil && *res.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("[command exited with code %d]", *res.ExitCode))
	}
	if res.Truncated {
		parts = append(parts, "[output truncated]")
	}
	return strings.Join(parts, "\n")
}
