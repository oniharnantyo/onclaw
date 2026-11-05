package agents

import (
	"context"
	"errors"
	"sync"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The group turn seam (add-agent-groups design D3/D6): the contract between
// the agent runtime and the groups service. The groups service (internal/
// groups, built on top) implements GroupTurnSource — it owns the transcript,
// the acceptance cursors, and the in-channel tools — and hands the runtime
// ready-made packets through Manager.SubmitGroup. internal/agents never
// imports internal/groups: the source binds late through GroupTurnRef.

// ErrGroupTurnsUnavailable is returned by SubmitGroup when no group turn
// source is reachable — WithGroupTurns was not wired, or the groups service
// has not bound itself into the ref yet.
var ErrGroupTurnsUnavailable = errors.New("group turn source is not available")

// The group transcript's author kinds (add-agent-groups design D1: the
// agent_group_messages author_kind CHECK vocabulary). The source fills them
// from the store rows; the runtime renders them into the packet input.
const (
	// GroupAuthorUser marks a transcript row authored by a user.
	GroupAuthorUser = "user"
	// GroupAuthorAgent marks a transcript row authored by a member agent.
	GroupAuthorAgent = "agent"
	// GroupAuthorCoordinator marks a coordinator row (routing's assigned
	// lines).
	GroupAuthorCoordinator = "coordinator"
)

// The group transcript's message kinds (add-agent-groups design D1: the
// agent_group_messages kind CHECK vocabulary).
const (
	GroupMessageKindMessage  = "message"
	GroupMessageKindAssigned = "assigned"
	GroupMessageKindResult   = "result"
	GroupMessageKindError    = "error"
	GroupMessageKindChange   = "change"
)

// GroupTranscriptMessage is one group transcript row as the packet source
// sees it (an agent_group_messages row): its identity and position, its kind
// and author, and its content.
type GroupTranscriptMessage struct {
	// ID is the row's stable message id.
	ID string
	// Seq is the row's per-group transcript sequence.
	Seq int64
	// Kind is the row's kind — the GroupMessageKind vocabulary.
	Kind string
	// AuthorKind is the row's author kind — the GroupAuthor vocabulary.
	AuthorKind string
	// AuthorAgentID is the authoring member's agent id; empty for user and
	// coordinator rows.
	AuthorAgentID string
	// Text is the row's display text; the packet input renders it verbatim
	// under the author label.
	Text string
	// Payload is the row's raw JSON payload (design D1) for kinds whose
	// content is structured (assigned, result). Used only when Text is empty.
	Payload string
}

// GroupTaskResult is one completed task result the packet's member depends
// on (add-agent-groups design D3: shared decisions), reported through a
// group tool.
type GroupTaskResult struct {
	// TaskID is the group task's id.
	TaskID string
	// Result is the result text the owner reported.
	Result string
}

// GroupPacket is a member turn's input packet (add-agent-groups design D3):
// everything the member's turn sees, built by the group turn source from the
// group transcript alone — the member session's own log is never replayed
// into a group turn (design D2). The runtime consumes it as-is: building and
// validating the packet is the source's job.
type GroupPacket struct {
	// GroupID is the group the turn belongs to — the cursor commit's key.
	GroupID string
	// GroupTitle is the group's display title.
	GroupTitle string
	// Instructions are the group's standing instructions.
	Instructions string
	// Responsibility is the member's responsibility line (derived from the
	// agent's description by the source).
	Responsibility string
	// Request is the current request text — the routed message driving this
	// turn.
	Request string
	// Attachments are the request's attachment references, resolved by the
	// source into registered-upload metadata; content is read through the
	// same multi-part path as session attachments at input time.
	Attachments []sessions.Attachment
	// Unread holds the transcript rows past the member's acceptance cursor,
	// oldest first.
	Unread []GroupTranscriptMessage
	// TaskResults holds the completed task results the member's work depends
	// on.
	TaskResults []GroupTaskResult
	// Recent holds the bounded recent window — the last rows, read or not —
	// for immediate context, oldest first.
	Recent []GroupTranscriptMessage
	// Summary is the versioned summary of the older group history; empty
	// when none is maintained yet.
	Summary string
	// SummaryVersion is the summary's version (agent_group_summaries);
	// zero without a summary.
	SummaryVersion int
	// ThroughSeq is the transcript sequence the packet covers: the cursor
	// commits to it after the turn.
	ThroughSeq int64
	// FullPacket reports that the packet carries the group's full history —
	// the member session is new or was compacted, so the acceptance cursor is
	// rebuilt to ThroughSeq instead of advanced past a delta. Informational
	// for the runtime: input assembly and the cursor commit behave
	// identically for both packet shapes.
	FullPacket bool
}

// GroupTurnRequest is what the groups service submits for one member turn
// (add-agent-groups design D3): the member's own session — the real session
// the turn runs on (design D2) — and the ready-made packet.
type GroupTurnRequest struct {
	// SessionID is the member's group session id.
	SessionID string
	// Packet is the member turn's input packet.
	Packet GroupPacket
}

// GroupTurnSource is the groups-side half of the group turn seam
// (add-agent-groups design D3/D4): everything the runtime needs from the
// group at dispatch and during the turn. internal/groups implements it;
// internal/agents never imports internal/groups, so the source binds late
// through GroupTurnRef.
type GroupTurnSource interface {
	// GroupTurnTools returns the in-channel tools the member turn is offered
	// (add-agent-groups design D4: group_history, group_assign_task,
	// group_task_result) — eino invokable tools that slot into the same
	// registration path as the builtins. They ride BESIDE the member's
	// ordinary catalog surface, which the runner resolves exactly as for a
	// session turn (fix-agent-group-member-tools design D1): the packet
	// replaces the session history, and group_history is the shared room's
	// only reader.
	GroupTurnTools(ctx context.Context, groupID, agentID string) ([]tool.BaseTool, error)
	// CommitCursor persists the member's acceptance cursor advance to
	// throughSeq after the turn's terminal outcome (design D3). A full packet
	// rebuilds the cursor; a delta advances it — the commit is identical.
	CommitCursor(ctx context.Context, groupID, agentID string, throughSeq int64) error
}

// GroupTurnRef is the late-binding holder for the group turn source
// (add-agent-groups task 4.3): the manager is constructed before the groups
// service exists and reaches the source through the ref at dispatch time.
// The zero value is ready; Source returns nil until Set.
type GroupTurnRef struct {
	mu  sync.Mutex
	src GroupTurnSource
}

// NewGroupTurnRef returns an empty holder.
func NewGroupTurnRef() *GroupTurnRef { return &GroupTurnRef{} }

// Set binds the source; the groups service calls it once at wiring.
func (r *GroupTurnRef) Set(src GroupTurnSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.src = src
}

// Source returns the bound source, nil before Set.
func (r *GroupTurnRef) Source() GroupTurnSource {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.src
}

// WithGroupTurns wires the late-binding group turn source holder
// (add-agent-groups design D3): SubmitGroup resolves the source through the
// ref at dispatch time. Unset — the default — group dispatching is
// unavailable: SubmitGroup fails with ErrGroupTurnsUnavailable.
func WithGroupTurns(ref *GroupTurnRef) Option {
	return func(m *manager) {
		if ref != nil {
			m.groupTurns = ref
		}
	}
}
