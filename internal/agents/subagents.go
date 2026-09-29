package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/schema"
)

// GeneralPurposeSubagentName is the built-in clone's name (design.md D4),
// mirroring deep's generalAgentName.
const GeneralPurposeSubagentName = "general-purpose"

// generalPurposeSubagentDescription adapts deep's generalAgentDescription
// (prebuilt/deep buildSubAgentsList): research-delegate framing, and the
// "(Tools: *)" spirit — the clone runs with the parent's full resolved tool
// surface, so the model reads an honest wildcard.
const generalPurposeSubagentDescription = "general-purpose agent for researching complex questions, searching code, and running multi-step tasks. When you are searching for a keyword or file and are not confident you will find the match quickly, use this agent to perform the search for you. It runs with the delegating agent's full tool surface. (Tools: *)"

// SubagentBackgroundConfig holds the background-delegation lane dependencies
// (design.md D5): the per-run process-local Runner, the append-opener that
// materializes child transcripts inside the agent jail (D6), and the
// jail-relative output directory. The runner constructs it per run (task 3.2);
// Compose only consumes it, keeping composition a pure function.
type SubagentBackgroundConfig struct {
	// Runner owns the background task lifecycle for this run; its Manager is
	// the binding point for the task_output/task_stop control tools, so
	// delegation and shell tasks share one task-ID space (design.md D11).
	Runner *backgroundlocal.Runner
	// OutputStore appends child transcripts under the agent jail so the fs
	// middleware's read_file can read them (design.md D6).
	OutputStore einofs.AppendOpener
	// OutputDir is the jail-relative directory output files reserve under.
	OutputDir string
}

// validateSubagentConfig enforces the subagent capability's fail-fast contract
// (add-agent-subagents-background task 2.1). Called from validateConfig; every
// rejection names the incoherence so a direct Compose misconfiguration is
// readable at the call site.
func validateSubagentConfig(ctx context.Context, cfg *Config) error {
	// An explicit enable signal with zero delegate-able instances composes an
	// `agent` tool that cannot succeed: suppression plus an empty list leaves
	// nothing to delegate to (design.md D4).
	if cfg.SubagentsEnabled && cfg.WithoutGeneralSubAgent && len(cfg.SubAgents) == 0 {
		return errors.New("compose: subagents enabled requires the general-purpose subagent or at least one declared subagent")
	}

	names := make(map[string]struct{}, len(cfg.SubAgents))
	for i, sa := range cfg.SubAgents {
		if sa == nil {
			return fmt.Errorf("compose: subagents[%d] is nil", i)
		}
		name := strings.TrimSpace(sa.Name(ctx))
		if name == "" {
			return fmt.Errorf("compose: subagents[%d] name is required", i)
		}
		if strings.TrimSpace(sa.Description(ctx)) == "" {
			return fmt.Errorf("compose: subagent %q description is required", name)
		}
		if _, dup := names[name]; dup {
			return fmt.Errorf("compose: duplicate subagent name %q", name)
		}
		names[name] = struct{}{}
	}

	// The background lane is only usable with every dependency wired: the
	// Runner launches tasks, the store materializes transcripts, and the model
	// is pointed at the output file by name (design.md D5/D6).
	if cfg.Background != nil && (cfg.Background.Runner == nil || cfg.Background.OutputStore == nil || cfg.Background.OutputDir == "") {
		return errors.New("compose: subagent background configuration requires runner, output store, and output dir")
	}
	return nil
}

// buildGeneralPurposeSubagent clones the delegating agent from the same Config
// via the pre-subagent handler slice (design.md D4): same instruction, model,
// resolved tool surface, and iteration cap, but no delegation steps — the clone
// cannot recurse because its handlers never contain the subagent or
// background-control middleware (spec: "Delegation cannot recurse"). It keeps
// every other capability wiring: attachments, hooks, gate, tool-error-result,
// and the filesystem tools (its fs middleware carries no background lane —
// background orchestration is a top-level concern only, deep.go's background
// asymmetry). handlers must be a freshly built pre-subagent list; middleware
// instances are never shared between the two agents.
func buildGeneralPurposeSubagent(ctx context.Context, cfg *Config, handlers []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]) (adk.TypedAgent[*schema.AgenticMessage], error) {
	agent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        GeneralPurposeSubagentName,
		Description: generalPurposeSubagentDescription,
		// Same finished instruction and model instance as the parent (D4): the
		// clone is this agent researching with its own context, not a persona.
		Instruction:   cfg.Instruction,
		Model:         cfg.ChatModel,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: toolsNodeConfig(cfg)},
		Handlers:      handlers,
		MaxIterations: effectiveMaxIterations(cfg),
	})
	if err != nil {
		return nil, fmt.Errorf("build general-purpose subagent: %w", err)
	}
	return agent, nil
}
