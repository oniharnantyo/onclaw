package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The subagents seam (spec agent-subagents, design D3): an agent with the
// capability enabled is offered the agent tool, which delegates a task to a
// general-purpose clone — the parent's own persona, model, and resolved tool
// surface, with a fresh context and no ability to delegate further. The
// child runs the same compose pipeline in a nested run inside this process
// (never through Manager.Send: no session, no recursion hazard), its final
// answer becomes the tool result, and its transcript is materialized under
// the workspace .tasks/ directory.
//
// A delegation may run in the background: the parent turn proceeds while the
// child runs detached, the task is addressable through task_output/task_stop
// for the lifetime of the run, and its completion is surfaced on the parent
// transcript as a task_activity record (design D10).

// ToolAgent is the delegation tool's name: how the parent model reaches a
// subagent. It is deliberately outside the built-in catalog — the capability
// is the agent's subagents_enabled column (design D3), not an allowlist entry.
const ToolAgent = "agent"

// ToolTaskOutput and ToolTaskStop are the background lane's control tools:
// collect a task's output, or cancel it. Both address the run's task-ID space.
const (
	ToolTaskOutput = "task_output"
	ToolTaskStop   = "task_stop"
)

// generalPurposeSubagentName names the clone (the donor's general-purpose
// agent). It is the agent tool's subject, not a separate stored agent.
const generalPurposeSubagentName = "general-purpose"

// agentToolDescription is the agent tool's description: delegation framing
// plus the honest wildcard (the clone runs with the parent's full tool
// surface) and the no-recursion fact.
const agentToolDescription = "Delegate a self-contained task to a general-purpose subagent. " +
	"The subagent is a clone of you — your instructions and your tools, with a fresh context that does not " +
	"see this conversation — so describe the whole task: it gets only what you write. It cannot delegate " +
	"further and returns a single final report. (Tools: everything you have, except the agent tool.)"

// agentToolBackgroundNote is appended to the agent tool's description only
// when the background lane is available (the parent turn has a run task
// space), so the model is told the whole contract or nothing.
const agentToolBackgroundNote = " Set run_in_background to true to launch the delegation and keep working: " +
	"the tool returns the task id immediately, and you poll task_output for the child's report or cancel it " +
	"with task_stop."

// taskOutputDescription and taskStopDescription describe the control tools.
const (
	taskOutputDescription = "Collect a background task's output: its state while running, or the subagent's " +
		"final report once it finished. Addresses a task of the current conversation only."
	taskStopDescription = "Cancel a running background task. The subagent's run is stopped and reported as " +
		"stopped; a task that already finished cannot be stopped."
)

// childIterationCap bounds a child turn's tool-call iterations. It is the
// same runtime constant the parent turn uses (design D3: the cap is a
// runtime constant, not a per-agent column), applied per child run so a
// delegation cannot loop unbounded on top of its parent.
const childIterationCap = DefaultMaxIterations

// delegationRequest is one agent-tool call as the model wrote it.
type delegationRequest struct {
	// Task is the delegated task's description — the entire context the child
	// sees, because its context starts fresh.
	Task string `json:"task" jsonschema:"description=The complete task for the subagent, with every detail and context it needs: it does not see this conversation."`
	// RunInBackground launches the delegation detached and returns the task
	// id immediately instead of the child's answer.
	RunInBackground bool `json:"run_in_background,omitempty" jsonschema:"description=Run the delegation in the background: the tool returns the task id at once and the child keeps working while you continue. Collect it with task_output, cancel it with task_stop."`
}

// delegationResult is the agent tool's outcome. State is one of the
// delegated-task states; Result carries the child's report for a foreground
// delegation or a completed background one. For an errored child run the
// tool returns the error to the model as the call's failure (the
// tool-error-result middleware renders it), so the model can react.
type delegationResult struct {
	TaskID         string `json:"task_id"`
	State          string `json:"state"`
	Result         string `json:"result,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
}

// taskOutputResult is task_output's payload.
type taskOutputResult struct {
	TaskID         string `json:"task_id"`
	State          string `json:"state"`
	Description    string `json:"description,omitempty"`
	Result         string `json:"result,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
}

// taskStopResult is task_stop's payload.
type taskStopResult struct {
	TaskID string `json:"task_id"`
	State  string `json:"state"`
}

// delegation is one turn's subagent capability: everything a child run needs
// to compose the same pipeline, plus the run's task space. It is built fresh
// per turn (the tool surface and model instances are per-turn) and never
// shared across turns.
type delegation struct {
	// registry owns this run's task-ID space and the parent transcript's
	// completion records.
	registry *taskRegistry

	// agent is the stored parent configuration the clone inherits from.
	agent store.Agent
	// model is the parent turn's built model instance; the clone shares it.
	model model.BaseChatModel
	// instruction is the parent turn's finished system instruction.
	instruction string
	// tools is the parent's resolved tool surface (fs/shell withheld flags
	// included); the clone runs the same set.
	surface *resolvedTools
	// mcpTools are the parent's resolved MCP tools, appended to the clone's
	// surface exactly as to the parent's.
	mcpTools []tool.BaseTool
	// skills, when non-nil, attaches the same skill backend to the child.
	skills *skillsCapability
	// hooksCap, when non-nil, gates the child's tool calls through the same
	// resolved chain the parent turn resolved.
	hooksCap *hooksCapability
	// tasksDir is the parent workspace's .tasks directory; empty when the
	// agent has no workspace (the child then runs without a transcript file).
	tasksDir string
	// turnID is the spawning turn's identity; each task's records carry it.
	turnID string
	// checkpoints backs the child shell's dangerous-command gate (design
	// D4): the same decision ledger the parent's shell consults, so a
	// delegated command cannot bypass the approval requirement.
	checkpoints store.CheckpointStore
}

// resolveDelegation builds the turn's subagent capability when the agent has
// the capability enabled (design D3: the dedicated column, default on). A
// disabled agent's turns get no delegation tool at all (spec agent-subagents
// "Disabled agent cannot delegate"). The capability degrades where data is
// absent: without a workspace, tasksDir stays empty and the child runs
// without a transcript file.
//
// The child composes from the PARENT TURN'S finished data — instruction,
// model, resolved surface — so the clone is this agent researching with its
// own context, not a persona. The delegation tools themselves are appended
// to the parent's surface only, so the clone's surface can never contain
// them.
func (m *manager) resolveDelegation(ctx context.Context, agent store.Agent, surface *resolvedTools, registry *taskRegistry, mdl model.BaseChatModel, instruction string, skills *skillsCapability, hookCap *hooksCapability, turnID string) (*delegation, error) {
	if !agent.SubagentsEnabled {
		return nil, nil
	}

	tasksDir := ""
	if surface.agentDir != "" && m.workspaces != nil && agent.Slug != "" {
		dir, err := m.workspaces.TasksDir(agent.Slug)
		if err != nil {
			return nil, fmt.Errorf("resolve tasks directory for agent %q: %w", agent.Slug, err)
		}
		tasksDir = dir
	}

	return &delegation{
		registry:    registry,
		agent:       agent,
		model:       mdl,
		instruction: instruction,
		surface:     surface,
		skills:      skills,
		hooksCap:    hookCap,
		tasksDir:    tasksDir,
		turnID:      turnID,
		checkpoints: m.store.Checkpoints(),
	}, nil
}

// tools returns the parent turn's delegation tools: the agent tool, plus the
// background lane's control tools when the lane is available.
func (d *delegation) tools(ctx context.Context) ([]tool.BaseTool, error) {
	agentTool, err := newAgentTool(d)
	if err != nil {
		return nil, err
	}
	out := []tool.BaseTool{agentTool}

	outputTool, err := newTaskOutputTool(d)
	if err != nil {
		return nil, err
	}
	stopTool, err := newTaskStopTool(d)
	if err != nil {
		return nil, err
	}
	return append(out, outputTool, stopTool), nil
}

// childToolSurface returns the clone's tool surface: the parent's resolved
// direct tools and workspace-rooted configuration, never the delegation
// tools themselves — a clone that could spawn agents would recurse without
// bound (spec agent-subagents "No recursion").
func (d *delegation) childToolSurface() *resolvedTools {
	return &resolvedTools{
		tools:    d.surface.tools,
		agentDir: d.surface.agentDir,
		shell:    d.surface.shell,
		// The clone mirrors the parent's withheld fs tools: same surface,
		// minus the delegation tools it must never have.
		fsDisabled: append([]string(nil), d.surface.fsDisabled...),
	}
}

// delegate runs one agent-tool call. A foreground call runs the child and
// returns its answer; a background call registers the task, launches the
// child detached, and returns the task id at once (spec agent-subagents
// "Parent continues while a task runs").
func (d *delegation) delegate(ctx context.Context, req delegationRequest) (delegationResult, error) {
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return delegationResult{}, errors.New("agent: task must not be empty")
	}

	rec, childCtx, err := d.registry.spawn(task, req.RunInBackground, d.turnID)
	if err != nil {
		return delegationResult{}, err
	}
	if err := d.startTranscript(rec); err != nil {
		// A transcript failure is not a delegation failure: the child still
		// runs, task_output still reports its answer, and the file tools just
		// have nothing to read.
		slog.WarnContext(ctx, "delegated task transcript unavailable", "task", rec.ID, "error", err)
	}

	// The running card rides the live channel only (design D10): the
	// parent's transcript holds the terminal record once the child ends.
	d.registry.emitRunning(*rec)

	if req.RunInBackground {
		d.launchDetached(childCtx, rec.ID, task)
		snapshot, _ := d.registry.lookup(rec.ID)
		return delegationResult{
			TaskID:         snapshot.ID,
			State:          string(snapshot.Status),
			TranscriptPath: snapshot.TranscriptPath,
		}, nil
	}

	// Foreground: the answer comes back as this call's result. The child's
	// context is bounded by the same run context, so a cancelled parent turn
	// cancels the child with it.
	answer, runErr := d.runChild(childCtx, rec.ID, task)
	snapshot := d.registry.finish(rec.ID, terminalStatus(runErr), answer)
	if runErr != nil {
		return delegationResult{}, fmt.Errorf("delegated task %s failed: %w", rec.ID, runErr)
	}
	return delegationResult{
		TaskID:         snapshot.ID,
		State:          string(snapshot.Status),
		Result:         snapshot.Result,
		TranscriptPath: snapshot.TranscriptPath,
	}, nil
}

// launchDetached runs a background child in its own goroutine, so the parent
// turn proceeds without waiting for it.
func (d *delegation) launchDetached(ctx context.Context, taskID, task string) {
	go func() {
		answer, err := d.runChild(ctx, taskID, task)
		d.registry.finish(taskID, terminalStatus(err), answer)
		if err != nil {
			slog.Warn("background delegated task failed", "task", taskID, "error", err)
		}
	}()
}

// output answers a task_output call from the run's task space.
func (d *delegation) output(taskID string) (taskOutputResult, error) {
	id := strings.TrimSpace(taskID)
	if id == "" {
		return taskOutputResult{}, errors.New("task_output: task_id must not be empty")
	}
	snapshot, err := d.registry.lookup(id)
	if err != nil {
		return taskOutputResult{}, err
	}
	return taskOutputResult{
		TaskID:         snapshot.ID,
		State:          string(snapshot.Status),
		Description:    taskSummary(snapshot.Description),
		Result:         snapshot.Result,
		TranscriptPath: snapshot.TranscriptPath,
	}, nil
}

// stop answers a task_stop call: the task's run is cancelled and reported
// stopped, and an unknown or already-terminal task is the typed conflict the
// model can read.
func (d *delegation) stop(taskID string) (taskStopResult, error) {
	id := strings.TrimSpace(taskID)
	if id == "" {
		return taskStopResult{}, errors.New("task_stop: task_id must not be empty")
	}
	snapshot, err := d.registry.cancel(id)
	if err != nil {
		return taskStopResult{}, err
	}
	return taskStopResult{TaskID: snapshot.ID, State: string(snapshot.Status)}, nil
}

// startTranscript materializes the task's transcript file under the parent
// workspace's .tasks directory (spec agent-subagents "Child transcripts") and
// records its path on the task, so the terminal task_activity record carries
// it. With no workspace there is nothing to write to and the task runs
// without a transcript.
func (d *delegation) startTranscript(rec *TaskRecord) error {
	if d.tasksDir == "" {
		return nil
	}
	transcript, err := newTaskTranscript(d.tasksDir, rec.ID, rec.Description)
	if err != nil {
		return err
	}
	// The writer is handed to the child run through the task's transcript
	// slot: runChild picks it up and closes it with the terminal stamp.
	d.registry.setTranscript(rec.ID, transcript)
	d.registry.setTranscriptPath(rec.ID, filepath.Join(tasksOutputDir, rec.ID+taskTranscriptSuffix))
	return nil
}

// runChild executes one delegated turn: compose → run → drain, with a fresh
// message context (just the delegated task) and the parent's instruction,
// model, and tool surface. It returns the child's final answer.
func (d *delegation) runChild(ctx context.Context, taskID, task string) (string, error) {
	surface := d.childToolSurface()
	cfg := &composeConfig{
		Name:        generalPurposeSubagentName,
		Instruction: d.instruction,
		ChatModel:   d.model,
		Tools:       append(surface.tools, d.mcpTools...),
		// The child's turn is bounded by its own iteration cap (design D3),
		// never by the parent's remaining budget: a delegation that loops is
		// stopped with the cap notice and still reports what it produced.
		MaxIterations: childIterationCap,
	}
	if surface.agentDir != "" {
		fsCap := &filesystemCapability{AgentDir: surface.agentDir, DisabledTools: surface.fsDisabled}
		if surface.shell {
			// The dangerous-command gate applies to delegated commands too
			// (design D4): the child's shell consults the same decision
			// ledger, so a safety-pattern match can never execute ungated.
			fsCap.Shell = backend.NewJailedShell(surface.agentDir,
				backend.WithApprovalGate(checkpointLedger{checkpoints: d.checkpoints}))
		}
		if d.skills != nil && d.skillsDir() != "" {
			fsCap.ExtraRoots = []string{d.skillsDir()}
		}
		cfg.Filesystem = fsCap
	}
	if d.skills != nil {
		cfg.Skills = d.skills
	}
	if d.hooksCap != nil {
		cfg.Hooks = d.hooksCap
	}

	childAgent, childToolErrors, err := composeTurnAgent(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("compose subagent: %w", err)
	}

	iter := childAgent.Run(ctx, &adk.AgentInput{
		Messages:        []*schema.Message{schema.UserMessage(task)},
		EnableStreaming: true,
	})
	return d.drainChild(iter, taskID, childToolErrors)
}

// drainChild consumes the child agent's event iterator, materializes its
// transcript as it goes, and returns its final answer. It is the child-sized
// shape of the parent's drainTurn: no session events, no parent compaction —
// the child's output lives in the transcript file and in the answer the
// parent receives (design: the child has a fresh context and owns nothing).
func (d *delegation) drainChild(iter *adk.AsyncIterator[*adk.AgentEvent], taskID string, toolErrors *toolErrorResult) (string, error) {
	transcript := d.registry.takeTranscript(taskID)
	// drained closes out the transcript with the terminal stamp and releases
	// the file exactly once, whichever path the run takes.
	drained := false
	finish := func(answer string, runErr error) (string, error) {
		if transcript != nil && !drained {
			drained = true
			transcript.terminal(string(terminalStatus(runErr)), answer)
		}
		return answer, runErr
	}

	var answer strings.Builder
	args := make(map[string]string)

	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			if errors.Is(ev.Err, adk.ErrExceedMaxIterations) {
				// Bounded runaway loop: the cap is a boundary, not a crash.
				// Whatever text the child produced stands as its answer.
				drainToEOF(iter)
				if transcript != nil {
					transcript.text(iterationCapNotice())
				}
				return finish(answer.String(), nil)
			}
			drainToEOF(iter)
			return finish(answer.String(), ev.Err)
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		d.consumeChildOutput(ev.Output.MessageOutput, &answer, transcript, args, toolErrors)
	}
	return finish(answer.String(), nil)
}

// consumeChildOutput maps one child event onto the transcript and, for
// assistant text, onto the accumulated answer. Reasoning, text, and tool
// records all land in the transcript file; only assistant text becomes the
// answer the parent reads.
func (d *delegation) consumeChildOutput(mo *adk.MessageVariant, answer *strings.Builder, transcript *taskTranscript, args map[string]string, toolErrors *toolErrorResult) {
	switch mo.Role {
	case schema.Assistant:
		calls := newToolCallAssembler()
		if mo.IsStreaming && mo.MessageStream != nil {
			for {
				frame, err := mo.MessageStream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil || frame == nil {
					continue
				}
				if frame.ReasoningContent != "" && transcript != nil {
					transcript.reasoning(frame.ReasoningContent)
				}
				if frame.Content != "" {
					answer.WriteString(frame.Content)
					if transcript != nil {
						transcript.text(frame.Content)
					}
				}
				calls.observe(frame.ToolCalls)
			}
		} else if mo.Message != nil {
			msg := mo.Message
			if msg.ReasoningContent != "" && transcript != nil {
				transcript.reasoning(msg.ReasoningContent)
			}
			if msg.Content != "" {
				answer.Reset()
				answer.WriteString(msg.Content)
				if transcript != nil {
					transcript.text(msg.Content)
				}
			}
			calls.observe(msg.ToolCalls)
		}
		for _, call := range calls.finish() {
			if call.CallID != "" {
				args[call.CallID] = call.Arguments
			}
			if transcript != nil {
				transcript.tool("call " + call.Name + " " + call.Arguments)
			}
		}
	case schema.Tool:
		msg, err := mo.GetMessage()
		if err != nil || msg == nil {
			return
		}
		name := mo.ToolName
		if name == "" {
			name = msg.ToolName
		}
		result := msg.Content
		if errText, failed := toolErrors.failureOf(msg.ToolCallID); failed {
			result = "error: " + errText
		}
		if transcript != nil {
			transcript.tool("result " + name + ": " + result)
		}
	}
}

// skillsDir returns the parent turn's global skills directory, when the
// runtime has one (used for the clone's read-only skill root).
func (d *delegation) skillsDir() string {
	if d.skills == nil {
		return ""
	}
	return d.skills.GlobalDir
}

// terminalStatus maps a child run's outcome onto the task's terminal state:
// a completed run is completed, an error is failed, and a cancelled run —
// task_stop or the parent turn ending — is stopped.
func terminalStatus(runErr error) sessions.ToolStatus {
	switch {
	case runErr == nil:
		return taskStatusCompleted
	case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded):
		return taskStatusStopped
	default:
		return taskStatusFailed
	}
}
