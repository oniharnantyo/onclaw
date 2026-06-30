package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Agent wraps eino ADK ChatModelAgent and configuration context.
type Agent struct {
	EinoAgent *adk.TypedChatModelAgent[*schema.AgenticMessage]
	Config    *store.Agent
	Workspace string
}

// AssembleAgent constructs a ChatModelAgent with persona configuration, tools, and summarization middleware.
func AssembleAgent(ctx context.Context, agentConf *store.Agent, chatModel model.AgenticModel, workspace string, userConfigDir string, shellPolicy string, shellAllowlist []string, contextWindow int, convStore store.ConversationStore, conversationID int64) (*Agent, error) {
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

	const persistedKey = "_onclaw_persisted"

	// 2. Build tools
	builtTools := tools.Builtin(&tools.Scope{
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
		for _, t := range builtTools {
			info, err := t.Info(ctx)
			if err != nil {
				continue
			}
			if allowedTools[info.Name] {
				filteredTools = append(filteredTools, t)
			}
		}
		builtTools = filteredTools
	}

	// 3. Assemble summarization middleware
	// We trigger when total tokens exceed 80% of context window
	triggerTokens := summarizationTrigger(contextWindow)
	summarizationMiddleware, err := summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model: chatModel,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: triggerTokens,
		},
		Callback: func(ctx context.Context, before adk.TypedChatModelAgentState[*schema.AgenticMessage], after adk.TypedChatModelAgentState[*schema.AgenticMessage]) error {
			beforeMap := make(map[*schema.AgenticMessage]bool)
			for _, msg := range before.Messages {
				beforeMap[msg] = true
			}

			var summaryMsg *schema.AgenticMessage
			for _, msg := range after.Messages {
				if !beforeMap[msg] {
					summaryMsg = msg
					break
				}
			}

			if summaryMsg == nil {
				return nil
			}

			afterMap := make(map[*schema.AgenticMessage]bool)
			for _, msg := range after.Messages {
				afterMap[msg] = true
			}

			var maxSeq int64
			for _, msg := range before.Messages {
				if !afterMap[msg] {
					if msg.Extra != nil {
						if seqVal, ok := msg.Extra["_onclaw_seq"].(int64); ok {
							if seqVal > maxSeq {
								maxSeq = seqVal
							}
						} else if seqValF, ok := msg.Extra["_onclaw_seq"].(float64); ok {
							seqVal := int64(seqValF)
							if seqVal > maxSeq {
								maxSeq = seqVal
							}
						}
					}
				}
			}

			redactedSummaryMsg := tools.RedactAgenticMessage(summaryMsg)
			if redactedSummaryMsg.Extra == nil {
				redactedSummaryMsg.Extra = make(map[string]interface{})
			}
			redactedSummaryMsg.Extra[persistedKey] = true

			summaryMsgJSON, err := json.Marshal(redactedSummaryMsg)
			if err != nil {
				return fmt.Errorf("marshal summary message: %w", err)
			}

			err = convStore.SaveSummary(ctx, conversationID, string(summaryMsgJSON), maxSeq)
			if err != nil {
				return fmt.Errorf("save summary: %w", err)
			}

			if summaryMsg.Extra == nil {
				summaryMsg.Extra = make(map[string]interface{})
			}
			summaryMsg.Extra[persistedKey] = true

			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create summarization middleware: %w", err)
	}

	historyMiddleware := middlewares.NewHistoryMiddleware(convStore, conversationID)

	maxIterations := agentConf.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 20 // Default to 20 cycles
	}

	// 4. Create TypedChatModelAgentConfig
	agentConfig := &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        agentConf.Name,
		Instruction: instruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               builtTools,
				ExecuteSequentially: true,
			},
		},
		MaxIterations: maxIterations,
		Handlers: []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{
			summarizationMiddleware,
			historyMiddleware,
		},
	}

	einoAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, agentConfig)
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

// Run executes a single turn of the agent and returns an EventIterator.
func (a *Agent) Run(ctx context.Context, userInput string) EventIterator {
	slog.Debug("agent_run",
		"agent_name", a.Config.Name,
		"workspace", a.Workspace,
		"system_prompt", a.Config.SystemPrompt,
	)

	slog.Debug("agent_user_input",
		"agent_name", a.Config.Name,
		"user_input", userInput,
		"input_length", len(userInput),
	)

	input := &adk.TypedAgentInput[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			schema.UserAgenticMessage(userInput),
		},
	}

	iterator := a.EinoAgent.Run(ctx, input)

	return &eventIterator{
		ctx:      ctx,
		iterator: iterator,
	}
}
