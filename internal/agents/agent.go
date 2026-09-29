package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	backgroundtaskmw "github.com/cloudwego/eino/adk/middlewares/backgroundtask"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
	einosumm "github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
)

// DefaultMaxIterations caps how many turns the ADK runner takes before
// stopping. Override per-agent via Config.MaxIterations.
const DefaultMaxIterations = 25

// DefaultMaxTokens is the run-time max-tokens the claude-family connectors
// construct with when the agent pins none (refactor-workspace-settings D4):
// inherit agents skip the save-time RequiresMaxTokens check — their effective
// provider type is only known at run start — so a requiring type proceeds on
// this documented default instead of failing the run. Pinned agents keep the
// strict save-time 400 and ride the same connector default.
const DefaultMaxTokens = 4096

// ReservedShellTool is the reserved tool name for the jailed shell tool. It
// is not a registry entry: the filesystem middleware registers its execute
// tool unless the agent's disabled_tools denies the name (agent-tools-denylist
// D3 — default-on, opt-out), or a per-turn override omits it.
const ReservedShellTool = "execute"

// FilesystemConfig holds the configuration for the filesystem capability.
type FilesystemConfig struct {
	// AgentDir is the agent's jailed workspace directory.
	AgentDir string
	// Shell, when non-nil, enables the middleware's execute tool, backed by
	// this implementation. Commands run with AgentDir as the working
	// directory; nil means the shell capability is off for this agent.
	Shell einofs.Shell
	// DisabledTools lists fs middleware tool names the turn's effective tool
	// set does not expose — denied by the agent's disabled_tools or stripped
	// by the workspace gate; each is attached to the middleware with its
	// per-tool Disable flag set (workspace-tool-catalog D3).
	DisabledTools []string
	// ReadOnlyRoots lists extra read-only roots the jail also resolves for
	// read operations (design D6: the workspace skills tree). Absolute paths
	// under these roots become readable; Write/Edit stay AgentDir-only.
	// Roots that do not exist on disk are skipped.
	ReadOnlyRoots []string
	// ProjectMountDir, when non-empty, is the channel's shared project
	// directory, mounted read-write at backend.ProjectMountPoint inside the
	// jail (channel-teams D5). Channel runs set it for member agents only;
	// empty elsewhere — non-channel composition is unchanged.
	ProjectMountDir string
	// Background, when non-nil, is the shell background lane the runner
	// resolves and supplies (add-agent-subagents-background design.md D11):
	// the fs middleware's execute tool gains run_in_background and its runs
	// join the per-run Runner's task-ID space. Nil means the lane is off.
	// Requires Shell — validated (a background lane cannot exist without the
	// shell capability; spec agent-background-shell "Opt-in does not grant
	// shell"). The top-level agent only: the subagent clone's fs middleware
	// never carries it (buildHandlers' clone scope strips the lane).
	Background *fsmw.BackgroundConfig
}

// SkillsConfig holds the configuration for the skills capability.
type SkillsConfig struct {
	// OnClawDir is the instance root; the skills resolver derives the three
	// tier directories from it.
	OnClawDir string
	// TenantSlug and AgentSlug address the workspace and agent tiers.
	TenantSlug string
	AgentSlug  string
	// EnabledSkills reads the workspace-skill registry master switch: only
	// enabled workspace skills attach. Injected by the composition root;
	// nil (unit tests only) treats every on-disk workspace skill as enabled.
	EnabledSkills backend.EnabledSkillReader
}

// SummarizationConfig holds the configuration for the summarization capability.
type SummarizationConfig struct {
	// TriggerTokens is the resolved context window × margin.
	TriggerTokens int
}

// Config represents the complete input needed to compose an executable agent.
// All fields are caller-supplied: identity, finished instruction, pre-built model,
// resolved tools, and optional capability configurations. Nil capability pointers
// mean the capability is disabled.
type Config struct {
	Name          string
	Description   string
	Instruction   string
	ChatModel     model.BaseModel[*schema.AgenticMessage]
	Tools         []tool.BaseTool
	MaxIterations int

	Filesystem    *FilesystemConfig
	Skills        *SkillsConfig
	Summarization *SummarizationConfig

	// Hooks is the run's resolved lifecycle-hook chain (design.md D2); nil or
	// a chain with no hooks attaches no hooks middleware. HooksBase carries
	// the per-run event identity (workspace, agent, session, user, origin)
	// every hook delivery is built from; it is non-nil whenever Hooks is.
	Hooks     *agenthooks.Resolved
	HooksBase *agenthooks.Event

	// SubagentsEnabled is the explicit enable signal for the delegation
	// capability (add-agent-subagents-background task 2.1): the runner wires
	// it only when the reserved `subagents` name is not denied by the agent's
	// disabled_tools (agent-tools-denylist D3 opt-out) or the per-turn
	// override names it (design.md D9). REQUIRED because every field below
	// keeps zero-value semantics — a zero Config must compose exactly as
	// before, so absence of declared subagents cannot itself distinguish
	// "capability off" from "capability on, nothing declared". Capability
	// wired ⇔ SubagentsEnabled.
	SubagentsEnabled bool
	// SubAgents lists caller-declared subagent instances the `agent` tool can
	// delegate to, offered alongside the built-in general-purpose clone unless
	// WithoutGeneralSubAgent suppresses it (design.md D4). Read only when
	// SubagentsEnabled is set; each entry needs a non-empty name and
	// description, and names must be unique (validated).
	SubAgents []adk.TypedAgent[*schema.AgenticMessage]
	// Background, when non-nil, enables background delegation: the `agent`
	// tool gains run_in_background and the task_output/task_stop control
	// tools attach, all bound to the config's per-run Runner (design.md D5).
	// Nil keeps delegation foreground-only — no control tools, no background
	// slot. The runner pairs it with SubagentsEnabled (task 3.2).
	Background *SubagentBackgroundConfig
	// WithoutGeneralSubAgent suppresses the built-in general-purpose clone,
	// mirroring the eino field name (design.md D4): the clone is neither built
	// nor offered, so with SubagentsEnabled set, delegation offers only the
	// declared SubAgents — and fails fast here when that list is empty.
	WithoutGeneralSubAgent bool

	// DelegationInstructionObserver receives the total byte length of the
	// run instruction as it stands AFTER the delegation capability's
	// BeforeAgent injections (add-agent-subagents-background task 6.1): the
	// subagent middleware appends its delegation instruction at BeforeAgent —
	// invisible to the compose-time measurement — so the context breakdown's
	// Instructions segment needs this after-the-fact total. Optional; nil
	// skips the measurement. Invoked once per turn, per BeforeAgent ordering
	// (adk's applyBeforeAgent runs handlers in order, each seeing the
	// previous handler's augmented instruction).
	DelegationInstructionObserver func(totalInstructionBytes int)

	// gate is the run's connection-tool gate (add-integration-authority
	// tasks 2.2–2.4): inactive for runs without connection tools — no
	// middleware, byte-identical composition. Unexported: runner-internal
	// wiring, set by composeAgent from the resolved config.
	gate *connectionGateConfig

	// CompactionObserver receives the display-only token estimates of every
	// summarization the composed agent performs (automatic threshold path,
	// chat-compact-command D5). Optional; nil skips the estimates and the
	// compaction event renders without them.
	CompactionObserver func(tokensBefore, tokensAfter int)
}

// Compose constructs an executable ADK agent from caller-supplied configuration.
// It is a pure function: data in, composed agent out, zero I/O.
func Compose(ctx context.Context, cfg *Config) (adk.TypedResumableAgent[*schema.AgenticMessage], error) {
	if err := validateConfig(ctx, cfg); err != nil {
		return nil, err
	}

	handlers, err := buildMiddlewares(ctx, cfg)
	if err != nil {
		return nil, err
	}

	agent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:          cfg.Name,
		Description:   cfg.Description,
		Instruction:   cfg.Instruction,
		Model:         cfg.ChatModel,
		Handlers:      handlers,
		MaxIterations: effectiveMaxIterations(cfg),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: toolsNodeConfig(cfg),
			// Forward child delegation events onto the run's stream, tagged
			// with the child session id (add-agent-subagents-background D7):
			// without this the child's token usage is invisible to the run's
			// accounting (task 5.4) and a forwarded event could never leak in
			// the first place — the drain loop drops every child-tagged event
			// before it can reach the transcript, so the delegation still
			// renders as exactly one tool-call card. The general-purpose
			// clone keeps the default: it cannot delegate, so it has nothing
			// to forward.
			EmitInternalEvents: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("compose agent: %w", err)
	}

	return agent, nil
}

// toolsNodeConfig builds the ToolsNode config both the delegating agent and its
// general-purpose clone compose from: the resolved business tools plus the
// unknown-tool seam (add-integration-authority task 2.2) — a run whose
// resolution pruned write-tier connection tools routes direct attempts at a
// pruned name through the gate's handler — the canonical block, never the
// engine's raw not-found run failure (D3). Every other run composes the
// baseline ToolsNode config untouched.
func toolsNodeConfig(cfg *Config) compose.ToolsNodeConfig {
	toolsNodeCfg := compose.ToolsNodeConfig{Tools: cfg.Tools}
	if cfg.gate != nil {
		toolsNodeCfg.UnknownToolsHandler = cfg.gate.unknownToolHandler()
	}
	return toolsNodeCfg
}

// effectiveMaxIterations returns the caller's iteration cap or the package
// default. The general-purpose clone shares the parent's cap (design.md D4).
func effectiveMaxIterations(cfg *Config) int {
	if cfg.MaxIterations <= 0 {
		return DefaultMaxIterations
	}
	return cfg.MaxIterations
}

// validateConfig enforces that required fields are present and that capability
// dependencies are met (e.g. summarization requires filesystem for transcript offload).
func validateConfig(ctx context.Context, cfg *Config) error {
	if cfg == nil {
		return errors.New("compose: config is required")
	}
	if cfg.ChatModel == nil {
		return errors.New("compose: chat model is required")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("compose: agent name is required")
	}
	if strings.TrimSpace(cfg.Instruction) == "" {
		return errors.New("compose: agent instruction is required")
	}
	if cfg.Summarization != nil && cfg.Filesystem == nil {
		return errors.New("compose: summarization requires filesystem configuration for transcript offload target")
	}
	// Background shell rides the fs middleware, which only exists with the
	// shell capability (add-agent-subagents-background task 2.1; spec
	// agent-background-shell "Opt-in does not grant shell").
	if cfg.Filesystem != nil && cfg.Filesystem.Background != nil && cfg.Filesystem.Shell == nil {
		return errors.New("compose: filesystem background shell requires the shell capability")
	}
	// One run wires ONE background task space: when both the delegation lane
	// and the shell lane are set they must share the same Runner, so both
	// lanes address one task-id space through one Manager (design.md D11).
	// The runner constructs both from the same per-run space; a direct Compose
	// call that wires different Runners is a wiring bug surfaced here.
	if cfg.Background != nil && cfg.Filesystem != nil && cfg.Filesystem.Background != nil &&
		cfg.Filesystem.Background.Local != nil && cfg.Background.Runner != cfg.Filesystem.Background.Local.Runner {
		return errors.New("compose: subagent background and filesystem background must share the same per-run Runner (one task-id space)")
	}
	return validateSubagentConfig(ctx, cfg)
}

// middlewareScope selects which variant of the handler list one build produces.
type middlewareScope struct {
	// delegating marks the top-level agent's list: the shell background lane
	// attaches to its fs middleware (design.md D11) and the delegation steps
	// (subagent then background control) attach when the capability is enabled.
	delegating bool
}

// buildMiddlewares builds the delegating agent's handler list.
func buildMiddlewares(ctx context.Context, cfg *Config) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	return buildHandlers(ctx, cfg, middlewareScope{delegating: true})
}

// buildHandlers conditionally attaches capability middlewares in the fixed order
// (design.md D3): patchtoolcalls → reduction → summarization → skill →
// filesystem → [subagent → background-control] → attachments → hooks →
// connection gate → tool-error-result. patchtoolcalls and the attachments
// placeholder policy are always attached; capabilities attach only when
// configured.
//
// The general-purpose clone composes from its own buildHandlers pass with a
// non-delegating scope: fresh middleware instances (never shared with the
// parent), no shell background lane on its fs middleware (deep's background
// asymmetry — background orchestration is a top-level concern only; a child
// shell task would be unobservable), and no delegation steps (spec:
// "Delegation cannot recurse").
func buildHandlers(ctx context.Context, cfg *Config, scope middlewareScope) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	handlers := make([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], 0, 11)

	// 1. patchtoolcalls: unconditional
	patchMW, err := patchtoolcalls.NewTyped[*schema.AgenticMessage](ctx, &patchtoolcalls.Config{})
	if err != nil {
		return nil, fmt.Errorf("build middlewares: patch tool calls: %w", err)
	}
	handlers = append(handlers, patchMW)

	// Build jail once if Filesystem is configured, shared by reduction and filesystem.
	var jail einofs.Backend
	if cfg.Filesystem != nil {
		if cfg.Filesystem.ProjectMountDir != "" {
			// Channel run with a shared project space (channel-teams D5):
			// /project mounts read-write alongside the read-only roots.
			fsJail, err := backend.NewFilesystemJailedWithMounts(cfg.Filesystem.AgentDir,
				[]backend.WritableMount{{Mount: backend.ProjectMountPoint, Dir: cfg.Filesystem.ProjectMountDir}},
				cfg.Filesystem.ReadOnlyRoots...)
			if err != nil {
				return nil, fmt.Errorf("build middlewares: filesystem jail: %w", err)
			}
			jail = fsJail
		} else {
			fsJail, err := backend.NewFilesystemJailedWithRoots(cfg.Filesystem.AgentDir, cfg.Filesystem.ReadOnlyRoots...)
			if err != nil {
				return nil, fmt.Errorf("build middlewares: filesystem jail: %w", err)
			}
			jail = fsJail
		}
	}

	// 2. reduction: attached if Filesystem is configured
	if cfg.Filesystem != nil {
		reductionMW, err := reduction.NewTyped[*schema.AgenticMessage](ctx, &reduction.TypedConfig[*schema.AgenticMessage]{
			Backend:          jail,
			RootDir:          backend.DefaultMountPoint,
			ReadFileToolName: "read_file",
		})
		if err != nil {
			return nil, fmt.Errorf("build middlewares: reduction: %w", err)
		}
		handlers = append(handlers, reductionMW)
	}

	// 3. summarization: attached if Summarization is configured
	if cfg.Summarization != nil {
		summMW, err := einosumm.NewTyped[*schema.AgenticMessage](ctx, &einosumm.TypedConfig[*schema.AgenticMessage]{
			Model:    cfg.ChatModel,
			Trigger:  &einosumm.TriggerCondition{ContextTokens: cfg.Summarization.TriggerTokens},
			Callback: newCompactionCallback(cfg.Filesystem.AgentDir, cfg.CompactionObserver),
		})
		if err != nil {
			return nil, fmt.Errorf("build middlewares: summarization: %w", err)
		}
		handlers = append(handlers, summMW)
	}

	// 4. skill: attached if Skills is configured
	if cfg.Skills != nil {
		skillMW, err := einoskill.NewTyped[*schema.AgenticMessage](ctx, &einoskill.TypedConfig[*schema.AgenticMessage]{
			Backend: backend.NewSkillBackend(cfg.Skills.OnClawDir, cfg.Skills.TenantSlug, cfg.Skills.AgentSlug, cfg.Skills.EnabledSkills),
		})
		if err != nil {
			return nil, fmt.Errorf("build middlewares: skill: %w", err)
		}
		handlers = append(handlers, skillMW)
	}

	// 5. filesystem: attached if Filesystem is configured; the execute tool is
	// registered only when a Shell backend is also supplied. Tools the turn's
	// effective set does not expose (denied by the agent's disabled_tools or
	// gated by the workspace) are disabled via their per-tool config.
	if cfg.Filesystem != nil {
		fsConfig := &fsmw.MiddlewareConfig{Backend: jail, Shell: cfg.Filesystem.Shell}
		// The shell background lane (add-agent-subagents-background design.md
		// D11): the runner resolves the reserved-name pair coherently and
		// supplies the config; the clone's non-delegating pass never carries
		// it. With the lane on, the execute tool gains run_in_background and
		// launches join the per-run Runner's task-ID space.
		if scope.delegating {
			fsConfig.Background = cfg.Filesystem.Background
			// The lane's execute tool is this custom one, not the
			// middleware's managed tool (shell_lane.go): a foreground run
			// must reach the shell directly — the managed tool's task
			// boundary swallows a shell approval interrupt, and the lane's
			// spec pins foreground behavior unchanged. Explicit background
			// launches route through the runner; launches of
			// approval-raising commands are refused readably (the design's
			// documented fallback).
			if cfg.Filesystem.Background != nil && cfg.Filesystem.Background.Local != nil {
				laneTool, err := newShellLaneExecuteTool(cfg.Filesystem.Shell, cfg.Filesystem.Background.Local, cfg.Filesystem.Background.NotificationSessionID)
				if err != nil {
					return nil, fmt.Errorf("build middlewares: filesystem: %w", err)
				}
				fsConfig.ExecuteToolConfig = &fsmw.ExecuteToolConfig{CustomTool: laneTool}
			}
		}
		// The fs tools contract absolute paths (the middleware's DeepAgents
		// container mount), and the jail maps DefaultMountPoint onto the agent
		// dir — tell the model where its workspace lives so it doesn't guess
		// unusable host paths. The shared project mount (channel-teams D5) is
		// announced alongside it, channel runs only.
		guidance := "Filesystem: your workspace is mounted at " + backend.DefaultMountPoint +
			". File tool paths are absolute paths under " + backend.DefaultMountPoint +
			" (e.g. " + backend.DefaultMountPoint + "/notes.md); relative paths and paths outside it fail."
		if cfg.Filesystem.ProjectMountDir != "" {
			guidance += " The channel's shared project space is mounted read-write at " + backend.ProjectMountPoint +
				" (e.g. " + backend.ProjectMountPoint + "/PLAN.md); PLAN.md is the tracker — claim your area before writing."
		}
		fsConfig.CustomSystemPrompt = &guidance
		for _, name := range cfg.Filesystem.DisabledTools {
			switch name {
			case "ls":
				fsConfig.LsToolConfig = &fsmw.ToolConfig{Disable: true}
			case "read_file":
				fsConfig.ReadFileToolConfig = &fsmw.ToolConfig{Disable: true}
			case "write_file":
				fsConfig.WriteFileToolConfig = &fsmw.ToolConfig{Disable: true}
			case "edit_file":
				fsConfig.EditFileToolConfig = &fsmw.ToolConfig{Disable: true}
			case "glob":
				fsConfig.GlobToolConfig = &fsmw.ToolConfig{Disable: true}
			case "grep":
				fsConfig.GrepToolConfig = &fsmw.ToolConfig{Disable: true}
			}
		}
		fsMW, err := fsmw.NewTyped[*schema.AgenticMessage](ctx, fsConfig)
		if err != nil {
			return nil, fmt.Errorf("build middlewares: filesystem: %w", err)
		}
		handlers = append(handlers, fsMW)
	}

	// 6. subagent then background-control: the delegation capability
	// (add-agent-subagents-background design.md D1–D4), attached only when the
	// run enabled it — runs without the capability compose byte-identically to
	// before. Position is pinned (D3): after filesystem and before
	// attachments/hooks/gate/tool-error-result, so the later wrappers stay
	// outermost and the parent's hooks and gate wrap the `agent` tool call —
	// a block returns the canonical block JSON as the tool result.
	subagentsDelegating := scope.delegating && cfg.SubagentsEnabled
	if subagentsDelegating {
		// Suppression (WithoutGeneralSubAgent) skips the clone entirely: the
		// `agent` tool then offers ONLY the declared SubAgents — validation
		// guarantees that list is non-empty (subagents.go fail-fast). Otherwise
		// the clone builds from its own fresh pre-subagent handler pass (D4):
		// non-delegating scope — fresh middleware instances, no background
		// lane, no delegation steps.
		subAgents := make([]adk.TypedAgent[*schema.AgenticMessage], 0, len(cfg.SubAgents)+1)
		if !cfg.WithoutGeneralSubAgent {
			cloneHandlers, err := buildHandlers(ctx, cfg, middlewareScope{})
			if err != nil {
				return nil, err
			}
			clone, err := buildGeneralPurposeSubagent(ctx, cfg, cloneHandlers)
			if err != nil {
				return nil, err
			}
			subAgents = append(subAgents, clone)
		}
		subAgents = append(subAgents, cfg.SubAgents...)
		subCfg := &subagent.TypedConfig[*schema.AgenticMessage]{
			SubAgents: subAgents,
			// design.md D2: eino's default name kept explicitly — the domain
			// noun; deep's `task` collides with Run/Scheduler vocabulary.
			ToolName: "agent",
		}
		if cfg.Background != nil {
			subCfg.Background = &subagent.TypedBackgroundConfig[*schema.AgenticMessage]{
				Local: &subagent.TypedLocalBackgroundConfig[*schema.AgenticMessage]{
					Runner:      cfg.Background.Runner,
					OutputStore: cfg.Background.OutputStore,
					OutputDir:   cfg.Background.OutputDir,
				},
			}
		}
		subMW, err := subagent.NewTyped(ctx, subCfg)
		if err != nil {
			return nil, fmt.Errorf("build middlewares: subagent: %w", err)
		}
		handlers = append(handlers, subMW)
	}

	// Control tools (task_output/task_stop) come ONLY from this middleware —
	// the single owner per its package contract — bound to the Manager of the
	// ONE per-run task space both lanes share (design.md D11). Attached when
	// either lane is on: background delegation (SubagentsEnabled + Background)
	// or the shell-only lane (Filesystem.Background, resolved by the runner
	// from the reserved background_shell name). The shell-only lane composes
	// no subagent middleware — its control tools address shell tasks alone.
	// Foreground-only delegation (Background nil, lane off) attaches nothing:
	// no control tools, no run_in_background slot.
	if scope.delegating {
		var controlManager *bgtask.Manager
		switch {
		case cfg.SubagentsEnabled && cfg.Background != nil:
			controlManager = cfg.Background.Runner.Manager()
		case cfg.Filesystem != nil && cfg.Filesystem.Background != nil:
			// The runner always wires the lane's Local half; a Recoverable-only
			// direct call has no Runner to bind and composes no control tools.
			if cfg.Filesystem.Background.Local != nil {
				controlManager = cfg.Filesystem.Background.Local.Runner.Manager()
			}
		}
		if controlManager != nil {
			controlMW, err := backgroundtaskmw.NewTyped[*schema.AgenticMessage](ctx, &backgroundtaskmw.TypedConfig[*schema.AgenticMessage]{
				Manager: controlManager,
				// Both local lanes emit progress records through the Manager's
				// runtime under the process-local executor key; without a
				// reader task_output renders the task record only (deep's
				// assembly registers readers solely for its durable-subagent
				// and managed-tool executor keys — verified against
				// alpha.35's prebuilt/deep). The reader mirrors
				// backgroundtool.ProgressReader's bounded newest-first view.
				ProgressReadersByExecutorKey: map[string]backgroundtaskmw.TaskProgressReader{
					processLocalExecutorKey: newLocalTaskProgressReader(controlManager),
				},
			})
			if err != nil {
				return nil, fmt.Errorf("build middlewares: background task control: %w", err)
			}
			handlers = append(handlers, controlMW)
		}
	}

	// Delegation-instruction measurement (task 6.1): appended after the
	// subagent (and, when present, control) middleware so this measurer's
	// BeforeAgent — handlers run in order — sees the run instruction with the
	// capability's BeforeAgent-injected instruction slices already appended.
	// Attached whenever the capability is on (the observer is nil-guarded,
	// the compaction-callback precedent), so the delegation stack's shape
	// never depends on whether anyone observes. Composition-only concern:
	// the clone's pass never carries it.
	if subagentsDelegating {
		handlers = append(handlers, &delegationInstructionMeasurer{observe: cfg.DelegationInstructionObserver})
	}

	// 7. attachments: unconditional (attachments design D7). Shape-keyed at
	// model time — byte-carrying blocks pass, URL-only references from older
	// turns collapse to placeholders — so it takes no capability
	// configuration. Appended after the fs middleware so it rewrites whatever
	// the earlier capability middlewares left behind, ahead of the model call.
	handlers = append(handlers, newAttachmentsPlaceholderMiddleware())

	// 8. hooks: the machine policy gate on tool calls (design.md D2/D3),
	// attached only when the run resolved a non-empty hook chain — a run with
	// no applicable hooks pays nothing. Appended BEFORE the tool-error-result
	// middleware so that wrapper stays outside it: a hook block returns the
	// block JSON as a successful tool result, while endpoint errors and
	// interrupt/cancel signals flow out to it (and to the ADK) untouched.
	if cfg.Hooks != nil && cfg.Hooks.HasHooks() && cfg.HooksBase != nil {
		handlers = append(handlers, newHooksMiddleware(cfg.Hooks, *cfg.HooksBase))
	}

	// 9. connection gate: the user-authority gate on connection-sourced tools
	// (add-integration-authority tasks 2.2–2.4), attached only when the run
	// resolved connection tools — runs without connections pay nothing.
	// Appended AFTER hooks (the gate decides before hooks see the call: a
	// denied call never reaches workspace policy) and BEFORE the
	// tool-error-result middleware, whose wrapper sits outside it: the gate's
	// block returns the canonical block JSON as a successful tool result,
	// while the escalation interrupt signal flows out untouched to the ADK.
	if cfg.gate.active() {
		handlers = append(handlers, newConnectionGateMiddleware(cfg.gate))
	}

	// 10. tool-error-result: appended last so it wraps every tool endpoint —
	// registry tools and middleware-registered fs/shell tools alike. A failed
	// tool call becomes an error result the model can read and react to
	// instead of a run-killing NodeRunError.
	handlers = append(handlers, newToolErrorResultMiddleware())

	return handlers, nil
}

// newCompactionCallback builds the summarization Callback shared by the
// composed agent's automatic threshold path and the compact command's
// standalone per-turn instance (chat-compact-command D3): both offload the
// full pre-compaction transcript into the agent jail, and both surface the
// display-only token estimates of the replacement to the given observer.
func newCompactionCallback(agentDir string, observer func(tokensBefore, tokensAfter int)) func(context.Context, adk.TypedChatModelAgentState[*schema.AgenticMessage], adk.TypedChatModelAgentState[*schema.AgenticMessage]) error {
	return func(_ context.Context, before, after adk.TypedChatModelAgentState[*schema.AgenticMessage]) error {
		if err := offloadTranscript(agentDir, before.Messages); err != nil {
			return err
		}
		if observer != nil {
			observer(estimateWindowTokens(before.Messages), estimateWindowTokens(after.Messages))
		}
		return nil
	}
}

// offloadTranscript writes the full pre-compaction history to transcript.md
// inside the agent jail so the summary that replaces the working window never
// destroys the prior record.
func offloadTranscript(agentDir string, msgs []*schema.AgenticMessage) error {
	var sb strings.Builder
	sb.WriteString("# Transcript (pre-compaction history)\n\n")
	for _, m := range msgs {
		if m == nil {
			continue
		}
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", m.Role, extractAgenticText(m))
	}
	path := filepath.Join(agentDir, "transcript.md")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("offloadTranscript: write %s: %w", path, err)
	}
	return nil
}

// processLocalExecutorKey is the backgroundlocal Runner's executor key — the
// Spec.ExecutorKey stamped on every task both local lanes launch. The
// constant is unexported upstream (adk/backgroundtask/local), so the literal
// is pinned here: an upstream rename would silently disable the progress
// projection (task_output still renders the task record), never break the
// build — the degrading direction.
const processLocalExecutorKey = "eino.dev/process-local"

// localTaskProgressMaxBytes bounds the rendered progress view — the same
// display budget backgroundtool.ProgressReader applies.
const localTaskProgressMaxBytes = 16 << 10

// localTaskProgressLimit is how many newest progress records task_output
// renders for local-lane tasks.
const localTaskProgressLimit = 20

// localTaskProgressReader projects a bounded newest-first view of the progress
// records the local lanes emit through the Manager's execution runtime: JSONL
// child-transcript lines from the delegation lane, output chunks from the
// shell lane. Implements backgroundtaskmw.TaskProgressReader; keyed to
// processLocalExecutorKey in the control middleware's wiring.
type localTaskProgressReader struct {
	manager *bgtask.Manager
}

func newLocalTaskProgressReader(manager *bgtask.Manager) *localTaskProgressReader {
	return &localTaskProgressReader{manager: manager}
}

// ReadProgress renders up to localTaskProgressLimit newest records, oldest
// last, capped at localTaskProgressMaxBytes. An empty record set renders
// nothing — task_output then shows the bare task record.
func (r *localTaskProgressReader) ReadProgress(ctx context.Context, task *bgtask.Task) (string, error) {
	if r == nil || r.manager == nil || task == nil {
		return "", nil
	}
	result, err := r.manager.ListTaskEvents(ctx, &bgtask.ListTaskEventsRequest{
		TaskID: task.Spec.ID, Limit: localTaskProgressLimit, NewestFirst: true,
	})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("Recent progress:")
	for i := len(result.Events) - 1; i >= 0; i-- {
		line := strings.TrimRight(string(result.Events[i].Data), "\n")
		if line == "" {
			continue
		}
		if sb.Len()+len(line)+1 > localTaskProgressMaxBytes {
			break
		}
		sb.WriteString("\n")
		sb.WriteString(line)
	}
	return sb.String(), nil
}

// delegationInstructionMeasurer is the BeforeAgent middleware that
// reports the run instruction's byte total AFTER the delegation capability's
// BeforeAgent injections (add-agent-subagents-background task 6.1): handlers
// run in order, so this measurer — appended after the subagent (and, when
// present, control) middleware — sees the augmented instruction the same way
// GenModelInput will. It never modifies the context.
type delegationInstructionMeasurer struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	observe func(totalInstructionBytes int)
}

// BeforeAgent reports len(Instruction) and passes everything through.
func (m *delegationInstructionMeasurer) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.AgenticMessage]) (context.Context, *adk.ChatModelAgentContext[*schema.AgenticMessage], error) {
	if runCtx != nil && m.observe != nil {
		m.observe(len(runCtx.Instruction))
	}
	return ctx, runCtx, nil
}
