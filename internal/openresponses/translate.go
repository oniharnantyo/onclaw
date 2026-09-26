package openresponses

import (
	"encoding/json"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
)

// Response statuses.
const (
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusIncomplete = "incomplete"
)

// Usage is the token usage block of a Response.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	FinalInputTokens int `json:"final_input_tokens,omitempty"`
	// ContextBreakdown is the optional display-only segment split of the
	// final call's input (usage.context_breakdown, adopt-assistant-ui-
	// elements D7). Additive and omitempty — clients reading only the legacy
	// fields behave exactly as before — and never present without a usage
	// block: it rides this struct, and the whole usage block is omitted when
	// the provider reported nothing.
	ContextBreakdown *ContextBreakdown `json:"context_breakdown,omitempty"`
}

// ContextBreakdown is the wire form of the display-only per-segment split of
// the final call's input. Every segment is omitempty: a segment the backend
// could not measure is absent, never zero.
type ContextBreakdown struct {
	Instructions int `json:"instructions,omitempty"`
	Tools        int `json:"tools,omitempty"`
	Conversation int `json:"conversation,omitempty"`
	Files        int `json:"files,omitempty"`
	Server       int `json:"server,omitempty"`
}

// UsageFromDomain converts runner usage into the wire usage block.
func UsageFromDomain(u *agents.UsagePayload) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		TotalTokens:      u.TotalTokens,
		FinalInputTokens: u.FinalInputTokens,
		ContextBreakdown: ContextBreakdownFromDomain(u.ContextBreakdown),
	}
}

// ContextBreakdownFromDomain converts the runner's breakdown into the wire
// form. A nil domain breakdown (turns without provider usage or with nothing
// measurable) stays nil, so a breakdown never appears without a usage block.
func ContextBreakdownFromDomain(cb *agents.ContextBreakdown) *ContextBreakdown {
	if cb == nil {
		return nil
	}
	return &ContextBreakdown{
		Instructions: cb.Instructions,
		Tools:        cb.Tools,
		Conversation: cb.Conversation,
		Files:        cb.Files,
		Server:       cb.Server,
	}
}

// ResponseError is the error object embedded in a failed Response.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Response is the OpenResponses Response object.
type Response struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	CreatedAt         int64             `json:"created_at"`
	Status            string            `json:"status"`
	Model             string            `json:"model"`
	Output            []map[string]any  `json:"output"`
	Usage             *Usage            `json:"usage,omitempty"`
	Metadata          map[string]string `json:"metadata"`
	Error             *ResponseError    `json:"error,omitempty"`
	IncompleteDetails map[string]any    `json:"incomplete_details,omitempty"`
}

// NewResponse builds the initial Response object for a turn.
func NewResponse(sessionID, model string, metadata map[string]string) *Response {
	return &Response{
		ID:        MintResponseID(sessionID, "pending"),
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Status:    StatusInProgress,
		Model:     model,
		Output:    []map[string]any{},
		Metadata:  metadata,
	}
}

func newOutputItemID(index int) string {
	return "item_" + itoa(index)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

// responseEvent builds a wire event map with the type and sequence number,
// plus the caller's fields.
func responseEvent(seq int, typ string, fields map[string]any) map[string]any {
	ev := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		ev[k] = v
	}
	ev["type"] = typ
	ev["sequence_number"] = seq
	return ev
}

// openToolItem is one in-flight function_call output item: the output index
// and item id its added minted, plus the call that started it.
type openToolItem struct {
	index int
	id    string
	call  *agents.ToolCallPayload
}

// Translator projects TranscriptEvents onto the OpenResponses wire. With a
// non-nil sink it emits the SSE event stream; with a nil sink it only folds
// items into the Response object (the aggregated path).
type Translator struct {
	seq    int
	resp   *Response
	emit   func(ev map[string]any)
	turnID string

	// Open message item state (delta path).
	msgOpen     bool
	msgIndex    int
	msgID       string
	msgText     string
	msgDedupped bool

	// Open function_call items keyed by call id (fix-duplicate-tool-call-cards
	// D2): each started call mints exactly one item and each finished call
	// closes its own; fcOpenOrder preserves start order for stable output
	// indices and closeOpenItems.
	fcOpenOrder []string
	fcOpen      map[string]*openToolItem

	// nextIndex is the next output index to allocate.
	nextIndex int
}

// NewTranslator creates a translator for the given response.
func NewTranslator(resp *Response, emit func(ev map[string]any)) *Translator {
	return &Translator{resp: resp, emit: emit, fcOpen: make(map[string]*openToolItem)}
}

// SetSink attaches (or replaces) the event sink after construction — used by
// the streaming endpoint once the SSE headers are committed.
func (t *Translator) SetSink(emit func(ev map[string]any)) {
	t.emit = emit
}

// Response returns the translated response object.
func (t *Translator) Response() *Response { return t.resp }

// TurnID returns the turn ID observed on the stream, if any.
func (t *Translator) TurnID() string { return t.turnID }

// SessionID returns the session the response belongs to.
func (t *Translator) SessionID() string {
	s, _, err := DecodeResponseID(t.resp.ID)
	if err != nil {
		return ""
	}
	return s
}

func (t *Translator) send(typ string, fields map[string]any) {
	if t.emit == nil {
		return
	}
	t.emit(responseEvent(t.seq, typ, fields))
	t.seq++
}

// Handle processes one transcript event. It returns true when the consumer
// should stop (approval short-circuit or terminal event reached).
func (t *Translator) Handle(ev *agents.TranscriptEvent) bool {
	if ev.TurnID != "" && t.turnID == "" {
		t.turnID = ev.TurnID
		t.resp.ID = MintResponseID(t.SessionID(), ev.TurnID)
	}

	switch ev.Kind {
	case agents.TranscriptEventTurnStarted:
		t.send("response.created", map[string]any{"response": t.resp})
		t.send("response.in_progress", map[string]any{"response": t.resp})

	case agents.TranscriptEventTextDelta:
		t.openMessage()
		t.msgText += ev.TextDelta
		t.send("response.output_text.delta", map[string]any{
			"item_id":       t.msgID,
			"output_index":  t.msgIndex,
			"content_index": 0,
			"delta":         ev.TextDelta,
		})

	case agents.TranscriptEventReasoningDelta:
		t.send("onclaw:reasoning_delta", map[string]any{"delta": ev.ReasoningDelta})

	case agents.TranscriptEventMessageCompleted:
		if ev.Message == nil || ev.Message.Role != "assistant" {
			return false
		}
		if t.msgOpen {
			t.closeMessage()
		} else if !t.msgDedupped {
			// Non-delta assembly path: emit the closed item directly.
			t.appendMessageItem(ev.Message.Content)
			t.msgDedupped = true
		}

	case agents.TranscriptEventToolCallStarted:
		if ev.ToolCall == nil {
			return false
		}
		// fix-duplicate-tool-call-cards D2: a re-announced call id never mints
		// a second added — one started call, exactly one open item.
		if _, open := t.fcOpen[ev.ToolCall.CallID]; open {
			return false
		}
		idx := t.nextIndex
		t.nextIndex++
		item := &openToolItem{index: idx, id: newOutputItemID(idx), call: ev.ToolCall}
		t.fcOpen[ev.ToolCall.CallID] = item
		t.fcOpenOrder = append(t.fcOpenOrder, ev.ToolCall.CallID)
		t.send("response.output_item.added", map[string]any{
			"output_index": item.index,
			"item": map[string]any{
				"type":      "function_call",
				"id":        item.id,
				"call_id":   ev.ToolCall.CallID,
				"name":      ev.ToolCall.Name,
				"arguments": ev.ToolCall.Arguments,
				"status":    "in_progress",
			},
		})

	case agents.TranscriptEventToolCallFinished:
		if ev.ToolResult != nil {
			// fix-duplicate-tool-call-cards D2: each finished call closes its
			// OWN item — the done carries that call's id, name and arguments
			// at its original output index, never a sibling started later.
			if item, open := t.fcOpen[ev.ToolResult.CallID]; open {
				doneItem := map[string]any{
					"type":      "function_call",
					"id":        item.id,
					"call_id":   item.call.CallID,
					"name":      item.call.Name,
					"arguments": item.call.Arguments,
					"status":    "completed",
				}
				t.send("response.output_item.done", map[string]any{
					"output_index": item.index,
					"item":         doneItem,
				})
				t.resp.Output = append(t.resp.Output, doneItem)
				delete(t.fcOpen, ev.ToolResult.CallID)
				t.removeFCOrder(ev.ToolResult.CallID)
			}
			idx := t.nextIndex
			t.nextIndex++
			itemID := newOutputItemID(idx)
			item := map[string]any{
				"type":    "onclaw.function_call_output",
				"id":      itemID,
				"call_id": ev.ToolResult.CallID,
				"name":    ev.ToolResult.Name,
				"result":  ev.ToolResult.Result,
			}
			if ev.ToolResult.Latency > 0 {
				item["latency_ms"] = ev.ToolResult.Latency.Milliseconds()
			}
			if ev.ToolResult.IsError {
				item["is_error"] = true
			}
			closed := make(map[string]any, len(item))
			for k, v := range item {
				closed[k] = v
			}
			closed["status"] = "completed"
			t.send("response.output_item.added", map[string]any{
				"output_index": idx,
				"item":         item,
			})
			t.send("response.output_item.done", map[string]any{
				"output_index": idx,
				"item":         closed,
			})
			t.resp.Output = append(t.resp.Output, closed)
		}

	case agents.TranscriptEventContextCompacted:
		// chat-compact-command D6: the compaction frame carries the display-only
		// token estimates; zeros are omitted (same style as latency_ms above).
		fields := map[string]any{}
		if ev.Compaction != nil {
			if ev.Compaction.TokensBefore != 0 {
				fields["tokens_before"] = ev.Compaction.TokensBefore
			}
			if ev.Compaction.TokensAfter != 0 {
				fields["tokens_after"] = ev.Compaction.TokensAfter
			}
		}
		t.send("onclaw:context_compacted", fields)

	case agents.TranscriptEventApprovalRequired:
		t.resp.Status = StatusIncomplete
		t.resp.IncompleteDetails = map[string]any{
			"reason":       "approval_required",
			"interrupt_id": approvalID(ev),
			"command":      approvalCommand(ev),
			"session_id":   t.SessionID(),
		}
		// The optional tool object (add-integration-authority): present only
		// on service-run write escalations — its presence discriminates the
		// service approval card from a shell approval, whose payload stays
		// byte-identical.
		fields := map[string]any{
			"interrupt_id": approvalID(ev),
			"command":      approvalCommand(ev),
			"response_id":  t.resp.ID,
			"session_id":   t.SessionID(),
		}
		if tool := approvalTool(ev); tool != nil {
			fields["tool"] = tool
		}
		t.send("onclaw:approval_required", fields)
		return true

	case agents.TranscriptEventTurnCompleted:
		t.closeOpenItems()
		t.resp.Status = StatusCompleted
		t.resp.Usage = UsageFromDomain(ev.Usage)
		t.send("response.completed", map[string]any{"response": t.resp})
		return true

	case agents.TranscriptEventError:
		t.closeOpenItems()
		t.resp.Status = StatusFailed
		t.resp.Usage = UsageFromDomain(ev.Usage)
		t.resp.Error = &ResponseError{Code: "model_error", Message: ev.Error}
		t.send("response.failed", map[string]any{"response": t.resp})
		return true

	case agents.TranscriptEventCancelled:
		t.closeOpenItems()
		t.resp.Status = StatusIncomplete
		t.resp.Usage = UsageFromDomain(ev.Usage)
		t.resp.IncompleteDetails = map[string]any{"reason": ev.CancelReason}
		t.send("response.incomplete", map[string]any{"response": t.resp})
		return true
	}

	return false
}

// KeepAlive re-emits the current response snapshot as an in_progress frame.
// A run can sit silent for minutes (a slow model call, a long browser tool);
// an idle SSE connection gets reaped by proxies, which strands the browser's
// stream client mid-turn. Clients ignore the repeated in_progress event and
// the sequence counter keeps frames ordered.
func (t *Translator) KeepAlive() {
	if t.emit == nil {
		return
	}
	t.send("response.in_progress", map[string]any{"response": t.resp})
}

func approvalID(ev *agents.TranscriptEvent) string {
	if ev.Approval != nil {
		return ev.Approval.InterruptID
	}
	return ""
}

func approvalCommand(ev *agents.TranscriptEvent) string {
	if ev.Approval != nil {
		return ev.Approval.Command
	}
	return ""
}

// approvalTool returns the write-tier connection tool identity of a
// service-run write escalation, nil for shell approvals.
func approvalTool(ev *agents.TranscriptEvent) *agents.ApprovalToolPayload {
	if ev.Approval != nil {
		return ev.Approval.Tool
	}
	return nil
}

// openMessage opens the assistant message item on the first text delta.
func (t *Translator) openMessage() {
	if t.msgOpen {
		return
	}
	t.msgOpen = true
	t.msgIndex = t.nextIndex
	t.nextIndex++
	t.msgID = newOutputItemID(t.msgIndex)
	t.msgText = ""
	t.send("response.output_item.added", map[string]any{
		"output_index": t.msgIndex,
		"item": map[string]any{
			"type":    "message",
			"id":      t.msgID,
			"role":    "assistant",
			"status":  "in_progress",
			"content": []map[string]any{},
		},
	})
	t.send("response.content_part.added", map[string]any{
		"item_id":       t.msgID,
		"output_index":  t.msgIndex,
		"content_index": 0,
		"part": map[string]any{
			"type":        "output_text",
			"text":        "",
			"annotations": []any{},
		},
	})
}

// closeMessage closes the open message item: text done → part done → item done.
func (t *Translator) closeMessage() {
	t.send("response.output_text.done", map[string]any{
		"item_id":       t.msgID,
		"output_index":  t.msgIndex,
		"content_index": 0,
		"text":          t.msgText,
	})
	t.send("response.content_part.done", map[string]any{
		"item_id":       t.msgID,
		"output_index":  t.msgIndex,
		"content_index": 0,
		"part": map[string]any{
			"type":        "output_text",
			"text":        t.msgText,
			"annotations": []any{},
		},
	})
	t.send("response.output_item.done", map[string]any{
		"output_index": t.msgIndex,
		"item": map[string]any{
			"type":   "message",
			"id":     t.msgID,
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{
				{"type": "output_text", "text": t.msgText, "annotations": []any{}},
			},
		},
	})
	t.resp.Output = append(t.resp.Output, map[string]any{
		"type":   "message",
		"id":     t.msgID,
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{
			{"type": "output_text", "text": t.msgText, "annotations": []any{}},
		},
	})
	t.msgOpen = false
	t.msgDedupped = true
}

// appendMessageItem folds an assistant message into the output without the
// delta framing (aggregate dedup path).
func (t *Translator) appendMessageItem(text string) {
	idx := t.nextIndex
	t.nextIndex++
	item := map[string]any{
		"type":   "message",
		"id":     newOutputItemID(idx),
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{
			{"type": "output_text", "text": text, "annotations": []any{}},
		},
	}
	t.send("response.output_item.done", map[string]any{
		"output_index": idx,
		"item":         item,
	})
	t.resp.Output = append(t.resp.Output, item)
}

// removeFCOrder drops a closed call id from the open-item start order.
func (t *Translator) removeFCOrder(callID string) {
	for i, id := range t.fcOpenOrder {
		if id == callID {
			t.fcOpenOrder = append(t.fcOpenOrder[:i], t.fcOpenOrder[i+1:]...)
			return
		}
	}
}

// closeOpenItems finalizes items left open when the turn terminates.
func (t *Translator) closeOpenItems() {
	if t.msgOpen {
		t.closeMessage()
	}
	// fix-duplicate-tool-call-cards D2: every still-open function_call item
	// closes at its own index, id and arguments, in start order.
	for _, callID := range t.fcOpenOrder {
		item := t.fcOpen[callID]
		t.send("response.output_item.done", map[string]any{
			"output_index": item.index,
			"item": map[string]any{
				"type":      "function_call",
				"id":        item.id,
				"call_id":   item.call.CallID,
				"name":      item.call.Name,
				"arguments": item.call.Arguments,
				"status":    "completed",
			},
		})
		t.resp.Output = append(t.resp.Output, map[string]any{
			"type":      "function_call",
			"id":        item.id,
			"call_id":   item.call.CallID,
			"name":      item.call.Name,
			"arguments": item.call.Arguments,
			"status":    "completed",
		})
	}
	t.fcOpenOrder = nil
	t.fcOpen = make(map[string]*openToolItem)
}

// JSON serializes a wire event for an SSE data frame.
func JSON(ev map[string]any) []byte {
	b, _ := json.Marshal(ev)
	return b
}
