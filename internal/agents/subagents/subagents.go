package subagents

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	bgmiddleware "github.com/cloudwego/eino/adk/middlewares/backgroundtask"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/compaction"
	"github.com/oniharnantyo/buddies/internal/agents/compose"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

const (
	ToolAgent      = "agent"
	ToolTaskOutput = "task_output"
	ToolTaskStop   = "task_stop"

	GeneralPurposeSubagentName = "general-purpose"
	ChildIterationCap          = 15
)

// Config configures the subagent and backgroundtask stack for one turn.
type Config struct {
	Instruction      string
	Model            model.BaseModel[*schema.AgenticMessage]
	Tools            []tool.BaseTool
	MCPTools         []tool.BaseTool
	AgentDir         string
	ExtraRoots       []string
	Shell            bool
	FSDisabled       []string
	PlantaskDisabled []string
	ContextLimit     int64

	TasksDir      string
	OutputBaseDir string
	TurnID        string

	Skills      *compose.SkillsCapability
	Hooks       *compose.HooksCapability
	Checkpoints backend.ShellOption
	Registry    *TaskRegistry
}

// Capabilities carries the initialized middlewares and background manager.
type Capabilities struct {
	SubagentMiddleware adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	// DelegationArgsMiddleware must ride the same handler chain as
	// SubagentMiddleware: it normalizes the agent tool's arguments (always
	// background) before the tool parses them.
	DelegationArgsMiddleware adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	BackgroundTaskMiddleware adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	Manager                  *bgtask.Manager
	LocalRunner              *backgroundlocal.Runner
}

// New constructs the eino subagent and backgroundtask middlewares deep-shaped.
func New(ctx context.Context, cfg *Config) (*Capabilities, error) {
	if cfg == nil {
		return nil, errors.New("subagents: config is required")
	}
	if cfg.Model == nil {
		return nil, errors.New("subagents: model is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("subagents: registry is required")
	}

	// 1. Compose child agent without delegation tools (no self-delegation).
	childTools := append([]tool.BaseTool(nil), cfg.Tools...)
	childTools = append(childTools, cfg.MCPTools...)

	childCfg := &compose.ComposeConfig{
		Name:          GeneralPurposeSubagentName,
		Instruction:   cfg.Instruction,
		ChatModel:     cfg.Model,
		Tools:         childTools,
		MaxIterations: ChildIterationCap,
	}

	if cfg.AgentDir != "" {
		fsCap := &compose.FilesystemCapability{
			AgentDir:      cfg.AgentDir,
			DisabledTools: cfg.FSDisabled,
			ExtraRoots:    cfg.ExtraRoots,
		}
		if cfg.Shell {
			var opts []backend.ShellOption
			if cfg.Checkpoints != nil {
				opts = append(opts, cfg.Checkpoints)
			}
			fsCap.Shell = backend.NewJailedShell(cfg.AgentDir, opts...)
		}
		childCfg.Filesystem = fsCap

		childCfg.Reduction = &compose.ReductionCapability{
			AgentDir:     cfg.AgentDir,
			ExtraRoots:   cfg.ExtraRoots,
			ContextLimit: cfg.ContextLimit,
		}
		childCfg.Plantask = &compose.PlantaskCapability{
			AgentDir:      cfg.AgentDir,
			ExtraRoots:    cfg.ExtraRoots,
			DisabledTools: cfg.PlantaskDisabled,
		}
		childCfg.DisabledPlantaskTools = cfg.PlantaskDisabled
	}

	if cfg.ContextLimit > 0 {
		childCfg.Summarization = &compaction.SummarizationCapability{
			Model:        cfg.Model,
			ContextLimit: cfg.ContextLimit,
		}
	}

	if cfg.Skills != nil {
		childCfg.Skills = cfg.Skills
	}
	if cfg.Hooks != nil {
		childCfg.Hooks = cfg.Hooks
	}

	childAgent, _, err := compose.ComposeTurnAgent(ctx, childCfg)
	if err != nil {
		return nil, fmt.Errorf("compose child subagent: %w", err)
	}

	// 2. The session-scoped background substrate: the registry owns the eino
	// Manager and local Runner so tasks outlive the turn that launched them
	// and a later turn's task_output still finds them. Transcripts land under
	// the workspace (".tasks/<tool-call-id>.output", the path eino reserves
	// and reports to the model).
	outputBaseDir := cfg.OutputBaseDir
	if outputBaseDir == "" {
		outputBaseDir = cfg.AgentDir
	}
	if outputBaseDir == "" {
		outputBaseDir = cfg.TasksDir
	}
	mgr, localRunner, err := cfg.Registry.Space(ctx, outputBaseDir, cfg.TurnID)
	if err != nil {
		return nil, err
	}

	formatFn := FormatAgenticTranscriptEntry

	// 3. Native subagent middleware. Every delegation is a background task:
	// DelegationArgs forces the flag on the way in, the agent tool returns the
	// task id at once, the island tracks the child, and the parent keeps its
	// turn.
	subagentMW, err := subagent.NewTyped[*schema.AgenticMessage](ctx, &subagent.TypedConfig[*schema.AgenticMessage]{
		SubAgents: []adk.TypedAgent[*schema.AgenticMessage]{childAgent},
		Background: &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
			Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
				Runner:      localRunner,
				OutputStore: &DiskAppendOpener{BaseDir: outputBaseDir},
				OutputDir:   workspaces.DirTasks,
			},
			TranscriptFormat: formatFn,
		},
		ToolDescriptionGenerator: DelegationToolDescription,
	})
	if err != nil {
		return nil, fmt.Errorf("build subagent middleware: %w", err)
	}

	// 4. Native backgroundtask middleware.
	bgMW, err := bgmiddleware.NewTyped[*schema.AgenticMessage](ctx, &bgmiddleware.TypedConfig[*schema.AgenticMessage]{
		Manager: mgr,
	})
	if err != nil {
		return nil, fmt.Errorf("build backgroundtask middleware: %w", err)
	}

	return &Capabilities{
		SubagentMiddleware:       subagentMW,
		DelegationArgsMiddleware: NewDelegationArgs(ToolAgent, GeneralPurposeSubagentName),
		BackgroundTaskMiddleware: bgMW,
		Manager:                  mgr,
		LocalRunner:              localRunner,
	}, nil
}
