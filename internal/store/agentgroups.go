package store

import "context"

// AgentGroupStore persists the agent-group aggregates (add-agent-groups
// design D1): the group rows, the membership roster, the shared transcript
// with per-group sequences, the per-member acceptance cursors, the
// revision-guarded task system, and the versioned transcript summary.
// Absent rows read as ErrNotFound; violated key uniqueness (member PK,
// message id, task PK) reads as ErrDuplicate. Member+lead bookkeeping is
// service logic — the store only persists what it is told.
type AgentGroupStore interface {
	// CreateGroup stores a new group, assigning ID and stamping the
	// timestamps when empty. LeadAgentID is nil until the first member
	// arrives (design D5).
	CreateGroup(ctx context.Context, g *AgentGroup) error
	// GroupByID returns the group with the given ID; ErrNotFound when
	// absent.
	GroupByID(ctx context.Context, id string) (*AgentGroup, error)
	// UpdateGroup rewrites the group's editable columns (name, title,
	// instructions, lead, archived, revision), keeping the original
	// CreatedAt; ErrNotFound when absent.
	UpdateGroup(ctx context.Context, g *AgentGroup) error
	// ListGroups returns every group ordered by creation time, then id.
	// The result is never nil.
	ListGroups(ctx context.Context) ([]AgentGroup, error)
	// DeleteGroup removes the group and — through the foreign-key
	// cascades — its members, transcript, cursors, tasks, and summary;
	// ErrNotFound when absent.
	DeleteGroup(ctx context.Context, id string) error

	// AddMember adds one roster row, stamping AddedAt when empty; the
	// member is already in the group (PK group_id, agent_id) returns
	// ErrDuplicate.
	AddMember(ctx context.Context, m AgentGroupMember) error
	// RemoveMember drops one roster row; ErrNotFound when absent.
	RemoveMember(ctx context.Context, groupID, agentID string) error
	// ListMembers returns the group's roster ordered by added time, then
	// agent id. The result is never nil.
	ListMembers(ctx context.Context, groupID string) ([]AgentGroupMember, error)

	// AppendMessage appends one transcript row, allocating the next
	// per-group Seq (MAX(seq)+1) and stamping OccurredAt when empty;
	// MessageID is assigned when empty. A MessageID already stored for
	// the group (UNIQUE group_id, message_id) returns ErrDuplicate.
	AppendMessage(ctx context.Context, m *AgentGroupMessage) error
	// UpdateMessage rewrites one transcript row in place, identified by
	// (group_id, message_id) and keeping its Seq — the regenerate path
	// (design D7); ErrNotFound when absent.
	UpdateMessage(ctx context.Context, m *AgentGroupMessage) error
	// ListMessages returns one window of the group's transcript: the
	// newest limit messages with Seq strictly below beforeSeq — or the
	// newest limit messages overall when beforeSeq is 0 — ordered by Seq
	// ascending, so the window replays in transcript order. The result is
	// empty (not nil) when the window is empty.
	ListMessages(ctx context.Context, groupID string, beforeSeq int64, limit int) ([]AgentGroupMessage, error)
	// LastSeq returns the group's highest allocated transcript Seq — 0
	// for a group without messages.
	LastSeq(ctx context.Context, groupID string) (int64, error)

	// GetCursor returns one member's acceptance cursor; ErrNotFound when
	// the member has none.
	GetCursor(ctx context.Context, groupID, agentID string) (*AgentGroupContext, error)
	// SetCursor upserts one member's acceptance cursor: an absent
	// (group_id, agent_id) pair inserts, a present pair rewrites
	// session_id, through_seq, and summary_version in place.
	SetCursor(ctx context.Context, c AgentGroupContext) error

	// CreateTask stores a new task, assigning TaskID when empty; a TaskID
	// already stored returns ErrDuplicate. State is persisted before
	// dispatch (design D8) — a crash never double-dispatches.
	CreateTask(ctx context.Context, t *AgentGroupTask) error
	// TaskByID returns the task with the given ID; ErrNotFound when
	// absent.
	TaskByID(ctx context.Context, taskID string) (*AgentGroupTask, error)
	// UpdateTask rewrites the task's mutable columns (owner, state, stop
	// reason, revision, assignment count, error), keeping the chain
	// fields (parent, root, request message); ErrNotFound when absent.
	UpdateTask(ctx context.Context, t *AgentGroupTask) error
	// ListTasksByGroup returns the group's tasks ordered by task id. The
	// result is never nil.
	ListTasksByGroup(ctx context.Context, groupID string) ([]AgentGroupTask, error)
	// ListOpenTasks returns every non-terminal task across all groups
	// (the restart drain's worklist), ordered by task id. The result is
	// never nil.
	ListOpenTasks(ctx context.Context) ([]AgentGroupTask, error)

	// GetSummary returns the group's transcript summary; ErrNotFound when
	// none exists yet.
	GetSummary(ctx context.Context, groupID string) (*AgentGroupSummary, error)
	// SaveSummary upserts the group's transcript summary: the caller
	// owns the versioning — an absent group inserts, a present group
	// rewrites version, through_seq, and text in place.
	SaveSummary(ctx context.Context, s *AgentGroupSummary) error
}
