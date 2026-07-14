package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	_ "github.com/oniharnantyo/onclaw/internal/agent/tools/browser"
	_ "github.com/oniharnantyo/onclaw/internal/agent/tools/web"
	"github.com/oniharnantyo/onclaw/internal/hooks"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Agent wraps eino ADK ChatModelAgent and configuration context.
type Agent struct {
	EinoAgent        *adk.TypedChatModelAgent[*schema.AgenticMessage]
	Config           *store.Agent
	Workspace        string
	Dispatcher       *hooks.Dispatcher
	Session          *middlewares.SessionState
	sessionStartOnce sync.Once
	Tools            []tool.BaseTool
	// memoryMiddleware is non-nil when memory is enabled; used for EventStop flush.
	memoryMiddleware  *middlewares.MemoryMiddleware
	historyMiddleware *middlewares.HistoryMiddleware
	// Pruner periodically prunes expired episodic summaries.
	Pruner        *memory.PeriodicPruner
	contextWindow int
}

type inMemoryEnabledChecker struct {
	enabledMap map[string]bool
}

func (c *inMemoryEnabledChecker) Enabled(name string) bool {
	enabled, ok := c.enabledMap[name]
	if !ok {
		return true // Default to enabled
	}
	return enabled
}

// AssembleAgent constructs a ChatModelAgent with persona configuration, tools, and summarization middleware.
func AssembleAgent(
	ctx context.Context,
	agentConf *store.Agent,
	chatModel model.AgenticModel,
	reviewModel model.AgenticModel,
	workspace string,
	userConfigDir string,
	shellPolicy string,
	shellAllowlist []string,
	shellDenylist []string,
	contextWindow int,
	convStore store.ConversationStore,
	conversationID int64,
	mcpTools []tool.BaseTool,
	hookStore store.HookStore,
	execStore store.HookExecutionStore,
	channel string,
	toolRegistryStore store.ToolRegistryStore,
	toolGroupCfg tools.ToolGroupCfg,
	kvStore store.KVStore,
	resolver secrets.SecretResolver,
	memoryStore memory.MemoryStore,
	coreStore memory.CoreStore,
	embedder *memory.Embedder,
	stagedWriteStore memory.StagedWriteStore,
	episodicStore memory.EpisodicStore,
	dreamer *memory.Dreamer,
	kgStore memory.KGStore,
	charLimit int,
	episodicTTLDays int,
	db *sql.DB,
	kgTraversalDepth int,
) (*Agent, error) {
	// Load existing persona/memory files and AGENTS.md
	var memOver memory.AgentMemoryConfig
	if agentConf.MemoryConfig != "" {
		if err := json.Unmarshal([]byte(agentConf.MemoryConfig), &memOver); err != nil {
			slog.Warn("AssembleAgent: Failed to parse memory config", "agent", agentConf.Name, "error", err)
		}
	}

	resolvedMem := memOver.Resolve(
		memoryStore != nil,
		coreStore != nil,
		episodicStore != nil,
		kgStore != nil,
		"", "", // embedder already constructed
		true,  // security scan default ON
		true,  // extraction default ON
		true,  // retrieval default ON
		true,  // dreaming default ON
		false, // staged write approval handled by dreamer
	)

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

	var enabledChecker tools.EnabledChecker
	if toolRegistryStore != nil {
		list, err := toolRegistryStore.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tools for enabled checker: %w", err)
		}
		enabledMap := make(map[string]bool)
		for _, t := range list {
			enabledMap[t.Name] = t.Enabled == 1
		}
		enabledChecker = &inMemoryEnabledChecker{enabledMap: enabledMap}
	}

	// 2. Build tools
	builtTools := tools.Builtin(&tools.Scope{
		Workspace:        workspace,
		ShellPolicy:      shellPolicy,
		ShellAllowlist:   shellAllowlist,
		ShellDenylist:    shellDenylist,
		ToolGroupCfg:     toolGroupCfg,
		KVStore:          kvStore,
		SecretResolver:   resolver,
		AgentName:        agentConf.Name,
		Db:               db,
		MemoryStore:      memoryStore,
		Embedder:         embedder,
		StagedWriteStore: stagedWriteStore,
		CharLimit:        charLimit,
		KGStore:          kgStore,
		KGTraversalDepth: kgTraversalDepth,
	}, enabledChecker)
	builtTools = append(builtTools, mcpTools...)

	// Filter tools based on agent-specific memory configuration features
	var finalTools []tool.BaseTool
	for _, t := range builtTools {
		info, err := t.Info(ctx)
		if err != nil {
			finalTools = append(finalTools, t)
			continue
		}
		if info.Name == "memory_search" {
			if !resolvedMem.RetrievalEnabled || memoryStore == nil || !resolvedMem.CuratedEnabled {
				continue
			}
		}
		if info.Name == "session_search" {
			if !resolvedMem.RetrievalEnabled || episodicStore == nil || !resolvedMem.EpisodicEnabled {
				continue
			}
		}
		if info.Name == "kg_search" {
			if !resolvedMem.RetrievalEnabled || kgStore == nil || !resolvedMem.KGEnabled {
				continue
			}
		}
		finalTools = append(finalTools, t)
	}
	builtTools = finalTools

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

	// 2b. Filesystem middleware (Eino) injects ls/read_file/write_file/edit_file/
	// glob/grep/execute, backed by onclaw-controlled Backend/Shell. The toggle
	// middleware enforces the tool_registry enable flag on those tools.
	fsMiddleware, err := filesystem.NewTyped[*schema.AgenticMessage](ctx, &filesystem.MiddlewareConfig{
		Backend: tools.NewFSBackend(workspace),
		Shell:   tools.NewFSShell(workspace, shellPolicy, shellAllowlist, shellDenylist),
	})
	if err != nil {
		return nil, fmt.Errorf("create filesystem middleware: %w", err)
	}
	fsToggle := middlewares.NewFSToggleMiddleware(enabledChecker)
	// Converts expected filesystem failures (path blocked, not found,
	// ambiguous edit, invalid pattern) into recoverable observations so the
	// agent turn continues. Must run after fsToggle so disabled tools stay
	// disabled rather than being "recovered".
	fsError := middlewares.NewFSErrorMiddleware()

	// 3. Assemble summarization middleware
	// We trigger when total tokens exceed 80% of context window
	triggerTokens := summarizationTrigger(contextWindow)
	// lastCompactionSummary captures the most recent compaction summary text
	// so that EpisodicStore can reuse it instead of making a second LLM call.
	// memMW is declared here so the summarization callback can store the
	// compaction summary on it before the MemoryMiddleware is fully assembled.
	var memMW *middlewares.MemoryMiddleware
	summarizationMiddleware, err := summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model: chatModel,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: triggerTokens,
		},
		Callback: func(ctx context.Context, before adk.TypedChatModelAgentState[*schema.AgenticMessage], after adk.TypedChatModelAgentState[*schema.AgenticMessage]) error {
			summary, err := handleSummarization(ctx, handleSummarizationParams{
				Before:           before,
				After:            after,
				ChatModel:        chatModel,
				MemoryStore:      memoryStore,
				Embedder:         embedder,
				KVStore:          kvStore,
				AgentName:        agentConf.Name,
				ConversationID:   conversationID,
				ConvStore:        convStore,
				SkipSecurityScan: !resolvedMem.SecurityScanEnabled,
			})
			if err == nil && summary != "" && memMW != nil {
				memMW.CompactionSummary = summary
			}
			return err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create summarization middleware: %w", err)
	}

	historyMiddleware := middlewares.NewHistoryMiddleware(convStore, conversationID, agentConf.Model)

	var dispatcher *hooks.Dispatcher
	var sessionState *middlewares.SessionState
	var hooksMiddleware adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]

	if hookStore != nil && execStore != nil {
		dispatcher = hooks.NewDispatcher(hookStore, execStore)
		sessionID := strconv.FormatInt(conversationID, 10)
		sessionState = &middlewares.SessionState{
			Channel:   channel,
			SessionID: sessionID,
		}
		hooksMiddleware = middlewares.NewHooksMiddleware(dispatcher, agentConf.Name, sessionState)
	}

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
	}

	skillMiddleware, err := middlewares.BuildMiddleware(ctx, userConfigDir, agentConf.Name)
	if err != nil {
		return nil, fmt.Errorf("build skill middleware: %w", err)
	}

	handlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{
		summarizationMiddleware,
		historyMiddleware,
		fsMiddleware,
		fsToggle,
		fsError,
	}
	if memoryStore != nil {
		// Curated Core Memory toggle
		var activeCoreStore memory.CoreStore
		if coreStore != nil && resolvedMem.CuratedEnabled {
			activeCoreStore = coreStore
		}

		// Episodic memory toggle
		var activeEpisodicStore memory.EpisodicStore
		var activeDreamer *memory.Dreamer
		if episodicStore != nil && resolvedMem.EpisodicEnabled {
			activeEpisodicStore = episodicStore
			if resolvedMem.DreamingEnabled {
				activeDreamer = dreamer
			}
		}

		// KG memory toggle
		var activeKGStore memory.KGStore
		if kgStore != nil && resolvedMem.KGEnabled {
			activeKGStore = kgStore
		}

		memMW = middlewares.NewMemoryMiddleware(
			activeCoreStore,
			memoryStore,
			embedder,
			kvStore,
			chatModel,
			reviewModel,
			workspace,
			agentConf.Name,
			conversationID,
			charLimit,
			activeEpisodicStore,
			activeDreamer,
			episodicTTLDays,
			activeKGStore,
		)
		memMW.SkipSecurityScan = !resolvedMem.SecurityScanEnabled
		memMW.ExtractionEnabled = resolvedMem.ExtractionEnabled
		handlers = append(handlers, memMW)
	}
	if skillMiddleware != nil {
		handlers = append(handlers, skillMiddleware)
	}
	if hooksMiddleware != nil {
		handlers = append(handlers, hooksMiddleware)
	}
	agentConfig.Handlers = handlers

	einoAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, agentConfig)
	if err != nil {
		return nil, fmt.Errorf("create Eino ChatModelAgent: %w", err)
	}

	agent := &Agent{
		EinoAgent:         einoAgent,
		Config:            agentConf,
		Workspace:         workspace,
		Dispatcher:        dispatcher,
		Session:           sessionState,
		Tools:             builtTools,
		memoryMiddleware:  memMW,
		historyMiddleware: historyMiddleware,
		contextWindow:     contextWindow,
	}

	if episodicStore != nil {
		agent.Pruner = memory.NewPeriodicPruner(episodicStore, 1*time.Hour)
		agent.Pruner.Start(ctx)
	}

	return agent, nil
}

func summarizationTrigger(contextWindow int) int {
	return int(float64(contextWindow) * 0.8)
}

// Run executes a single turn of the agent and returns an EventIterator.
func (a *Agent) Run(ctx context.Context, userInput string, contentBlocks ...*schema.ContentBlock) EventIterator {
	a.sessionStartOnce.Do(func() {
		if a.Dispatcher != nil && a.Session != nil {
			_, _ = a.Dispatcher.Fire(ctx, hooks.EventSessionStart, hooks.Payload{
				Agent:     a.Config.Name,
				Channel:   a.Session.Channel,
				SessionID: a.Session.SessionID,
			})
		}
	})

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

	msg := schema.UserAgenticMessage(userInput)
	for _, cb := range contentBlocks {
		if cb != nil {
			msg.ContentBlocks = append(msg.ContentBlocks, cb)
		}
	}

	input := &adk.TypedAgentInput[*schema.AgenticMessage]{
		Messages: []*schema.AgenticMessage{
			msg,
		},
		EnableStreaming: middlewares.StreamingFromContext(ctx),
	}

	iterator := a.EinoAgent.Run(ctx, input)

	onTurnError := func(err error) {
		if a.Dispatcher != nil && a.Session != nil {
			_, _ = a.Dispatcher.Fire(ctx, hooks.EventStop, hooks.Payload{
				Agent:     a.Config.Name,
				Channel:   a.Session.Channel,
				SessionID: a.Session.SessionID,
				Error:     err.Error(),
			})
		}
	}
	// EventStop flush (D3 / task 4.4): fire once when the turn ends so memory is persisted.
	// Uses the most recent compaction summary (if any) to avoid a second LLM call.
	onStopFlush := func(msgs []*schema.AgenticMessage) {
		if a.memoryMiddleware != nil {
			a.memoryMiddleware.FlushMessages(ctx, msgs, a.memoryMiddleware.CompactionSummary)
		}
	}
	return &eventIterator{
		ctx:         ctx,
		iterator:    iterator,
		onTurnError: onTurnError,
		onStopFlush: onStopFlush,
	}
}

// LastTurnMeta retrieves metadata for the most recently committed turn.
func (a *Agent) LastTurnMeta() *store.TurnMeta {
	if a.historyMiddleware == nil {
		return nil
	}
	return a.historyMiddleware.LastTurnMeta()
}

// ContextWindow returns the resolved context window limit for the agent.
func (a *Agent) ContextWindow() int {
	return a.contextWindow
}

// AgentName returns the name of the agent.
func (a *Agent) AgentName() string {
	if a.Config != nil {
		return a.Config.Name
	}
	return ""
}

type handleSummarizationParams struct {
	Before           adk.TypedChatModelAgentState[*schema.AgenticMessage]
	After            adk.TypedChatModelAgentState[*schema.AgenticMessage]
	ChatModel        model.AgenticModel
	MemoryStore      memory.MemoryStore
	Embedder         *memory.Embedder
	KVStore          store.KVStore
	AgentName        string
	ConversationID   int64
	ConvStore        store.ConversationStore
	SkipSecurityScan bool
}

// handleSummarization saves the compaction summary message and returns the summary text
// for reuse in episodic summarization. Returns empty string when no compaction occurred.
func handleSummarization(ctx context.Context, p handleSummarizationParams) (string, error) {
	const persistedKey = "_onclaw_persisted"

	beforeMap := make(map[*schema.AgenticMessage]bool)
	for _, msg := range p.Before.Messages {
		beforeMap[msg] = true
	}

	var summaryMsg *schema.AgenticMessage
	for _, msg := range p.After.Messages {
		if !beforeMap[msg] {
			summaryMsg = msg
			break
		}
	}

	if summaryMsg == nil {
		return "", nil
	}

	// Extract the summary text from the compaction message for episodic reuse.
	var compactionSummary string
	for _, block := range summaryMsg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.AssistantGenText != nil && block.AssistantGenText.Text != "" {
			if compactionSummary != "" {
				compactionSummary += "\n"
			}
			compactionSummary += block.AssistantGenText.Text
		}
		if block.UserInputText != nil && block.UserInputText.Text != "" {
			if compactionSummary != "" {
				compactionSummary += "\n"
			}
			compactionSummary += block.UserInputText.Text
		}
	}

	afterMap := make(map[*schema.AgenticMessage]bool)
	for _, msg := range p.After.Messages {
		afterMap[msg] = true
	}

	var discardedMessages []*schema.AgenticMessage
	var maxSeq int64
	for _, msg := range p.Before.Messages {
		if !afterMap[msg] {
			discardedMessages = append(discardedMessages, msg)
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

	if len(discardedMessages) > 0 && p.MemoryStore != nil {
		_ = memory.ExtractAndFlush(ctx, p.ChatModel, p.MemoryStore, p.Embedder, p.KVStore, p.AgentName, p.ConversationID, discardedMessages, p.SkipSecurityScan)
	}

	redactedSummaryMsg := tools.RedactAgenticMessage(summaryMsg)
	if redactedSummaryMsg.Extra == nil {
		redactedSummaryMsg.Extra = make(map[string]interface{})
	}
	redactedSummaryMsg.Extra[persistedKey] = true

	summaryMsgJSON, err := json.Marshal(redactedSummaryMsg)
	if err != nil {
		return "", fmt.Errorf("marshal summary message: %w", err)
	}

	err = p.ConvStore.SaveSummary(ctx, p.ConversationID, string(summaryMsgJSON), maxSeq)
	if err != nil {
		return "", fmt.Errorf("save summary: %w", err)
	}

	if summaryMsg.Extra == nil {
		summaryMsg.Extra = make(map[string]interface{})
	}
	summaryMsg.Extra[persistedKey] = true

	return compactionSummary, nil
}

// ToolGroupCfgWrapper wraps a store.ToolGroupConfigStore to implement tools.ToolGroupCfg.
type ToolGroupCfgWrapper struct {
	Store store.ToolGroupConfigStore
}

// GetConfig reads the category configuration and returns it as a JSON string.
func (w *ToolGroupCfgWrapper) GetConfig(ctx context.Context, category string) (string, error) {
	if w.Store == nil {
		return "{}", nil
	}
	cfg, err := w.Store.GetConfig(ctx, category)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "{}", nil
		}
		return "", err
	}
	return cfg.Config, nil
}
