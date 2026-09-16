package agents

import (
	"errors"
	"time"

	"github.com/cloudwego/eino/adk"
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
	// TranscriptEventPromptBlocked terminates a turn whose user prompt was
	// blocked by a user_prompt_submit hook (design.md D6): the model was never
	// called. It is followed by a well-formed turn_completed so live streams
	// and reloaded transcripts both show why the turn has no assistant reply.
	// Blocked tool calls deliberately ride the existing tool_call_started /
	// tool_call_finished pair (their ToolResultPayload.Result carries the
	// block JSON) — this kind is only for the prompt gate.
	TranscriptEventPromptBlocked TranscriptEventKind = "prompt_blocked"
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

// AttachmentMeta is the transcript attachment identity of one user-message
// attachment (attachments design D10): name, media type, size, and the
// capability URL. The JSON tags are the web contract — identical for the
// live stream and the hydrated read path.
type AttachmentMeta struct {
	Name     string `json:"name"`
	MimeType string `json:"mime"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

// CompletedMessage represents a fully assembled assistant message.
type CompletedMessage struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallPayload `json:"tool_calls,omitempty"`
	// Attachments carries the message's attachment metadata when it carried
	// attachments (design D10); nil otherwise.
	Attachments []AttachmentMeta `json:"attachments,omitempty"`
}

// CompactionPayload carries context window compaction metadata.
type CompactionPayload struct {
	Summary     string `json:"summary,omitempty"`
	OffloadPath string `json:"offload_path,omitempty"`
	// TokensBefore/TokensAfter are display-only estimates of the window size
	// around the replacement (chat-compact-command D5, ~4 chars/token). They
	// feed the compaction divider's "154k → 9.2k" copy — never billing or
	// trigger math.
	TokensBefore int `json:"tokens_before"`
	TokensAfter  int `json:"tokens_after"`
}

// PromptBlockedPayload carries a hook-blocked prompt notice: which hook
// blocked the submission and why (design.md D6).
type PromptBlockedPayload struct {
	Hook   string `json:"hook"`
	Reason string `json:"reason"`
}

// sessionEventKindPromptBlocked is the application-owned session-event kind
// (the ADK extension namespace) that persists a hook-blocked prompt notice.
// The ADK runner never produces it; the runner's prompt gate appends it so a
// reloaded transcript renders the notice identically to the live stream (D6).
// The turn terminal itself is not persisted — History synthesizes
// turn_completed at the turn boundary exactly as it does for every other turn.
const sessionEventKindPromptBlocked = adk.SessionEventKind("x.prompt_blocked")

// promptBlockedEvent is the durable payload of sessionEventKindPromptBlocked.
// Hook/Reason round-trip into PromptBlockedPayload verbatim. The concrete type
// is registered below because the ADK serializer reconstructs the
// SessionExtensionEvent.Data any field from registered concrete types.
type promptBlockedEvent struct {
	Hook   string `json:"hook"`
	Reason string `json:"reason"`
}

// sessionExtraKeyCompaction is the Extra key under which a window-replacement
// session event carries its display-only token estimates (chat-compact-command
// D4/D5). The stock adk.SessionEventMessagesReplaced record has no estimate
// fields, and Extra is ignored by ADK replay — the runner stamps the estimates
// onto the record it appends so a hydrated History projection fills the same
// CompactionPayload the live stream delivered.
const sessionExtraKeyCompaction = "onclaw_compaction_estimates"

// compactionEstimates is the durable Extra payload behind
// sessionExtraKeyCompaction. Registered below: Extra is a map[string]any, and
// the ADK serializer reconstructs registered concrete types stored behind
// interface fields.
type compactionEstimates struct {
	TokensBefore int `json:"tokens_before"`
	TokensAfter  int `json:"tokens_after"`
}

func init() {
	schema.Register[promptBlockedEvent]()
	schema.Register[compactionEstimates]()
}

// TranscriptEvent represents a single domain-level event in an agent turn transcript.
type TranscriptEvent struct {
	ID             string                `json:"id,omitempty"`
	Kind           TranscriptEventKind   `json:"kind"`
	OccurredAt     time.Time             `json:"occurred_at"`
	TurnID         string                `json:"turn_id,omitempty"`
	TextDelta      string                `json:"text_delta,omitempty"`
	ReasoningDelta string                `json:"reasoning_delta,omitempty"`
	ToolCall       *ToolCallPayload      `json:"tool_call,omitempty"`
	ToolResult     *ToolResultPayload    `json:"tool_result,omitempty"`
	Message        *CompletedMessage     `json:"message,omitempty"`
	Compaction     *CompactionPayload    `json:"compaction,omitempty"`
	Approval       *ApprovalPayload      `json:"approval,omitempty"`
	PromptBlocked  *PromptBlockedPayload `json:"prompt_blocked,omitempty"`
	Error          string                `json:"error,omitempty"`
	CancelReason   string                `json:"cancel_reason,omitempty"`
	RetryAttempt   int                   `json:"retry_attempt,omitempty"`
	Usage          *UsagePayload         `json:"usage,omitempty"`
	// TraceID carries the turn's pinned Langfuse trace id on terminal events
	// (integrate-langfuse-tracing D3/D5), set only when the turn sampled in
	// for export — a sampled-out turn carries no id, so a persisted run
	// record always targets a real trace. Run-record owners (the scheduler
	// drain) read it off the terminal event; every other consumer ignores it.
	TraceID string `json:"trace_id,omitempty"`
}

// AttachmentRef identifies one attachment carried on a turn (attachments
// design D9). Lane is one of the attachment lane values: "inline-image",
// "inline-pdf", "inline-text", "drop".
type AttachmentRef struct {
	ID       string
	Name     string
	MimeType string
	Lane     string
	Size     int64
}

// ExecRequest contains all parameters required to execute an agent turn.
type ExecRequest struct {
	WorkspaceID string
	AgentID     string
	SessionID   string
	UserID      string
	Input       string

	// Origin identifies what triggered the run (hooks design.md D1):
	// OriginUser, OriginScheduler, OriginChannel, OriginTelegram, or
	// OriginHeartbeat. Empty selects OriginUser — every current caller is
	// user-initiated; the scheduler service sets OriginScheduler when a
	// scheduled run is submitted, the Telegram gateway sets OriginTelegram for
	// every paired-member turn (integrate-telegram-gateway task 6.3), and the
	// heartbeat ticker sets OriginHeartbeat for every tick
	// (add-agent-heartbeat D11/D14).
	Origin string

	// Channel-run coordinates (OriginChannel). ChannelID is required when
	// Origin normalizes to OriginChannel; RootMessageID/ChainDepth carry the
	// summon chain state (integrate-agent-channels D4/D8) and are owned by
	// the channel chokepoint that submits the run.
	ChannelID     string
	RootMessageID string
	ChainDepth    int

	// WorkSessionID links the run to the channel's work session it was
	// minted for (channel-teams D1/D3); "" outside sessions. Set only by the
	// channel chokepoint, for runs whose hop was billed against the session.
	WorkSessionID string

	// AllowedTools replaces the agent's tool allowlist for this turn when
	// non-nil (an empty slice runs the turn with no tools). nil keeps the
	// agent's configured allowlist. Callers that narrow must intersect with
	// the agent allowlist themselves — a request can narrow, never widen.
	AllowedTools []string

	// Command names a built-in slash command executed as a turn
	// (chat-compact-command D2): CommandCompact compacts the session's
	// context window, with Input carrying the summarizer's focus text
	// instead of a user prompt. Optional — empty (or any unrecognized value,
	// which must never be an error) runs an ordinary model turn.
	Command string

	// Attachments carries the attachments referenced by this turn's input
	// (attachments design D9). nil for scheduler/channel/compact runs — those
	// callers are untouched. Only drop-lane refs are materialized into the
	// run-scoped read-only mount (D17); inline lanes are resolved by the
	// message-construction layer instead.
	Attachments []AttachmentRef

	// SchedulerNoReply teaches the unattended-run contract the literal suppression
	// token for channel-target scheduler runs ("NO_REPLY"); empty for thread
	// targets (the contract then asks for a plain "Nothing to report."). NonEmpty
	// only when Origin normalizes to OriginScheduler.
	SchedulerNoReply string

	// ScheduleName names the scheduler a scheduler-origin run fires for
	// (integrate-langfuse-tracing D2): it becomes the exported trace's name —
	// "trace name = schedule name for scheduler fires, first input line
	// otherwise". Purely observational: it rides the run's trace context
	// only, and every non-scheduler origin ignores it.
	ScheduleName string

	// HeartbeatChecklist is the agent's HEARTBEAT checklist prompt for
	// heartbeat-origin runs (add-agent-heartbeat D3/D9): it composes into the
	// instruction's checklist section, never the user input. Only read when
	// Origin normalizes to OriginHeartbeat.
	HeartbeatChecklist string

	// HeartbeatDigest is the pre-composed workspace-activity digest section
	// body for heartbeat-origin runs (add-agent-heartbeat D9): channel message
	// previews and scheduler run outcomes since the heartbeat's previous tick.
	// Empty means nothing happened — the composer then renders the
	// no-recent-activity fallback itself. Only read when Origin normalizes to
	// OriginHeartbeat.
	HeartbeatDigest string
}

// Run origins (hooks design.md D1). ExecRequest.Origin carries one; empty
// selects OriginUser.
const (
	OriginUser      = "user"
	OriginScheduler = "scheduler"
	OriginChannel   = "channel"
	// OriginTelegram marks gateway-submitted runs (integrate-telegram-gateway
	// task 6.3): Telegram turns ride the same event and hook-payload origin
	// contract as scheduler and channel runs.
	OriginTelegram = "telegram"
	// OriginHeartbeat marks heartbeat-ticker-submitted runs
	// (add-agent-heartbeat D11): heartbeat ticks ride the same event and
	// hook-payload origin contract as scheduler and channel runs.
	OriginHeartbeat = "heartbeat"
)

// Built-in slash commands executed as turns (chat-compact-command D2).
// ExecRequest.Command carries one; empty selects a normal model turn.
const CommandCompact = "compact"

// normalizeCommand maps a request's command onto the fixed value set: only
// the documented CommandCompact branches to a command turn, anything else
// (including empty) is a normal model turn — unknown values are not errors.
func normalizeCommand(command string) string {
	if command == CommandCompact {
		return CommandCompact
	}
	return ""
}

// normalizeOrigin maps a request's origin onto the fixed D1 value set: the
// documented values pass through, anything else (including empty) is
// user-initiated.
func normalizeOrigin(origin string) string {
	switch origin {
	case OriginScheduler, OriginChannel, OriginTelegram, OriginHeartbeat:
		return origin
	default:
		return OriginUser
	}
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
	if normalizeOrigin(r.Origin) == OriginChannel && r.ChannelID == "" {
		return errors.New("exec request: channel_id is required for channel runs")
	}
	return nil
}

// extractAgenticText extracts the text content from an AgenticMessage's ContentBlocks.
// Blocks marked AttachmentPointerExtraKey (drop-lane pointer notes and the
// model-time stale-attachment placeholders, attachments design D8) are
// model-facing plumbing and never part of the visible text.
func extractAgenticText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb []byte
	for _, block := range msg.ContentBlocks {
		if block == nil || isAttachmentPointerNote(block) {
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
