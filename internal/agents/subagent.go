package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	"github.com/cloudwego/eino/adk/filesystem"
	bgmiddleware "github.com/cloudwego/eino/adk/middlewares/backgroundtask"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The subagents seam (spec agent-subagents, design D3, D4): an agent with the
// capability enabled is offered the agent tool and backgroundtask tools
// (task_output, task_stop) through eino middlewares.
//
// The child runs a cloned agent with the parent's instructions, model, and
// resolved tool surface (without delegation tools: no recursion), with a fresh
// context. Materialized transcripts land in .tasks/ under the workspace.

// Tool names for delegation and background task control.
const (
	ToolAgent      = "agent"
	ToolTaskOutput = "task_output"
	ToolTaskStop   = "task_stop"
)

// generalPurposeSubagentName names the clone agent type.
const generalPurposeSubagentName = "general-purpose"

// childIterationCap bounds a child turn's tool-call iterations.
const childIterationCap = DefaultMaxIterations

// delegationToolNames is the set of delegation control tools.
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

// errDuplicateDelegationTool is returned if a base tool attempts to use a reserved delegation tool name.
var errDuplicateDelegationTool = errors.New("agents: the tool catalog must not define the delegation tools")

// assertNoDelegationToolCollision ensures the provided base tools do not collide with delegation tool names.
func assertNoDelegationToolCollision(tools []tool.BaseTool) error {
	for _, t := range tools {
		if t == nil {
			continue
		}
		info, err := t.Info(context.Background())
		if err != nil {
			return fmt.Errorf("read tool info: %w", err)
		}
		if info != nil && isDelegationTool(strings.TrimSpace(info.Name)) {
			return fmt.Errorf("%w: %q", errDuplicateDelegationTool, info.Name)
		}
	}
	return nil
}

// delegationResult is the agent tool's outcome.
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

// diskAppendOpener satisfies filesystem.AppendOpener over a base host directory.
type diskAppendOpener struct {
	baseDir string
}

func (d *diskAppendOpener) OpenAppend(_ context.Context, req *filesystem.OpenAppendRequest) (io.WriteCloser, error) {
	p := req.FilePath
	if !filepath.IsAbs(p) && d.baseDir != "" {
		p = filepath.Join(d.baseDir, p)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("create dir for append: %w", err)
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// bridgingTaskStore wraps a bgtask.InMemoryStore to bridge lifecycle events to taskRegistry.
type bridgingTaskStore struct {
	*bgtask.InMemoryStore
	reg      *taskRegistry
	turnID   string
	tasksDir string
}

func newBridgingTaskStore(inner *bgtask.InMemoryStore, reg *taskRegistry, turnID, tasksDir string) *bridgingTaskStore {
	return &bridgingTaskStore{
		InMemoryStore: inner,
		reg:           reg,
		turnID:        turnID,
		tasksDir:      tasksDir,
	}
}

func (s *bridgingTaskStore) Create(ctx context.Context, req *bgtask.CreateTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.reg != nil {
		s.reg.mu.Lock()
		if rec, ok := s.reg.tasksB[task.Spec.ID]; ok {
			rec.Background = true
		}
		s.reg.mu.Unlock()
	}
	return task, nil
}

func (s *bridgingTaskStore) Complete(ctx context.Context, req *bgtask.CompleteTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.reg != nil {
		s.reg.finish(req.TaskID, taskStatusCompleted, string(req.Data))
	}
	return task, nil
}

func (s *bridgingTaskStore) Fail(ctx context.Context, req *bgtask.FailTaskRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.Fail(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.reg != nil {
		s.reg.finish(req.TaskID, taskStatusFailed, req.Error)
	}
	return task, nil
}

func (s *bridgingTaskStore) AckCancel(ctx context.Context, req *bgtask.AckCancelRequest) (*bgtask.Task, error) {
	task, err := s.InMemoryStore.AckCancel(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.reg != nil {
		s.reg.finish(req.TaskID, taskStatusStopped, "")
	}
	return task, nil
}

// delegation is one turn's subagent capability using eino subagent and backgroundtask middlewares.
type delegation struct {
	subagentCap *subagentCapability
	bgCap       *backgroundTaskCapability
	manager     *bgtask.Manager
	registry    *taskRegistry
	mcpTools    []tool.BaseTool
}

// childToolSurface returns the clone's tool surface without delegation tools.
func childToolSurface(parent *resolvedTools) *resolvedTools {
	return &resolvedTools{
		tools:            parent.tools,
		agentDir:         parent.agentDir,
		shell:            parent.shell,
		fsDisabled:       append([]string(nil), parent.fsDisabled...),
		plantaskDisabled: append([]string(nil), parent.plantaskDisabled...),
	}
}

// resolveDelegation constructs subagent and backgroundtask capabilities when enabled.
func (m *manager) resolveDelegation(
	ctx context.Context,
	agent store.Agent,
	surface *resolvedTools,
	registry *taskRegistry,
	mdl model.BaseChatModel,
	instruction string,
	skills *skillsCapability,
	hookCap *hooksCapability,
	turnID string,
	mcpTools []tool.BaseTool,
) (*delegation, error) {
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

	cloneSurface := childToolSurface(surface)
	childTools := append([]tool.BaseTool(nil), cloneSurface.tools...)
	childTools = append(childTools, mcpTools...)

	childCfg := &composeConfig{
		Name:          generalPurposeSubagentName,
		Instruction:   instruction,
		ChatModel:     mdl,
		Tools:         childTools,
		MaxIterations: childIterationCap,
	}

	if cloneSurface.agentDir != "" {
		fsCap := &filesystemCapability{
			AgentDir:      cloneSurface.agentDir,
			DisabledTools: cloneSurface.fsDisabled,
		}
		if cloneSurface.shell {
			fsCap.Shell = backend.NewJailedShell(cloneSurface.agentDir,
				backend.WithApprovalGate(checkpointLedger{checkpoints: m.store.Checkpoints()}))
		}
		if skills != nil && m.skillsDir != "" {
			fsCap.ExtraRoots = []string{m.skillsDir}
		}
		childCfg.Filesystem = fsCap

		extraRoots := fsCap.ExtraRoots
		childCfg.Reduction = &reductionCapability{
			AgentDir:     cloneSurface.agentDir,
			ExtraRoots:   extraRoots,
			ContextLimit: int64(m.contextLimit(ctx, agent)),
		}
		childCfg.Plantask = &plantaskCapability{
			AgentDir:      cloneSurface.agentDir,
			ExtraRoots:    extraRoots,
			DisabledTools: cloneSurface.plantaskDisabled,
		}
		childCfg.DisabledPlantaskTools = cloneSurface.plantaskDisabled
	}

	childCfg.Summarization = &summarizationCapability{
		Model:        mdl,
		ContextLimit: int64(m.contextLimit(ctx, agent)),
	}

	if skills != nil {
		childCfg.Skills = skills
	}
	if hookCap != nil {
		childCfg.Hooks = hookCap
	}

	childAgent, _, err := composeTurnAgent(ctx, childCfg)
	if err != nil {
		return nil, fmt.Errorf("compose child subagent: %w", err)
	}

	inMemStore := bgtask.NewInMemoryStore(nil)
	bridgedStore := newBridgingTaskStore(inMemStore, registry, turnID, tasksDir)
	executors := bgtask.NewExecutorRegistry()

	idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
		return registry.allocateTask(tasksDir), nil
	}

	mgr, err := bgtask.New(ctx, &bgtask.Config{
		Tasks:      bridgedStore,
		TaskEvents: bridgedStore,
		Executors:  executors,
		IDGen:      idGen,
		SendTaskCreatedEvent: func(context.Context, *bgtask.Task) error {
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build background task manager: %w", err)
	}

	localRunner, err := backgroundlocal.New(&backgroundlocal.Config{
		Manager:   mgr,
		Executors: executors,
	})
	if err != nil {
		return nil, fmt.Errorf("build background local runner: %w", err)
	}

	formatFn := func(_ context.Context, _ string, message *schema.Message) (string, error) {
		if message == nil {
			return "", nil
		}
		if transcript := registry.activeTranscript(); transcript != nil {
			if message.ReasoningContent != "" {
				transcript.reasoning(message.ReasoningContent)
			}
			if message.Role == schema.Assistant && message.Content != "" {
				transcript.text(message.Content)
			}
			if message.Role == schema.Tool && message.Content != "" {
				transcript.tool(message.Content)
			}
		}
		var entry transcriptEntry
		switch message.Role {
		case schema.User:
			entry = transcriptEntry{Step: stepTask, Content: message.Content}
		case schema.Assistant:
			entry = transcriptEntry{Step: stepText, Content: message.Content}
		case schema.Tool:
			entry = transcriptEntry{Step: stepTool, Content: message.Content}
		default:
			entry = transcriptEntry{Step: stepText, Content: message.Content}
		}
		b, err := json.Marshal(entry)
		return string(b), err
	}

	outputBaseDir := surface.agentDir
	if outputBaseDir == "" {
		outputBaseDir = tasksDir
	}

	subagentMW, err := subagent.New(ctx, &subagent.Config{
		SubAgents: []adk.Agent{childAgent},
		Background: &subagent.BackgroundConfig{
			Local: &subagent.LocalBackgroundConfig{
				Runner:      localRunner,
				OutputStore: &diskAppendOpener{baseDir: outputBaseDir},
				OutputDir:   workspaces.DirTasks,
			},
			TranscriptFormat: formatFn,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build subagent middleware: %w", err)
	}

	bgMW, err := bgmiddleware.New(ctx, &bgmiddleware.Config{
		Manager: mgr,
	})
	if err != nil {
		return nil, fmt.Errorf("build backgroundtask middleware: %w", err)
	}

	wrappedSubagent := &wrappedSubagentMW{
		ChatModelAgentMiddleware: subagentMW,
		registry:                 registry,
		tasksDir:                 tasksDir,
		turnID:                   turnID,
	}
	wrappedBg := &wrappedBgMW{
		ChatModelAgentMiddleware: bgMW,
		manager:                  mgr,
		registry:                 registry,
		tasksDir:                 tasksDir,
	}

	return &delegation{
		subagentCap: &subagentCapability{Middleware: wrappedSubagent},
		bgCap:       &backgroundTaskCapability{Middleware: wrappedBg},
		manager:     mgr,
		registry:    registry,
		mcpTools:    mcpTools,
	}, nil
}

type wrappedSubagentMW struct {
	adk.ChatModelAgentMiddleware
	registry *taskRegistry
	tasksDir string
	turnID   string
}

func (w *wrappedSubagentMW) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.Message]) (context.Context, *adk.ChatModelAgentContext[*schema.Message], error) {
	ctx, runCtx, err := w.ChatModelAgentMiddleware.BeforeAgent(ctx, runCtx)
	if err != nil {
		return ctx, runCtx, err
	}
	if runCtx == nil {
		return ctx, runCtx, nil
	}
	tools := make([]tool.BaseTool, len(runCtx.Tools))
	copy(tools, runCtx.Tools)
	for i, t := range tools {
		info, err := t.Info(ctx)
		if err == nil && info != nil && info.Name == ToolAgent {
			tools[i] = &wrappedAgentTool{
				inner:    t.(tool.InvokableTool),
				registry: w.registry,
				tasksDir: w.tasksDir,
				turnID:   w.turnID,
			}
		}
	}
	runCtx.Tools = tools
	return ctx, runCtx, nil
}

type wrappedAgentTool struct {
	inner    tool.InvokableTool
	registry *taskRegistry
	tasksDir string
	turnID   string
}

func (t *wrappedAgentTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *wrappedAgentTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return "", err
	}
	task, _ := raw["task"].(string)
	prompt, _ := raw["prompt"].(string)
	if task == "" && prompt != "" {
		task = prompt
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", errors.New("agent: task must not be empty")
	}
	runInBackground, _ := raw["run_in_background"].(bool)
	t.registry.setPending(taskSummary(task), runInBackground, t.turnID)

	einoIn := map[string]any{
		"subagent_type":     generalPurposeSubagentName,
		"prompt":            task,
		"description":       taskSummary(task),
		"run_in_background": runInBackground,
	}
	inBytes, _ := json.Marshal(einoIn)

	if !runInBackground {
		out, err := t.inner.InvokableRun(ctx, string(inBytes), opts...)
		taskID := t.registry.latestAllocatedID()
		if err != nil {
			t.registry.finish(taskID, taskStatusFailed, err.Error())
			return "", err
		}

		t.registry.finish(taskID, taskStatusCompleted, out)

		res := delegationResult{
			TaskID:         taskID,
			State:          string(taskStatusCompleted),
			Result:         out,
			TranscriptPath: filepath.Join(workspaces.DirTasks, taskID+taskTranscriptSuffix),
		}
		b, _ := json.Marshal(res)
		return string(b), nil
	}

	out, err := t.inner.InvokableRun(ctx, string(inBytes), opts...)
	if err != nil {
		return "", err
	}

	taskID := t.registry.latestAllocatedID()
	if taskID == "" {
		if idx := strings.Index(out, "with ID: "); idx != -1 {
			rest := out[idx+len("with ID: "):]
			taskID = strings.Split(rest, ".")[0]
		}
	}
	res := delegationResult{
		TaskID:         taskID,
		State:          string(taskStatusRunning),
		TranscriptPath: filepath.Join(workspaces.DirTasks, taskID+taskTranscriptSuffix),
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

type wrappedBgMW struct {
	adk.ChatModelAgentMiddleware
	manager  *bgtask.Manager
	registry *taskRegistry
	tasksDir string
}

func (w *wrappedBgMW) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.Message]) (context.Context, *adk.ChatModelAgentContext[*schema.Message], error) {
	ctx, runCtx, err := w.ChatModelAgentMiddleware.BeforeAgent(ctx, runCtx)
	if err != nil {
		return ctx, runCtx, err
	}
	if runCtx == nil {
		return ctx, runCtx, nil
	}
	tools := make([]tool.BaseTool, len(runCtx.Tools))
	copy(tools, runCtx.Tools)
	for i, t := range tools {
		info, err := t.Info(ctx)
		if err == nil && info != nil {
			switch info.Name {
			case ToolTaskOutput:
				tools[i] = &wrappedTaskOutputTool{
					inner:    t.(tool.InvokableTool),
					manager:  w.manager,
					registry: w.registry,
				}
			case ToolTaskStop:
				tools[i] = &wrappedTaskStopTool{
					inner:    t.(tool.InvokableTool),
					manager:  w.manager,
					registry: w.registry,
				}
			}
		}
	}
	runCtx.Tools = tools
	return ctx, runCtx, nil
}

type wrappedTaskOutputTool struct {
	inner    tool.InvokableTool
	manager  *bgtask.Manager
	registry *taskRegistry
}

func (t *wrappedTaskOutputTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *wrappedTaskOutputTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	var in struct {
		TaskID string `json:"task_id"`
		Block  *bool  `json:"block"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		return "", errors.New("task_output: task_id must not be empty")
	}

	_, err := t.inner.InvokableRun(ctx, input, opts...)
	if err != nil {
		return "", err
	}

	task, err := t.manager.Get(ctx, taskID)
	if err != nil {
		if errors.Is(err, bgtask.ErrNotFound) {
			return "", fmt.Errorf("task %q: %w", taskID, ErrTaskNotFound)
		}
		return "", err
	}

	state := string(taskStatusRunning)
	switch task.Status {
	case bgtask.StatusCompleted:
		state = string(taskStatusCompleted)
	case bgtask.StatusCanceled:
		state = string(taskStatusStopped)
	case bgtask.StatusFailed:
		state = string(taskStatusFailed)
	}

	res := taskOutputResult{
		TaskID:         taskID,
		State:          state,
		Description:    task.Spec.Description,
		Result:         string(task.ResultData),
		TranscriptPath: filepath.Join(workspaces.DirTasks, taskID+taskTranscriptSuffix),
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

type wrappedTaskStopTool struct {
	inner    tool.InvokableTool
	manager  *bgtask.Manager
	registry *taskRegistry
}

func (t *wrappedTaskStopTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *wrappedTaskStopTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	var in struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		return "", errors.New("task_stop: task_id must not be empty")
	}

	if rec, err := t.registry.lookup(taskID); err == nil && rec.Terminal() {
		return "", fmt.Errorf("task %q: %w", taskID, ErrTaskNotRunning)
	}

	_, err := t.inner.InvokableRun(ctx, input, opts...)
	if err != nil {
		return "", err
	}

	_, _ = t.registry.cancel(taskID)

	res := taskStopResult{
		TaskID: taskID,
		State:  string(taskStatusStopped),
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}
