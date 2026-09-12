package agents

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/agents/hooks"
)

// estimateWindowTokens estimates a message window's size with the
// summarization middleware's default display estimator (~4 chars/token),
// counting renderable text, reasoning, and tool-call/-result payloads.
// Display-only (chat-compact-command D5): never billing or trigger math.
func estimateWindowTokens(msgs []*schema.AgenticMessage) int {
	var chars int
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			if block.AssistantGenText != nil {
				chars += len(block.AssistantGenText.Text)
			}
			if block.UserInputText != nil {
				chars += len(block.UserInputText.Text)
			}
			if block.Reasoning != nil {
				chars += len(block.Reasoning.Text)
			}
			if block.FunctionToolCall != nil {
				chars += len(block.FunctionToolCall.Name) + len(block.FunctionToolCall.Arguments)
			}
			if block.FunctionToolResult != nil {
				for _, cb := range block.FunctionToolResult.Content {
					if cb != nil && cb.Text != nil {
						chars += len(cb.Text.Text)
					}
				}
			}
		}
	}
	return chars / 4
}

// compactionState carries the display-only token estimates the summarization
// Callback captures at compaction time to the seam that emits the
// context_compacted transcript event. The callback runs on the run's graph
// goroutine strictly before the middleware forwards the window-replacement
// session event the drain loop observes, so a snapshot taken at that event is
// always the estimates of the compaction that produced it.
type compactionState struct {
	mu     sync.Mutex
	before int
	after  int
	set    bool
}

func (c *compactionState) record(before, after int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.before, c.after, c.set = before, after, true
}

func (c *compactionState) snapshot() (before, after int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.before, c.after, c.set
}

// usageCapturingModel wraps the turn's ChatModel so the compact turn can stamp
// the summarizer call's provider usage on its terminal event: the middleware
// consumes the raw response internally, and the finalizer's summary message
// carries no usage of its own.
type usageCapturingModel struct {
	inner Model

	mu    sync.Mutex
	usage *schema.TokenUsage
}

func (m *usageCapturingModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	msg, err := m.inner.Generate(ctx, input, opts...)
	if msg != nil && msg.ResponseMeta != nil && msg.ResponseMeta.TokenUsage != nil {
		m.mu.Lock()
		m.usage = msg.ResponseMeta.TokenUsage
		m.mu.Unlock()
	}
	return msg, err
}

func (m *usageCapturingModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return m.inner.Stream(ctx, input, opts...)
}

func (m *usageCapturingModel) capturedUsage() *schema.TokenUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

// loadSessionWindow reconstructs the session's current message window from
// the persisted log, mirroring the ADK's durable replay: the window restarts
// at the latest MessagesReplaced boundary and every message event after it
// appends in log order. Runtime-generated system messages are absent by
// construction — they are stripped before persistence and rebuilt per run.
func loadSessionWindow(ctx context.Context, sessionStore adk.SessionEventStore[*schema.AgenticMessage], sessionID string) ([]*schema.AgenticMessage, error) {
	result, err := sessionStore.LoadEvents(ctx, sessionID, &adk.LoadSessionEventsRequest{})
	if err != nil {
		return nil, fmt.Errorf("load session window: %w", err)
	}
	var window []*schema.AgenticMessage
	start := 0
	for i, ev := range result.Events {
		if ev != nil && ev.MessagesReplaced != nil {
			window = append([]*schema.AgenticMessage{}, *ev.MessagesReplaced...)
			start = i + 1
		}
	}
	for _, ev := range result.Events[start:] {
		if ev == nil || ev.Message == nil {
			continue
		}
		window = append(window, ev.Message)
	}
	return window, nil
}

// persistCompactionRecord appends the window-replacement record to the
// session store from the runner (chat-compact-command D4): the middleware's
// own emission requires a run-execution context the compact turn does not
// have, and the adapter's append path is runner-scoped by design. The record
// carries the token estimates in Extra so hydrated History fills the same
// CompactionPayload the live stream delivered.
func persistCompactionRecord(ctx context.Context, sessionStore adk.SessionEventStore[*schema.AgenticMessage], sessionID, turnID string, msgs []*schema.AgenticMessage, before, after int) error {
	ev := &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:          uuid.NewString(),
		TurnID:           turnID,
		Timestamp:        time.Now().UTC(),
		Kind:             adk.SessionEventMessagesReplaced,
		Extra:            map[string]any{sessionExtraKeyCompaction: compactionEstimates{TokensBefore: before, TokensAfter: after}},
		MessagesReplaced: &msgs,
	}
	if err := sessionStore.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{ev}); err != nil {
		return fmt.Errorf("persist compaction record: %w", err)
	}
	return nil
}

// runCompact enters a compact-command turn into the normal run pipeline
// (chat-compact-command D1): the run manager's active-run guard serializes it
// against running turns, and the events flow over the standard stream +
// broadcast fan-out. Hooks observe run_started here — user_prompt_submit is
// deliberately skipped, since the turn carries no user prompt to gate.
func (r *Runner) runCompact(
	ctx context.Context,
	req ExecRequest,
	ephemeral bool,
	cfg agentConfig,
	hookChain *hooks.Resolved,
	hookBase hooks.Event,
) (*EventStream, error) {
	if hookChain.HasHooks() {
		hookChain.ObserveRunStarted(ctx, hookBase)
		r.rememberHookChain(runKeyOf(req), hookChain)
	}

	// Ephemeral runs load nothing and persist nothing (the no-store adapter
	// reports empty history, so the turn degrades to the quiet no-op) — the
	// same store choice execute() makes.
	var sessionStore adk.SessionEventStore[*schema.AgenticMessage] = NewADKSessionAdapter(r.sessionEvents, r.checkpoints, req.WorkspaceID)
	if ephemeral {
		sessionStore = NewEphemeralSessionAdapter()
	}

	// The manager requires the ADK cancel pair per run; the compact turn
	// never runs the ADK machine, so the run option is discarded and explicit
	// cancel unwinds through the manager's context cancel, which handle.ctx
	// carries into the summarizer call.
	_, agentCancel := adk.WithCancel()
	handle, err := r.runMgr.start(RunKey{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
	}, agentCancel)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	// The compact turn is a persistent execution and indexes its session at
	// start like any other (agent-session-index D2) — but with an empty
	// title: the input is summarizer focus text that must never become the
	// session title (empty stores '' on birth, rewrites nothing on conflict).
	if !ephemeral {
		r.indexAgentSession(ctx, req, "")
	}

	stream := NewEventStream(128)
	go r.streamCompactTurn(handle, stream, req, sessionStore, ephemeral, cfg, hookChain, hookBase)
	return stream, nil
}

// streamCompactTurn executes a compact-command turn (chat-compact-command
// D3/D4): a standalone summarization instance rewrites the session's current
// message window, the runner persists the replacement record itself, and the
// transcript stream receives context_compacted (estimates filled) followed by
// turn_completed carrying the summarizer call's usage. A session with no
// compactable history completes quietly — no compaction event, zero tokens.
func (r *Runner) streamCompactTurn(
	handle *runHandle,
	stream *EventStream,
	req ExecRequest,
	sessionStore adk.SessionEventStore[*schema.AgenticMessage],
	ephemeral bool,
	cfg agentConfig,
	hookChain *hooks.Resolved,
	hookBase hooks.Event,
) {
	key := runKeyOf(req)
	defer handle.finish()
	// Same subscriber-close ordering as streamRun: ahead of finish() so every
	// attached live subscriber drains to EOF on all exit paths.
	defer r.runMgr.CloseSubscribers(key)
	defer handle.cancel()
	defer func() { _ = stream.Close() }()
	defer r.logTapDrops(stream, req)

	settle := func(status string) {
		r.forgetHookChain(key)
		if hookChain != nil && hookChain.HasHooks() {
			hookChain.RunFinished(handle.ctx, hookBase, status)
		}
	}
	emit := func(ev *TranscriptEvent) {
		stream.Send(ev)
		r.runMgr.Broadcast(key, ev)
	}
	turnID := uuid.NewString()
	failTurn := func(err error) {
		if errors.Is(err, context.Canceled) || errors.Is(handle.ctx.Err(), context.Canceled) {
			emit(&TranscriptEvent{
				Kind:         TranscriptEventCancelled,
				OccurredAt:   time.Now().UTC(),
				TurnID:       turnID,
				CancelReason: "execution cancelled",
			})
			settle(hookRunStatusCancelled)
			return
		}
		emit(&TranscriptEvent{
			Kind:       TranscriptEventError,
			OccurredAt: time.Now().UTC(),
			TurnID:     turnID,
			Error:      err.Error(),
		})
		settle(hookRunStatusFailed)
	}

	emit(&TranscriptEvent{
		Kind:       TranscriptEventTurnStarted,
		OccurredAt: time.Now().UTC(),
		TurnID:     turnID,
	})

	// The active-run guard serialized this turn against every other run on
	// the session (D1), so the persisted log IS the current window.
	window, err := loadSessionWindow(handle.ctx, sessionStore, req.SessionID)
	if err != nil {
		failTurn(err)
		return
	}
	if len(window) == 0 {
		// Quiet no-op: nothing compactable — a normal completed turn with no
		// compaction event and nothing spent.
		emit(&TranscriptEvent{
			Kind:       TranscriptEventTurnCompleted,
			OccurredAt: time.Now().UTC(),
			TurnID:     turnID,
		})
		settle(hookRunStatusCompleted)
		return
	}

	estimates := &compactionState{}
	summarizer := &usageCapturingModel{inner: cfg.Model}
	// Standalone instance per compact turn (D3): the turn's ChatModel, the
	// same transcript-offload callback Compose wires, and the focus text as
	// the summarizer's user instruction. Summarize bypasses the threshold
	// gate — a manual command always compacts.
	mwAny, err := summarization.NewTyped[*schema.AgenticMessage](handle.ctx, &summarization.TypedConfig[*schema.AgenticMessage]{
		Model:           summarizer,
		UserInstruction: req.Input,
		Callback:        newCompactionCallback(cfg.Filesystem.AgentDir, estimates.record),
	})
	if err != nil {
		failTurn(fmt.Errorf("build summarizer: %w", err))
		return
	}
		// NewTyped constructs exactly this concrete type; Summarize lives on it,
		// not on the middleware interface it is returned as.
		mw, ok := mwAny.(*summarization.TypedMiddleware[*schema.AgenticMessage])
		if !ok {
			failTurn(fmt.Errorf("build summarizer: unexpected middleware type %T", mwAny))
			return
		}

		window = CollapseStaleAttachmentBlocks(window)
		finalMsgs, err := mw.Summarize(handle.ctx, &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: window})
		if err != nil {
			failTurn(fmt.Errorf("summarize: %w", err))
			return
		}

	before, after, _ := estimates.snapshot()
	if !ephemeral {
		if err := persistCompactionRecord(handle.ctx, sessionStore, req.SessionID, turnID, finalMsgs, before, after); err != nil {
			failTurn(err)
			return
		}
	}

	emit(&TranscriptEvent{
		Kind:       TranscriptEventContextCompacted,
		OccurredAt: time.Now().UTC(),
		TurnID:     turnID,
		Compaction: &CompactionPayload{TokensBefore: before, TokensAfter: after},
	})

	var usage UsagePayload
	accumulateTokenUsage(&usage, summarizer.capturedUsage())
	emit(&TranscriptEvent{
		Kind:       TranscriptEventTurnCompleted,
		OccurredAt: time.Now().UTC(),
		TurnID:     turnID,
		Usage:      usageOf(usage),
	})
	settle(hookRunStatusCompleted)
}
