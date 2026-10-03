package handlers

// Skill-curation review surface (add-skill-curation-from-traces 6.1/8.2):
// the candidates queue, the approve/reject human gate, the pattern wiki
// listing, and the decision audit. Every policy decision (re-validation,
// budget, materialization, probation) lives in internal/skillcuration —
// this file is a thin translator between gin and the service methods, so
// the cycle's manual-trigger paths (task 7) reuse the exact same code.
//
// Guard contract mirrors the skills routes: reads ride skills.read (Member
// reviews the queue and the patterns); approve/reject/retry are the human
// gate — skills.write (Owner/Admin), enforced by the router's
// RequirePermission middleware. Deleted evidence never blocks: evidence ids
// are opaque strings returned to the UI for deep-linking, never resolved
// here — the deleted-source marker (source_available) is a best-effort
// LIST-side annotation only, never an approval input.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/skillcuration"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// PatternLister lists the workspace's skill-wiki pattern pages — active and
// superseded alike (superseded pages stay readable with their successor
// pointer). The composition root wires it to skillcuration's Wiki.List over
// the workspace's wiki directory; tests stub it.
type PatternLister func(ctx context.Context, tenantSlug string) ([]skillcuration.Page, error)

// SessionChecker reports whether one evidence session still exists in the
// workspace — the deleted-source marker seam (skill-curation spec, human
// approval gate: "still lists in review with a 'source no longer available'
// note"). One EXISTS-style read through the session-events store; on any
// error the handler defaults to available (the marker never blocks, and
// approval never consults it). Optional capability: unwired, every row
// reports available.
type SessionChecker func(ctx context.Context, workspaceID, sessionID string) (bool, error)

// SkillCurationCycle is the narrow consumer-side seam the cycle endpoints
// drive (add-skill-curation-from-traces 7.1, the MemoryConsolidator
// precedent): the manual trigger and the ticker share one code path — the
// same in-flight set, the same runWorkspace stages — and the status read is
// the same record both write. *skillcuration.Cycle satisfies it.
type SkillCurationCycle interface {
	RunNow(ctx context.Context, workspaceID string) error
	Status(ctx context.Context, workspaceID string) skillcuration.CycleStatus
}

// SkillCurationRetryer is the manual-retry seam (extraction-failed cards):
// one fresh draft attempt for a failed candidate's cluster through the same
// draft path the cycle runs. *skillcuration.Proposer satisfies it.
type SkillCurationRetryer interface {
	RetryCandidate(ctx context.Context, workspaceID, candidateID string) (*domain.SkillCandidate, error)
}

// SkillCurationRetryerFunc adapts a plain function to SkillCurationRetryer —
// the composition root builds the retry as a closure over the workspace
// store, config source, and model resolver (the per-workspace wiki).
type SkillCurationRetryerFunc func(ctx context.Context, workspaceID, candidateID string) (*domain.SkillCandidate, error)

// RetryCandidate implements SkillCurationRetryer.
func (f SkillCurationRetryerFunc) RetryCandidate(ctx context.Context, workspaceID, candidateID string) (*domain.SkillCandidate, error) {
	return f(ctx, workspaceID, candidateID)
}

// SkillCurationOption configures the skill-curation handlers; every knob has
// a safe (capability-absent) default.
type SkillCurationOption func(*skillCurationHandlers)

// WithSkillCurationCycle wires the cycle endpoints. Unwired, the cycle
// routes are not registered by the router and the review surface stands
// alone.
func WithSkillCurationCycle(cycle SkillCurationCycle) SkillCurationOption {
	return func(h *skillCurationHandlers) {
		if cycle != nil {
			h.cycle = cycle
		}
	}
}

// WithSessionChecker wires the deleted-source marker (best-effort list
// annotation). Unwired, every candidate reports source_available.
func WithSessionChecker(checker SessionChecker) SkillCurationOption {
	return func(h *skillCurationHandlers) {
		if checker != nil {
			h.sessions = checker
		}
	}
}

// WithSkillCurationRetry wires the retry endpoint. Unwired, the route is not
// registered by the router and failed cards stay review-only.
func WithSkillCurationRetry(retry SkillCurationRetryer) SkillCurationOption {
	return func(h *skillCurationHandlers) {
		if retry != nil {
			h.retry = retry
		}
	}
}

// skillCurationHandlers serves the review endpoints. All dependencies are
// granular; none may be nil (AGENTS.md: injected dependencies are never
// nil); the cycle, session-checker, and retry seams are optional
// capabilities.
type skillCurationHandlers struct {
	candidates store.SkillCandidateStore
	approver   *skillcuration.Approver
	patterns   PatternLister
	cycle      SkillCurationCycle
	sessions   SessionChecker
	retry      SkillCurationRetryer
}

// NewSkillCurationHandlers constructs the handlers from their granular
// dependencies: the candidate store (list/detail/audit), the approver
// service (approve/reject), and the pattern-wiki lister. The cycle seam
// (run/status) rides WithSkillCurationCycle.
func NewSkillCurationHandlers(candidates store.SkillCandidateStore, approver *skillcuration.Approver, patterns PatternLister, opts ...SkillCurationOption) *skillCurationHandlers {
	h := &skillCurationHandlers{
		candidates: candidates,
		approver:   approver,
		patterns:   patterns,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// skillCandidateView is one candidates-list row over the wire: the
// candidate plus the deleted-source marker (spec, human approval gate — a
// candidate whose first evidence session is gone "still lists in review
// with a 'source no longer available' note"). Approval never consults the
// marker: the unblocking behavior lives in the Approver, which never looks
// a session up.
type skillCandidateView struct {
	domain.SkillCandidate
	SourceAvailable bool `json:"source_available"`
}

// ListCandidates returns the workspace's review queue, optionally filtered
// by lifecycle status (?status=pending — the default review view; an empty
// status lists every state, including disabled rows awaiting re-review
// with their tallies and failed extraction rows with their retry action).
// Each row carries source_available: best-effort — only the first evidence
// session is checked, checker errors default to true, and an unwired
// checker (or a candidate without evidence) always reports available.
func (h *skillCurationHandlers) ListCandidates(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	status := domain.SkillCandidateStatus(strings.ToLower(strings.TrimSpace(c.Query("status"))))
	if status != "" && !domain.ValidSkillCandidateStatus(status) {
		RespondError(c, fmt.Errorf("%w: unknown candidate status %q", domain.ErrInvalid, status))
		return
	}

	rows, err := h.candidates.List(c.Request.Context(), ws.ID, status)
	if err != nil {
		RespondError(c, err)
		return
	}
	views := make([]skillCandidateView, 0, len(rows))
	for _, row := range rows {
		views = append(views, skillCandidateView{
			SkillCandidate:  row,
			SourceAvailable: h.sourceAvailable(c.Request.Context(), ws.ID, row),
		})
	}
	RespondOK(c, gin.H{"candidates": views, "count": len(rows)})
}

// sourceAvailable reports whether the candidate's first evidence session
// still exists. Best-effort by contract: any checker error — and any
// candidate without evidence ids — defaults to true, so the marker can only
// add information to the review queue, never block it.
func (h *skillCurationHandlers) sourceAvailable(ctx context.Context, workspaceID string, row domain.SkillCandidate) bool {
	if h.sessions == nil || len(row.EvidenceEventIDs) == 0 {
		return true
	}
	exists, err := h.sessions(ctx, workspaceID, row.EvidenceEventIDs[0])
	if err != nil {
		return true
	}
	return exists
}

// GetCandidate returns one candidate with its rendered evidence: the
// evidence event ids and cited pattern refs the UI deep-links — ids only,
// never hydrated transcripts. A deleted source session is not detectable
// (and must not block): the ids are opaque.
func (h *skillCurationHandlers) GetCandidate(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	row, err := h.candidates.Get(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{
		"candidate": row,
		"evidence": gin.H{
			"event_ids":      nonNilStrings(row.EvidenceEventIDs),
			"cited_patterns": nonNilStrings(row.CitedPatternRefs),
		},
	})
}

// approveRequest is the approve body — empty by contract (the reviewer's
// identity rides the auth context).
type approveRequest struct{}

// RejectRequest is the reject body: the reviewer-selected reason is
// REQUIRED — the four canonical reasons are a UI concern, the server
// accepts any non-empty string.
type RejectRequest struct {
	Reason string `json:"reason"`
}

// Approve materializes a pending candidate as an agent-tier skill and moves
// it into probation. Refusals: 404 unknown candidate, 409 when the
// candidate was already decided, when the approve-time collision check
// blocks, or when the catalog is at budget (the message names the remedy).
func (h *skillCurationHandlers) Approve(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req approveRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondError(c, domain.ErrInvalid)
			return
		}
	}

	row, err := h.approver.Approve(c.Request.Context(), skillcuration.ReviewRequest{
		WorkspaceID: ws.ID,
		CandidateID: c.Param("id"),
		Reviewer:    user.ID,
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"candidate": row})
}

// Reject records the reviewer's rejection. A missing or blank reason is a
// 400; the reason lands verbatim in the audit entry with the reviewer's
// identity. The wiki is untouched by design.
func (h *skillCurationHandlers) Reject(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req RejectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, fmt.Errorf("%w: a rejection requires a reason", domain.ErrInvalid))
		return
	}

	row, err := h.approver.Reject(c.Request.Context(), skillcuration.ReviewRequest{
		WorkspaceID: ws.ID,
		CandidateID: c.Param("id"),
		Reviewer:    user.ID,
	}, req.Reason)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"candidate": row})
}

// Retry re-runs extraction for one failed candidate's cluster (the
// extraction-failed card's retry action; Owner/Admin — skills.write): the
// SAME draft path the cycle runs, one model call. Success flips the row
// back to pending with the fresh draft; failure refreshes the row's error
// message and surfaces here as an error response — the card stays in the
// queue with the updated message, the queue itself never breaks. Refusals:
// 404 unknown candidate, 409 a non-failed status.
func (h *skillCurationHandlers) Retry(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	row, err := h.retry.RetryCandidate(c.Request.Context(), ws.ID, c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"candidate": row})
}

// ListPatterns returns the workspace's pattern wiki: every page — active
// and superseded — with its successor pointer and the evidence runs it
// cites. A missing wiki is the empty list.
func (h *skillCurationHandlers) ListPatterns(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	pages, err := h.patterns(c.Request.Context(), ws.Slug)
	if err != nil {
		RespondError(c, err)
		return
	}
	items := make([]gin.H, 0, len(pages))
	for _, page := range pages {
		items = append(items, gin.H{
			"slug":          page.Slug,
			"title":         page.Title,
			"status":        page.Status,
			"superseded_by": page.SupersededBy,
			"evidence_runs": nonNilStrings(page.EvidenceRuns),
			"body":          page.Body,
		})
	}
	RespondOK(c, gin.H{"patterns": items})
}

// ListAudit returns the workspace's decision audit trail (verdicts,
// reasons, reviewers, diffs), oldest first, optionally from ?since=
// (RFC 3339).
func (h *skillCurationHandlers) ListAudit(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var since time.Time
	if raw := c.Query("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			RespondError(c, fmt.Errorf("%w: since must be an RFC 3339 timestamp", domain.ErrInvalid))
			return
		}
		since = parsed
	}

	entries, err := h.candidates.ListImpactEntries(c.Request.Context(), ws.ID, since)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"entries": nonNilEntries(entries)})
}

// RunCycle is the manual trigger (spec: "Manual trigger runs a cycle now"):
// one cycle executes immediately — the SAME entry point the ticker drives,
// through the shared in-flight set — and the response carries the cycle's
// status so the caller observes running → succeeded/failed. A cycle already
// in flight maps to 409 through the wrapped domain.ErrConflict (the
// double-approve precedent); an unknown workspace maps to 404.
func (h *skillCurationHandlers) RunCycle(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	if err := h.cycle.RunNow(c.Request.Context(), ws.ID); err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"status": h.cycle.Status(c.Request.Context(), ws.ID)})
}

// CycleStatus returns the workspace's last (or current) cycle record — the
// same record both triggers write. A workspace that never ran a cycle
// reports the idle state.
func (h *skillCurationHandlers) CycleStatus(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	RespondOK(c, gin.H{"status": h.cycle.Status(c.Request.Context(), ws.ID)})
}

// nonNilEntries is the audit-list counterpart.
func nonNilEntries(entries []domain.SkillImpactEntry) []domain.SkillImpactEntry {
	if entries == nil {
		return []domain.SkillImpactEntry{}
	}
	return entries
}

// nonNilStrings normalizes an evidence list for the wire.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
