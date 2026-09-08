package agents

import (
	"errors"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Model is the Eino chat-model interface used by the ADK runtime agent.
type Model = model.BaseModel[*schema.AgenticMessage]

// TranscriptEventKind identifies the domain event kind emitted by an agent execution.
type TranscriptEventKind string

const (
	TranscriptEventTurnStarted      TranscriptEventKind = "turn_started"
	TranscriptEventTextDelta        TranscriptEventKind = "text_delta"
	TranscriptEventReasoningDelta   TranscriptEventKind = "reasoning_delta"
	TranscriptEventToolCallStarted  TranscriptEventKind = "tool_call_started"
	TranscriptEventToolCallFinished TranscriptEventKind = "tool_call_finished"
	TranscriptEventMessageCompleted TranscriptEventKind = "message_completed"
	TranscriptEventContextCompacted TranscriptEventKind = "context_compacted"
	// TranscriptEventApprovalRequired pauses a turn: a dangerous shell command
	// awaits human approval. The stream ends after this event without a
	// terminal kind; Resume continues the turn.
	TranscriptEventApprovalRequired TranscriptEventKind = "approval_required"
	TranscriptEventTurnCompleted    TranscriptEventKind = "turn_completed"
	TranscriptEventError            TranscriptEventKind = "error"
	TranscriptEventCancelled        TranscriptEventKind = "cancelled"
	// TranscriptEventRunActive is a synthetic status frame the streaming
	// session-events endpoint writes when its tap attaches to a live run —
	// never persisted or broadcast by the runner. It tells a reconnected
	// client the turn is still executing so the running state shows
	// immediately, before any run event lands (a run between persisted events
	// is invisible to the history snapshot).
	TranscriptEventRunActive TranscriptEventKind = "run_active"
)

// ApprovalPayload carries a pending shell-approval interrupt.
type ApprovalPayload struct {
	// InterruptID addresses the interrupt in the resume call.
	InterruptID string `json:"interrupt_id"`
	// Command is the shell command awaiting approval.
	Command string `json:"command"`
}

// ToolCallPayload carries information about a requested tool invocation.
type ToolCallPayload struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultPayload carries the execution result of a tool call.
type ToolResultPayload struct {
	CallID  string        `json:"call_id"`
	Name    string        `json:"name"`
	Result  string        `json:"result"`
	Latency time.Duration `json:"latency,omitempty"`
	IsError bool          `json:"is_error,omitempty"`
}

// UsagePayload carries the token usage of one executed turn, as reported by
// the model provider. InputTokens sums every model call of the turn;
// FinalInputTokens is the input size of the last call — what the model's
// context last held.
type UsagePayload struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	FinalInputTokens int `json:"final_input_tokens,omitempty"`
}

// CompletedMessage represents a fully assembled assistant message.
type CompletedMessage struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallPayload `json:"tool_calls,omitempty"`
}

// CompactionPayload carries context window compaction metadata.
type CompactionPayload struct {
	Summary     string `json:"summary,omitempty"`
	OffloadPath string `json:"offload_path,omitempty"`
}

// TranscriptEvent represents a single domain-level event in an agent turn transcript.
type TranscriptEvent struct {
	ID             string              `json:"id,omitempty"`
	Kind           TranscriptEventKind `json:"kind"`
	OccurredAt     time.Time           `json:"occurred_at"`
	TurnID         string              `json:"turn_id,omitempty"`
	TextDelta      string              `json:"text_delta,omitempty"`
	ReasoningDelta string              `json:"reasoning_delta,omitempty"`
	ToolCall       *ToolCallPayload    `json:"tool_call,omitempty"`
	ToolResult     *ToolResultPayload  `json:"tool_result,omitempty"`
	Message        *CompletedMessage   `json:"message,omitempty"`
	Compaction     *CompactionPayload  `json:"compaction,omitempty"`
	Approval       *ApprovalPayload    `json:"approval,omitempty"`
	Error          string              `json:"error,omitempty"`
	CancelReason   string              `json:"cancel_reason,omitempty"`
	RetryAttempt   int                 `json:"retry_attempt,omitempty"`
	Usage          *UsagePayload       `json:"usage,omitempty"`
}

// ExecRequest contains all parameters required to execute an agent turn.
type ExecRequest struct {
	WorkspaceID string
	AgentID     string
	SessionID   string
	UserID      string
	Input       string

	// AllowedTools replaces the agent's tool allowlist for this turn when
	// non-nil (an empty slice runs the turn with no tools). nil keeps the
	// agent's configured allowlist. Callers that narrow must intersect with
	// the agent allowlist themselves — a request can narrow, never widen.
	AllowedTools []string
}

// Validate checks that required fields on ExecRequest are present.
func (r ExecRequest) Validate() error {
	if r.WorkspaceID == "" {
		return errors.New("exec request: workspace_id is required")
	}
	if r.AgentID == "" {
		return errors.New("exec request: agent_id is required")
	}
	if r.SessionID == "" {
		return errors.New("exec request: session_id is required")
	}
	if r.UserID == "" {
		return errors.New("exec request: user_id is required")
	}
	return nil
}

// extractAgenticText extracts the text content from an AgenticMessage's ContentBlocks.
func extractAgenticText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb []byte
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.AssistantGenText != nil {
			sb = append(sb, block.AssistantGenText.Text...)
		}
		if block.UserInputText != nil {
			sb = append(sb, block.UserInputText.Text...)
		}
	}
	return string(sb)
}

// agenticReasoningText extracts the reasoning text from an AgenticMessage's
// ContentBlockTypeReasoning blocks. Reasoning stays distinct from visible
// text: extractAgenticText never includes it.
func agenticReasoningText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb []byte
	for _, block := range msg.ContentBlocks {
		if block == nil || block.Reasoning == nil {
			continue
		}
		sb = append(sb, block.Reasoning.Text...)
	}
	return string(sb)
}

// agenticToolCalls extracts function tool calls from an agentic message —
// streaming frames carry the call chunks, completed messages the full call.
func agenticToolCalls(msg *schema.AgenticMessage) []ToolCallPayload {
	if msg == nil {
		return nil
	}
	var out []ToolCallPayload
	for _, block := range msg.ContentBlocks {
		if block == nil || block.FunctionToolCall == nil {
			continue
		}
		out = append(out, ToolCallPayload{
			CallID:    block.FunctionToolCall.CallID,
			Name:      block.FunctionToolCall.Name,
			Arguments: block.FunctionToolCall.Arguments,
		})
	}
	return out
}

// agenticToolResults extracts function tool results from an agentic message,
// flattening the result content blocks to text.
func agenticToolResults(msg *schema.AgenticMessage) []ToolResultPayload {
	if msg == nil {
		return nil
	}
	var out []ToolResultPayload
	for _, block := range msg.ContentBlocks {
		if block == nil || block.FunctionToolResult == nil {
			continue
		}
		res := block.FunctionToolResult
		var sb []byte
		for _, cb := range res.Content {
			if cb != nil && cb.Text != nil {
				sb = append(sb, cb.Text.Text...)
			}
		}
		out = append(out, ToolResultPayload{
			CallID: res.CallID,
			Name:   res.Name,
			Result: string(sb),
		})
	}
	return out
}
