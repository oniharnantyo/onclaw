package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/plantask"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	einocompose "github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/compaction"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
)

// DefaultMaxIterations caps how many tool-call iterations one turn may take.
const DefaultMaxIterations = 25

// DefaultMaxLengthForTrunc is the default maximum character length of a tool
// result before truncation and offload occur (design D2).
const DefaultMaxLengthForTrunc = 50000

// DefaultClearRetentionSuffixLimit is the default number of recent tool-call
// rounds retained intact during clear phase.
const DefaultClearRetentionSuffixLimit = 1

// DefaultPlantaskBaseDir is the mount-scoped directory where plantask files reside.
const DefaultPlantaskBaseDir = backend.DefaultMountPoint + "/.tasks"

// FilesystemCapability carries the configuration for the filesystem middleware.
type FilesystemCapability struct {
	Backend       einofs.Backend
	Shell         einofs.Shell
	AgentDir      string
	ExtraRoots    []string
	DisabledTools []string
}

// ReductionCapability configures the tool-output reduction middleware.
type ReductionCapability struct {
	AgentDir                  string
	ExtraRoots                []string
	ContextLimit              int64
	MaxTokensForClear         int64
	MaxLengthForTrunc         int
	ReadFileToolName          string
	ClearRetentionSuffixLimit int
	TokenCounter              func(ctx context.Context, msg []*schema.Message, tools []*schema.ToolInfo) (int64, error)
}

// PlantaskCapability configures the structured task plan middleware.
type PlantaskCapability struct {
	AgentDir      string
	ExtraRoots    []string
	BaseDir       string
	DisabledTools []string
}

// SkillsCapability configures the skill-loading middleware.
type SkillsCapability struct {
	Backend einoskill.Backend
}

// HooksCapability carries the turn's resolved hooks and identity.
type HooksCapability struct {
	Resolved *hooks.Resolved
	Base     hooks.Event
}

// SubagentCapability carries the subagent middleware instance.
type SubagentCapability struct {
	Middleware adk.ChatModelAgentMiddleware
}

// BackgroundTaskCapability carries the backgroundtask middleware instance.
type BackgroundTaskCapability struct {
	Middleware adk.ChatModelAgentMiddleware
}

// ComposeConfig is the complete input needed to compose one turn's executable agent.
type ComposeConfig struct {
	Name                  string
	Instruction           string
	ChatModel             model.BaseChatModel
	Tools                 []tool.BaseTool
	MaxIterations         int
	Filesystem            *FilesystemCapability
	Reduction             *ReductionCapability
	Summarization         *compaction.SummarizationCapability
	Plantask              *PlantaskCapability
	DisabledPlantaskTools []string
	Skills                *SkillsCapability
	Hooks                 *HooksCapability
	Subagent              *SubagentCapability
	BackgroundTask        *BackgroundTaskCapability
}

// ComposeTurnAgent constructs the turn's executable ADK agent from cfg.
func ComposeTurnAgent(ctx context.Context, cfg *ComposeConfig) (adk.Agent, *ToolErrorResult, error) {
	if cfg == nil {
		return nil, nil, errors.New("compose: config is required")
	}
	if cfg.ChatModel == nil {
		return nil, nil, errors.New("compose: chat model is required")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, nil, errors.New("compose: agent name is required")
	}

	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = DefaultMaxIterations
	}

	handlers, toolErrors, err := buildHandlers(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("compose: handlers: %w", err)
	}

	agentCfg := &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Instruction:   cfg.Instruction,
		Model:         cfg.ChatModel,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: einocompose.ToolsNodeConfig{Tools: cfg.Tools}},
		MaxIterations: maxIter,
		Handlers:      handlers,
	}

	agent, err := adk.NewChatModelAgent(ctx, agentCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("compose: new agent: %w", err)
	}
	return agent, toolErrors, nil
}

func buildHandlers(ctx context.Context, cfg *ComposeConfig) ([]adk.ChatModelAgentMiddleware, *ToolErrorResult, error) {
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 8)

	// 1. patchtoolcalls: appended first so it is the outermost middleware.
	patchMW, err := patchtoolcalls.New(ctx, &patchtoolcalls.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("build middlewares: patchtoolcalls: %w", err)
	}
	handlers = append(handlers, patchMW)

	// 2. reduction: tool output truncation and workspace offload (design D2).
	if cfg.Reduction != nil {
		agentDir := cfg.Reduction.AgentDir
		extraRoots := cfg.Reduction.ExtraRoots
		if agentDir == "" && cfg.Filesystem != nil {
			agentDir = cfg.Filesystem.AgentDir
			if len(extraRoots) == 0 {
				extraRoots = cfg.Filesystem.ExtraRoots
			}
		}
		if agentDir == "" {
			return nil, nil, fmt.Errorf("build middlewares: reduction: workspace is not rooted")
		}

		redBackend, err := backend.NewReductionBackend(agentDir, extraRoots...)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: reduction backend: %w", err)
		}

		maxLength := cfg.Reduction.MaxLengthForTrunc
		if maxLength <= 0 {
			maxLength = DefaultMaxLengthForTrunc
		}
		readFileToolName := cfg.Reduction.ReadFileToolName
		if readFileToolName == "" {
			readFileToolName = ToolReadFile
		}
		retention := cfg.Reduction.ClearRetentionSuffixLimit
		if retention <= 0 {
			retention = DefaultClearRetentionSuffixLimit
		}
		maxTokensForClear := cfg.Reduction.MaxTokensForClear
		if maxTokensForClear <= 0 && cfg.Reduction.ContextLimit > 0 {
			maxTokensForClear = int64(float64(cfg.Reduction.ContextLimit) * compaction.CompactionThreshold)
		}
		if maxTokensForClear <= 0 {
			maxTokensForClear = 160000
		}

		redCfg := &reduction.Config{
			Backend:                   redBackend,
			RootDir:                   backend.DefaultMountPoint,
			ReadFileToolName:          readFileToolName,
			MaxLengthForTrunc:         maxLength,
			MaxTokensForClear:         maxTokensForClear,
			ClearRetentionSuffixLimit: retention,
			TokenCounter:              cfg.Reduction.TokenCounter,
		}
		redMW, err := reduction.New(ctx, redCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: reduction: %w", err)
		}
		handlers = append(handlers, newSafeReductionMiddleware(redMW))
	}

	// 2.5 summarization: in-run safety net before skill (design D1).
	if cfg.Summarization != nil && cfg.Summarization.Model != nil {
		sumMW, err := compaction.NewSummarizationMiddleware(ctx, cfg.Summarization)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: summarization: %w", err)
		}
		handlers = append(handlers, sumMW)
	}

	// 3. skill: attached when configured.
	if cfg.Skills != nil {
		skillMW, err := einoskill.NewMiddleware(ctx, &einoskill.Config{Backend: cfg.Skills.Backend})
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: skill: %w", err)
		}
		handlers = append(handlers, skillMW)
	}

	// 4. filesystem: attached when configured.
	if cfg.Filesystem != nil {
		jail, err := backend.NewFilesystemJailedWithRoots(cfg.Filesystem.AgentDir, cfg.Filesystem.ExtraRoots...)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: filesystem jail: %w", err)
		}
		fsConfig := &fsmw.MiddlewareConfig{Backend: jail, Shell: cfg.Filesystem.Shell}
		guidance := "Filesystem: your workspace is mounted at " + backend.DefaultMountPoint +
			". File tool paths are absolute paths under " + backend.DefaultMountPoint +
			" (e.g. " + backend.DefaultMountPoint + "/notes.md); relative paths and paths outside it fail."
		fsConfig.CustomSystemPrompt = &guidance
		for _, name := range cfg.Filesystem.DisabledTools {
			switch name {
			case fsmw.ToolNameLs:
				fsConfig.LsToolConfig = &fsmw.ToolConfig{Disable: true}
			case fsmw.ToolNameReadFile:
				fsConfig.ReadFileToolConfig = &fsmw.ToolConfig{Disable: true}
			case fsmw.ToolNameWriteFile:
				fsConfig.WriteFileToolConfig = &fsmw.ToolConfig{Disable: true}
			case fsmw.ToolNameEditFile:
				fsConfig.EditFileToolConfig = &fsmw.ToolConfig{Disable: true}
			case fsmw.ToolNameGlob:
				fsConfig.GlobToolConfig = &fsmw.ToolConfig{Disable: true}
			case fsmw.ToolNameGrep:
				fsConfig.GrepToolConfig = &fsmw.ToolConfig{Disable: true}
			}
		}
		fsMW, err := fsmw.New(ctx, fsConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: filesystem: %w", err)
		}
		handlers = append(handlers, fsMW)
	}

	// 5. plantask: attached when configured.
	if cfg.Plantask != nil {
		agentDir := cfg.Plantask.AgentDir
		extraRoots := cfg.Plantask.ExtraRoots
		if agentDir == "" && cfg.Filesystem != nil {
			agentDir = cfg.Filesystem.AgentDir
			if len(extraRoots) == 0 {
				extraRoots = cfg.Filesystem.ExtraRoots
			}
		}
		if agentDir == "" {
			return nil, nil, fmt.Errorf("build middlewares: plantask: workspace is not rooted")
		}

		ptBackend, err := backend.NewPlantaskBackend(agentDir, extraRoots...)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: plantask backend: %w", err)
		}

		baseDir := cfg.Plantask.BaseDir
		if baseDir == "" {
			baseDir = DefaultPlantaskBaseDir
		}

		ptMW, err := plantask.New(ctx, &plantask.Config{
			Backend: ptBackend,
			BaseDir: baseDir,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: plantask: %w", err)
		}

		var disabledTools []string
		if len(cfg.Plantask.DisabledTools) > 0 {
			disabledTools = append(disabledTools, cfg.Plantask.DisabledTools...)
		}
		if len(cfg.DisabledPlantaskTools) > 0 {
			disabledTools = append(disabledTools, cfg.DisabledPlantaskTools...)
		}

		handlers = append(handlers, newPlantaskGatedMiddleware(ptMW, disabledTools))
	}

	// 6. hooks: attached when configured.
	if cfg.Hooks != nil {
		handlers = append(handlers, NewHooksMiddleware(cfg.Hooks.Resolved, cfg.Hooks.Base))
	}

	// 8. subagent: attached when configured.
	if cfg.Subagent != nil && cfg.Subagent.Middleware != nil {
		handlers = append(handlers, cfg.Subagent.Middleware)
	}

	// 9. backgroundtask: attached when configured.
	if cfg.BackgroundTask != nil && cfg.BackgroundTask.Middleware != nil {
		handlers = append(handlers, cfg.BackgroundTask.Middleware)
	}

	// 10. tool-error-result: appended last so it wraps every tool endpoint.
	toolErrors := NewToolErrorResult()
	handlers = append(handlers, toolErrors)

	return handlers, toolErrors, nil
}

type safeReductionMiddleware struct {
	adk.ChatModelAgentMiddleware
}

func newSafeReductionMiddleware(mw adk.ChatModelAgentMiddleware) *safeReductionMiddleware {
	return &safeReductionMiddleware{ChatModelAgentMiddleware: mw}
}

func (w *safeReductionMiddleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	var origOutput string
	var origErr error
	spyEndpoint := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		origOutput, origErr = endpoint(ctx, argumentsInJSON, opts...)
		return origOutput, origErr
	}
	wrapped, err := w.ChatModelAgentMiddleware.WrapInvokableToolCall(ctx, spyEndpoint, tCtx)
	if err != nil {
		return endpoint, nil
	}
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		out, err := wrapped(ctx, argumentsInJSON, opts...)
		if err != nil && origErr == nil {
			return origOutput, nil
		}
		return out, err
	}, nil
}

func (w *safeReductionMiddleware) WrapEnhancedInvokableToolCall(ctx context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	var origOutput *schema.ToolResult
	var origErr error
	spyEndpoint := func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
		origOutput, origErr = endpoint(ctx, toolArgument, opts...)
		return origOutput, origErr
	}
	wrapped, err := w.ChatModelAgentMiddleware.WrapEnhancedInvokableToolCall(ctx, spyEndpoint, tCtx)
	if err != nil {
		return endpoint, nil
	}
	return func(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
		out, err := wrapped(ctx, toolArgument, opts...)
		if err != nil && origErr == nil {
			return origOutput, nil
		}
		return out, err
	}, nil
}

func (w *safeReductionMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	newCtx, newState, err := w.ChatModelAgentMiddleware.BeforeModelRewriteState(ctx, state, mc)
	if err != nil {
		return ctx, state, nil
	}
	return newCtx, newState, nil
}

var plantaskNameToSnake = map[string]string{
	plantask.TaskCreateToolName: ToolTaskCreate,
	plantask.TaskGetToolName:    ToolTaskGet,
	plantask.TaskUpdateToolName: ToolTaskUpdate,
	plantask.TaskListToolName:   ToolTaskList,
	ToolTaskCreate:              ToolTaskCreate,
	ToolTaskGet:                 ToolTaskGet,
	ToolTaskUpdate:              ToolTaskUpdate,
	ToolTaskList:                ToolTaskList,
}

func normalizePlantaskToolName(name string) string {
	if snake, ok := plantaskNameToSnake[name]; ok {
		return snake
	}
	return name
}

type plantaskGatedMiddleware struct {
	adk.ChatModelAgentMiddleware
	disabled map[string]struct{}
}

func newPlantaskGatedMiddleware(mw adk.ChatModelAgentMiddleware, disabled []string) *plantaskGatedMiddleware {
	m := make(map[string]struct{}, len(disabled)*2)
	for _, name := range disabled {
		m[name] = struct{}{}
		m[normalizePlantaskToolName(name)] = struct{}{}
	}
	return &plantaskGatedMiddleware{
		ChatModelAgentMiddleware: mw,
		disabled:                 m,
	}
}

func (m *plantaskGatedMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.Message]) (context.Context, *adk.ChatModelAgentContext[*schema.Message], error) {
	ctx, nRunCtx, err := m.ChatModelAgentMiddleware.BeforeAgent(ctx, runCtx)
	if err != nil || nRunCtx == nil {
		return ctx, nRunCtx, err
	}

	filtered := make([]tool.BaseTool, 0, len(nRunCtx.Tools))
	for _, t := range nRunCtx.Tools {
		info, err := t.Info(ctx)
		if err != nil {
			filtered = append(filtered, t)
			continue
		}
		snakeName, isPlantask := plantaskNameToSnake[info.Name]
		if !isPlantask {
			filtered = append(filtered, t)
			continue
		}

		if _, dis := m.disabled[info.Name]; dis {
			continue
		}
		if _, dis := m.disabled[snakeName]; dis {
			continue
		}

		filtered = append(filtered, wrapRenamedTool(t, snakeName))
	}

	res := *nRunCtx
	res.Tools = filtered
	return ctx, &res, nil
}

type renamedTool struct {
	tool.BaseTool
	name string
}

func (r *renamedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := r.BaseTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	cp := *info
	cp.Name = r.name
	return &cp, nil
}

type renamedInvokableTool struct {
	renamedTool
	inv tool.InvokableTool
}

func (r *renamedInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	return r.inv.InvokableRun(ctx, argumentsInJSON, opts...)
}

type renamedStreamableTool struct {
	renamedInvokableTool
	stream tool.StreamableTool
}

func (r *renamedStreamableTool) StreamableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
	return r.stream.StreamableRun(ctx, argumentsInJSON, opts...)
}

func wrapRenamedTool(t tool.BaseTool, newName string) tool.BaseTool {
	base := renamedTool{BaseTool: t, name: newName}
	inv, isInv := t.(tool.InvokableTool)
	stream, isStream := t.(tool.StreamableTool)

	if isInv && isStream {
		return &renamedStreamableTool{
			renamedInvokableTool: renamedInvokableTool{renamedTool: base, inv: inv},
			stream:               stream,
		}
	}
	if isInv {
		return &renamedInvokableTool{renamedTool: base, inv: inv}
	}
	return &base
}
