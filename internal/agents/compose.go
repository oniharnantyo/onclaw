package agents

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
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
)

// DefaultMaxIterations caps how many tool-call iterations (model generation
// cycles) one turn may take before the runtime stops it with a visible cap
// notice (design D3: a runtime constant, not a per-agent column; spec
// agent-tools "Runaway loop is bounded").
const DefaultMaxIterations = 25

// DefaultMaxLengthForTrunc is the default maximum character length of a tool
// result before truncation and offload occur (design D2).
const DefaultMaxLengthForTrunc = 50000

// DefaultClearRetentionSuffixLimit is the default number of recent tool-call
// rounds retained intact during clear phase.
const DefaultClearRetentionSuffixLimit = 1

// DefaultPlantaskBaseDir is the mount-scoped directory where plantask files
// (.tasks/*.json and .highwatermark) reside.
const DefaultPlantaskBaseDir = backend.DefaultMountPoint + "/.tasks"

// composeConfig is the complete input needed to compose one turn's executable
// agent (design D1: the ported onclaw Compose(Config) seam). All fields are
// caller-supplied: identity, finished instruction, pre-built model, resolved
// tools, and optional capability configurations. A nil capability pointer
// means the capability is disabled. Later waves extend this struct and the
// ordered handler list in composeTurnAgent the same way onclaw composes its
// middlewares (skills, hooks, MCP, subagents) — never by editing the loop.
type composeConfig struct {
	// Name is the composed agent's name (kept for future subagent naming).
	Name string
	// Instruction is the finished system instruction: the composed persona
	// plus the compaction summary. Capability middlewares append their own
	// guidance to it at BeforeAgent.
	Instruction string
	// ChatModel is the pre-built model for the turn (registry product).
	ChatModel model.BaseChatModel
	// Tools are the turn's resolved direct tools (catalog products and the
	// tools/backend packages' workspace-scoped tools).
	Tools []tool.BaseTool
	// MaxIterations is the turn's tool-call iteration cap; <= 0 applies
	// DefaultMaxIterations.
	MaxIterations int
	// Filesystem, when non-nil, attaches the filesystem middleware: the
	// jailed fs tools plus, when Shell is set, the jailed execute tool.
	Filesystem *filesystemCapability
	// Reduction, when non-nil, attaches the tool-output reduction middleware
	// (design D2): oversized tool outputs are truncated and offloaded to the
	// workspace jail, and historical rounds are cleared when token usage
	// approaches context limits.
	Reduction *reductionCapability
	// Summarization, when non-nil, attaches the in-run summarization safety net
	// middleware (design D1): in-run state collapses when token count exceeds
	// ~0.9 of the model's context limit.
	Summarization *summarizationCapability
	// Plantask, when non-nil, attaches the plantask middleware (design D3):
	// structured task management tools (task_create, task_get, task_update,
	// task_list) backed by the workspace jail.
	Plantask *plantaskCapability
	// DisabledPlantaskTools lists plantask tool names to withhold. When set,
	// merged with Plantask.DisabledTools.
	DisabledPlantaskTools []string
	// Skills, when non-nil, attaches the skill middleware: eligible skills'
	// metadata rides the turn's context while their bodies load on demand
	// through the middleware's skill tool (spec agent-skills).
	Skills *skillsCapability
	// Hooks, when non-nil, attaches the hooks middleware: pre_tool_use gates
	// every tool call, post_tool_use observes executions (spec agent-hooks).
	Hooks *hooksCapability
	// Subagent, when non-nil, attaches the eino subagent middleware (design D4).
	Subagent *subagentCapability
	// BackgroundTask, when non-nil, attaches the eino backgroundtask middleware
	// (design D4) injecting task_output and task_stop.
	BackgroundTask *backgroundTaskCapability
}

// subagentCapability holds the subagent middleware instance.
type subagentCapability struct {
	Middleware adk.ChatModelAgentMiddleware
}

// backgroundTaskCapability holds the background task control middleware instance.
type backgroundTaskCapability struct {
	Middleware adk.ChatModelAgentMiddleware
}

// filesystemCapability holds the configuration for the filesystem capability.
type filesystemCapability struct {
	// AgentDir is the agent's jailed workspace directory (must exist).
	AgentDir string
	// Shell, when non-nil, enables the middleware's execute tool, backed by
	// this implementation. Commands run with AgentDir as the working
	// directory; nil means the shell capability is off for this turn.
	Shell einofs.Shell
	// DisabledTools lists fs middleware tool names the turn's gated tool set
	// does not offer — each is attached to the middleware with its per-tool
	// Disable flag set, mirroring onclaw's workspace gate.
	DisabledTools []string
	// ExtraRoots are additional read-only jail roots (design D6: the global
	// skills tree): reads may land in them, writes stay confined to
	// AgentDir. Absent roots are skipped.
	ExtraRoots []string
}

// reductionCapability holds configuration for the tool-output reduction
// capability (design D2): oversized tool outputs are truncated and offloaded
// to the jailed workspace, and historical rounds are cleared when token usage
// approaches context limits.
type reductionCapability struct {
	// AgentDir is the agent's workspace directory where offloaded files land.
	// If empty, falls back to Filesystem.AgentDir if Filesystem is configured.
	AgentDir string
	// ExtraRoots are additional read-only jail roots (design D6: skills library).
	// If empty, falls back to Filesystem.ExtraRoots.
	ExtraRoots []string
	// ContextLimit is the model's context token limit used to derive
	// MaxTokensForClear (0.8 * ContextLimit) if MaxTokensForClear is not explicitly set.
	ContextLimit int64
	// MaxTokensForClear explicitly overrides the clear threshold if > 0.
	MaxTokensForClear int64
	// MaxLengthForTrunc is the tool-result string length threshold triggering
	// truncation. If <= 0, defaults to DefaultMaxLengthForTrunc (50000).
	MaxLengthForTrunc int
	// ReadFileToolName is the tool name used by the model to retrieve offloaded
	// files. Defaults to "read_file".
	ReadFileToolName string
	// ClearRetentionSuffixLimit is the number of recent tool rounds preserved
	// intact during clear. Defaults to DefaultClearRetentionSuffixLimit (1).
	ClearRetentionSuffixLimit int
	// TokenCounter is an optional token estimator function. If nil, uses default
	// token counter (~4 chars per token).
	TokenCounter func(ctx context.Context, msg []*schema.Message, tools []*schema.ToolInfo) (int64, error)
}

// plantaskCapability holds configuration for the plantask capability (design D3):
// structured task management tools (task_create, task_get, task_update, task_list)
// backed by the workspace jail.
type plantaskCapability struct {
	// AgentDir is the agent's workspace directory where task files (.tasks/...) land.
	// If empty, falls back to Filesystem.AgentDir if Filesystem is configured.
	AgentDir string
	// ExtraRoots are additional read-only jail roots. If empty, falls back to
	// Filesystem.ExtraRoots.
	ExtraRoots []string
	// BaseDir is the mount-scoped base directory for task files.
	// Defaults to DefaultPlantaskBaseDir ("/workspace/.tasks").
	BaseDir string
	// DisabledTools lists plantask tool names (e.g. task_create, task_get,
	// task_update, task_list) withheld from the turn's tool set. Both snake_case
	// and PascalCase tool names are recognized.
	DisabledTools []string
}

// skillsCapability holds the configuration for the skills capability.
type skillsCapability struct {
	// Backend resolves the turn's eligible skills (metadata list, body on
	// demand) across the global and agent tiers.
	Backend einoskill.Backend
	// GlobalDir is the runtime's global skills library directory. It rides
	// the composition because the jailed filesystem mounts it as a read-only
	// extra root — including on a delegated child run, which composes its own
	// filesystem capability from the same data.
	GlobalDir string
}

// hooksCapability holds the configuration for the hooks capability: the
// turn's resolved chain plus the event identity its deliveries clone.
type hooksCapability struct {
	// Resolved is the per-turn hook execution plan the middleware dispatches
	// through (the per-call decision cache lives on it).
	Resolved *hooks.Resolved
	// Base is the per-run event identity each delivery clones (agent, session,
	// turn, origin).
	Base hooks.Event
}

// composeTurnAgent constructs the turn's executable ADK agent from
// caller-supplied configuration: system instruction + tools → ChatModelAgent
// with the tool loop. It is a pure function: data in, composed agent out,
// zero I/O. The returned toolErrorResult middleware carries the turn's
// tool-failure registry the event drain consults when persisting terminal
// tool records.
func composeTurnAgent(ctx context.Context, cfg *composeConfig) (adk.Agent, *toolErrorResult, error) {
	if err := validateComposeConfig(cfg); err != nil {
		return nil, nil, err
	}

	handlers, toolErrors, err := buildHandlers(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Instruction:   cfg.Instruction,
		Model:         cfg.ChatModel,
		Handlers:      handlers,
		MaxIterations: effectiveMaxIterations(cfg),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: cfg.Tools},
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("compose agent: %w", err)
	}
	return agent, toolErrors, nil
}

// effectiveMaxIterations returns the caller's iteration cap or the package
// default (design D3: runtime constant).
func effectiveMaxIterations(cfg *composeConfig) int {
	if cfg.MaxIterations <= 0 {
		return DefaultMaxIterations
	}
	return cfg.MaxIterations
}

// validateComposeConfig enforces that required fields are present.
func validateComposeConfig(cfg *composeConfig) error {
	if cfg == nil {
		return errors.New("compose: config is required")
	}
	if cfg.ChatModel == nil {
		return errors.New("compose: chat model is required")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("compose: agent name is required")
	}
	return nil
}

// buildHandlers conditionally attaches capability middlewares in the fixed
// order (OpenSpec design D2, D3):
// patchtoolcalls → reduction → summarization (future wave) → skill →
// filesystem → plantask → hooks → tool-error-result.
//
// tool-error-result is appended last so it wraps every tool endpoint —
// resolved tools and middleware-registered fs/shell/skill/plantask tools
// alike: a failed tool call becomes an error result the model can read and
// react to instead of a run-killing NodeRunError (spec agent-tools "Tool
// failure feeds back"). hooks sits directly before it: a block result is a
// successful tool output and passes through the outer wrapper untouched.
func buildHandlers(ctx context.Context, cfg *composeConfig) ([]adk.ChatModelAgentMiddleware, *toolErrorResult, error) {
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 8)

	// 1. patchtoolcalls: unconditional (onclaw composes it first).
	patchMW, err := patchtoolcalls.New(ctx, &patchtoolcalls.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("build middlewares: patch tool calls: %w", err)
	}
	handlers = append(handlers, patchMW)

	// 2. reduction: attached when configured (design D2). Sits before
	// summarization and skill so oversized outputs are offloaded before
	// downstream middlewares or models inspect context.
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
			readFileToolName = "read_file"
		}
		retention := cfg.Reduction.ClearRetentionSuffixLimit
		if retention <= 0 {
			retention = DefaultClearRetentionSuffixLimit
		}
		maxTokensForClear := cfg.Reduction.MaxTokensForClear
		if maxTokensForClear <= 0 && cfg.Reduction.ContextLimit > 0 {
			maxTokensForClear = int64(float64(cfg.Reduction.ContextLimit) * compactionThreshold)
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

	// 2.5 summarization (design D1: in-run safety net before skill)
	if cfg.Summarization != nil && cfg.Summarization.Model != nil {
		sumMW, err := newSummarizationMiddleware(ctx, cfg.Summarization)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: summarization: %w", err)
		}
		handlers = append(handlers, sumMW)
	}

	// 3. skill: attached when configured (the onclaw position: after
	// patchtoolcalls/reduction/summarization, before filesystem). The middleware
	// appends its skill guidance to the instruction, refreshes the available-skills
	// metadata reminder on the first model call, and registers the skill tool that
	// loads bodies on demand — all gated by the backend's eligibility.
	if cfg.Skills != nil {
		skillMW, err := einoskill.NewMiddleware(ctx, &einoskill.Config{Backend: cfg.Skills.Backend})
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: skill: %w", err)
		}
		handlers = append(handlers, skillMW)
	}

	// 4. filesystem: attached when configured; the execute tool is registered
	// only when a Shell backend is also supplied. Tools the turn's gated set
	// does not offer are disabled via their per-tool config.
	if cfg.Filesystem != nil {
		jail, err := backend.NewFilesystemJailedWithRoots(cfg.Filesystem.AgentDir, cfg.Filesystem.ExtraRoots...)
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: filesystem jail: %w", err)
		}
		fsConfig := &fsmw.MiddlewareConfig{Backend: jail, Shell: cfg.Filesystem.Shell}
		// The fs tools contract absolute paths (the middleware's DeepAgents
		// container mount), and the jail maps the mount onto the agent dir —
		// tell the model where its workspace lives so it doesn't guess
		// unusable host paths (onclaw's guidance, same mount contract).
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

	// 5. plantask: attached when configured (design D3: after filesystem,
	// before hooks). Tools the turn's gated set does not offer are withheld
	// via plantaskGatedMiddleware, and registered tools are normalized to
	// snake_case.
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

	// 6. hooks: attached when the turn resolved a hook chain (spec
	// agent-hooks). pre_tool_use gates every tool endpoint — a blocked call
	// never reaches its tool — and post_tool_use observes executions
	// detached. It sits BEFORE tool-error-result so a block result is a
	// successful output the outer wrapper passes through untouched.
	if cfg.Hooks != nil {
		handlers = append(handlers, newHooksMiddleware(cfg.Hooks.Resolved, cfg.Hooks.Base))
	}

	// 8. subagent: attached when configured (design D4).
	if cfg.Subagent != nil && cfg.Subagent.Middleware != nil {
		handlers = append(handlers, cfg.Subagent.Middleware)
	}

	// 9. backgroundtask: attached when configured (design D4).
	if cfg.BackgroundTask != nil && cfg.BackgroundTask.Middleware != nil {
		handlers = append(handlers, cfg.BackgroundTask.Middleware)
	}

	// 10. tool-error-result: appended last so it wraps every tool endpoint.
	toolErrors := newToolErrorResult()
	handlers = append(handlers, toolErrors)

	return handlers, toolErrors, nil
}

// safeReductionMiddleware wraps the reduction middleware to ensure offload
// write failures do not fail the turn (spec agent-runtime "Offload failure keeps
// the original": any failure while offloading leaves the original content in place).
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
			// Offload write failed, but tool execution succeeded:
			// keep the original tool output so the turn proceeds.
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
		// Offload failure during clear phase leaves conversation intact.
		return ctx, state, nil
	}
	return newCtx, newState, nil
}

// plantaskNameToSnake maps upstream plantask tool names to their snake_case equivalents.
var plantaskNameToSnake = map[string]string{
	plantask.TaskCreateToolName: "task_create",
	plantask.TaskGetToolName:    "task_get",
	plantask.TaskUpdateToolName: "task_update",
	plantask.TaskListToolName:   "task_list",
	"task_create":               "task_create",
	"task_get":                  "task_get",
	"task_update":               "task_update",
	"task_list":                 "task_list",
}

// normalizePlantaskToolName normalizes PascalCase and snake_case plantask tool names.
func normalizePlantaskToolName(name string) string {
	if snake, ok := plantaskNameToSnake[name]; ok {
		return snake
	}
	return name
}

// plantaskGatedMiddleware wraps the plantask middleware to filter out any
// plantask tools withheld by the turn's tool gate (spec agent-tools "Task plan tools")
// and renames registered tools to snake_case.
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

		// Check gating by either original or snake_case name
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
