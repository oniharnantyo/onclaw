package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
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

// ReservedShellTool is the reserved allowlist name that enables the jailed
// shell tool. It is not a registry entry: the filesystem middleware registers
// its execute tool when the agent's tools allowlist contains this name.
const ReservedShellTool = "execute"

// FilesystemConfig holds the configuration for the filesystem capability.
type FilesystemConfig struct {
	// AgentDir is the agent's jailed workspace directory.
	AgentDir string
	// Shell, when non-nil, enables the middleware's execute tool, backed by
	// this implementation. Commands run with AgentDir as the working
	// directory; nil means the shell capability is off for this agent.
	Shell einofs.Shell
	// DisabledTools lists fs middleware tool names the agent's effective
	// allowlist did not select; each is attached to the middleware with its
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

	// CompactionObserver receives the display-only token estimates of every
	// summarization the composed agent performs (automatic threshold path,
	// chat-compact-command D5). Optional; nil skips the estimates and the
	// compaction event renders without them.
	CompactionObserver func(tokensBefore, tokensAfter int)
}

// Compose constructs an executable ADK agent from caller-supplied configuration.
// It is a pure function: data in, composed agent out, zero I/O.
func Compose(ctx context.Context, cfg *Config) (adk.TypedResumableAgent[*schema.AgenticMessage], error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = DefaultMaxIterations
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
		MaxIterations: maxIterations,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: cfg.Tools}},
	})
	if err != nil {
		return nil, fmt.Errorf("compose agent: %w", err)
	}

	return agent, nil
}

// validateConfig enforces that required fields are present and that capability
// dependencies are met (e.g. summarization requires filesystem for transcript offload).
func validateConfig(cfg *Config) error {
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
	return nil
}

// buildMiddlewares conditionally attaches capability middlewares in the verified order:
// patchtoolcalls → reduction → summarization → skill → filesystem → attachments.
// patchtoolcalls and the attachments placeholder policy are always attached.
// Capabilities attach only when configured.
func buildMiddlewares(ctx context.Context, cfg *Config) ([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	handlers := make([]adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], 0, 6)

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
	// registered only when a Shell backend is also supplied. Tools the agent's
	// allowlist did not select are disabled via their per-tool config.
	if cfg.Filesystem != nil {
		fsConfig := &fsmw.MiddlewareConfig{Backend: jail, Shell: cfg.Filesystem.Shell}
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

	// 6. attachments: unconditional (attachments design D7). Shape-keyed at
	// model time — byte-carrying blocks pass, URL-only references from older
	// turns collapse to placeholders — so it takes no capability
	// configuration. Appended after the fs middleware so it rewrites whatever
	// the earlier capability middlewares left behind, ahead of the model call.
	handlers = append(handlers, newAttachmentsPlaceholderMiddleware())

	// 7. hooks: the machine policy gate on tool calls (design.md D2/D3),
	// attached only when the run resolved a non-empty hook chain — a run with
	// no applicable hooks pays nothing. Appended BEFORE the tool-error-result
	// middleware so that wrapper stays outside it: a hook block returns the
	// block JSON as a successful tool result, while endpoint errors and
	// interrupt/cancel signals flow out to it (and to the ADK) untouched.
	if cfg.Hooks != nil && cfg.Hooks.HasHooks() && cfg.HooksBase != nil {
		handlers = append(handlers, newHooksMiddleware(cfg.Hooks, *cfg.HooksBase))
	}

	// 8. tool-error-result: appended last so it wraps every tool endpoint —
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
