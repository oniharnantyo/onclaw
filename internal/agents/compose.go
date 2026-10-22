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
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
)

// DefaultMaxIterations caps how many tool-call iterations (model generation
// cycles) one turn may take before the runtime stops it with a visible cap
// notice (design D3: a runtime constant, not a per-agent column; spec
// agent-tools "Runaway loop is bounded").
const DefaultMaxIterations = 25

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
	// Skills, when non-nil, attaches the skill middleware: eligible skills'
	// metadata rides the turn's context while their bodies load on demand
	// through the middleware's skill tool (spec agent-skills).
	Skills *skillsCapability
	// Hooks, when non-nil, attaches the hooks middleware: pre_tool_use gates
	// every tool call, post_tool_use observes executions (spec agent-hooks).
	Hooks *hooksCapability
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
// order (the onclaw ordering, trimmed to the landed capabilities):
// patchtoolcalls → skill → filesystem → hooks → tool-error-result. Later
// waves insert their middlewares at their onclaw positions in this one list
// (reduction and summarization before skill; subagents after hooks) — the
// list is the extension seam.
//
// tool-error-result is appended last so it wraps every tool endpoint —
// resolved tools and middleware-registered fs/shell/skill tools alike: a
// failed tool call becomes an error result the model can read and react to
// instead of a run-killing NodeRunError (spec agent-tools "Tool failure
// feeds back"). hooks sits directly before it: a block result is a
// successful tool output and passes through the outer wrapper untouched.
func buildHandlers(ctx context.Context, cfg *composeConfig) ([]adk.ChatModelAgentMiddleware, *toolErrorResult, error) {
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 4)

	// 1. patchtoolcalls: unconditional (onclaw composes it first).
	patchMW, err := patchtoolcalls.New(ctx, &patchtoolcalls.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("build middlewares: patch tool calls: %w", err)
	}
	handlers = append(handlers, patchMW)

	// 2. skill: attached when configured (the onclaw position: after
	// patchtoolcalls, before filesystem). The middleware appends its skill
	// guidance to the instruction, refreshes the available-skills metadata
	// reminder on the first model call, and registers the skill tool that
	// loads bodies on demand — all gated by the backend's eligibility.
	if cfg.Skills != nil {
		skillMW, err := einoskill.NewMiddleware(ctx, &einoskill.Config{Backend: cfg.Skills.Backend})
		if err != nil {
			return nil, nil, fmt.Errorf("build middlewares: skill: %w", err)
		}
		handlers = append(handlers, skillMW)
	}

	// 3. filesystem: attached when configured; the execute tool is registered
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

	// 3.5 hooks: attached when the turn resolved a hook chain (spec
	// agent-hooks). pre_tool_use gates every tool endpoint — a blocked call
	// never reaches its tool — and post_tool_use observes executions
	// detached. It sits BEFORE tool-error-result so a block result is a
	// successful output the outer wrapper passes through untouched.
	if cfg.Hooks != nil {
		handlers = append(handlers, newHooksMiddleware(cfg.Hooks.Resolved, cfg.Hooks.Base))
	}

	// 4. tool-error-result: appended last so it wraps every tool endpoint.
	toolErrors := newToolErrorResult()
	handlers = append(handlers, toolErrors)

	return handlers, toolErrors, nil
}
