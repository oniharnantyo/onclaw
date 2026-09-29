package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// PendingApproval returns the session's pending shell approval, if any. An
// approval is pending when the latest persisted interrupt event is not
// followed by any later turn activity (the resumed turn persists new events,
// which is the resolution marker). Returns (nil, nil) when nothing is pending.
func (r *Runner) PendingApproval(ctx context.Context, workspaceID, sessionID string) (*ApprovalPayload, error) {
	rows, err := r.sessionEvents.LoadEvents(ctx, store.LoadSessionEventsParams{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("agent.PendingApproval: load events: %w", err)
	}

	var pending *ApprovalPayload
	for _, row := range rows {
		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := eventSerializer.Unmarshal(row.Payload, &se); err != nil {
			return nil, fmt.Errorf("agent.PendingApproval: unmarshal event %q: %w", row.EventID, err)
		}
		kind := se.Kind
		if kind == "" {
			kind = adk.SessionEventKind(row.Kind)
		}
		switch kind {
		case adk.SessionEventInterrupt:
			approval := &ApprovalPayload{}
			if se.Interrupt != nil && len(se.Interrupt.Contexts) > 0 {
				approval.InterruptID = se.Interrupt.Contexts[0].InterruptID
				if cmd, ok := se.Interrupt.Contexts[0].Info.(backend.ShellApprovalInfo); ok {
					approval.Command = cmd.Command
				}
				if ta, ok := se.Interrupt.Contexts[0].Info.(ToolApprovalInfo); ok {
					// Service-run write escalation (add-integration-authority
					// task 2.4): the hydrated view carries the same tool
					// object the live stream did.
					approval.Tool = &ApprovalToolPayload{
						Name:         ta.Name,
						Service:      ta.Service,
						ServiceName:  ta.ServiceName,
						ConnectionID: ta.ConnectionID,
						Tier:         ta.Tier,
					}
				}
			}
			pending = approval
		case adk.SessionEventMessage, adk.SessionEventCancel:
			// Turn activity after the interrupt: the approval was resolved.
			pending = nil
		}
	}
	return pending, nil
}

// HistoryRequest specifies query parameters for retrieving session transcript history.
type HistoryRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	After       string `json:"after"`
	Limit       int    `json:"limit"`
}

// HistoryResult contains the translated transcript events and next pagination cursor.
type HistoryResult struct {
	Events []TranscriptEvent `json:"events"`
	Next   string            `json:"next"`
}

// History loads persisted session events and translates them into domain TranscriptEvents.
func (r *Runner) History(ctx context.Context, req HistoryRequest) (*HistoryResult, error) {
	if req.WorkspaceID == "" || req.SessionID == "" {
		return nil, fmt.Errorf("%w: workspace_id and session_id are required", domain.ErrInvalid)
	}

	if req.AgentID != "" {
		if _, err := r.agents.ByID(ctx, req.WorkspaceID, req.AgentID); err != nil {
			return nil, err
		}
	}

	params := store.LoadSessionEventsParams{
		WorkspaceID:  req.WorkspaceID,
		SessionID:    req.SessionID,
		AfterEventID: req.After,
		Limit:        req.Limit,
	}

	rows, err := r.sessionEvents.LoadEvents(ctx, params)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return &HistoryResult{Events: []TranscriptEvent{}, Next: ""}, nil
		}
		return nil, fmt.Errorf("agent.History: load events: %w", err)
	}

	var next string
	if req.Limit > 0 && len(rows) >= req.Limit {
		next = rows[len(rows)-1].EventID
	}

	parsed, joins, err := parseSessionEvents(rows)
	if err != nil {
		return nil, err
	}

	events := make([]TranscriptEvent, 0, len(rows))

	// Usage accumulates per turn from the persisted model-span-end events and
	// surfaces on a turn_completed event emitted at each turn boundary, so a
	// reloaded transcript reports the same totals the live stream did.
	turnUsage := map[string]*UsagePayload{}
	// Turns that ended in an approval interrupt are not terminal — the resumed
	// turn continues them, so no turn_completed is emitted for them.
	interruptedTurns := map[string]bool{}
	prevTurn := ""
	prevTurnHadEvents := false
	var lastOccurredAt time.Time

	for _, pe := range parsed {
		se := pe.event
		kind := pe.kind

		occurredAt := pe.occurredAt
		lastOccurredAt = occurredAt

		turnID := pe.turnID

		// Turn boundary: close the previous turn with its usage before the
		// first event of the next turn.
		if prevTurn != "" && turnID != prevTurn && prevTurnHadEvents && !interruptedTurns[prevTurn] {
			events = append(events, TranscriptEvent{
				Kind:       TranscriptEventTurnCompleted,
				OccurredAt: occurredAt,
				TurnID:     prevTurn,
				Usage:      turnUsage[prevTurn],
			})
			prevTurnHadEvents = false
		}
		prevTurn = turnID

		// Accumulate provider usage from the persisted model-span-end events.
		if kind == adk.SessionEventSpanModelRequestEnd && se.Span != nil && se.Span.Model != nil && se.Span.Model.Usage != nil {
			mu := se.Span.Model.Usage
			u := turnUsage[turnID]
			if u == nil {
				u = &UsagePayload{}
				turnUsage[turnID] = u
			}
			u.InputTokens += mu.InputTokens
			u.OutputTokens += mu.OutputTokens
			if mu.Raw != nil {
				u.TotalTokens += mu.Raw.TotalTokens
			} else {
				u.TotalTokens += mu.InputTokens + mu.OutputTokens
			}
			// The last model span's input is what the context last held.
			u.FinalInputTokens = mu.InputTokens
		}

		id := pe.id

		before := len(events)
		switch kind {
		case adk.SessionEventMessage:
			if se.Message != nil {
				role := strings.ToLower(strings.TrimSpace(string(se.Message.Role)))
				if role == "user" || role == "assistant" {
					if role == "assistant" && se.Message.ResponseMeta != nil && se.Message.ResponseMeta.TokenUsage != nil {
						u := turnUsage[turnID]
						if u == nil {
							u = &UsagePayload{}
							turnUsage[turnID] = u
						}
						accumulateTokenUsage(u, se.Message.ResponseMeta.TokenUsage)
					}
					content := extractAgenticText(se.Message)
					reasoning := agenticReasoningText(se.Message)
					attachments := attachmentMetasOf(se.Message)
					// Tool-call requests and tool results persist as plain
					// assistant/user messages with no renderable text (the ADK
					// stores results under role user). Emitting them would mint
					// empty bubbles in the hydrated transcript — mirror the
					// live runner's content guard. The exception is an
					// attachment-only user message (attachments design D10):
					// no text, but its chips render, so attachments keep it.
					if strings.TrimSpace(content) == "" && strings.TrimSpace(reasoning) == "" && len(attachments) == 0 {
						break
					}
					events = append(events, TranscriptEvent{
						ID:         id,
						Kind:       TranscriptEventMessageCompleted,
						OccurredAt: occurredAt,
						TurnID:     turnID,
						Message: &CompletedMessage{
							Role:             role,
							Content:          content,
							ReasoningContent: reasoning,
							Attachments:      attachments,
						},
					})
				}
			}
		case adk.SessionEventSpanToolCallStart:
			callID := ""
			name := ""
			if se.Span != nil && se.Span.Tool != nil {
				callID = se.Span.Tool.ToolUseID
				name = se.Span.Tool.Name
			}
			arguments := joins.toolArguments(se.Span, callID)
			events = append(events, TranscriptEvent{
				ID:         id,
				Kind:       TranscriptEventToolCallStarted,
				OccurredAt: occurredAt,
				TurnID:     turnID,
				ToolCall: &ToolCallPayload{
					CallID:    callID,
					Name:      name,
					Arguments: arguments,
				},
			})
		case adk.SessionEventSpanToolCallEnd:
			callID := ""
			name := ""
			if se.Span != nil && se.Span.Tool != nil {
				callID = se.Span.Tool.ToolUseID
				name = se.Span.Tool.Name
			}
			result, isError := joins.toolResult(se.Span, callID)
			latency := joins.toolLatency(se.Span, occurredAt)
			events = append(events, TranscriptEvent{
				ID:         id,
				Kind:       TranscriptEventToolCallFinished,
				OccurredAt: occurredAt,
				TurnID:     turnID,
				ToolResult: &ToolResultPayload{
					CallID:  callID,
					Name:    name,
					Result:  result,
					Latency: latency,
					IsError: isError,
				},
			})
		case adk.SessionEventMessagesReplaced:
			// Estimates ride the record's Extra when the runner appended it
			// (chat-compact-command D4); events without them hydrate with an
			// empty payload, as before.
			payload := &CompactionPayload{}
			if se.Extra != nil {
				if est, ok := se.Extra[sessionExtraKeyCompaction].(compactionEstimates); ok {
					payload.TokensBefore = est.TokensBefore
					payload.TokensAfter = est.TokensAfter
				}
			}
			events = append(events, TranscriptEvent{
				ID:         id,
				Kind:       TranscriptEventContextCompacted,
				OccurredAt: occurredAt,
				TurnID:     turnID,
				Compaction: payload,
			})
		case adk.SessionEventCancel:
			reason := "cancelled"
			if se.Cancel != nil && se.Cancel.Reason != "" {
				reason = se.Cancel.Reason
			}
			events = append(events, TranscriptEvent{
				ID:           id,
				Kind:         TranscriptEventCancelled,
				OccurredAt:   occurredAt,
				TurnID:       turnID,
				CancelReason: reason,
			})
		case adk.SessionEventInterrupt:
			interruptedTurns[turnID] = true
			approval := &ApprovalPayload{}
			if se.Interrupt != nil && len(se.Interrupt.Contexts) > 0 {
				ic := se.Interrupt.Contexts[0]
				approval.InterruptID = ic.InterruptID
				if cmd, ok := ic.Info.(backend.ShellApprovalInfo); ok {
					approval.Command = cmd.Command
				}
			}
			events = append(events, TranscriptEvent{
				ID:         id,
				Kind:       TranscriptEventApprovalRequired,
				OccurredAt: occurredAt,
				TurnID:     turnID,
				Approval:   approval,
			})
		case sessionEventKindPromptBlocked:
			// Hook-blocked prompt (D6): render the notice exactly as the live
			// stream delivered it. The turn terminal is synthesized at the
			// turn boundary below, like every other turn's.
			if se.Extension != nil {
				if blocked, ok := se.Extension.Data.(promptBlockedEvent); ok {
					events = append(events, TranscriptEvent{
						ID:            id,
						Kind:          TranscriptEventPromptBlocked,
						OccurredAt:    occurredAt,
						TurnID:        turnID,
						PromptBlocked: &PromptBlockedPayload{Hook: blocked.Hook, Reason: blocked.Reason},
					})
				}
			}
		case memory.SessionEventKindMemoryIngested:
			// Post-turn memory chip (integrate-agent-zero-memory D11): render
			// the counts payload exactly as the live event carried it. The
			// payload type is the registered concrete struct, so the
			// serializer hands it back whole.
			if se.Extension != nil {
				if chip, ok := se.Extension.Data.(memory.MemoryIngestedPayload); ok {
					events = append(events, TranscriptEvent{
						ID:             id,
						Kind:           TranscriptEventMemoryIngested,
						OccurredAt:     occurredAt,
						TurnID:         turnID,
						MemoryIngested: &chip,
					})
				}
			}
		case sessionEventKindTaskCompleted:
			// Background task completion chip (add-agent-subagents-background
			// D8): render the terminal notice exactly as the live stream
			// delivered it. The payload type is the registered concrete
			// struct, so the serializer hands it back whole.
			if se.Extension != nil {
				if completed, ok := se.Extension.Data.(TaskCompletedPayload); ok {
					events = append(events, TranscriptEvent{
						ID:            id,
						Kind:          TranscriptEventTaskCompleted,
						OccurredAt:    occurredAt,
						TurnID:        turnID,
						TaskCompleted: &completed,
					})
				}
			}
		}
		// Accumulate: a trailing row that renders nothing (run status spans,
		// model-span bookkeeping) must not un-render the turn's earlier events.
		prevTurnHadEvents = prevTurnHadEvents || len(events) > before
	}

	// Close the final turn — but only at the end of pagination; a limited
	// page that ends mid-turn must not report a terminal event.
	if next == "" && prevTurn != "" && prevTurnHadEvents && !interruptedTurns[prevTurn] {
		events = append(events, TranscriptEvent{
			Kind:       TranscriptEventTurnCompleted,
			OccurredAt: lastOccurredAt,
			TurnID:     prevTurn,
			Usage:      turnUsage[prevTurn],
		})
	}

	if events == nil {
		events = []TranscriptEvent{}
	}

	return &HistoryResult{
		Events: events,
		Next:   next,
	}, nil
}

// persistedSessionEvent is a deserialized session event row with its identity
// fields resolved once, before the History projection loop.
type persistedSessionEvent struct {
	id         string
	kind       adk.SessionEventKind
	occurredAt time.Time
	turnID     string
	event      adk.SessionEvent[*schema.AgenticMessage]
}

// historyJoins indexes persisted messages and tool-span timestamps so hydrated
// transcripts carry the same fidelity the live stream delivered: tool arguments
// joined to the assistant message that requested them, results joined to the
// tool-result message, and latency from the persisted span pair (design D4).
type historyJoins struct {
	assistantMsgByID   map[string]*schema.AgenticMessage
	toolResultMsgByID  map[string]*schema.AgenticMessage
	toolCallByCallID   map[string]*ToolCallPayload
	toolResultByCallID map[string]*ToolResultPayload
	toolStartAt        map[string]time.Time // keyed by ToolUseID and start-span event id
}

// parseSessionEvents deserializes every persisted row once and builds the
// hydration join index. It replaces the per-row unmarshal the projection loop
// used to perform, so the join adds no extra pass over the payloads.
func parseSessionEvents(rows []domain.SessionEvent) ([]persistedSessionEvent, *historyJoins, error) {
	parsed := make([]persistedSessionEvent, 0, len(rows))
	joins := &historyJoins{
		assistantMsgByID:   map[string]*schema.AgenticMessage{},
		toolResultMsgByID:  map[string]*schema.AgenticMessage{},
		toolCallByCallID:   map[string]*ToolCallPayload{},
		toolResultByCallID: map[string]*ToolResultPayload{},
		toolStartAt:        map[string]time.Time{},
	}
	for _, row := range rows {
		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := eventSerializer.Unmarshal(row.Payload, &se); err != nil {
			return nil, nil, fmt.Errorf("agent.History: unmarshal event %q: %w", row.EventID, err)
		}
		_ = adk.NormalizeSessionEventKind(&se)

		kind := se.Kind
		if kind == "" {
			kind = adk.SessionEventKind(row.Kind)
		}
		occurredAt := se.Timestamp
		if occurredAt.IsZero() {
			occurredAt = row.OccurredAt
		}
		turnID := se.TurnID
		if turnID == "" {
			turnID = row.TurnID
		}
		id := row.EventID
		if id == "" {
			id = se.EventID
		}
		parsed = append(parsed, persistedSessionEvent{
			id:         id,
			kind:       kind,
			occurredAt: occurredAt,
			turnID:     turnID,
			event:      se,
		})

		switch kind {
		case adk.SessionEventMessage:
			if se.Message == nil {
				continue
			}
			if role := strings.ToLower(strings.TrimSpace(string(se.Message.Role))); role == "assistant" {
				joins.assistantMsgByID[id] = se.Message
				for _, tc := range agenticToolCalls(se.Message) {
					toolCall := tc
					joins.toolCallByCallID[toolCall.CallID] = &toolCall
				}
			}
			results := agenticToolResults(se.Message)
			if len(results) > 0 {
				joins.toolResultMsgByID[id] = se.Message
				for _, tr := range results {
					toolResult := tr
					joins.toolResultByCallID[toolResult.CallID] = &toolResult
				}
			}
		case adk.SessionEventSpanToolCallStart:
			if se.Span == nil || se.Span.Tool == nil || se.Span.Tool.ToolUseID == "" {
				continue
			}
			startAt := se.Span.StartedAt
			if startAt.IsZero() {
				startAt = occurredAt
			}
			joins.toolStartAt[se.Span.Tool.ToolUseID] = startAt
			if id != "" {
				joins.toolStartAt[id] = startAt
			}
		}
	}
	return parsed, joins, nil
}

// toolMeta returns a span's tool metadata, or nil when the span carries none.
func (j *historyJoins) toolMeta(span *adk.SpanEvent) *adk.ToolSpanMeta {
	if span == nil {
		return nil
	}
	return span.Tool
}

// toolArguments resolves a started tool call's arguments: joined via the span's
// AssistantMessageEventID to the assistant message whose FunctionToolCall
// blocks carry call id + arguments, falling back to call-id matching across
// all persisted assistant messages.
func (j *historyJoins) toolArguments(span *adk.SpanEvent, callID string) string {
	if meta := j.toolMeta(span); meta != nil && meta.AssistantMessageEventID != "" {
		if msg := j.assistantMsgByID[meta.AssistantMessageEventID]; msg != nil {
			for _, tc := range agenticToolCalls(msg) {
				if tc.CallID == callID {
					return tc.Arguments
				}
			}
		}
	}
	if tc := j.toolCallByCallID[callID]; tc != nil {
		return tc.Arguments
	}
	return ""
}

// toolResult resolves a finished tool call's result text and error flag:
// joined via the span's ToolResultMessageEventID to the persisted tool-result
// message, falling back to call-id matching. A span that ended in error
// without producing a result message reports the span error as its result.
func (j *historyJoins) toolResult(span *adk.SpanEvent, callID string) (result string, isError bool) {
	if span != nil && (span.Status == "error" || span.Err != "") {
		isError = true
		result = span.Err
	}
	if meta := j.toolMeta(span); meta != nil && meta.ToolResultMessageEventID != "" {
		if msg := j.toolResultMsgByID[meta.ToolResultMessageEventID]; msg != nil {
			for _, tr := range agenticToolResults(msg) {
				if tr.CallID == callID {
					return tr.Result, isError
				}
			}
		}
	}
	if tr := j.toolResultByCallID[callID]; tr != nil {
		return tr.Result, isError
	}
	return result, isError
}

// toolLatency derives a finished tool call's latency from the persisted span
// pair's timestamps: the end span's own StartedAt snapshot, falling back to
// the start span located via ToolCallStartEventID or ToolUseID.
func (j *historyJoins) toolLatency(span *adk.SpanEvent, endedAt time.Time) time.Duration {
	if span == nil {
		return 0
	}
	end := span.EndedAt
	if end.IsZero() {
		end = endedAt
	}
	if end.IsZero() {
		return 0
	}
	start := span.StartedAt
	if start.IsZero() {
		key := ""
		if meta := j.toolMeta(span); meta != nil {
			if meta.ToolCallStartEventID != "" {
				key = meta.ToolCallStartEventID
			} else {
				key = meta.ToolUseID
			}
		}
		start = j.toolStartAt[key]
	}
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}
