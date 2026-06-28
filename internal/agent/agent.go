package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Agent wraps eino ADK ChatModelAgent and configuration context.
type Agent struct {
	EinoAgent *adk.ChatModelAgent
	Config    *store.Agent
	Workspace string
}

// AssembleAgent constructs a ChatModelAgent with persona configuration, tools, and summarization middleware.
func AssembleAgent(ctx context.Context, agentConf *store.Agent, chatModel model.ToolCallingChatModel, workspace string, userConfigDir string, shellPolicy string, shellAllowlist []string, contextWindow int) (*Agent, error) {
	// Load existing persona/memory files and AGENTS.md
	persona, err := LoadPersonaContext(ctx, workspace, userConfigDir)
	if err != nil {
		return nil, fmt.Errorf("load persona context: %w", err)
	}

	var promptParts []string
	// Agent-specific system prompt comes first (highest priority)
	if agentConf.SystemPrompt != "" {
		promptParts = append(promptParts, agentConf.SystemPrompt)
	}
	// Persona context files second (identity, vibes, workspace rules)
	if persona != "" {
		promptParts = append(promptParts, persona)
	}

	// Workspace grounding
	grounding := fmt.Sprintf("Your active workspace directory is: %s", workspace)
	promptParts = append(promptParts, grounding)

	// Base instruction
	promptParts = append(promptParts, "You can execute commands in this workspace using your tools.")

	instruction := strings.Join(promptParts, "\n\n")

	// 2. Build tools
	tools := tools.Builtin(&tools.Scope{
		Workspace:      workspace,
		ShellPolicy:    shellPolicy,
		ShellAllowlist: shellAllowlist,
	})

	// Filter tools if a tool subset is configured on the agent
	if agentConf.Tools != "" {
		allowedTools := make(map[string]bool)
		for _, t := range strings.Split(agentConf.Tools, ",") {
			allowedTools[strings.TrimSpace(t)] = true
		}
		var filteredTools []tool.BaseTool
		for _, t := range tools {
			info, err := t.Info(ctx)
			if err != nil {
				continue
			}
			if allowedTools[info.Name] {
				filteredTools = append(filteredTools, t)
			}
		}
		tools = filteredTools
	}

	// 3. Assemble summarization middleware
	// We trigger when total tokens exceed 80% of context window
	transcriptPath := filepath.Join(userConfigDir, "conversations", fmt.Sprintf("%s_transcript.jsonl", agentConf.Name))

	triggerTokens := summarizationTrigger(contextWindow)
	sumMW, err := summarization.New(ctx, &summarization.Config{
		Model: chatModel,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: triggerTokens,
		},
		TranscriptFilePath: transcriptPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create summarization middleware: %w", err)
	}

	maxIterations := agentConf.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 20 // Default to 20 cycles
	}

	// 4. Create ChatModelAgentConfig
	agentConfig := &adk.ChatModelAgentConfig{
		Name:        agentConf.Name,
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               tools,
				ExecuteSequentially: true,
			},
		},
		MaxIterations: maxIterations,
		Handlers: []adk.ChatModelAgentMiddleware{
			sumMW,
		},
	}

	einoAgent, err := adk.NewChatModelAgent(ctx, agentConfig)
	if err != nil {
		return nil, fmt.Errorf("create Eino ChatModelAgent: %w", err)
	}

	return &Agent{
		EinoAgent: einoAgent,
		Config:    agentConf,
		Workspace: workspace,
	}, nil
}

func summarizationTrigger(contextWindow int) int {
	return int(float64(contextWindow) * 0.8)
}
