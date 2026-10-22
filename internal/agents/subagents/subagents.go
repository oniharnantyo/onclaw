package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	bgmiddleware "github.com/cloudwego/eino/adk/middlewares/backgroundtask"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/workspaces"
)

const (
	ToolAgent      = "agent"
	ToolTaskOutput = "task_output"
	ToolTaskStop   = "task_stop"
)

// SubagentCapabilities carries the initialized middlewares and runner for delegation.
type SubagentCapabilities struct {
	SubagentMiddleware       adk.ChatModelAgentMiddleware
	BackgroundTaskMiddleware adk.ChatModelAgentMiddleware
	Manager                  *bgtask.Manager
	LocalRunner              *backgroundlocal.Runner
}

// BuildSubagentCapabilities constructs the eino subagent and backgroundtask middlewares.
func BuildSubagentCapabilities(
	ctx context.Context,
	turnID string,
	tasksDir string,
	outputBaseDir string,
	childAgent adk.Agent,
	registry *TaskRegistry,
) (*SubagentCapabilities, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}

	inMemStore := bgtask.NewInMemoryStore(nil)
	bridgedStore := NewBridgingTaskStore(inMemStore, registry, turnID, tasksDir)
	executors := bgtask.NewExecutorRegistry()

	idGen := func(_ context.Context, _ *bgtask.AllocateTaskIDRequest) (string, error) {
		return registry.AllocateTask(tasksDir), nil
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
		var entry TranscriptEntry
		switch message.Role {
		case schema.User:
			entry = TranscriptEntry{Step: StepTask, Content: message.Content}
		case schema.Assistant:
			entry = TranscriptEntry{Step: StepText, Content: message.Content}
		case schema.Tool:
			entry = TranscriptEntry{Step: StepTool, Content: message.Content}
		default:
			entry = TranscriptEntry{Step: StepText, Content: message.Content}
		}
		b, err := json.Marshal(entry)
		return string(b), err
	}

	if outputBaseDir == "" {
		outputBaseDir = tasksDir
	}

	subagentMW, err := subagent.New(ctx, &subagent.Config{
		SubAgents: []adk.Agent{childAgent},
		Background: &subagent.BackgroundConfig{
			Local: &subagent.LocalBackgroundConfig{
				Runner:      localRunner,
				OutputStore: &DiskAppendOpener{BaseDir: outputBaseDir},
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

	return &SubagentCapabilities{
		SubagentMiddleware:       wrappedSubagent,
		BackgroundTaskMiddleware: wrappedBg,
		Manager:                  mgr,
		LocalRunner:              localRunner,
	}, nil
}

type wrappedSubagentMW struct {
	adk.ChatModelAgentMiddleware
	registry *TaskRegistry
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
	registry *TaskRegistry
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
	subType, _ := raw["subagent_type"].(string)
	if subType == "" {
		subType = "general-purpose"
	}
	runInBg, _ := raw["run_in_background"].(bool)

	adapted := map[string]any{
		"prompt":            task,
		"description":       task,
		"subagent_type":     subType,
		"run_in_background": runInBg,
	}
	adaptedJSON, err := json.Marshal(adapted)
	if err != nil {
		return "", err
	}

	out, err := t.inner.InvokableRun(ctx, string(adaptedJSON), opts...)
	if err != nil {
		return "", err
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err == nil {
		if taskID, ok := parsed["task_id"].(string); ok {
			status, _ := parsed["status"].(string)
			result, _ := parsed["result"].(string)
			desc, _ := parsed["description"].(string)
			if desc == "" {
				desc = task
			}
			t.registry.mu.Lock()
			rec, exists := t.registry.tasks[taskID]
			if exists {
				if status == string(TaskStatusCompleted) {
					rec.Status = TaskStatusCompleted
					rec.Result = result
				}
			}
			t.registry.mu.Unlock()
			t.registry.EmitTask(taskID)
		}
	}
	return out, nil
}

type wrappedBgMW struct {
	adk.ChatModelAgentMiddleware
	manager  *bgtask.Manager
	registry *TaskRegistry
	tasksDir string
}

func (w *wrappedBgMW) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.Message]) (context.Context, *adk.ChatModelAgentContext[*schema.Message], error) {
	return w.ChatModelAgentMiddleware.BeforeAgent(ctx, runCtx)
}
