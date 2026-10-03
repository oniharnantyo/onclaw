package domain

import (
	"fmt"
	"time"
)

// SkillCandidateStatus is the review lifecycle of a curated-skill candidate
// (add-skill-curation-from-traces D6): proposed rows wait in pending; human
// approval moves them through approved into the provisional probation;
// rejection records the reviewer reason; a probation breach or manual
// disable archives the row as disabled (evidence retained, never deleted).
// failed marks an extraction the proposer dropped after its bounded retries
// (or a manual retry that failed again): the row holds the cluster linkage
// and the error message so the reviewer can re-attempt the extraction.
type SkillCandidateStatus string

const (
	SkillCandidatePending     SkillCandidateStatus = "pending"
	SkillCandidateApproved    SkillCandidateStatus = "approved"
	SkillCandidateRejected    SkillCandidateStatus = "rejected"
	SkillCandidateProvisional SkillCandidateStatus = "provisional"
	SkillCandidateDisabled    SkillCandidateStatus = "disabled"
	SkillCandidateFailed      SkillCandidateStatus = "failed"
)

// ValidSkillCandidateStatus reports whether status is one of the six
// lifecycle states.
func ValidSkillCandidateStatus(status SkillCandidateStatus) bool {
	switch status {
	case SkillCandidatePending, SkillCandidateApproved, SkillCandidateRejected,
		SkillCandidateProvisional, SkillCandidateDisabled, SkillCandidateFailed:
		return true
	}
	return false
}

// SkillCandidate is one proposed curated skill: the model-drafted SKILL.md
// body plus its evidence chain (cluster linkage, raw-event citations, cited
// wiki patterns) and its review lifecycle state. The DB row is the system of
// record (D3); the materialized skill directory is a projection of an
// approved row.
type SkillCandidate struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	// ClusterID groups candidates and runs by task shape (the similarity
	// cluster the proposal was drafted from). A store-level string, not a
	// foreign key: clusters live in the curation pipeline, not in a table.
	ClusterID string `json:"cluster_id"`
	// SkillName is the draft's validated slug (D4: slug rules, cross-tier
	// collision checks run before the row is stored).
	SkillName string               `json:"skill_name"`
	Status    SkillCandidateStatus `json:"status"`
	// ProposedContent is the drafted SKILL.md body.
	ProposedContent string `json:"proposed_content"`
	// IsEdit marks a superseding edit of an existing curated skill
	// (D4: contradictions produce edits, never sibling skills).
	IsEdit bool `json:"is_edit"`
	// SupersedesSkillName names the curated skill this edit replaces; empty
	// for first-time proposals.
	SupersedesSkillName string `json:"supersedes_skill_name,omitempty"`
	// SupersededContent preserves the prior skill's content for versioned
	// lineage; empty for first-time proposals.
	SupersededContent string `json:"superseded_content,omitempty"`
	// EvidenceEventIDs cite the raw session events (runs, tool calls) the
	// draft was derived from; CitedPatternRefs cite the wiki pattern pages
	// it builds on. Both are stored as jsonb arrays.
	EvidenceEventIDs []string `json:"evidence_event_ids"`
	CitedPatternRefs []string `json:"cited_pattern_refs"`
	// Reason records the reviewer's rejection reason (empty unless
	// status is rejected).
	Reason string `json:"reason,omitempty"`
	// Probation tallies (D6): helpful/harmful outcome classifications and
	// total attach/use count while provisional.
	HelpfulCount int        `json:"helpful_count"`
	HarmfulCount int        `json:"harmful_count"`
	UseCount     int        `json:"use_count"`
	ProposedAt   time.Time  `json:"proposed_at"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// SkillImpactVerdict is the audit verdict of one review decision.
type SkillImpactVerdict string

const (
	SkillImpactApproved   SkillImpactVerdict = "approved"
	SkillImpactRejected   SkillImpactVerdict = "rejected"
	SkillImpactSuperseded SkillImpactVerdict = "superseded"
)

// ValidSkillImpactVerdict reports whether verdict is one of the three audit
// verdicts.
func ValidSkillImpactVerdict(verdict SkillImpactVerdict) bool {
	switch verdict {
	case SkillImpactApproved, SkillImpactRejected, SkillImpactSuperseded:
		return true
	}
	return false
}

// SkillImpactEntry is one audited review decision over a cluster/skill pair
// (D3: the audit trail lives in the store — the system of record — and is
// rendered into the proposer's prompt to suppress re-proposals). Rows are
// append-only: nothing overwrites or deletes an entry.
type SkillImpactEntry struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspace_id"`
	ClusterID   string             `json:"cluster_id"`
	SkillName   string             `json:"skill_name"`
	Verdict     SkillImpactVerdict `json:"verdict"`
	// Diff is the rendered content delta the decision was made on.
	Diff string `json:"diff,omitempty"`
	// Reason is the reviewer's reason (required for rejections).
	Reason string `json:"reason,omitempty"`
	// Reviewer names the deciding user (empty for superseded entries the
	// pipeline itself records).
	Reviewer  string    `json:"reviewer,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks the candidate's write shape: the scope tuple, draft name,
// and content are present, the status is a known lifecycle state, and an
// edit names the skill it supersedes.
func (c *SkillCandidate) Validate() error {
	if c == nil {
		return ErrInvalid
	}
	if c.WorkspaceID == "" || c.AgentID == "" || c.ClusterID == "" {
		return fmt.Errorf("%w: skill candidate needs workspace, agent, and cluster", ErrInvalid)
	}
	// A failed row records an extraction that never produced a usable draft:
	// the attempted name and the content are both allowed to be empty (the
	// error message rides Reason). Every live lifecycle state drafts both.
	if c.Status != SkillCandidateFailed && c.SkillName == "" {
		return fmt.Errorf("%w: skill candidate needs a skill name", ErrInvalid)
	}
	if c.Status != SkillCandidateFailed && c.ProposedContent == "" {
		return fmt.Errorf("%w: skill candidate needs proposed content", ErrInvalid)
	}
	if !ValidSkillCandidateStatus(c.Status) {
		return fmt.Errorf("%w: unknown skill candidate status %q", ErrInvalid, c.Status)
	}
	if c.IsEdit && c.SupersedesSkillName == "" {
		return fmt.Errorf("%w: a superseding edit must name the skill it replaces", ErrInvalid)
	}
	if !c.IsEdit && c.SupersedesSkillName != "" {
		return fmt.Errorf("%w: only a superseding edit may carry a superseded skill name", ErrInvalid)
	}
	return nil
}

// Validate checks the impact entry's write shape: the scope tuple, skill
// name, and verdict are present, and a rejection carries a reason.
func (e *SkillImpactEntry) Validate() error {
	if e == nil {
		return ErrInvalid
	}
	if e.WorkspaceID == "" || e.ClusterID == "" || e.SkillName == "" {
		return fmt.Errorf("%w: skill impact entry needs workspace, cluster, and skill name", ErrInvalid)
	}
	if !ValidSkillImpactVerdict(e.Verdict) {
		return fmt.Errorf("%w: unknown skill impact verdict %q", ErrInvalid, e.Verdict)
	}
	if e.Verdict == SkillImpactRejected && e.Reason == "" {
		return fmt.Errorf("%w: a rejection impact entry requires a reason", ErrInvalid)
	}
	return nil
}
