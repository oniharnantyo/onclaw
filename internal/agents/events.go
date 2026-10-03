package agents

import (
	"errors"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/skillcuration"
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
	// TranscriptEventMemoryIngested is the post-turn memory chip
	// (integrate-agent-zero-memory D11, task 3.6): the count and visibility
	// breakdown of what the background pipeline committed for the turn —
	// never the content. It is emitted asynchronously after the run has
	// terminalized, so live consumers see it via the run broadcast only while
	// the run is still registered; the durable form is the x.memory_ingested
	// session event the hydrated History projection renders identically.
	// Scheduled and heartbeat runs never emit it (spec agent-memory-pipeline,
	// Ingested-chip event).
	TranscriptEventMemoryIngested TranscriptEventKind = "memory_ingested"
	// TranscriptEventSkillCandidate is the skill-curation chip
	// (add-skill-curation-from-traces, spec "Qualifying run emits a candidate
	// signal"): the qualifying run's coordinates and, once the proposer has
	// drafted one, the candidate's id and skill name — the review queue's
	// transcript breadcrumb. Like the memory chip it is emitted
	// asynchronously after the run has terminalized; the durable form is the
	// x.skill_candidate session event the hydrated History projection renders
	// identically. Scheduled and heartbeat runs never emit it (the memory
	// chip's origin rule, cloned).
	TranscriptEventSkillCandidate TranscriptEventKind = "skill_candidate"
	// TranscriptEventTaskCompleted announces a background task (delegation or
	// shell) reaching a terminal status (add-agent-subagents-background D8).
	// The task space is process-local — scoped to one run — and the event
	// lands via the per-run notification pump, so it is transcript/honesty
	// surface only, never a polling channel: the model-facing detail channel
	// is task_output. It is persisted under the x.task_completed session
	// event so the hydrated History projection renders it identically to the
	// live stream.
	TranscriptEventTaskCompleted TranscriptEventKind = "task_completed"
	// TranscriptEventRunActive is a synthetic status frame the streaming
	// session-events endpoint writes when its tap attaches to a live run —
	// never persisted or broadcast by the runner. It tells a reconnected
	// client the turn is still executing so the running state shows
	// immediately, before any run event lands (a run between persisted events
	// is invisible to the history snapshot).
	TranscriptEventRunActive TranscriptEventKind = "run_active"
)

// ApprovalPayload carries a pending approval interrupt. Command is set for
// shell approvals; Tool is set ONLY for service-run write escalations
// (add-integration-authority task 2.4) — its presence discriminates the
// service approval card from the shell one, and shell payloads stay
// byte-identical (Tool is omitted when nil).
type ApprovalPayload struct {
	// InterruptID addresses the interrupt in the resume call.
	InterruptID string `json:"interrupt_id"`
	// Command is the shell command awaiting approval (shell approvals only).
	Command string `json:"command"`
	// Tool identifies the write-tier connection tool awaiting approval
	// (service-run escalations only).
	Tool *ApprovalToolPayload `json:"tool,omitempty"`
}

// ApprovalToolPayload is the tool identity a service-run write escalation
// carries (add-integration-authority escalation contract): the applied tool
// name, its originating connection, and the effective tier. The wire shape
// the web's approval card branches on.
type ApprovalToolPayload struct {
	// Name is the applied tool name ("<recipe>.<verb>" or "mcp__<server>__<tool>").
	Name string `json:"name"`
	// Service is the recipe id.
	Service string `json:"service"`
	// ServiceName is the recipe's display name, when registered.
	ServiceName string `json:"service_name,omitempty"`
	// ConnectionID is the owning workspace connection.
	ConnectionID string `json:"connection_id,omitempty"`
	// Tier is the tool's effective tier (always "write" — only write-tier
	// calls escalate).
	Tier string `json:"tier"`
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

// ContextBreakdown is the display-only, display-grade split of a turn's
// final model call's input into labeled segments (adopt-assistant-ui-elements
// D7, spec agent-runtime "Context breakdown measurement"): instructions (the
// composed instruction), tools (marshaled tool schemas), conversation (the
// true session window's messages), files (in-window attachment payloads), and
// server (the composed share not attributable to the other segments). Every
// segment is omitempty: a segment that could not be measured is omitted, never
// reported as zero. The breakdown feeds context diagnostics only — never
// billing, trigger math, or summarization decisions.
type ContextBreakdown struct {
	Instructions int `json:"instructions,omitempty"`
	Tools        int `json:"tools,omitempty"`
	Conversation int `json:"conversation,omitempty"`
	Files        int `json:"files,omitempty"`
	Server       int `json:"server,omitempty"`
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
	// ContextBreakdown is the optional display-only segment split of the
	// final call's input (D7). Additive and omitempty — clients that ignore
	// it remain fully functional — and present only on turns whose provider
	// reported usage: the block never appears without a usage block.
	ContextBreakdown *ContextBreakdown `json:"context_breakdown,omitempty"`
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

// TaskCompletedPayload carries a background task's terminal notice
// (add-agent-subagents-background D8): the process-local task id, the lane
// kind ("delegation" for the subagent lane, "shell" for the fs shell lane),
// the outcome ("completed" | "failed" | "canceled"), the output file path the
// model can Read inside the jail, and a one-line human summary derived from
// the task description. The JSON tags are the web contract — identical for
// the live emission and the hydrated read path.
type TaskCompletedPayload struct {
	TaskID     string `json:"task_id"`
	Kind       string `json:"kind"`
	Outcome    string `json:"outcome"`
	OutputPath string `json:"output_path,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

// sessionEventKindTaskCompleted is the application-owned session-event kind
// (the ADK extension namespace, the x.prompt_blocked convention) that
// persists a background task completion notice. The ADK runner never
// produces it; the per-run notification pump appends it so a reloaded
// transcript renders the completion identically to the live stream (D8).
const sessionEventKindTaskCompleted = adk.SessionEventKind("x.task_completed")

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
	// The task-completion payload rides the session-event Extension any
	// field; registering the concrete type is what lets the serializer
	// round-trip it as the struct instead of a generic map (the
	// promptBlockedEvent precedent).
	schema.Register[TaskCompletedPayload]()
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
	// MemoryIngested carries the post-turn memory chip payload
	// (integrate-agent-zero-memory D11): committed ids + visibility counts,
	// never content. The type is the memory package's own registered payload
	// so the live event and the persisted session event serialize identically.
	MemoryIngested *memory.MemoryIngestedPayload `json:"memory_ingested,omitempty"`
	// SkillCandidate carries the skill-curation chip payload
	// (add-skill-curation-from-traces): the qualifying run's coordinates plus
	// the drafted candidate's id and skill name once a proposal exists. The
	// type is the skillcuration package's registered payload so the live
	// event and the persisted session event serialize identically.
	SkillCandidate *skillcuration.SkillCandidatePayload `json:"skill_candidate,omitempty"`
	// TaskCompleted carries the background task completion chip
	// (add-agent-subagents-background D8): task id, lane kind, terminal
	// outcome, output path, and summary — the transcript/honesty surface of
	// the run-scoped process-local task space. The type is the registered
	// concrete payload so the live event and the persisted session event
	// serialize identically.
	TaskCompleted *TaskCompletedPayload `json:"task_completed,omitempty"`
	Error         string                `json:"error,omitempty"`
	CancelReason  string                `json:"cancel_reason,omitempty"`
	RetryAttempt  int                   `json:"retry_attempt,omitempty"`
	Usage         *UsagePayload         `json:"usage,omitempty"`
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
	// OriginUser, OriginScheduler, OriginChannel, OriginTelegram,
	// OriginHeartbeat, or OriginService. Empty selects OriginUser — every
	// current caller is user-initiated; the scheduler service sets
	// OriginScheduler when a scheduled run is submitted, the Telegram gateway
	// sets OriginTelegram for every paired-member turn
	// (integrate-telegram-gateway task 6.3), the heartbeat ticker sets
	// OriginHeartbeat for every tick (add-agent-heartbeat D11/D14), and the
	// connection-webhook ingress sets OriginService for every event-triggered
	// run (add-connection-webhooks contract §6).
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

	// Service-authority attribution (add-connection-webhooks contract §6,
	// design.md D5): ConnectionID, ConnectionService, and Event name the
	// exact trigger of an OriginService run — the connection whose webhook
	// fired, its recipe service id, and the derived catalog event id
	// ("pull_request.opened"). They ride the run's trace metadata as
	// {authority, connection_id, connection_service, event} — the authority
	// discriminator itself derives from the Origin. Empty for every other
	// origin; v1 applies no new gating, so nothing else reads them.
	ConnectionID      string
	ConnectionService string
	Event             string

	// AllowedTools replaces the agent's tool selection for this turn when
	// non-nil: an explicit request-scoped allowlist (an empty slice runs the
	// turn with no tools). nil keeps denylist resolution — the agent's
	// disabled_tools subtracted from the catalog (agent-tools-denylist D2).
	// Callers that narrow must intersect with the effective set themselves —
	// a request can narrow, never widen.
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
	// OriginService marks service-authority runs (add-connection-webhooks
	// contract §6, design.md D5): event-triggered runs carry the connection
	// and event identity in ExecRequest.ConnectionID/ConnectionService/Event
	// instead of a requesting user — first-class in the origin normalizer so
	// traces, transcripts, and future gating key off the origin honestly.
	OriginService = "service"
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
	case OriginScheduler, OriginChannel, OriginTelegram, OriginHeartbeat, OriginService:
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
