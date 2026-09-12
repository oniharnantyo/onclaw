package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ChannelStore manages workspace-scoped channels (design:
// integrate-agent-channels D5/D6): the room, its heterogeneous user/agent
// membership roster, and the shared feed of attributed messages.
//
// Tenant isolation: every method carries a workspaceID predicate — no query
// runs without it — and a channel belonging to another workspace is
// indistinguishable from an unknown id (domain.ErrNotFound). Member and
// message operations additionally require the referenced channel to exist in
// that workspace; references to users/agents outside the workspace are
// rejected (domain.ErrNotFound), mirroring the FK-plus-precheck precedent.
//
// Slug uniqueness is case-insensitive within a workspace; violations return
// domain.ErrChannelSlugConflict. Adding a user/agent that is already a member
// of the channel returns domain.ErrDuplicateChannelMember.
//
// Message seq values are assigned by the store (per channel, monotonically
// increasing from 1) and written back onto the inserted message; feed reads
// are cursor-paginated on that seq.
type ChannelStore interface {
	// ----- Channels -----

	CreateChannel(ctx context.Context, channel *domain.Channel) error
	ChannelByID(ctx context.Context, workspaceID, id string) (*domain.Channel, error)
	// ChannelBySlug resolves a channel by slug within a workspace. Slug
	// matching is case-insensitive, matching the uniqueness rule.
	ChannelBySlug(ctx context.Context, workspaceID, slug string) (*domain.Channel, error)
	ListChannels(ctx context.Context, workspaceID string) ([]domain.Channel, error)
	// UpdateChannel replaces the channel's editable fields (name, slug,
	// purpose, conventions). created_by and created_at persist; they are not
	// writable through Update.
	UpdateChannel(ctx context.Context, channel *domain.Channel) error
	DeleteChannel(ctx context.Context, workspaceID, id string) error

	// ----- Membership -----

	// AddChannelMember attaches one user or agent to the channel. The
	// referenced user must be a member of the workspace and the referenced
	// agent must belong to it; a duplicate roster row (same channel and
	// reference) returns domain.ErrDuplicateChannelMember.
	AddChannelMember(ctx context.Context, member *domain.ChannelMember) error
	RemoveChannelMember(ctx context.Context, workspaceID, channelID, memberID string) error
	// UpdateChannelMemberSpecialization rewrites the per-channel role note;
	// the row itself (type, reference, timestamps) is untouched.
	UpdateChannelMemberSpecialization(ctx context.Context, workspaceID, channelID, memberID, specialization string) error
	// UpdateChannelMemberRole sets the roster row's channel role
	// (channel-teams D2: member or facilitator). Promoting a second
	// facilitator returns domain.ErrChannelFacilitatorExists (promoting the
	// row that already holds the role is a no-op); demoting to member always
	// succeeds. Unknown rows return domain.ErrNotFound.
	UpdateChannelMemberRole(ctx context.Context, workspaceID, channelID, memberID string, role domain.ChannelMemberRole) error
	// ListChannelMembers returns the roster in add order (added_at, id).
	ListChannelMembers(ctx context.Context, workspaceID, channelID string) ([]domain.ChannelMember, error)
	// ChannelMemberByRef resolves the roster row for a (channel, type,
	// user-or-agent id) triple; unknown triples return domain.ErrNotFound.
	// Callers use it for duplicate detection before AddChannelMember.
	ChannelMemberByRef(ctx context.Context, workspaceID, channelID string, memberType domain.ChannelMemberType, refID string) (*domain.ChannelMember, error)

	// ----- Messages -----

	// InsertChannelMessage appends one message to the channel feed and writes
	// the store-assigned seq back onto it. Mentions persist as a jsonb list;
	// a nil RunSummary persists as SQL NULL and reads back as nil.
	InsertChannelMessage(ctx context.Context, message *domain.ChannelMessage) error
	// ListChannelMessages returns the feed in ascending seq order (oldest
	// first), strictly after params.AfterSeq (0 selects from the beginning);
	// params.Limit <= 0 returns all matching messages.
	ListChannelMessages(ctx context.Context, params ListChannelMessagesParams) ([]domain.ChannelMessage, error)
	// ChannelMessagesByRoot returns the reply chain under a root message in
	// ascending seq order. A root without replies yields an empty slice —
	// absence is a normal state, not an error.
	ChannelMessagesByRoot(ctx context.Context, workspaceID, channelID, rootMessageID string) ([]domain.ChannelMessage, error)
	// SetChannelMessageWorkSession stamps a persisted feed message with its
	// work session (channel-teams D1). The kickoff path needs it because the
	// schema's FKs point both ways — the message must exist before the
	// session row (root_message_id), and work_session_id must point at an
	// existing session — so the message is inserted first and stamped after
	// the session is created. Unknown message or session returns
	// domain.ErrNotFound. Passing nil clears the link (the ON DELETE SET NULL
	// parity).
	SetChannelMessageWorkSession(ctx context.Context, workspaceID, channelID, messageID string, workSessionID *string) error
	// UpdateChannelMessageRunSummary writes a finished run's footprint onto
	// every feed message linked to the (sessionID, turnID) pair in the
	// channel — the messages the run posted through the chokepoint
	// (integrate-agent-channels D9). An empty turnID matches rows whose
	// turn_id is NULL (posts that raced turn-id discovery on the stream);
	// an empty sessionID is domain.ErrInvalid — without it the update would
	// not be run-scoped. No match is not an error: a run that posted nothing
	// has no rows to annotate.
	UpdateChannelMessageRunSummary(ctx context.Context, workspaceID, channelID, sessionID, turnID string, summary domain.RunSummary) error
}

// ListChannelMessagesParams configures a feed read.
type ListChannelMessagesParams struct {
	WorkspaceID string
	ChannelID   string
	// AfterSeq is the exclusive cursor: only messages with a greater seq are
	// returned. 0 selects from the beginning of the feed.
	AfterSeq int64
	// Limit caps the result; <= 0 means no cap.
	Limit int
}
