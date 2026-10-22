package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// The delegation tools (spec agent-subagents): the agent tool that delegates
// a task to a general-purpose clone, and the background lane's task_output /
// task_stop controls. They are eino invokable tools over the turn's
// delegation state — never catalog tools: the capability is the agent's
// subagents_enabled column (design D3), and these tools are attached to a
// turn's tool surface by the runner, not by the allowlist gate.

// newAgentTool builds the agent tool: its schema depends on whether the
// background lane is available (the lane's shared task space always exists,
// but a turn without a workspace still delegates — it just has no transcript
// file to point at).
func newAgentTool(d *delegation) (tool.BaseTool, error) {
	description := agentToolDescription + agentToolBackgroundNote
	agentTool, err := utils.InferTool(ToolAgent, description,
		func(ctx context.Context, req delegationRequest) (delegationResult, error) {
			return d.delegate(ctx, req)
		})
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", ToolAgent, err)
	}
	return agentTool, nil
}

// newTaskOutputTool builds the task_output tool: it reads the run's task
// space, so it can only ever address a task of the conversation it is
// offered in.
func newTaskOutputTool(d *delegation) (tool.BaseTool, error) {
	type input struct {
		TaskID string `json:"task_id" jsonschema:"description=The task id the agent tool returned (for example task-1)."`
	}
	outputTool, err := utils.InferTool(ToolTaskOutput, taskOutputDescription,
		func(_ context.Context, in input) (taskOutputResult, error) {
			return d.output(in.TaskID)
		})
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", ToolTaskOutput, err)
	}
	return outputTool, nil
}

// newTaskStopTool builds the task_stop tool.
func newTaskStopTool(d *delegation) (tool.BaseTool, error) {
	type input struct {
		TaskID string `json:"task_id" jsonschema:"description=The id of the running task to cancel."`
	}
	stopTool, err := utils.InferTool(ToolTaskStop, taskStopDescription,
		func(_ context.Context, in input) (taskStopResult, error) {
			return d.stop(in.TaskID)
		})
	if err != nil {
		return nil, fmt.Errorf("build %s tool: %w", ToolTaskStop, err)
	}
	return stopTool, nil
}

// delegationToolNames is the delegation tools' set: the names the child's
// surface must never contain and the merged surface must not duplicate.
var delegationToolNames = map[string]struct{}{
	ToolAgent:      {},
	ToolTaskOutput: {},
	ToolTaskStop:   {},
}

// isDelegationTool reports whether name is one of the delegation tools.
func isDelegationTool(name string) bool {
	_, ok := delegationToolNames[name]
	return ok
}

// assertNoDelegationToolCollision fails the turn when the base surface
// already defines a delegation tool's name: the capability's own tool would
// be shadowed, and the model would call a tool that does not delegate. It is
// a fail-fast wiring guard (the catalog must never define these names), not a
// runtime possibility.
func assertNoDelegationToolCollision(ctx context.Context, base, delegationTools []tool.BaseTool) error {
	names, err := toolNames(ctx, base)
	if err != nil {
		return err
	}
	for _, t := range delegationTools {
		info, err := t.Info(ctx)
		if err != nil {
			return fmt.Errorf("read delegation tool info: %w", err)
		}
		if info == nil {
			continue
		}
		if _, clash := names[strings.TrimSpace(info.Name)]; clash {
			return fmt.Errorf("%w: %q", errDuplicateDelegationTool, info.Name)
		}
	}
	return nil
}

// toolNames returns the names of a tool set, for the duplicate guard.
func toolNames(ctx context.Context, tools []tool.BaseTool) (map[string]struct{}, error) {
	names := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info: %w", err)
		}
		if info == nil {
			continue
		}
		names[strings.TrimSpace(info.Name)] = struct{}{}
	}
	return names, nil
}

// errDuplicateDelegationTool is the fail-fast guard against a catalog entry
// claiming a delegation tool's name: it would shadow the capability's own
// tool, so the turn fails loudly instead of silently delegating nowhere.
var errDuplicateDelegationTool = errors.New("agents: the tool catalog must not define the delegation tools")
