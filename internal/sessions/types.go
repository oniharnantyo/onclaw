package sessions

import (
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Session is one chat session. It aliases the store row: the session's
// queryable identity lives in exactly one place.
type Session = store.Session

// Kind discriminates session events. The set is the transcript vocabulary:
// every event a turn can append to a session's log, and (for the live-only
// kinds) everything a subscriber can observe while a turn runs.
type Kind string

const (
	// KindUserMessage carries the user's turn input (persisted).
	KindUserMessage Kind = "user_message"
	// KindTextDelta carries one incremental slice of assistant text.
	// Live-only: the runtime persists the final assistant message instead,
	// so replaying the log never depends on delta granularity.
	KindTextDelta Kind = "text_delta"
	// KindReasoningDelta carries one incremental slice of reasoning output.
	// Live-only, like KindTextDelta; the durable form is KindReasoning.
	KindReasoningDelta Kind = "reasoning_delta"
	// KindAssistantMessage carries a completed assistant text message
	// (persisted).
	KindAssistantMessage Kind = "assistant_message"
	// KindReasoning carries completed reasoning output (persisted when the
	// model produced any). Its live counterpart is KindTextDelta-shaped
	// reasoning deltas on the stream.
	KindReasoning Kind = "reasoning"
	// KindToolActivity carries one observed tool call. The persisted form is
	// the terminal record only (status completed/failed with its result,
	// design D10); the running form is live-only, like the delta kinds — a
	// replayed transcript always shows final states.
	KindToolActivity Kind = "tool_activity"
	// KindTaskActivity carries one delegated subagent task's lifecycle: a
	// running form while the child turn executes and a terminal form
	// (completed/failed/stopped) once it ends. The persistence rule is the
	// tool-activity rule (design D10): the running form is live-only and the
	// terminal form — with the child's answer — is persisted.
	KindTaskActivity Kind = "task_activity"
	// KindTurnCompleted terminates a successful turn and carries its usage
	// (persisted).
	KindTurnCompleted Kind = "turn_completed"
	// KindTurnError terminates a failed turn and carries the failure message
	// (persisted).
	KindTurnError Kind = "turn_error"
	// KindTurnCancelled terminates a cancelled turn (persisted): streaming
	// and tool activity stopped on the user's request. Text carries the
	// in-flight model call's partial output, if any — the durable events of
	// completed model calls landed before the cancellation and stay.
	KindTurnCancelled Kind = "turn_cancelled"
	// KindApprovalRequired marks a turn paused on a dangerous-command gate
	// (persisted). The turn has NO terminal event while the approval is
	// pending: the payload names the interrupt, and pending state derives
	// from the log (a KindApprovalRequired with no matching
	// KindApprovalResolved). Survives restarts by replay.
	KindApprovalRequired Kind = "approval_required"
	// KindApprovalResolved records one approval decision (persisted): the
	// outcome (approved, denied, or cancelled) for the named interrupt. A
	// cancelled outcome accompanies a discarded run's KindTurnCancelled.
	KindApprovalResolved Kind = "approval_resolved"
	// KindInputRequired marks a turn parked on a browser attention request —
	// a secret the agent needs submitted, or a browser takeover (persisted,
	// design D3 of add-browser-login-handover). Like KindApprovalRequired the
	// turn has NO terminal event while the request is pending: pending state
	// derives from the log (a KindInputRequired with no matching
	// KindInputResolved). The payload is a receipt-safe card description —
	// it NEVER carries the secret's value.
	KindInputRequired Kind = "input_required"
	// KindInputResolved records one browser attention resolution (persisted):
	// a receipt only — outcome, the redacted input length, and the optional
	// saved-login id. The value itself never enters any event (design D3).
	KindInputResolved Kind = "input_resolved"
	// KindCompaction marks a compaction of the session's working context
	// (persisted). The stored transcript is never truncated by compaction.
	KindCompaction Kind = "compaction"
	// KindPlanActivity carries a full snapshot of the agent's task plan (the
	// plantask tools' tasks) after a plan tool changed it (persisted: replay
	// folds to the newest snapshot).
	KindPlanActivity Kind = "plan_activity"
)

// Approval outcomes — the decision recorded on a KindApprovalResolved event.
const (
	// ApprovalApproved marks an approved command: the turn resumed from its
	// checkpoint and the command executed.
	ApprovalApproved = "approved"
	// ApprovalDenied marks a denied command: the refusal was fed back to the
	// model and the turn continued without executing.
	ApprovalDenied = "denied"
	// ApprovalCancelled marks a discarded run: the turn was cancelled while
	// the approval was pending.
	ApprovalCancelled = "cancelled"
)

// Attention kinds — what a KindInputRequired event parks the turn on
// (design D6 of add-browser-login-handover). The kind picks the attention
// card's renderer: a masked secret input, or the takeover decision card.
const (
	// AttentionSecret marks a browser secret submission (the agent called
	// submit_secret): the card collects the value through a masked input.
	AttentionSecret = "secret"
	// AttentionTakeover marks a browser handover request (the agent called
	// request_takeover): the card presents Approve/Cancel, then Done/Cancel.
	AttentionTakeover = "takeover"
)

// Input outcomes — the receipt recorded on a KindInputResolved event. A
// pending request that outlives the process folds expired on the next load
// (the frontend transcript fold normalizes a still-pending card once the
// session is no longer busy — expireUnresolvedInputs in
// frontend/src/features/chat/entries.ts); the runtime itself records only
// the outcomes below.
const (
	// InputCompleted marks a delivered secret or a takeover the user finished.
	InputCompleted = "completed"
	// InputCancelled marks a takeover (or pending request) the user cancelled.
	InputCancelled = "cancelled"
	// InputExpired marks a request whose process (and pending-value map) is
	// gone; folded client-side (the transcript fold's expiry normalization),
	// never appended by the runtime.
	InputExpired = "expired"
	// InputFailed marks a delivery the child browser engine refused.
	InputFailed = "failed"
)

// ToolStatus is the execution state of a ToolActivity. Exactly one of the
// three states applies; the zero value (empty) is the legacy form persisted
// before statuses existed and is terminal like completed.
type ToolStatus string

const (
	// ToolStatusRunning marks a tool call that started executing. It is
	// live-only: the runtime emits it on the turn stream so the UI can show
	// a spinner, and persists the terminal record instead once the call
	// finishes (design D10).
	ToolStatusRunning ToolStatus = "running"
	// ToolStatusCompleted marks a tool call that finished successfully. Its
	// record is persisted with the real Result.
	ToolStatusCompleted ToolStatus = "completed"
	// ToolStatusFailed marks a tool call that finished with an error. Its
	// record is persisted with the error text as Result (and IsError set for
	// older clients).
	ToolStatusFailed ToolStatus = "failed"
	// ToolStatusStopped marks a task whose subagent run was cancelled — by a
	// task_stop call or by its parent turn ending. It is terminal: the task
	// will never produce a different outcome.
	ToolStatusStopped ToolStatus = "stopped"
)

// ToolActivity is one tool call observed during a turn.
type ToolActivity struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	// Status is the execution state. Transcripts hold terminal records only:
	// the runtime broadcasts the running form live and persists the terminal
	// record (completed/failed) with the real Result once execution ends
	// (design D10). Empty on records persisted before statuses existed.
	Status ToolStatus `json:"status,omitempty"`
	// Result is the execution result. Empty until the terminal record lands
	// — and, for completed calls, when the tool produced no output.
	Result string `json:"result,omitempty"`
	// DurationMs is the call's wall-clock duration in milliseconds, measured
	// by the runtime from the running announcement to the terminal record
	// (design D5). Zero on records persisted before durations were recorded
	// — such a record shows no latency rather than a fabricated one.
	DurationMs int  `json:"duration_ms,omitempty"`
	IsError    bool `json:"is_error,omitempty"`
}

// Terminal reports whether the activity is in a state that may be persisted:
// completed, failed, or the legacy empty form. The running state is
// live-only — transcripts never hold it (design D10). A nil activity is
// terminal (there is nothing running).
func (a *ToolActivity) Terminal() bool {
	return a == nil || a.Status != ToolStatusRunning
}

// TaskActivity is one delegated subagent task observed during a turn. The
// task identifier is the run's task-ID space key, so a task_output or
// task_stop call names the record unambiguously. The terminal form is stored
// on the parent session's transcript (spec agent-subagents "Task completion
// surfacing"), the running form rides the live channel only.
type TaskActivity struct {
	// TaskID is the run-scoped task identifier ("task-1", ...).
	TaskID string `json:"task_id"`
	// Description is the delegated task's text, as the parent model asked for
	// it (bounded to its first line for display).
	Description string `json:"description,omitempty"`
	// Status is the task's lifecycle state. Empty on records persisted before
	// statuses existed, which are terminal like completed.
	Status ToolStatus `json:"status,omitempty"`
	// Result is the child's final answer once the task reached a terminal
	// state; empty while running and for a cancelled task that produced none.
	Result string `json:"result,omitempty"`
	// TranscriptPath is the workspace-relative path of the task's materialized
	// transcript (".tasks/<task tool-call id>.output"), readable through the
	// jailed file tools.
	TranscriptPath string `json:"transcript_path,omitempty"`
	// Background reports whether the delegation ran in the background lane.
	Background bool `json:"background,omitempty"`
	// Progress is the child's latest activity while running — one line derived
	// from its most recent transcript record (a tool call, a text fragment).
	// Live-only: it rides running snapshots and is empty on terminal records.
	Progress string `json:"progress,omitempty"`
	// Steps counts the transcript records the child has produced so far.
	Steps int `json:"steps,omitempty"`
}

// Terminal reports whether the task is in a state that may be persisted:
// completed, failed, stopped, or the legacy empty form. The running state is
// live-only (design D10): transcripts never hold a spinner that can no longer
// resolve. A nil activity is terminal.
func (t *TaskActivity) Terminal() bool {
	return t == nil || t.Status != ToolStatusRunning
}

// Attachment is the transcript metadata of one stored upload (design D5,
// spec chat-attachments "Multi-part user messages"): what a user_message
// event persists so replay reconstructs the attachment chips. Key is the
// unguessable capability key — the only address the transcript carries; the
// bytes are fetched separately through the capability-gated content call.
type Attachment struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// ArtifactChange is how a turn changed one artifact against its
// start-of-turn record: created or modified. Empty on a cold resume, where
// the record was lost and only the modification time is known.
type ArtifactChange string

const (
	ArtifactCreated  ArtifactChange = "created"
	ArtifactModified ArtifactChange = "modified"
)

// MaxArtifacts bounds the artifact list one turn_completed event carries;
// ArtifactsTruncated reports a turn that produced more.
const MaxArtifacts = 50

// Artifact is one file the turn produced for the user under the workspace's
// artifacts/ directory (add-turn-artifacts design D4): the workspace-relative
// path (so a chip opens the preview directly), its byte size, and the change
// kind when known.
type Artifact struct {
	Path   string         `json:"path"`
	Size   int64          `json:"size"`
	Change ArtifactChange `json:"change,omitempty"`
}

// Usage is one turn's provider-reported token usage. InputTokens/OutputTokens/
// TotalTokens sum every model call of the turn; FinalInputTokens is the input
// size of the last call — what the model's context last held.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
	FinalInputTokens int `json:"final_input_tokens,omitempty"`
}

// Compaction is a compaction marker's payload: which checkpoint holds the
// summary, and the display-grade token estimates around the replacement.
type Compaction struct {
	CheckpointID string `json:"checkpoint_id"`
	Summary      string `json:"summary,omitempty"`
	TokensBefore int    `json:"tokens_before"`
	TokensAfter  int    `json:"tokens_after"`
}

// Event is one session event — the unit of both the persisted transcript and
// the live turn stream. Events are JSON-serializable and carry no runtime
// (model) types: the same struct serves the store mapping and the UI bridge.
// Exactly the payload fields matching Kind are set.
type Event struct {
	// ID is the store-assigned event id; empty for live-only events that are
	// never persisted (text and reasoning deltas). Persisted events carry it
	// on their live form too, so consumers can correlate the live stream
	// with the replay.
	ID string `json:"id,omitempty"`
	// Seq is the store-assigned per-session sequence. It is filled on
	// replay (Events) and always zero on the live stream — the store does
	// not report sequences back on append.
	Seq        int64     `json:"seq,omitempty"`
	TurnID     string    `json:"turn_id,omitempty"`
	Kind       Kind      `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`

	// Text carries the kind's text payload: the user message text, a text
	// delta, the final assistant message, or the reasoning text.
	Text string `json:"text,omitempty"`

	// Attachments carries the user message's attachment metadata
	// (KindUserMessage only); persisted for replay (design D5).
	Attachments []Attachment `json:"attachments,omitempty"`

	// VariantOf marks a regenerated turn's events with the original turn's id
	// (design D7): reply events carry it so replay reconstructs the branch
	// picker, and the regeneration's live user_message marker carries it in
	// place of a persisted duplicate prompt.
	VariantOf string `json:"variant_of,omitempty"`

	// Origin names who started the turn (KindUserMessage only): the resolved
	// send origin — user, scheduler, or telegram per internal/agents/hooks —
	// stamped when the prompt persisted. Empty on records persisted before
	// origins were recorded and on every other kind; an empty origin reads
	// as a user turn.
	Origin string `json:"origin,omitempty"`

	// Tool carries the kind's tool-call payload (KindToolActivity); Task the
	// delegated-task payload (KindTaskActivity).
	Tool  *ToolActivity `json:"tool,omitempty"`
	Task  *TaskActivity `json:"task,omitempty"`
	Usage *Usage        `json:"usage,omitempty"`
	// Artifacts carries the files the turn produced under the workspace's
	// artifacts/ directory (KindTurnCompleted only; add-turn-artifacts design
	// D4): path-sorted, capped at MaxArtifacts, with ArtifactsTruncated
	// reporting a hit cap.
	Artifacts          []Artifact  `json:"artifacts,omitempty"`
	ArtifactsTruncated bool        `json:"artifacts_truncated,omitempty"`
	Error              string      `json:"error,omitempty"`
	Compaction         *Compaction `json:"compaction,omitempty"`
	// Approval carries the approval payload: the pending dangerous-command
	// request (KindApprovalRequired) or the decision (KindApprovalResolved).
	Approval *Approval `json:"approval,omitempty"`
	// Input carries the browser-attention payload: the pending secret/takeover
	// request (KindInputRequired) or the resolution receipt
	// (KindInputResolved). The value a user types NEVER rides this payload —
	// the resolved receipt records its length only (design D3).
	Input *Input `json:"input,omitempty"`
	// Plan carries the task-plan snapshot (KindPlanActivity).
	Plan *PlanSnapshot `json:"plan,omitempty"`
}

// PlanSnapshot is the agent's whole task plan at one moment, in plan order.
// It mirrors the plantask tools' records so the UI can render the plan's
// progress without parsing tool results.
type PlanSnapshot struct {
	Tasks []PlanTask `json:"tasks"`
}

// PlanTask is one plan item. Status is the plantask vocabulary ("pending",
// "in_progress", "completed"); deleted tasks are not snapshotted. A pending
// task with a non-empty BlockedBy is waiting on other tasks.
type PlanTask struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	ActiveForm  string   `json:"active_form,omitempty"`
	BlockedBy   []string `json:"blocked_by,omitempty"`
}

// Approval is the payload of the approval events: the dangerous command a
// turn paused on (KindApprovalRequired) and the decision recorded for it
// (KindApprovalResolved). Exactly the fields matching the kind are set.
type Approval struct {
	// InterruptID is the runtime-assigned identity of the tool interrupt —
	// the resume target key.
	InterruptID string `json:"interrupt_id"`
	// Command is the requested shell command, as the model issued it (mount
	// prefixes already translated).
	Command string `json:"command,omitempty"`
	// CheckpointID is the persisted run checkpoint the approval pauses on;
	// resumption restores the run from it.
	CheckpointID string `json:"checkpoint_id,omitempty"`
	// Outcome is the decision (KindApprovalResolved only): approved, denied,
	// or cancelled.
	Outcome string `json:"outcome,omitempty"`
}

// Input is the payload of the browser-attention events: the secret or
// takeover request a turn parked on (KindInputRequired) and the resolution
// receipt recorded for it (KindInputResolved). Exactly the fields matching
// the kind (and the attention kind) are set. The struct is receipt-safe by
// construction: there is no field a secret's value could occupy (design D3 —
// the value lives only in the parent's in-memory pending map and the child's
// fill frame).
type Input struct {
	// InterruptID is the runtime-assigned identity of the attention request —
	// the ResolveInput/BeginTakeover/ResolveAttention target key.
	InterruptID string `json:"interrupt_id"`
	// Prompt is the card text shown to the user (KindInputRequired only).
	// Composed by the runtime from the request's origin — it never carries
	// any value.
	Prompt string `json:"prompt,omitempty"`
	// Method is the secret's submission method — password, otp, or
	// authenticator, as the agent requested (KindInputRequired only).
	Method string `json:"method,omitempty"`
	// Digits is the OTP length the card renders digit boxes for
	// (KindInputRequired only, method-dependent).
	Digits int `json:"digits,omitempty"`
	// Origin is the HTTPS origin of the sign-in page (KindInputRequired
	// only).
	Origin string `json:"origin,omitempty"`
	// AttentionKind selects the card: AttentionSecret or AttentionTakeover
	// (KindInputRequired only).
	AttentionKind string `json:"attention_kind,omitempty"`
	// Username is the account name the agent passed for the sign-in
	// (KindInputRequired only, optional).
	Username string `json:"username,omitempty"`
	// Identifier marks that the card also collects a username-or-email
	// identifier beside the secret — the agent declared an identifierTarget
	// (KindInputRequired only). The typed identifier itself never enters the
	// event; it rides the delivery like the value.
	Identifier bool `json:"identifier,omitempty"`
	// LoginID is a saved-vault login id the agent referenced
	// (KindInputRequired only, optional).
	LoginID string `json:"login_id,omitempty"`
	// Outcome is the resolution (KindInputResolved only): completed,
	// cancelled, expired, or failed.
	Outcome string `json:"outcome,omitempty"`
	// Length is the REDACTED length of the submitted value — the receipt's
	// only trace of what the user typed (KindInputResolved only; a completed
	// secret delivery never records the value itself).
	Length int `json:"length,omitempty"`
	// SavedLoginID is the vault login id the value was saved under, when the
	// user checked save-to-vault (KindInputResolved only, optional).
	SavedLoginID string `json:"saved_login_id,omitempty"`
}

// CheckpointVersionAgenticV1 marks a checkpoint payload formatted for the
// agentic runtime (design D2). Historical unmarked checkpoints are legacy.
const CheckpointVersionAgenticV1 = "agentic-v1"

// Checkpoint is a compaction checkpoint: the summary replacing the session's
// older working context, and the log boundary (UpToSeq) it summarizes — every
// event at or below UpToSeq is represented by Summary in the model's working
// context. The persisted transcript itself is never truncated: the checkpoint
// affects only what the model sees.
type Checkpoint struct {
	Version      string    `json:"version,omitempty"`
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	Summary      string    `json:"summary"`
	UpToSeq      int64     `json:"up_to_seq"`
	TokensBefore int       `json:"tokens_before"`
	TokensAfter  int       `json:"tokens_after"`
	CreatedAt    time.Time `json:"created_at"`
}
