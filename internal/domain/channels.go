package domain

import (
	"fmt"
	"strings"
	"time"
)

// ChannelMemberType discriminates the two roster/author kinds sharing a
// channel: human users and agents.
type ChannelMemberType string

// Channel member and message author types. A channel member is exactly one of
// the two; the same discriminator drives message authorship.
const (
	ChannelMemberTypeUser  ChannelMemberType = "user"
	ChannelMemberTypeAgent ChannelMemberType = "agent"
)

// ErrChannelSlugConflict indicates a channel slug is already used by another
// channel within the same workspace (case-insensitive). It chains to
// ErrConflict so generic conflict mapping keeps working.
var ErrChannelSlugConflict = fmt.Errorf("%w: channel slug already taken", ErrConflict)

// ErrDuplicateChannelMember indicates the user or agent is already a member of
// the channel. It chains to ErrConflict so generic conflict mapping keeps
// working.
var ErrDuplicateChannelMember = fmt.Errorf("%w: channel member already exists", ErrConflict)

// ErrChannelFacilitatorExists indicates a channel already has a facilitator
// and a second one was proposed (channel-teams D2: at most one per channel,
// backed by the partial unique index). It chains to ErrConflict.
var ErrChannelFacilitatorExists = fmt.Errorf("%w: channel already has a facilitator", ErrConflict)

// ErrWorkSessionActive indicates a kickoff was attempted while the channel
// still has a non-closed work session (channel-teams D1: at most one per
// channel). It chains to ErrConflict.
var ErrWorkSessionActive = fmt.Errorf("%w: channel already has an active work session", ErrConflict)

// ErrWorkSessionClosed indicates a lifecycle transition was attempted on a
// closed work session — closed is terminal. It chains to ErrConflict.
var ErrWorkSessionClosed = fmt.Errorf("%w: work session is closed", ErrConflict)

// ErrWorkSessionNotOpen indicates a pause or hop consumption raced a
// transition: the session is no longer open and the first transition won.
// It chains to ErrConflict.
var ErrWorkSessionNotOpen = fmt.Errorf("%w: work session is not open", ErrConflict)

// ErrWorkSessionHopUnavailable indicates a hop could not be consumed: the
// session is not open or hops_used has reached the budget. It chains to
// ErrConflict so the budget-exhaustion pause path can distinguish it from
// store failures.
var ErrWorkSessionHopUnavailable = fmt.Errorf("%w: work session hop unavailable", ErrConflict)

// ErrNotFacilitator indicates a non-facilitator attempted a facilitator-only
// power (closing a work session). It chains to ErrForbidden.
var ErrNotFacilitator = fmt.Errorf("%w: only the channel facilitator may close a work session", ErrForbidden)

// ChannelMemberRole is the per-channel role of one roster member
// (channel-teams D2): members participate; the single facilitator owns the
// kickoff summon, the stall watchdog summon, and session.close.
type ChannelMemberRole string

// Channel member roles. The zero value means member — Validate normalizes it.
const (
	ChannelMemberRoleMember      ChannelMemberRole = "member"
	ChannelMemberRoleFacilitator ChannelMemberRole = "facilitator"
)

// Channel is a team room inside a workspace where humans and agents share one
// attributed feed (design: integrate-agent-channels D6). The slug is the URL
// and #handle form of the name.
type Channel struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Purpose     string    `json:"purpose"`
	Conventions string    `json:"conventions"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Validate checks the channel structurally: workspace scope present, display
// name non-blank, and the slug a valid kebab-case handle.
func (c *Channel) Validate() error {
	if c == nil {
		return ErrInvalid
	}
	if c.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalid)
	}
	return ValidateChannelSlug(c.Slug)
}

// ValidateChannelSlug validates a channel slug: 1-63 characters, lowercase
// kebab-case (lowercase alphanumeric with interior hyphens), matching the
// shared slug shape used for workspace and agent slugs.
func ValidateChannelSlug(slug string) error {
	if len(slug) < 1 || len(slug) > 63 {
		return fmt.Errorf("%w: slug must be between 1 and 63 characters", ErrInvalid)
	}
	if !slugRegex.MatchString(slug) {
		return fmt.Errorf("%w: slug must be kebab-case (lowercase alphanumeric with interior hyphens)", ErrInvalid)
	}
	return nil
}

// ChannelMember is one heterogeneous roster row: a human user or an agent
// attached to a channel, with an optional per-channel specialization note
// (design D5) and a per-channel role (channel-teams D2: at most one
// facilitator per channel). Exactly one of UserID / AgentID is set.
type ChannelMember struct {
	ID             string            `json:"id"`
	WorkspaceID    string            `json:"workspace_id"`
	ChannelID      string            `json:"channel_id"`
	MemberType     ChannelMemberType `json:"member_type"`
	UserID         string            `json:"user_id,omitempty"`
	AgentID        string            `json:"agent_id,omitempty"`
	Specialization string            `json:"specialization"`
	Role           ChannelMemberRole `json:"role"`
	AddedAt        time.Time         `json:"added_at"`
}

// Validate checks the member row structurally: scopes present, member type
// one of the allowed constants, and exactly one of UserID / AgentID set.
func (m *ChannelMember) Validate() error {
	if m == nil {
		return ErrInvalid
	}
	if m.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if m.ChannelID == "" {
		return fmt.Errorf("%w: channel id cannot be empty", ErrInvalid)
	}
	switch m.MemberType {
	case ChannelMemberTypeUser:
		if m.UserID == "" {
			return fmt.Errorf("%w: user_id is required for %s members", ErrInvalid, ChannelMemberTypeUser)
		}
		if m.AgentID != "" {
			return fmt.Errorf("%w: exactly one of user_id or agent_id must be set", ErrInvalid)
		}
	case ChannelMemberTypeAgent:
		if m.AgentID == "" {
			return fmt.Errorf("%w: agent_id is required for %s members", ErrInvalid, ChannelMemberTypeAgent)
		}
		if m.UserID != "" {
			return fmt.Errorf("%w: exactly one of user_id or agent_id must be set", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: member_type %q must be %q or %q", ErrInvalid, m.MemberType, ChannelMemberTypeUser, ChannelMemberTypeAgent)
	}
	// An absent role normalizes to member; only the two fixed roles are
	// accepted (channel-teams D2).
	if m.Role == "" {
		m.Role = ChannelMemberRoleMember
	}
	switch m.Role {
	case ChannelMemberRoleMember, ChannelMemberRoleFacilitator:
	default:
		return fmt.Errorf("%w: role %q must be %q or %q", ErrInvalid, m.Role, ChannelMemberRoleMember, ChannelMemberRoleFacilitator)
	}
	return nil
}

// RefID returns the row's single reference id (UserID or AgentID according to
// the member type).
func (m *ChannelMember) RefID() string {
	if m == nil {
		return ""
	}
	if m.MemberType == ChannelMemberTypeAgent {
		return m.AgentID
	}
	return m.UserID
}

// RunSummary is the run footprint written onto an agent-authored feed message
// at run finish (design D9): tool name -> call count plus total duration.
// Absent summaries are nil pointers on the message — never empty objects.
type RunSummary struct {
	Tools      map[string]int `json:"tools,omitempty"`
	DurationMS int64          `json:"duration_ms"`
}

// Mention is one resolved @handle reference stored on a channel message's
// mentions list ([{"type","id","handle"}]). Type is the same user|agent
// discriminator as membership; ID is the referenced user/agent id and Handle
// is the slug-form handle as it appeared in the body.
type Mention struct {
	Type   ChannelMemberType `json:"type"`
	ID     string            `json:"id"`
	Handle string            `json:"handle"`
}

// Validate checks the mention structurally.
func (m Mention) Validate() error {
	switch m.Type {
	case ChannelMemberTypeUser, ChannelMemberTypeAgent:
	default:
		return fmt.Errorf("%w: mention type %q must be %q or %q", ErrInvalid, m.Type, ChannelMemberTypeUser, ChannelMemberTypeAgent)
	}
	if m.ID == "" {
		return fmt.Errorf("%w: mention id cannot be empty", ErrInvalid)
	}
	if m.Handle == "" {
		return fmt.Errorf("%w: mention handle cannot be empty", ErrInvalid)
	}
	return nil
}

// ChannelMessage is one row of the shared channel feed: an attributed
// utterance from a human or an agent, optionally linked to the run that
// produced it and to the chain of replies under a root message (design D4/D6),
// and to the work session it belongs to (channel-teams D1).
type ChannelMessage struct {
	ID            string            `json:"id"`
	WorkspaceID   string            `json:"workspace_id"`
	ChannelID     string            `json:"channel_id"`
	Seq           int64             `json:"seq"`
	AuthorType    ChannelMemberType `json:"author_type"`
	AuthorUserID  string            `json:"author_user_id,omitempty"`
	AuthorAgentID string            `json:"author_agent_id,omitempty"`
	Body          string            `json:"body"`
	Mentions      []Mention         `json:"mentions"`
	SessionID     *string           `json:"session_id,omitempty"`
	TurnID        *string           `json:"turn_id,omitempty"`
	RunSummary    *RunSummary       `json:"run_summary,omitempty"`
	RootMessageID *string           `json:"root_message_id,omitempty"`
	ChainDepth    int               `json:"chain_depth"`
	// WorkSessionID links the message to the work session it was posted
	// under (channel-teams D1); nil outside sessions. IsKickoff flags the
	// human-posted message a session was kicked off from.
	WorkSessionID *string   `json:"work_session_id,omitempty"`
	IsKickoff     bool      `json:"is_kickoff"`
	CreatedAt     time.Time `json:"created_at"`
}

// Validate checks the message structurally: scopes present, author type one
// of the allowed constants with exactly one author reference, non-empty body,
// non-negative chain depth, and well-formed resolved mentions.
func (msg *ChannelMessage) Validate() error {
	if msg == nil {
		return ErrInvalid
	}
	if msg.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if msg.ChannelID == "" {
		return fmt.Errorf("%w: channel id cannot be empty", ErrInvalid)
	}
	switch msg.AuthorType {
	case ChannelMemberTypeUser:
		if msg.AuthorUserID == "" {
			return fmt.Errorf("%w: author_user_id is required for %s authors", ErrInvalid, ChannelMemberTypeUser)
		}
		if msg.AuthorAgentID != "" {
			return fmt.Errorf("%w: exactly one of author_user_id or author_agent_id must be set", ErrInvalid)
		}
	case ChannelMemberTypeAgent:
		if msg.AuthorAgentID == "" {
			return fmt.Errorf("%w: author_agent_id is required for %s authors", ErrInvalid, ChannelMemberTypeAgent)
		}
		if msg.AuthorUserID != "" {
			return fmt.Errorf("%w: exactly one of author_user_id or author_agent_id must be set", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: author_type %q must be %q or %q", ErrInvalid, msg.AuthorType, ChannelMemberTypeUser, ChannelMemberTypeAgent)
	}
	if strings.TrimSpace(msg.Body) == "" {
		return fmt.Errorf("%w: body cannot be empty", ErrInvalid)
	}
	if msg.ChainDepth < 0 {
		return fmt.Errorf("%w: chain_depth cannot be negative", ErrInvalid)
	}
	for _, mention := range msg.Mentions {
		if err := mention.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Work sessions (channel-teams D1): a bounded, human-gated engagement with a
// lifecycle, a hop budget, and a single owner of termination.
// -------------------------------------------------------------------------

// WorkSessionStatus is the lifecycle state of a work session. Transitions are
// validated state changes: open→paused, paused→open, {open,paused}→closed;
// closed is terminal (WorkSessionStatus.CanTransition).
type WorkSessionStatus string

// Work session lifecycle states.
const (
	WorkSessionOpen   WorkSessionStatus = "open"
	WorkSessionPaused WorkSessionStatus = "paused"
	WorkSessionClosed WorkSessionStatus = "closed"
)

// CanTransition reports whether the status change is a legal lifecycle move:
// open→paused, paused→open, {open,paused}→closed. Closed is terminal.
func (s WorkSessionStatus) CanTransition(to WorkSessionStatus) bool {
	switch s {
	case WorkSessionOpen:
		return to == WorkSessionPaused || to == WorkSessionClosed
	case WorkSessionPaused:
		return to == WorkSessionOpen || to == WorkSessionClosed
	default: // closed is terminal; unknown states transition nowhere
		return false
	}
}

// WorkSessionPauseReason records why a session paused. A pause is always
// lifted by a human post or ended by the facilitator's close.
type WorkSessionPauseReason string

// Pause reasons: the agent team tagged a human and awaits a reply, or the hop
// budget ran out and the facilitator must post a status.
const (
	WorkSessionPauseAwaitingHuman   WorkSessionPauseReason = "awaiting-human"
	WorkSessionPauseBudgetExhausted WorkSessionPauseReason = "budget-exhausted"
)

// DefaultWorkSessionBudget is the default hop budget of a work session
// (channel-teams D11): every agent run minted inside the session consumes one
// hop, the kickoff summon included.
const DefaultWorkSessionBudget = 12

// WorkSession is one bounded engagement in a channel: rooted at the kickoff
// message, charged per agent hop, pausable for humans, closable only by the
// facilitator with a stored summary.
type WorkSession struct {
	ID            string                 `json:"id"`
	WorkspaceID   string                 `json:"workspace_id"`
	ChannelID     string                 `json:"channel_id"`
	RootMessageID string                 `json:"root_message_id"`
	Goal          string                 `json:"goal"`
	Status        WorkSessionStatus      `json:"status"`
	PauseReason   WorkSessionPauseReason `json:"pause_reason,omitempty"`
	Budget        int                    `json:"budget"`
	HopsUsed      int                    `json:"hops_used"`
	Summary       string                 `json:"summary,omitempty"`
	ClosedAt      *time.Time             `json:"closed_at,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// Validate checks the session structurally: scopes and root present, a goal,
// a known status, a pause reason only while paused, and a positive budget.
func (s *WorkSession) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if s.ChannelID == "" {
		return fmt.Errorf("%w: channel id cannot be empty", ErrInvalid)
	}
	if s.RootMessageID == "" {
		return fmt.Errorf("%w: root message id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(s.Goal) == "" {
		return fmt.Errorf("%w: goal cannot be empty", ErrInvalid)
	}
	switch s.Status {
	case WorkSessionOpen:
		if s.PauseReason != "" {
			return fmt.Errorf("%w: pause_reason must be empty while %s", ErrInvalid, WorkSessionOpen)
		}
	case WorkSessionPaused:
		switch s.PauseReason {
		case WorkSessionPauseAwaitingHuman, WorkSessionPauseBudgetExhausted:
		default:
			return fmt.Errorf("%w: pause_reason %q must be %q or %q while paused", ErrInvalid, s.PauseReason, WorkSessionPauseAwaitingHuman, WorkSessionPauseBudgetExhausted)
		}
	case WorkSessionClosed:
	default:
		return fmt.Errorf("%w: status %q must be %q, %q, or %q", ErrInvalid, s.Status, WorkSessionOpen, WorkSessionPaused, WorkSessionClosed)
	}
	if s.Budget <= 0 {
		return fmt.Errorf("%w: budget must be positive", ErrInvalid)
	}
	if s.HopsUsed < 0 {
		return fmt.Errorf("%w: hops_used cannot be negative", ErrInvalid)
	}
	return nil
}
