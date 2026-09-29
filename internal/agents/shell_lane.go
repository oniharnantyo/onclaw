package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// The shell background lane's execute tool (add-agent-subagents-background
// design.md D11 and the approval row of its risk register). The fs
// middleware's managed execute tool routes EVERY run through the task
// runner, and a shell approval interrupt cannot cross that task boundary —
// an approval-raising command would surface as an error tool result and the
// turn would never pause for the human. That violates the lane's own spec
// pin ("Foreground shell unchanged": with the capability on, a foreground
// call behaves exactly as it does without it), so the lane supplies this
// custom execute tool instead:
//
//   - Foreground runs call the shell directly on the tool-call context, the
//     identical path the lane-less execute tool uses — the interrupt
//     surfaces through the ToolNode, the approval flow and both resume
//     paths (same-process resume data, the durable decision ledger) work
//     unchanged.
//   - Explicit background launches route through the run's task runner and
//     join the shared task-ID space, so task_output and task_stop address
//     them like any other task. A launch of a command the shell would
//     interrupt for approval is refused with a readable result (the
//     documented fallback): the model reruns it in the foreground, where
//     the approver is attached.

// shellLaneExecuteArgs is the lane execute tool's input. The schema mirrors
// the middleware's managed execute tool (command + run_in_background) so the
// model-facing surface is the capability the spec pins; the timeout argument
// is deliberately absent — the shell's own command budget governs, exactly
// as on the lane-less path.
type shellLaneExecuteArgs struct {
	Command string `json:"command" jsonschema:"required" jsonschema_description:"The command to execute"`
	// RunInBackground launches the command as a background task when set.
	RunInBackground bool `json:"run_in_background,omitempty" jsonschema_description:"Set to true to run the command in the background. Use task_output to query it and task_stop to cancel it."`
}

// shellLaneExecuteToolDesc is the lane execute tool's description: the
// foreground contract first (the capability changes nothing about it), then
// the background lane, then the approval boundary.
const shellLaneExecuteToolDesc = `Executes a shell command inside the agent workspace jail (the workspace directory is the working directory). ` +
	`Without run_in_background the command runs to completion and the result carries its output, exit code, and truncation status. ` +
	`With run_in_background set, the launch returns promptly with the task id and the output file location while the command keeps executing: poll task_output for interim progress, cancel with task_stop. ` +
	`Background tasks are process-local — they do not survive the run ending or the server restarting. ` +
	`A command that requires operator approval cannot be launched in the background; run it in the foreground to request approval.`

// Output markers mirror the fs middleware's execute-tool notes so a
// command's result reads identically on the lane's foreground and background
// paths and on the lane-less path (upstream convExecuteResponse).
const (
	shellLaneTruncatedNote = "[Output was truncated due to size limits]"
	shellLaneFailedFmt     = "[Command failed with exit code %d]"
	shellLaneTimedOutNote  = "[Command timed out and was stopped]"
)

// shellLaneLaunchRefusalFmt is the readable refusal for a background launch
// of an approval-raising command (the design's documented fallback: the
// approver approves a foreground rerun).
const shellLaneLaunchRefusalFmt = "Command not launched: %q requires operator approval, and a background task has no approver attached. Run it in the foreground (omit run_in_background) to request approval."

// shellLanePayloadV1 is the launch payload recorded on the task spec. The
// process-local lane never replays it (only a recoverable executor would),
// but a versioned shape keeps the record debuggable.
type shellLanePayloadV1 struct {
	Version int    `json:"version"`
	Command string `json:"command"`
}

// newShellLaneExecuteTool builds the custom execute tool the shell
// background lane attaches to the fs middleware. shell is the jailed shell
// the foreground path calls directly; lane carries the run's task runner and
// the output sink the launch path records through; sessionID resolves the
// parent session a launch notifies on completion (nil means no
// notification). Requires Shell — the compose-time validation already
// guarantees a background lane never exists without the shell capability.
func newShellLaneExecuteTool(shell einofs.Shell, lane *fsmw.LocalBackgroundConfig, sessionID func(context.Context) (string, error)) (tool.InvokableTool, error) {
	if lane == nil || lane.Runner == nil {
		return nil, fmt.Errorf("shell lane execute tool: the lane's task runner is required")
	}
	return utils.InferTool(ReservedShellTool, shellLaneExecuteToolDesc,
		func(ctx context.Context, input shellLaneExecuteArgs) (string, error) {
			if !input.RunInBackground {
				// Foreground: the direct shell call — interrupts for
				// approval cross the ToolNode boundary exactly as they do
				// without the lane ("Foreground shell unchanged").
				result, err := shell.Execute(ctx, &einofs.ExecuteRequest{Command: input.Command})
				if err != nil {
					return "", err
				}
				return formatShellLaneOutput(result), nil
			}

			// Launch pre-execution approval check: a command the shell
			// would interrupt cannot detach — refuse readably and let the
			// foreground rerun carry the approval.
			if checker, ok := shell.(backend.ApprovalChecker); ok && checker.WouldInterrupt(input.Command) {
				return fmt.Sprintf(shellLaneLaunchRefusalFmt, input.Command), nil
			}

			parentSessionID := ""
			if sessionID != nil {
				id, err := sessionID(ctx)
				if err != nil {
					return "", err
				}
				parentSessionID = id
			}
			payload, err := json.Marshal(shellLanePayloadV1{Version: 1, Command: input.Command})
			if err != nil {
				return "", fmt.Errorf("shell lane launch: marshal payload: %w", err)
			}
			outputPath := shellLaneOutputPath(ctx)
			task, err := lane.Runner.Run(ctx, &backgroundlocal.Input{
				Description:     input.Command,
				Kind:            fsmw.ExecuteTaskKind,
				Payload:         payload,
				OutputFile:      outputPath,
				SessionID:       parentSessionID,
				NotifySession:   parentSessionID != "",
				RunInBackground: true,
			}, shellLaneWork(shell, &einofs.ExecuteRequest{Command: input.Command}, outputPath, lane.OutputStore))
			if err != nil {
				return "", err
			}
			return shellLaneLaunchMessage(task, outputPath, parentSessionID != ""), nil
		})
}

// shellLaneWork adapts the launched command into process-local managed work:
// the shell runs to completion (its own command budget governs), the result
// appends to the task's output file, and the output emits as the task's
// progress record so task_output replays it.
func shellLaneWork(shell einofs.Shell, req *einofs.ExecuteRequest, outputPath string, opener einofs.AppendOpener) backgroundlocal.WorkFunc {
	return func(ctx context.Context, runtime bgtask.ExecutionRuntime) (string, error) {
		result, err := shell.Execute(ctx, req)
		if err != nil {
			return "", err
		}
		out := formatShellLaneOutput(result)
		if err := appendShellLaneOutput(ctx, opener, outputPath, out); err != nil {
			return "", err
		}
		if out != "" {
			if _, err := runtime.EmitProgress(ctx, "", []byte(out)); err != nil {
				return "", err
			}
		}
		return out, nil
	}
}

// appendShellLaneOutput writes the command's full output to the task's
// output file — one open, one append, one close (the buffered shape upstream
// uses). No opener or no path means the launch carries no output file.
func appendShellLaneOutput(ctx context.Context, opener einofs.AppendOpener, outputPath, out string) error {
	if opener == nil || outputPath == "" {
		return nil
	}
	w, err := opener.OpenAppend(ctx, &einofs.OpenAppendRequest{FilePath: outputPath})
	if err != nil {
		return fmt.Errorf("shell lane: open output file: %w", err)
	}
	if _, err := io.WriteString(w, out+"\n"); err != nil {
		_ = w.Close()
		return fmt.Errorf("shell lane: append output file: %w", err)
	}
	return w.Close()
}

// shellLaneOutputPath derives the task's output-file path: the launching
// tool-call id when present, a fresh uuid otherwise (concurrent untagged
// launches must not collide). Relative under the canonical tasks directory —
// the jailed append opener resolves it.
func shellLaneOutputPath(ctx context.Context) string {
	id := compose.GetToolCallID(ctx)
	if id == "" {
		id = uuid.NewString()
	}
	return filepath.Join(TasksOutputDir, id+".output")
}

// shellLaneLaunchMessage renders the launch result: the task id first, then
// the output location and the completion notice. A launch that already
// reached a terminal status before the caller reads it reports that outcome
// directly — the same status switch the middleware's managed tool applies.
func shellLaneLaunchMessage(task *bgtask.Task, outputPath string, notifies bool) string {
	switch task.Status {
	case bgtask.StatusCompleted:
		return string(task.ResultData)
	case bgtask.StatusPending, bgtask.StatusRunning,
		bgtask.StatusWaitingInput, bgtask.StatusSuspended:
		var b strings.Builder
		fmt.Fprintf(&b, "Command running in background with ID: %s.", task.Spec.ID)
		if outputPath != "" {
			fmt.Fprintf(&b, " Output is being written to: %s.", outputPath)
		}
		if notifies {
			b.WriteString(" You will be notified when it completes.")
		} else {
			b.WriteString(" Use task_output to check status and retrieve the result.")
		}
		return b.String()
	case bgtask.StatusFailed:
		return fmt.Sprintf("Command launch %s failed: %s", task.Spec.ID, task.ResultError)
	case bgtask.StatusCanceled:
		return fmt.Sprintf("Command launch %s was canceled.", task.Spec.ID)
	default:
		return fmt.Sprintf("Command launch %s has unknown status %q.", task.Spec.ID, task.Status)
	}
}

// formatShellLaneOutput renders one shell response the way the fs
// middleware's execute tool does: output, then the timeout or exit-code
// note, then the truncation note, joined by newlines.
func formatShellLaneOutput(response *einofs.ExecuteResponse) string {
	if response == nil {
		return ""
	}
	var parts []string
	if response.Output != "" {
		parts = append(parts, response.Output)
	}
	if response.TimedOut {
		parts = append(parts, shellLaneTimedOutNote)
	} else if response.ExitCode != nil && *response.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf(shellLaneFailedFmt, *response.ExitCode))
	}
	if response.Truncated {
		parts = append(parts, shellLaneTruncatedNote)
	}
	return strings.Join(parts, "\n")
}
