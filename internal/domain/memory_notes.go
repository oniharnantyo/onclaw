package domain

import (
	"errors"
	"fmt"
	"time"
)

// Memory visibility tiers (integrate-agent-zero-memory D4), the within-tenant
// scope axis on every memory_events / memory_notes row. Narrowness is total:
// agent (one agent) < user (one member) < shared (tenant-wide). Promotion may
// widen by humans only; nothing narrows silently.
type MemoryVisibility string

const (
	MemoryVisibilityAgent  MemoryVisibility = "agent"
	MemoryVisibilityUser   MemoryVisibility = "user"
	MemoryVisibilityShared MemoryVisibility = "shared"
)

// Memory origins (integrate-agent-zero-memory D5), the first member of the
// provenance birth tuple. A transcript claiming things learns dialogue/infer
// provenance — never manual — making the tuple simultaneously the citation
// target and the injection-defense audit trail.
type MemoryOrigin string

const (
	MemoryOriginManual   MemoryOrigin = "manual"
	MemoryOriginDialogue MemoryOrigin = "dialogue"
	MemoryOriginInfer    MemoryOrigin = "infer"
	MemoryOriginDoc      MemoryOrigin = "doc"
)

var (
	// ErrMemoryVisibilityExceeded is returned (wrapped, naming both tiers)
	// when a write proposes a visibility wider than the source session's
	// ceiling (D4: ceilings are enforced in op validation, never in prompts —
	// a DM can birth at most user-visibility facts, a scheduled run at most
	// agent-visibility).
	ErrMemoryVisibilityExceeded = errors.New("memory visibility exceeds the session ceiling")

	// ErrMemoryProvenanceIncomplete is returned (wrapped, naming the missing
	// member) when a memory write lacks its complete birth tuple
	// (origin, event_time, learned_at, source_event_id) — no write without
	// provenance, and no backfill path exists.
	ErrMemoryProvenanceIncomplete = errors.New("memory provenance incomplete")
)

// memoryVisibilityRank orders the tiers narrowest (agent) to widest (shared);
// ok is false for values outside the CHECK-constrained set.
func memoryVisibilityRank(v MemoryVisibility) (rank int, ok bool) {
	switch v {
	case MemoryVisibilityAgent:
		return 0, true
	case MemoryVisibilityUser:
		return 1, true
	case MemoryVisibilityShared:
		return 2, true
	default:
		return 0, false
	}
}

// ValidMemoryVisibility reports whether v is one of the three tiers.
func ValidMemoryVisibility(v MemoryVisibility) bool {
	_, ok := memoryVisibilityRank(v)
	return ok
}

// ValidMemoryOrigin reports whether o is one of the four origins.
func ValidMemoryOrigin(o MemoryOrigin) bool {
	switch o {
	case MemoryOriginManual, MemoryOriginDialogue, MemoryOriginInfer, MemoryOriginDoc:
		return true
	default:
		return false
	}
}

// ValidateMemoryVisibilityWithin enforces the visibility ceiling (D4): the
// proposed tier must be at most as wide as the ceiling inherited from the
// source session's shape. Unknown tiers on either side are invalid input.
func ValidateMemoryVisibilityWithin(ceiling, proposed MemoryVisibility) error {
	ceilingRank, ok := memoryVisibilityRank(ceiling)
	if !ok {
		return fmt.Errorf("%w: unknown memory visibility ceiling %q", ErrInvalid, ceiling)
	}
	proposedRank, ok := memoryVisibilityRank(proposed)
	if !ok {
		return fmt.Errorf("%w: unknown memory visibility %q", ErrInvalid, proposed)
	}
	if proposedRank > ceilingRank {
		return fmt.Errorf("%w: proposed %q is wider than the session ceiling %q", ErrMemoryVisibilityExceeded, proposed, ceiling)
	}
	return nil
}

// ValidateMemoryProvenance enforces birth-tuple completeness (D5): a valid
// origin, both timestamps set, and a non-empty evidence pointer into
// session_events. Rows without the complete tuple must not be written.
func ValidateMemoryProvenance(origin MemoryOrigin, eventTime, learnedAt time.Time, sourceEventID string) error {
	if !ValidMemoryOrigin(origin) {
		return fmt.Errorf("%w: unknown memory origin %q", ErrMemoryProvenanceIncomplete, origin)
	}
	if eventTime.IsZero() {
		return fmt.Errorf("%w: event_time is unset", ErrMemoryProvenanceIncomplete)
	}
	if learnedAt.IsZero() {
		return fmt.Errorf("%w: learned_at is unset", ErrMemoryProvenanceIncomplete)
	}
	if sourceEventID == "" {
		return fmt.Errorf("%w: source_event_id is empty", ErrMemoryProvenanceIncomplete)
	}
	return nil
}

// ValidateMemoryEventOwner mirrors the memory_events owner CHECK: the user
// column is set exactly when the tier is user (for the agent tier the
// producing agent is the owner, and its ID is a required column).
func ValidateMemoryEventOwner(visibility MemoryVisibility, userID *string) error {
	if !ValidMemoryVisibility(visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", ErrInvalid, visibility)
	}
	if visibility == MemoryVisibilityUser && (userID == nil || *userID == "") {
		return fmt.Errorf("%w: user-visibility events require a user owner", ErrInvalid)
	}
	if visibility != MemoryVisibilityUser && userID != nil {
		return fmt.Errorf("%w: %s-visibility events must not carry a user owner", ErrInvalid, visibility)
	}
	return nil
}

// ValidateMemoryNoteOwner mirrors the memory_notes owner CHECK: exactly one
// owner column set for user/agent rows, none for shared rows.
func ValidateMemoryNoteOwner(visibility MemoryVisibility, userID, agentID *string) error {
	if !ValidMemoryVisibility(visibility) {
		return fmt.Errorf("%w: unknown memory visibility %q", ErrInvalid, visibility)
	}
	hasUser := userID != nil && *userID != ""
	hasAgent := agentID != nil && *agentID != ""
	switch visibility {
	case MemoryVisibilityUser:
		if !hasUser || hasAgent {
			return fmt.Errorf("%w: user-visibility notes require a user owner and no agent owner", ErrInvalid)
		}
	case MemoryVisibilityAgent:
		if !hasAgent || hasUser {
			return fmt.Errorf("%w: agent-visibility notes require an agent owner and no user owner", ErrInvalid)
		}
	case MemoryVisibilityShared:
		if hasUser || hasAgent {
			return fmt.Errorf("%w: shared-visibility notes must not carry an owner", ErrInvalid)
		}
	}
	return nil
}

// MemoryVisibleTo is the structural visibility predicate (D8) every memory
// read applies in its WHERE clause: the visible set is shared rows, the
// viewer's own user rows, and the serving agent's rows. ownerAgentID is the
// agent owner for the agent tier (the producing agent for events, the owner
// column for notes). Cross-member exclusion happens here — never by
// post-filtering or prompt policing.
func MemoryVisibleTo(visibility MemoryVisibility, ownerUserID, ownerAgentID *string, viewerUserID, servingAgentID string) bool {
	switch visibility {
	case MemoryVisibilityShared:
		return true
	case MemoryVisibilityUser:
		return ownerUserID != nil && *ownerUserID != "" && *ownerUserID == viewerUserID
	case MemoryVisibilityAgent:
		return ownerAgentID != nil && *ownerAgentID != "" && *ownerAgentID == servingAgentID
	default:
		return false
	}
}

// MemoryParticipant names one entity present in a gist's window; the gister's
// participant rule (no human → agent, one human → user, ≥2 humans → shared)
// reads the human count from this list, not from the transcript.
type MemoryParticipant struct {
	Kind string `json:"kind"` // "user" | "agent"
	ID   string `json:"id"`
}

// MemoryEvent is one episodic gist row (D3): the summarized window of a
// session since the previous gist. AgentID is the producing agent and doubles
// as the owner for agent-visibility rows. TurnID is the turn whose run end
// triggered the gisting — the chip's per-turn attribution.
type MemoryEvent struct {
	ID            string              `json:"id"`
	WorkspaceID   string              `json:"workspace_id"`
	AgentID       string              `json:"agent_id"`
	SessionID     string              `json:"session_id"`
	TurnID        string              `json:"turn_id"`
	Visibility    MemoryVisibility    `json:"visibility"`
	UserID        *string             `json:"user_id"` // set iff visibility=user
	Origin        MemoryOrigin        `json:"origin"`
	EventTime     time.Time           `json:"event_time"`
	LearnedAt     time.Time           `json:"learned_at"`
	SourceEventID string              `json:"source_event_id"`
	Description   string              `json:"description"`
	Outcome       string              `json:"outcome"`
	Participants  []MemoryParticipant `json:"participants"`
	TombstonedAt  *time.Time          `json:"tombstoned_at"`
}

// MemoryNote is one curated semantic fact (the paper's HDM tier) with its
// provenance birth tuple, non-destructive update pointers (supersedes /
// superseded_by), and the audited human promotion record. Owner columns are
// set exactly per the visibility tier; conflict_flag holds the doc-conflict
// review flag (D7) — the pipeline never edits the kept documents.
type MemoryNote struct {
	ID            string           `json:"id"`
	WorkspaceID   string           `json:"workspace_id"`
	Visibility    MemoryVisibility `json:"visibility"`
	UserID        *string          `json:"user_id"`  // set iff visibility=user
	AgentID       *string          `json:"agent_id"` // set iff visibility=agent
	Origin        MemoryOrigin     `json:"origin"`
	EventTime     time.Time        `json:"event_time"`
	LearnedAt     time.Time        `json:"learned_at"`
	SourceEventID string           `json:"source_event_id"`
	Content       string           `json:"content"`
	Importance    int              `json:"importance"`
	Pinned        bool             `json:"pinned"`
	Topic         *string          `json:"topic"`
	ConflictFlag  *string          `json:"conflict_flag"`
	Supersedes    *string          `json:"supersedes"`
	SupersededBy  *string          `json:"superseded_by"`
	PromotedBy    *string          `json:"promoted_by"`
	PromotedAt    *time.Time       `json:"promoted_at"`
	TombstonedAt  *time.Time       `json:"tombstoned_at"`
}
