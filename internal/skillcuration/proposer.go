package skillcuration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The proposer (add-skill-curation-from-traces 5.1–5.3, design D4): one
// bounded side-call per qualified cluster per cycle, drafting at most ONE
// atomic create-or-edit. The code — never the model — owns the gates:
// cluster convergence first, unresolved-rejection suppression second, the
// one-pending-candidate-per-cluster rule third, and edit-over-sibling
// always (a second skill for the same procedure is rejected in code). The
// model drafts text against the wiki index, the matched pattern pages, the
// sampled cluster windows, and the rendered rejection audit (D3: the DB is
// the system of record, rendered into the prompt); the output is parsed
// STRICTLY, validated by the DraftValidator (5.2), retried with the
// problems appended, and recorded as a failed candidate row after the
// bounded retries (the extraction-failed card, with the review surface's
// retry action — retry.go).
//
// Fail-soft: a model or parse failure returns as an error the cycle logs
// and defers to the next tick (D7); a dropped draft returns nil — the cycle
// continues either way, and the ingest path and raw session events are
// never touched.

// auditDiffRenderBudget bounds one audit entry's rendered diff inside the
// proposal prompt.
const auditDiffRenderBudget = 500

// Proposer drafts one skill candidate per qualified cluster per cycle.
type Proposer struct {
	sessions store.SessionEventStore
	clusters store.SkillCandidateStore
	// config resolves the workspace's curation config at call time — the
	// cluster minimum lives there.
	config   ConfigSource
	resolver ModelResolver
	wiki     *Wiki
	validate *DraftValidator
	// chip is the one optional capability (the qualifier's
	// WithCandidateChipSink precedent): unwired, candidates still store —
	// only the transcript signal is dropped.
	chip CandidateChipSink
	log  *slog.Logger

	budget       time.Duration
	draftRetries int
}

// ProposerOption configures the proposer; every knob has a safe default.
type ProposerOption func(*Proposer)

// WithProposalBudget overrides the side-call deadline for the whole
// proposal attempt (retries included — DefaultSideCallBudget by default;
// the parent context's earlier deadline always wins). Named apart from the
// maintainer's WithSideCallBudget: same package, different option types.
func WithProposalBudget(d time.Duration) ProposerOption {
	return func(p *Proposer) {
		if d > 0 {
			p.budget = d
		}
	}
}

// WithDraftRetries overrides the bounded retry count for invalid drafts
// (default 1: one retry with the validation problems appended to the
// prompt). Negative values keep the default.
func WithDraftRetries(n int) ProposerOption {
	return func(p *Proposer) {
		if n >= 0 {
			p.draftRetries = n
		}
	}
}

// WithProposalChipSink wires the proposer's chip emission (the memory
// ChipSink precedent: an optional capability, never a nil-able
// dependency). The composition root applies it only when the session-event
// seam exists; the sink implementation (task 7) appends the
// SessionEventKindSkillCandidate event to the drafted run's transcript.
func WithProposalChipSink(fn CandidateChipSink) ProposerOption {
	return func(p *Proposer) {
		p.chip = fn
	}
}

// NewProposer constructs the proposer from its granular dependencies: the
// session-event store the sampled windows load from (the maintainer's
// window seam), the candidate store the cluster memberships, candidates,
// and audit trail read and persist through, the workspace config source
// (the cluster minimum), the model resolver (CurationModelResolver in
// production), the workspace's wiki, the draft validator (5.2), and the
// logger. The chip sink is the one optional capability.
func NewProposer(sessions store.SessionEventStore, clusters store.SkillCandidateStore, config ConfigSource, resolver ModelResolver, wiki *Wiki, validator *DraftValidator, log *slog.Logger, opts ...ProposerOption) *Proposer {
	p := &Proposer{
		sessions:     sessions,
		clusters:     clusters,
		config:       config,
		resolver:     resolver,
		wiki:         wiki,
		validate:     validator,
		log:          log,
		budget:       DefaultSideCallBudget,
		draftRetries: 1,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ProposeRequest scopes one cluster's proposal side-call. The cluster is
// agent-anchored (ClusterKey hashes the agent), so the agent id resolves
// both the model and the agent-tier collision surface.
type ProposeRequest struct {
	WorkspaceID string
	AgentID     string
	ClusterID   string
}

// ProposeCluster runs one proposal attempt for one cluster: gate →
// suppress → single-pending check → assemble → side-call → validate →
// store → emit. Every no path is a quiet nil with a log; exhausted retries
// land as a failed candidate row (retry.go's RetryCandidate re-runs the
// same draft path); only infrastructure failures (store, model) return
// errors for the cycle to defer on.
func (p *Proposer) ProposeCluster(ctx context.Context, req ProposeRequest) error {
	ctx, cancel := context.WithTimeout(ctx, p.budget)
	defer cancel()

	cfg := p.config(ctx, req.WorkspaceID)

	membership, err := p.clusters.ListClusterRunsByCluster(ctx, req.WorkspaceID, req.ClusterID)
	if err != nil {
		return fmt.Errorf("skillcuration: list cluster runs: %w", err)
	}
	if len(membership) == 0 {
		return nil // nothing indexed yet — quiet no-op
	}

	// Gate one — the cluster gate, in code (spec: "Cluster gate before
	// drafting"): a first-ever qualifying run opens or extends the cluster
	// without proposing.
	count, err := ClusterQualifyingCount(ctx, p.clusters, req.WorkspaceID, req.ClusterID)
	if err != nil {
		return err
	}
	if !ClusterGateOpen(count, cfg.ClusterMinimum) {
		// Info, not Debug (fix: invisible at the server's default level) —
		// this is the "the run qualified but no new skill is drafted yet"
		// answer: the cluster needs more qualifying runs first.
		p.log.InfoContext(ctx, "skillcuration: proposal skipped; cluster gate closed",
			"workspace_id", req.WorkspaceID, "agent_id", req.AgentID,
			"cluster_id", req.ClusterID, "qualifying", count, "minimum", cfg.ClusterMinimum)
		return nil
	}

	// Gate two — suppression (spec: "Clusters whose rejection audit is
	// unresolved SHALL NOT be re-proposed unless materially new evidence
	// has accumulated since the rejection"): a rejection suppresses the
	// cluster until a qualifying run joins it AFTER the rejection.
	latest, err := p.clusters.LatestImpactEntryForCluster(ctx, req.WorkspaceID, req.ClusterID)
	if err != nil {
		return fmt.Errorf("skillcuration: read cluster audit: %w", err)
	}
	if latest != nil && latest.Verdict == domain.SkillImpactRejected && !hasQualifyingRunAfter(membership, latest.CreatedAt) {
		p.log.DebugContext(ctx, "skillcuration: proposal skipped; cluster suppressed by rejection",
			"workspace_id", req.WorkspaceID, "agent_id", req.AgentID,
			"cluster_id", req.ClusterID, "rejected_skill", latest.SkillName,
			"rejected_at", latest.CreatedAt.UTC().Format(time.RFC3339))
		return nil
	}

	// Gate three — one atomic create-or-edit per cluster per cycle (D4): a
	// pending candidate awaits the human gate; drafting behind it would
	// queue a sibling for the same verdict.
	candidates, err := p.clusters.ListByCluster(ctx, req.WorkspaceID, req.ClusterID)
	if err != nil {
		return fmt.Errorf("skillcuration: list cluster candidates: %w", err)
	}
	for _, c := range candidates {
		if c.Status == domain.SkillCandidatePending {
			p.log.DebugContext(ctx, "skillcuration: proposal skipped; candidate already pending",
				"workspace_id", req.WorkspaceID, "agent_id", req.AgentID,
				"cluster_id", req.ClusterID, "candidate_id", c.ID, "skill_name", c.SkillName)
			return nil
		}
	}

	// The sibling rule: a live curated skill (approved or provisional —
	// disabled is archived, rejected never materialized) in this cluster
	// forces the proposal to be an edit of it.
	sibling := liveSibling(candidates)

	start := time.Now()

	// The shared draft path: input assembly + the bounded-retry side-call
	// loop (the manual retry runs the exact same path).
	out, err := p.draft(ctx, req, membership, count, sibling, p.draftRetries)
	if err != nil {
		return err
	}
	if out.declined != "" {
		p.log.DebugContext(ctx, out.declined,
			"workspace_id", req.WorkspaceID, "agent_id", req.AgentID, "cluster_id", req.ClusterID)
		return nil
	}
	if out.problems != nil {
		// The drop is no longer silent: the failure lands as a failed
		// candidate row (the extraction-failed card, with the retry action)
		// and the cycle still continues (nil — fail-soft to the tick).
		p.log.InfoContext(ctx, "skillcuration: proposal dropped after retries",
			"workspace_id", req.WorkspaceID, "agent_id", req.AgentID,
			"cluster_id", req.ClusterID, "attempts", p.draftRetries+1,
			"error", out.problems.Error())
		if err := p.recordFailure(ctx, req, out.draftName, out.problems); err != nil {
			return err
		}
		return nil
	}
	return p.emit(ctx, req, out.candidate, newestQualifyingRun(membership), time.Since(start))
}

// draftOutcome is the shared draft path's result: exactly one of a validated
// candidate, a legitimate decline (its message doubles as the debug log), or
// the accumulated problems from exhausted retries (draftName carries the last
// attempted draft's name — empty when parsing never succeeded). The error
// return is reserved for infrastructure failures (store, model, seam reads).
type draftOutcome struct {
	candidate *domain.SkillCandidate
	declined  string
	draftName string
	problems  error
}

// draft is the proposal attempt's shared core (ProposeCluster and the manual
// retry both run it): model resolution, input assembly (D4: wiki index +
// matched pattern pages, sampled cluster windows, the cluster's rendered
// audit, the sibling for an edit), then the bounded-retry loop — one
// side-call, then up to retries more with the accumulated problems appended.
// Parse failures and DraftRejections are draft failures (retryable); model
// and store failures are stage errors.
func (p *Proposer) draft(ctx context.Context, req ProposeRequest, membership []domain.SkillClusterRun, qualifying int, sibling *domain.SkillCandidate, retries int) (draftOutcome, error) {
	out := draftOutcome{}

	model, err := p.resolver(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		return out, fmt.Errorf("skillcuration: resolve proposer model: %w", err)
	}

	// Input assembly (D4): wiki index + matched pattern pages, sampled
	// cluster windows (the maintainer's sampler over the same membership),
	// the cluster's rendered audit, and — for an edit — the sibling's
	// current content.
	pages, err := p.wiki.List()
	if err != nil {
		return out, fmt.Errorf("skillcuration: list wiki pages: %w", err)
	}
	knownPatterns := make(map[string]bool, len(pages))
	for _, page := range pages {
		knownPatterns[page.Slug] = true
	}
	matched := matchedPages(pages, membershipSessions(membership))

	sampled := sampleWindows(membership)
	windows, knownRuns := p.loadWindows(ctx, req.WorkspaceID, sampled)
	if len(windows) == 0 {
		out.declined = "skillcuration: proposal skipped; no readable windows"
		return out, nil
	}
	// A matched page's evidence runs are citations the model was shown.
	for _, page := range matched {
		for _, run := range page.EvidenceRuns {
			knownRuns[run] = true
		}
	}

	audit, err := p.clusters.ListImpactEntries(ctx, req.WorkspaceID, time.Time{})
	if err != nil {
		return out, fmt.Errorf("skillcuration: list impact entries: %w", err)
	}
	clusterAudit := make([]domain.SkillImpactEntry, 0, len(audit))
	for _, entry := range audit {
		if entry.ClusterID == req.ClusterID {
			clusterAudit = append(clusterAudit, entry)
		}
	}

	siblingContent := ""
	if sibling != nil {
		content, found, err := p.validate.AgentSkillContent(ctx, req.WorkspaceID, req.AgentID, sibling.SkillName)
		if err != nil {
			return out, fmt.Errorf("skillcuration: read sibling skill content: %w", err)
		}
		if found {
			siblingContent = content
		}
	}

	basePrompt := proposerUserPrompt(proposerPromptInput{
		pages:          pages,
		matched:        matched,
		toolNames:      p.validate.ToolNames(),
		windows:        windows,
		qualifying:     qualifying,
		audit:          clusterAudit,
		sibling:        sibling,
		siblingContent: siblingContent,
	})

	// The bounded-retry loop: one side-call, then up to retries more with
	// the accumulated problems appended. Parse failures are draft
	// failures here (retryable); model and store failures fail the stage.
	var problems error
	for attempt := 0; attempt <= retries; attempt++ {
		prompt := basePrompt
		if problems != nil {
			prompt += fmt.Sprintf("\n\n## Previous attempt rejected\n%s\nFix every listed problem and answer again with the full JSON object.", problems.Error())
		}

		raw, err := generateSideCallText(ctx, model, proposerSystemPrompt, prompt)
		if err != nil {
			return out, fmt.Errorf("skillcuration: proposer model call: %w", err)
		}

		draft, err := parseProposerDraft(raw)
		if err != nil {
			problems = fmt.Errorf("response is not a valid proposal object: %v", err)
			continue
		}
		if draft == nil {
			// The model legitimately proposed nothing (JSON null) — the
			// windows did not converge on one procedure. Quiet no-op.
			out.declined = "skillcuration: proposer declined to draft"
			return out, nil
		}

		out.draftName = strings.TrimSpace(draft.Name)
		candidate, err := p.buildCandidate(ctx, req, draft, sibling, knownRuns, knownPatterns)
		if err == nil {
			out.candidate = candidate
			return out, nil
		}
		var rejection *DraftRejection
		if errors.As(err, &rejection) {
			problems = rejection // retryable — feed the problems back
			continue
		}
		return out, err // store or seam read failure — stage error
	}

	out.problems = problems
	return out, nil
}

// recordFailure persists a dropped draft as a failed candidate row (the
// extraction-failed card, design D4 "dropped with the failure logged" plus
// the review surface's retry action): the cluster linkage survives so the
// reviewer can re-attempt the extraction, and Reason carries the accumulated
// problems verbatim. The draft never materialized — no content, no evidence
// citations on the row yet.
func (p *Proposer) recordFailure(ctx context.Context, req ProposeRequest, draftName string, problems error) error {
	row := &domain.SkillCandidate{
		WorkspaceID:      req.WorkspaceID,
		AgentID:          req.AgentID,
		ClusterID:        req.ClusterID,
		SkillName:        draftName,
		Status:           domain.SkillCandidateFailed,
		Reason:           problems.Error(),
		EvidenceEventIDs: []string{},
		CitedPatternRefs: []string{},
	}
	if err := p.clusters.Save(ctx, row); err != nil {
		return fmt.Errorf("skillcuration: save failed candidate: %w", err)
	}
	return nil
}

// PendingCount returns the workspace's pending-candidate count — the
// review-badge number the candidates API (task 6) serves. The store's
// CountPending is the system of record; the proposer only exposes it.
func (p *Proposer) PendingCount(ctx context.Context, workspaceID string) (int, error) {
	return p.clusters.CountPending(ctx, workspaceID)
}

// emit persists the built candidate and fires the chip. The sink receives
// the drafted run's coordinates (the cluster's newest qualifying member —
// the run whose convergence triggered this proposal) with the candidate's
// id and skill name in the payload.
func (p *Proposer) emit(ctx context.Context, req ProposeRequest, candidate *domain.SkillCandidate, trigger *domain.SkillClusterRun, elapsed time.Duration) error {
	if err := p.clusters.Save(ctx, candidate); err != nil {
		return fmt.Errorf("skillcuration: save candidate: %w", err)
	}
	p.log.InfoContext(ctx, "skillcuration: candidate drafted",
		"workspace_id", req.WorkspaceID, "agent_id", req.AgentID,
		"cluster_id", req.ClusterID, "candidate_id", candidate.ID,
		"skill_name", candidate.SkillName, "is_edit", candidate.IsEdit,
		"elapsed_ms", elapsed.Milliseconds())

	if p.chip != nil && trigger != nil {
		p.chip(ctx, ingest.Job{
			WorkspaceID: trigger.WorkspaceID,
			AgentID:     trigger.AgentID,
			SessionID:   trigger.SessionID,
			TurnID:      trigger.TurnID,
			Origin:      trigger.Origin,
			Status:      string(trigger.RunStatus),
		}, SkillCandidatePayload{
			WorkspaceID: req.WorkspaceID,
			AgentID:     req.AgentID,
			SessionID:   trigger.SessionID,
			RunID:       trigger.TurnID,
			Cluster:     req.ClusterID,
			SkillName:   candidate.SkillName,
			CandidateID: candidate.ID,
		})
	}
	return nil
}

// buildCandidate applies the cluster-aware code checks the DraftValidator
// cannot see (edit-over-sibling, citation grounding) plus the validator's
// own checks, and assembles the candidate row. Draft failures return a
// *DraftRejection (retryable); infrastructure failures return plain errors.
func (p *Proposer) buildCandidate(ctx context.Context, req ProposeRequest, draft *SkillDraft, sibling *domain.SkillCandidate, knownRuns, knownPatterns map[string]bool) (*domain.SkillCandidate, error) {
	var problems []string

	// Edit-over-sibling, re-checked in code (D4: "The edit-over-sibling
	// rule is enforced in the prompt contract and re-checked in code").
	exempt := ""
	isEdit := false
	superseded := ""
	if sibling != nil {
		if draft.Supersedes != sibling.SkillName {
			problems = append(problems, fmt.Sprintf(
				"a curated skill %q already covers this procedure; a second skill for the same procedure is forbidden — set \"supersedes\" to %q and propose the corrected full text",
				sibling.SkillName, sibling.SkillName))
		} else {
			isEdit = true
			superseded = sibling.SkillName
			exempt = sibling.SkillName
		}
	} else if draft.Supersedes != "" {
		problems = append(problems, fmt.Sprintf(
			"no curated skill exists for this procedure; \"supersedes\" must be empty (got %q)", draft.Supersedes))
	}

	// Citation grounding: only patterns the wiki index named and only runs
	// the prompt showed (the maintainer's knownRuns discipline).
	if len(draft.CitedRuns) == 0 {
		problems = append(problems, "cites no evidence runs")
	}
	for _, ref := range draft.CitedPatterns {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			problems = append(problems, "cites an empty pattern ref")
			continue
		}
		if !knownPatterns[ref] {
			problems = append(problems, fmt.Sprintf("cites unknown pattern %q", ref))
		}
	}
	for _, run := range draft.CitedRuns {
		run = strings.TrimSpace(run)
		if run == "" {
			problems = append(problems, "cites an empty evidence run")
			continue
		}
		if !knownRuns[run] {
			problems = append(problems, fmt.Sprintf("cites unknown evidence run %q", run))
		}
	}

	if err := p.validate.ValidateDraft(ctx, req.WorkspaceID, req.AgentID, *draft, exempt); err != nil {
		var rejection *DraftRejection
		if errors.As(err, &rejection) {
			problems = append(problems, rejection.Problems...)
		} else {
			return nil, err // store/seam read failure — stage error
		}
	}

	if len(problems) > 0 {
		return nil, &DraftRejection{Problems: problems}
	}

	candidate := &domain.SkillCandidate{
		WorkspaceID:         req.WorkspaceID,
		AgentID:             req.AgentID,
		ClusterID:           req.ClusterID,
		SkillName:           strings.TrimSpace(draft.Name),
		Status:              domain.SkillCandidatePending,
		ProposedContent:     draft.Content,
		IsEdit:              isEdit,
		SupersedesSkillName: superseded,
		EvidenceEventIDs:    trimNonEmpty(draft.CitedRuns),
		CitedPatternRefs:    trimNonEmpty(draft.CitedPatterns),
	}
	if isEdit {
		// SupersededContent = the sibling's current skill content read
		// from disk if available (absence keeps the lineage pointer only).
		content, found, err := p.validate.AgentSkillContent(ctx, req.WorkspaceID, req.AgentID, sibling.SkillName)
		if err != nil {
			return nil, fmt.Errorf("skillcuration: read superseded skill content: %w", err)
		}
		if found {
			candidate.SupersededContent = content
		}
	}
	return candidate, nil
}

// loadWindows loads and renders the sampled runs' transcript windows — the
// maintainer's window loop mirrored over the proposer's own store and log
// (maintainer.go is outside this task's file scope). Deleted or empty
// sessions contribute nothing and their ids stay unknown — the model can
// only cite windows it was shown. knownRuns is never nil.
func (p *Proposer) loadWindows(ctx context.Context, workspaceID string, sampled []sampledRun) ([]renderedWindow, map[string]bool) {
	knownRuns := make(map[string]bool, len(sampled))
	windows := make([]renderedWindow, 0, len(sampled))
	for _, s := range sampled {
		events, err := p.sessions.LoadEvents(ctx, store.LoadSessionEventsParams{
			WorkspaceID: workspaceID,
			SessionID:   s.run.SessionID,
		})
		if err != nil {
			p.log.DebugContext(ctx, "skillcuration: window load failed; skipping",
				"session_id", s.run.SessionID, "turn_id", s.run.TurnID, "error", err)
			continue
		}
		window := renderRunWindow(turnEvents(events, s.run.TurnID))
		if window == "" {
			continue
		}
		knownRuns[s.run.SessionID] = true
		windows = append(windows, renderedWindow{
			SessionID: s.run.SessionID,
			Stratum:   s.stratum,
			Tally:     s.citations,
			Body:      window,
		})
	}
	return windows, knownRuns
}

// membershipSessions collects the cluster's distinct session ids.
func membershipSessions(membership []domain.SkillClusterRun) map[string]bool {
	sessions := make(map[string]bool, len(membership))
	for _, run := range membership {
		sessions[run.SessionID] = true
	}
	return sessions
}

// hasQualifyingRunAfter reports whether a qualifying run joined the cluster
// after at (the suppression rule's "materially new evidence": a qualifying
// member indexed after the rejection's CreatedAt).
func hasQualifyingRunAfter(membership []domain.SkillClusterRun, at time.Time) bool {
	for _, run := range membership {
		if run.Qualifying && run.IndexedAt.After(at) {
			return true
		}
	}
	return false
}

// liveSibling returns the cluster's live curated skill: an approved or
// provisional candidate. Disabled is archived (not live); rejected and
// pending never materialized. When several exist (history the loop's
// edit-over-sibling rule did not create), the newest proposal wins
// deterministically, id as tie-break. nil when the cluster has none.
func liveSibling(candidates []domain.SkillCandidate) *domain.SkillCandidate {
	var sibling *domain.SkillCandidate
	for i := range candidates {
		c := candidates[i]
		if c.Status != domain.SkillCandidateApproved && c.Status != domain.SkillCandidateProvisional {
			continue
		}
		if sibling == nil || c.ProposedAt.After(sibling.ProposedAt) ||
			(c.ProposedAt.Equal(sibling.ProposedAt) && c.ID > sibling.ID) {
			sibling = &candidates[i]
		}
	}
	return sibling
}

// newestQualifyingRun returns the cluster's most recently indexed
// qualifying member — the run whose convergence triggered this proposal and
// whose transcript carries the drafted chip. nil when none (the cluster
// gate already excluded that, defensively nil-safe for the emit path).
func newestQualifyingRun(membership []domain.SkillClusterRun) *domain.SkillClusterRun {
	var newest *domain.SkillClusterRun
	for i := range membership {
		run := membership[i]
		if !run.Qualifying {
			continue
		}
		if newest == nil || run.IndexedAt.After(newest.IndexedAt) ||
			(run.IndexedAt.Equal(newest.IndexedAt) && run.ID > newest.ID) {
			newest = &membership[i]
		}
	}
	return newest
}

// matchedPages returns the active pages whose evidence runs intersect the
// cluster's sessions — D4's "matched pattern pages". Superseded pages stay
// index-only (their successors carry the live guidance).
func matchedPages(pages []Page, sessions map[string]bool) []Page {
	var matched []Page
	for _, page := range pages {
		if page.Status != PageActive {
			continue
		}
		for _, run := range page.EvidenceRuns {
			if sessions[run] {
				matched = append(matched, page)
				break
			}
		}
	}
	return matched
}

// trimNonEmpty trims each entry and drops the empties; the result is never
// nil.
func trimNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// proposerPromptInput is everything one proposal prompt renders.
type proposerPromptInput struct {
	pages          []Page
	matched        []Page
	toolNames      []string
	windows        []renderedWindow
	qualifying     int
	audit          []domain.SkillImpactEntry
	sibling        *domain.SkillCandidate
	siblingContent string
}

// proposerSystemPrompt is the strict JSON contract for the proposal
// side-call.
const proposerSystemPrompt = `You are the skill proposer for an AI agent workspace. You are shown the workspace's pattern wiki, sampled transcript windows from one cluster (repeated runs of the same procedure by one agent), the cluster's review audit, and — when one exists — the curated skill that already encodes this procedure. Decide the single skill proposal the evidence supports: a new skill, or a superseding edit when a curated skill for this procedure already exists.

Emit ONLY a JSON object — no prose:
{"name":"kebab-case-skill-name","description":"what the skill does and when to use it","tools":["tool.key"],"cited_patterns":["wiki-page-slug"],"cited_runs":["session-id"],"supersedes":"","content":"the complete SKILL.md content"}

Rules:
- name is a slug: lowercase alphanumeric with interior hyphens. It must not collide with any existing skill; colliding names are rejected.
- description is one non-empty sentence of at least ten characters.
- tools lists the tool keys the procedure requires; every key must appear in the tool catalog shown below. Declare only what the steps actually call.
- cited_patterns lists wiki pattern slugs the skill builds on — only slugs shown below. cited_runs lists the evidence session IDs the skill derives from — only IDs shown below, at least one.
- content is the complete SKILL.md: an H1 title, a purpose paragraph, then the procedure as numbered steps with the failure modes and workarounds the evidence supports.
- When the prompt shows an existing curated skill for this procedure you MUST propose an edit: set "supersedes" to that skill's exact name and rewrite "content" as the corrected full text. Proposing a second skill for the same procedure is forbidden and will be rejected.
- When no curated skill exists for the procedure, "supersedes" must be "".
- If the windows do not converge on one repeatable procedure, emit null instead of inventing a proposal.`

// proposerUserPrompt renders the proposal call's user turn: the tool
// catalog, the wiki index with the matched pages' full content, the sampled
// cluster windows, the rendered audit, and the sibling skill when one
// exists.
func proposerUserPrompt(in proposerPromptInput) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Tool catalog\n%s\n", strings.Join(in.toolNames, ", "))

	sb.WriteString("\n## Pattern wiki\n")
	if len(in.pages) == 0 {
		sb.WriteString("(empty — no pattern pages yet)\n")
	}
	for _, page := range in.pages {
		fmt.Fprintf(&sb, "- %s — %s [%s]\n", page.Slug, page.Title, page.Status)
	}
	if len(in.matched) > 0 {
		sb.WriteString("\n### Matched patterns (evidence overlaps this cluster)\n")
		for _, page := range in.matched {
			fmt.Fprintf(&sb, "\n#### %s — %s\nEvidence: %s\n\n%s\n",
				page.Slug, page.Title, strings.Join(page.EvidenceRuns, ", "),
				truncateRunes(page.Body, windowRenderBudget))
		}
	}

	fmt.Fprintf(&sb, "\n## Cluster evidence\n%d qualifying runs of the same procedure family.\n", in.qualifying)
	sb.WriteString("\n## Sampled run windows\nEach window is one run of the family; cite runs by session id.\n")
	for _, w := range in.windows {
		fmt.Fprintf(&sb, "\n### Run %s (%s; %s)\n\n%s\n", w.SessionID, w.Stratum, w.Tally, w.Body)
	}

	sb.WriteString("\n## Review audit for this cluster\n")
	if len(in.audit) == 0 {
		sb.WriteString("(none — this family has never been reviewed)\n")
	} else {
		for _, entry := range in.audit {
			by := entry.Reviewer
			if by == "" {
				by = "pipeline"
			}
			fmt.Fprintf(&sb, "- %s %s %q (by %s)", entry.CreatedAt.UTC().Format(logTimeFormat), entry.Verdict, entry.SkillName, by)
			if entry.Reason != "" {
				fmt.Fprintf(&sb, ": %s", truncateRunes(entry.Reason, 300))
			}
			sb.WriteString("\n")
			if entry.Diff != "" {
				fmt.Fprintf(&sb, "  diff: %s\n", truncateRunes(entry.Diff, auditDiffRenderBudget))
			}
		}
	}

	sb.WriteString("\n## Existing curated skill for this procedure\n")
	if in.sibling == nil {
		sb.WriteString("(none — propose a new skill with \"supersedes\": \"\")\n")
	} else {
		fmt.Fprintf(&sb, "%q already encodes this procedure; your proposal MUST be an edit of it.\nCurrent content:\n\n%s\n",
			in.sibling.SkillName,
			truncateRunes(orUnavailable(in.siblingContent), windowRenderBudget))
	}
	return sb.String()
}

// orUnavailable marks an edit target whose on-disk content could not be
// read — the model then drafts the corrected full text from evidence alone.
func orUnavailable(content string) string {
	if strings.TrimSpace(content) == "" {
		return "(current content unavailable on disk — propose the corrected full text from the evidence)"
	}
	return content
}

// proposerDraftJSON is the strict JSON the proposal side-call emits.
type proposerDraftJSON struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tools         []string `json:"tools"`
	CitedPatterns []string `json:"cited_patterns"`
	CitedRuns     []string `json:"cited_runs"`
	Supersedes    string   `json:"supersedes"`
	Content       string   `json:"content"`
}

// parseProposerDraft extracts the proposal object from the model response:
// code fences and surrounding prose are tolerated, an explicit null (or an
// all-empty object) is the legitimate "nothing proposed". Returns
// (nil, nil) for propose-nothing, (nil, err) for a present-shaped but
// unparseable response (retryable — the maintainer's parseWikiOps posture).
func parseProposerDraft(raw string) (*SkillDraft, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		// No object — accept a fenced/bare null as propose-nothing.
		inner := strings.TrimSpace(strings.NewReplacer("```json", "", "```", "").Replace(trimmed))
		if inner == "null" {
			return nil, nil
		}
		return nil, errors.New("no JSON object in response")
	}
	var out proposerDraftJSON
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("decode proposal: %w", err)
	}
	if out.Name == "" && out.Content == "" && out.Description == "" {
		return nil, nil // an empty object proposes nothing
	}
	return &SkillDraft{
		Name:          strings.TrimSpace(out.Name),
		Description:   strings.TrimSpace(out.Description),
		Content:       out.Content,
		Tools:         out.Tools,
		CitedPatterns: out.CitedPatterns,
		CitedRuns:     out.CitedRuns,
		Supersedes:    strings.TrimSpace(out.Supersedes),
	}, nil
}
