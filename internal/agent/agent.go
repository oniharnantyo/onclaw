package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
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
	"github.com/oniharnantyo/onclaw/internal/membus"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/tokens"
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
	memoryMiddleware *middlewares.MemoryMiddleware
	// Bus is the in-process memory event bus for async background processing.
	Bus *membus.Bus
	// Pruner periodically prunes expired episodic summaries (legacy; replaced by bus PrunerWorker).
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

// AssembleAgentOpts contains configuration parameters for assembling an Agent.
type AssembleAgentOpts struct {
	AgentConf         *store.Agent
	ChatModel         model.AgenticModel
	ReviewModel       model.AgenticModel
	Workspace         string
	UserConfigDir     string
	ShellPolicy       string
	ShellAllowlist    []string
	ShellDenylist     []string
	ContextWindow     int
	ConvStore         store.ConversationStore
	ConversationID    int64
	McpTools          []tool.BaseTool
	HookStore         store.HookStore
	ExecStore         store.HookExecutionStore
	Channel           string
	ToolRegistryStore store.ToolRegistryStore
	ToolGroupCfg      tools.ToolGroupCfg
	KVStore           store.KVStore
	Resolver          secrets.SecretResolver
	MemoryStore       memory.MemoryStore
	CoreStore         memory.CoreStore
	Embedder          *memory.Embedder
	StagedWriteStore  memory.StagedWriteStore
	EpisodicStore     memory.EpisodicStore
	Dreamer           *memory.Dreamer
	KGStore           memory.KGStore
	CharLimit         int
	EpisodicTTLDays   int
	DB                *sql.DB
	KGTraversalDepth  int
	SummarizationOpts SummarizationOpts
}

type SummarizationOpts struct {
	TriggerContextTokens   int
	TriggerContextMessages int
	TranscriptFilePath     string
	PreviousMessagesKeep   int
}

type agentBuilder struct {
	opts             AssembleAgentOpts
	instruction      string
	resolvedMemory   *memory.ResolvedMemoryConfig
	tools            []tool.BaseTool
	enabledChecker   tools.EnabledChecker
	memoryMiddleware *middlewares.MemoryMiddleware
	dispatcher       *hooks.Dispatcher
	sessionState     *middlewares.SessionState
	handlers         []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
}

// applySummarizationDefaults applies default values to SummarizationOpts if not set.
func applySummarizationDefaults(opts AssembleAgentOpts) AssembleAgentOpts {
	if opts.SummarizationOpts.TriggerContextTokens == 0 {
		opts.SummarizationOpts.TriggerContextTokens = 64000
	}
	if opts.SummarizationOpts.TriggerContextMessages == 0 {
		opts.SummarizationOpts.TriggerContextMessages = 100
	}
	if opts.SummarizationOpts.PreviousMessagesKeep == 0 {
		opts.SummarizationOpts.PreviousMessagesKeep = 4
	}
	// TranscriptFilePath will be set in buildMiddleware if not provided
	return opts
}

// AssembleAgent constructs a ChatModelAgent with persona configuration, tools, and summarization middleware.
func AssembleAgent(ctx context.Context, opts AssembleAgentOpts) (*Agent, error) {
	opts = applySummarizationDefaults(opts)
	b := &agentBuilder{opts: opts}
	if err := b.resolveConfig(); err != nil {
		return nil, err
	}
	if err := b.buildPrompt(ctx); err != nil {
		return nil, err
	}
	if err := b.buildTools(ctx); err != nil {
		return nil, err
	}
	if err := b.buildMiddleware(ctx); err != nil {
		return nil, err
	}
	return b.assemble(ctx)
}

func (b *agentBuilder) resolveConfig() error {
	var memOver memory.AgentMemoryConfig
	if b.opts.AgentConf.MemoryConfig != "" {
		if err := json.Unmarshal([]byte(b.opts.AgentConf.MemoryConfig), &memOver); err != nil {
			slog.Warn("AssembleAgent: Failed to parse memory config", "agent", b.opts.AgentConf.Name, "error", err)
		}
	}

	b.resolvedMemory = memOver.Resolve(
		b.opts.MemoryStore != nil,
		b.opts.CoreStore != nil,
		b.opts.EpisodicStore != nil,
		b.opts.KGStore != nil,
		"", "",
		true,
		true,
		true,
		true,
		true,
	)
	return nil
}

func (b *agentBuilder) buildPrompt(ctx context.Context) error {
	persona, err := LoadPersonaContext(ctx, b.opts.Workspace, b.opts.UserConfigDir)
	if err != nil {
		return fmt.Errorf("load persona context: %w", err)
	}

	var promptParts []string
	if persona != "" {
		promptParts = append(promptParts, persona)
	}

	grounding := fmt.Sprintf("Your active workspace directory is: %s", b.opts.Workspace)
	promptParts = append(promptParts, grounding)

	promptParts = append(promptParts, "You can execute commands in this workspace using your tools.")

	b.instruction = strings.Join(promptParts, "\n\n")
	return nil
}

func (b *agentBuilder) buildTools(ctx context.Context) error {
	if b.opts.ToolRegistryStore != nil {
		list, err := b.opts.ToolRegistryStore.ListTools(ctx)
		if err != nil {
			return fmt.Errorf("list tools for enabled checker: %w", err)
		}
		enabledMap := make(map[string]bool)
		for _, t := range list {
			enabledMap[t.Name] = t.Enabled == 1
		}
		b.enabledChecker = &inMemoryEnabledChecker{enabledMap: enabledMap}
	}

	builtTools := tools.Builtin(&tools.Scope{
		Workspace:        b.opts.Workspace,
		ShellPolicy:      b.opts.ShellPolicy,
		ShellAllowlist:   b.opts.ShellAllowlist,
		ShellDenylist:    b.opts.ShellDenylist,
		ToolGroupCfg:     b.opts.ToolGroupCfg,
		KVStore:          b.opts.KVStore,
		SecretResolver:   b.opts.Resolver,
		AgentName:        b.opts.AgentConf.Name,
		SessionID:        strconv.FormatInt(b.opts.ConversationID, 10),
		Db:               b.opts.DB,
		MemoryStore:      b.opts.MemoryStore,
		Embedder:         b.opts.Embedder,
		StagedWriteStore: b.opts.StagedWriteStore,
		CharLimit:        b.opts.CharLimit,
		KGStore:          b.opts.KGStore,
		KGTraversalDepth: b.opts.KGTraversalDepth,
	}, b.enabledChecker)
	builtTools = append(builtTools, b.opts.McpTools...)

	var finalTools []tool.BaseTool
	for _, t := range builtTools {
		_, err := t.Info(ctx)
		if err != nil {
			finalTools = append(finalTools, t)
			continue
		}
		finalTools = append(finalTools, t)
	}
	builtTools = finalTools

	// Filter tools if a denylist is configured on the agent
	if b.opts.AgentConf.DisabledTools != "" {
		disabledTools := make(map[string]bool)
		for _, t := range strings.Split(b.opts.AgentConf.DisabledTools, ",") {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				disabledTools[trimmed] = true
			}
		}
		var filteredTools []tool.BaseTool
		for _, t := range builtTools {
			info, err := t.Info(ctx)
			if err != nil {
				continue
			}
			if !disabledTools[info.Name] {
				filteredTools = append(filteredTools, t)
			}
		}
		builtTools = filteredTools
	}
	b.tools = builtTools

	// Input-safety floor guard: fail fast if the fixed input floor would consume too much context window.
	floorToolInfos := make([]*schema.ToolInfo, 0, len(b.tools))
	for _, t := range b.tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		floorToolInfos = append(floorToolInfos, info)
	}
	floor, err := estimateFloorTokens(ctx, b.instruction, floorToolInfos)
	if err != nil {
		return fmt.Errorf("input floor estimate: %w", err)
	}
	if floor >= middlewares.FloorSafetyLimit(b.opts.ContextWindow) {
		return fmt.Errorf("input floor %d tokens exceeds safety limit %d tokens for context window %d: %w",
			floor, middlewares.FloorSafetyLimit(b.opts.ContextWindow), b.opts.ContextWindow, middlewares.ErrInputFloorExceedsSafetyLimit)
	}

	return nil
}

func (b *agentBuilder) buildMemoryMiddleware(ctx context.Context) error {
	if b.opts.MemoryStore == nil {
		return nil
	}

	// Curated Core Memory toggle
	var activeCoreStore memory.CoreStore
	if b.opts.CoreStore != nil && b.resolvedMemory.CuratedEnabled {
		activeCoreStore = b.opts.CoreStore
	}

	// Episodic memory toggle
	var activeEpisodicStore memory.EpisodicStore
	var activeDreamer *memory.Dreamer
	if b.opts.EpisodicStore != nil && b.resolvedMemory.EpisodicEnabled {
		activeEpisodicStore = b.opts.EpisodicStore
		if b.resolvedMemory.DreamingEnabled {
			activeDreamer = b.opts.Dreamer
		}
	}

	// KG memory toggle
	var activeKGStore memory.KGStore
	if b.opts.KGStore != nil && b.resolvedMemory.KGEnabled {
		activeKGStore = b.opts.KGStore
	}

	b.memoryMiddleware = middlewares.NewMemoryMiddleware(
		activeCoreStore,
		b.opts.MemoryStore,
		b.opts.Embedder,
		b.opts.KVStore,
		b.opts.ChatModel,
		b.opts.ReviewModel,
		b.opts.Workspace,
		b.opts.AgentConf.Name,
		b.opts.ConversationID,
		b.opts.CharLimit,
		activeEpisodicStore,
		activeDreamer,
		b.opts.EpisodicTTLDays,
		activeKGStore,
	)
	b.memoryMiddleware.SkipSecurityScan = !b.resolvedMemory.SecurityScanEnabled
	b.memoryMiddleware.ExtractionEnabled = b.resolvedMemory.ExtractionEnabled
	b.memoryMiddleware.RetrievalEnabled = b.resolvedMemory.RetrievalEnabled
	return nil
}

func (b *agentBuilder) buildMiddleware(ctx context.Context) error {
	fsMiddleware, err := filesystem.NewTyped[*schema.AgenticMessage](ctx, &filesystem.MiddlewareConfig{
		Backend: tools.NewFSBackend(b.opts.Workspace),
		Shell:   tools.NewFSShell(b.opts.Workspace, b.opts.ShellPolicy, b.opts.ShellAllowlist, b.opts.ShellDenylist),
	})
	if err != nil {
		return fmt.Errorf("create filesystem middleware: %w", err)
	}
	fsToggle := middlewares.NewFSToggleMiddleware(b.enabledChecker)
	fsError := middlewares.NewFSErrorMiddleware()

	if err := b.buildMemoryMiddleware(ctx); err != nil {
		return err
	}

	// Set default transcript file path if not provided
	transcriptPath := b.opts.SummarizationOpts.TranscriptFilePath
	if transcriptPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get user home dir: %w", err)
		}
		transcriptPath = buildTranscriptPath(homeDir, b.opts.AgentConf.Name)
	}

	slog.Info("summarization config",
		"agent", b.opts.AgentConf.Name,
		"context_window", b.opts.ContextWindow,
		"trigger_context_tokens", b.opts.SummarizationOpts.TriggerContextTokens,
		"trigger_context_messages", b.opts.SummarizationOpts.TriggerContextMessages,
		"previous_messages_keep", b.opts.SummarizationOpts.PreviousMessagesKeep,
		"transcript_path", transcriptPath,
	)

	summarizationMiddleware, err := summarization.NewTyped[*schema.AgenticMessage](ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model:              b.opts.ChatModel,
		EmitInternalEvents: true,
		ReusePromptCaching: true,
		TokenCounter: func(ctx context.Context, input *summarization.TypedTokenCounterInput[*schema.AgenticMessage]) (int, error) {
			var count int
			fromUsage, estimated := 0, 0
			for _, msg := range input.Messages {
				if msg.ResponseMeta != nil && msg.ResponseMeta.TokenUsage != nil && msg.ResponseMeta.TokenUsage.TotalTokens != 0 {
					count = msg.ResponseMeta.TokenUsage.TotalTokens
					fromUsage++
				} else {
					count += tokens.EstimateMessage(msg)
					estimated++
				}
			}
			slog.Info("summarization token counter",
				"agent", b.opts.AgentConf.Name,
				"messages", len(input.Messages),
				"counted_from_usage", fromUsage,
				"estimated", estimated,
				"total_tokens", count,
				"trigger_tokens", b.opts.SummarizationOpts.TriggerContextTokens,
				"trigger_messages", b.opts.SummarizationOpts.TriggerContextMessages,
			)
			return count, nil
		},
		Trigger: &summarization.TriggerCondition{
			ContextTokens:   b.opts.SummarizationOpts.TriggerContextTokens,
			ContextMessages: b.opts.SummarizationOpts.TriggerContextMessages,
		},
		TranscriptFilePath: transcriptPath,
		Finalize: func(ctx context.Context, originalMessages []*schema.AgenticMessage, summary *schema.AgenticMessage) ([]*schema.AgenticMessage, error) {
			// Wire CompactionSummary so episodic summarization can reuse it.
			if b.memoryMiddleware != nil && summary != nil {
				text := getAgenticSummaryText(summary)
				if text != "" {
					b.memoryMiddleware.CompactionSummary = text
				}
			}
			return reconstructMessages(ctx, originalMessages, summary, b.opts.SummarizationOpts.PreviousMessagesKeep)
		},
	})
	if err != nil {
		return fmt.Errorf("create summarization middleware: %w", err)
	}

	var hooksMiddleware adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]
	if b.opts.HookStore != nil && b.opts.ExecStore != nil {
		b.dispatcher = hooks.NewDispatcher(b.opts.HookStore, b.opts.ExecStore)
		sessionID := strconv.FormatInt(b.opts.ConversationID, 10)
		b.sessionState = &middlewares.SessionState{
			Channel:   b.opts.Channel,
			SessionID: sessionID,
		}
		hooksMiddleware = middlewares.NewHooksMiddleware(b.dispatcher, b.opts.AgentConf.Name, b.sessionState)
	}

	skillMiddleware, err := middlewares.BuildMiddleware(ctx, b.opts.UserConfigDir, b.opts.AgentConf.Name)
	if err != nil {
		return fmt.Errorf("build skill middleware: %w", err)
	}

	inputSafetyMiddleware := middlewares.NewInputSafetyMiddleware(tokens.Estimate(len(b.instruction)), b.opts.ContextWindow)

	handlers := []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{
		inputSafetyMiddleware,
		summarizationMiddleware,
		fsMiddleware,
		fsToggle,
		fsError,
	}

	if b.memoryMiddleware != nil {
		handlers = append(handlers, b.memoryMiddleware)
	}
	if skillMiddleware != nil {
		handlers = append(handlers, skillMiddleware)
	}
	if hooksMiddleware != nil {
		handlers = append(handlers, hooksMiddleware)
	}

	b.handlers = handlers
	return nil
}

func (b *agentBuilder) assemble(ctx context.Context) (*Agent, error) {
	maxIterations := b.opts.AgentConf.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 20
	}

	agentConfig := &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        b.opts.AgentConf.Name,
		Description: b.opts.AgentConf.Description,
		Instruction: b.instruction,
		Model:       b.opts.ChatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools:               b.tools,
				ExecuteSequentially: true,
			},
		},
		MaxIterations: maxIterations,
		Handlers:      b.handlers,
	}

	einoAgent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, agentConfig)
	if err != nil {
		return nil, fmt.Errorf("create Eino ChatModelAgent: %w", err)
	}

	agent := &Agent{
		EinoAgent:        einoAgent,
		Config:           b.opts.AgentConf,
		Workspace:        b.opts.Workspace,
		Dispatcher:       b.dispatcher,
		Session:          b.sessionState,
		Tools:            b.tools,
		memoryMiddleware: b.memoryMiddleware,
		contextWindow:    b.opts.ContextWindow,
	}

	// Wire memory bus: register workers and start the bus.
	if b.memoryMiddleware != nil {
		bus := membus.New(membus.DefaultBufferSize)

		// KG extraction worker
		if b.opts.KGStore != nil {
			bus.Register(&membus.KGExtractionWorker{
				KGStore:          b.opts.KGStore,
				ChatModel:        b.opts.ChatModel,
				ReviewModel:      b.opts.ReviewModel,
				AgentName:        b.opts.AgentConf.Name,
				SkipSecurityScan: !b.resolvedMemory.SecurityScanEnabled,
			})
		}

		// Dreamer worker
		if b.opts.Dreamer != nil && b.resolvedMemory.DreamingEnabled {
			bus.Register(&membus.DreamerWorker{
				Dreamer: b.opts.Dreamer,
			})
		}

		// Pruner worker (replaces standalone PeriodicPruner goroutine)
		if b.opts.EpisodicStore != nil {
			bus.Register(&membus.PrunerWorker{
				EpisodicStore: b.opts.EpisodicStore,
			})
			// Timer fires prune_tick events every hour.
			timer := membus.NewTimerWorker(bus, 1*time.Hour, membus.PruneTick{})
			timer.Start(ctx)
		}

		bus.Start(ctx)
		agent.Bus = bus
		b.memoryMiddleware.Bus = bus
	}

	return agent, nil
}

// Run executes a single turn of the agent given an assembled message list and returns an EventIterator.
func (a *Agent) Run(ctx context.Context, messages []*schema.AgenticMessage) EventIterator {
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
		"description", a.Config.Description,
		"message_count", len(messages),
	)

	input := &adk.TypedAgentInput[*schema.AgenticMessage]{
		Messages:        messages,
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

// ContextWindow returns the resolved context window limit for the agent.
func (a *Agent) ContextWindow() int {
	return a.contextWindow
}

// Stop performs graceful shutdown of background resources (memory bus, pruner).
// It drains pending events before returning. Safe to call multiple times.
func (a *Agent) Stop() {
	if a.Bus != nil {
		a.Bus.Stop()
	}
	if a.Pruner != nil {
		a.Pruner.Stop()
	}
}

// AgentName returns the name of the agent.
func (a *Agent) AgentName() string {
	if a.Config != nil {
		return a.Config.Name
	}
	return ""
}

// reconstructMessages rebuilds the message list after summarization by keeping
// the system message, the last N messages based on PreviousMessagesKeep, and
// appending the summary. This ensures the context window contains the most
// relevant recent history plus the condensed earlier context.
func reconstructMessages(_ context.Context, originalMessages []*schema.AgenticMessage, summary *schema.AgenticMessage, previousMessagesKeep int) ([]*schema.AgenticMessage, error) {
	// Default to keeping 2 previous messages if not specified
	if previousMessagesKeep <= 0 {
		previousMessagesKeep = 2
	}

	// Separate system messages from regular messages
	var systemMessages []*schema.AgenticMessage
	var regularMessages []*schema.AgenticMessage

	for _, msg := range originalMessages {
		if msg == nil {
			continue
		}
		if msg.Role == schema.AgenticRoleTypeSystem {
			systemMessages = append(systemMessages, msg)
		} else {
			regularMessages = append(regularMessages, msg)
		}
	}

	// Start building the result with system messages
	var result []*schema.AgenticMessage
	result = append(result, systemMessages...)

	// Keep the last N regular messages (most recent history)
	keepCount := previousMessagesKeep
	if len(regularMessages) < keepCount {
		keepCount = len(regularMessages)
	}

	if keepCount > 0 {
		startIdx := len(regularMessages) - keepCount
		result = append(result, regularMessages[startIdx:]...)
	}

	// Append the summary message
	if summary != nil {
		result = append(result, summary)
	}

	slog.Debug("reconstruct_messages",
		"system_messages", len(systemMessages),
		"regular_messages", len(regularMessages),
		"kept_messages", keepCount,
		"final_count", len(result),
	)

	return result, nil
}

func buildTranscriptPath(homeDir, agentName string) string {
	return fmt.Sprintf("%s/.onclaw/workspace/agents/%s/summary_transcript", homeDir, agentName)
}

// getAgenticSummaryText extracts the text content from an AgenticMessage (used for compaction summary).
func getAgenticSummaryText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.UserInputText != nil {
			sb.WriteString(block.UserInputText.Text)
		} else if block.AssistantGenText != nil {
			sb.WriteString(block.AssistantGenText.Text)
		}
	}
	return sb.String()
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
