package skillcuration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Approval and rejection (add-skill-curation-from-traces 6.1, design D5/D6).
// Approval is the oracle; creation is one atomic write: re-validate the
// draft's name against the CURRENT tier state (a skill installed between
// proposal and approval blocks the approve with a conflict — the candidate
// stays pending), enforce the per-agent catalog budget with a refusal that
// names the remedy, then materialize SKILL.md + PURPOSE.md under the owning
// agent's skills directory with the promptdocs stage-temp-rename discipline.
// No registry row: agent tier is unregistered, the directory scan is the
// state (workspace-skills spec). The materialized skill enters probation
// immediately — the row flips to provisional, not approved (6.2) — and the
// decision lands in the append-only audit trail.
//
// The cycle/manual trigger paths (task 7) reuse Approver.Approve; the HTTP
// handler (6.1) is a thin translator over these methods.
//
// Deleted source sessions never block: evidence ids are opaque strings by
// contract (D5) — nothing here looks a session up, so approval cannot 404
// on deleted evidence.

const (
	// skillFileName is the skill body file every tier scan recognizes:
	// <tierDir>/<name>/SKILL.md is the only mounted shape.
	skillFileName = "SKILL.md"
	// purposeFileName is the curated skill's on-disk provenance file (D4):
	// it maps the skill to its cited wiki patterns and evidence runs and
	// travels with the skill into the archive on disable.
	purposeFileName = "PURPOSE.md"

	// approvalDiffMaxLines bounds the rendered decision diff inside one
	// audit entry (the proposer's audit renderer truncates again when it
	// feeds the prompt).
	approvalDiffMaxLines = 40
)

// Approver materializes approved candidates and records rejections. All
// dependencies are granular stores and seams; none may be nil (AGENTS.md:
// injected dependencies are never nil).
type Approver struct {
	candidates store.SkillCandidateStore
	agents     store.AgentStore
	workspaces store.WorkspaceStore
	// workspaceSkills is the workspace-tier collision surface (registry row
	// names), shared with draft validation through the one validator below.
	workspaceSkills store.WorkspaceSkillStore
	// skillContent is the agent-tier collision surface (the skills
	// directory on disk) — the same seam draft validation reads.
	skillContent SkillContentReader
	tools        ToolNamesFunc
	config       ConfigSource
	// onClawDir is the OnClaw root AgentSkillsDir and the archive dir
	// derive from.
	onClawDir string
	log       *slog.Logger
}

// NewApprover constructs the approver from its granular dependencies: the
// candidate store, the agent/workspace stores (slug resolution for the
// on-disk paths), the workspace-skill registry and the agent-tier content
// reader (the collision surfaces), the tool catalog source, the workspace
// curation config source (the catalog budget), the OnClaw root, and the
// logger.
func NewApprover(
	candidates store.SkillCandidateStore,
	agents store.AgentStore,
	workspaces store.WorkspaceStore,
	workspaceSkills store.WorkspaceSkillStore,
	skillContent SkillContentReader,
	tools ToolNamesFunc,
	config ConfigSource,
	onClawDir string,
	log *slog.Logger,
) *Approver {
	return &Approver{
		candidates:      candidates,
		agents:          agents,
		workspaces:      workspaces,
		workspaceSkills: workspaceSkills,
		skillContent:    skillContent,
		tools:           tools,
		config:          config,
		onClawDir:       onClawDir,
		log:             log,
	}
}

// Validator returns the draft validator this approver uses for approve-time
// collision re-validation, built over the same granular surfaces the
// constructor received — one source of truth for the collision rules.
func (a *Approver) Validator() *DraftValidator {
	return NewDraftValidator(a.workspaceSkills, a.skillContent, a.tools)
}

// ReviewRequest scopes one review decision. Reviewer names the deciding
// user (the HTTP caller's id); pipeline callers may leave it empty.
type ReviewRequest struct {
	WorkspaceID string
	CandidateID string
	Reviewer    string
}

// Approve materializes one pending candidate as an agent-tier skill and
// moves it into probation (row status provisional, D6). Errors:
// domain.ErrNotFound for unknown/foreign candidates, domain.ErrConflict when
// the candidate is not pending, when the approve-time collision check
// fails, or when the catalog is at budget (the message names the remedy),
// domain.ErrInvalid for a rejected-shape candidate. On every refusal the
// candidate stays pending.
func (a *Approver) Approve(ctx context.Context, req ReviewRequest) (*domain.SkillCandidate, error) {
	candidate, err := a.candidates.Get(ctx, req.WorkspaceID, req.CandidateID)
	if err != nil {
		return nil, err
	}
	if candidate.Status != domain.SkillCandidatePending {
		return nil, fmt.Errorf("%w: candidate %s is %s, only a pending candidate can be approved",
			domain.ErrConflict, candidate.ID, candidate.Status)
	}

	ws, err := a.workspaces.ByID(ctx, req.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: resolve workspace: %w", err)
	}
	agent, err := a.agents.ByID(ctx, req.WorkspaceID, candidate.AgentID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: resolve agent: %w", err)
	}

	// Gate one — collisions against the CURRENT tier state (the race the
	// human gate exists to close): a name that collides since the proposal
	// refuses the approve and the candidate stays pending for re-review.
	exempt := ""
	if candidate.IsEdit {
		exempt = candidate.SupersedesSkillName
	}
	if err := a.Validator().ValidateCollisions(ctx, req.WorkspaceID, candidate.AgentID, candidate.SkillName, exempt); err != nil {
		var rejection *DraftRejection
		if errors.As(err, &rejection) {
			return nil, fmt.Errorf("%w: approval blocked, the draft no longer fits the catalog: %s",
				domain.ErrConflict, strings.Join(rejection.Problems, "; "))
		}
		return nil, err // store or seam read failure — not the draft's fault
	}

	// Gate two — the per-agent catalog budget (D6), enforced at approval
	// with the refusal naming the remedy. A superseding edit replaces a
	// live skill one-for-one and so never grows the catalog: only
	// first-time proposals consume budget.
	if !candidate.IsEdit {
		cfg := a.config(ctx, req.WorkspaceID)
		live, err := a.candidates.CountApprovedCuratedByAgent(ctx, req.WorkspaceID, candidate.AgentID)
		if err != nil {
			return nil, fmt.Errorf("skillcuration: count curated catalog: %w", err)
		}
		if live >= cfg.CatalogBudgetPerAgent {
			return nil, fmt.Errorf("%w: curated catalog for agent %s at capacity (%d/%d): disable or merge existing curated skills first",
				domain.ErrConflict, agent.Slug, live, cfg.CatalogBudgetPerAgent)
		}
	}

	// Materialize (D5: creation is one atomic write — stage temp + rename
	// per file, so a reader never sees a partial skill; a crash leaves the
	// previous state, and a retry rewrites cleanly).
	skillsDir := domain.AgentSkillsDir(a.onClawDir, ws.Slug, agent.Slug)
	skillDir := filepath.Join(skillsDir, candidate.SkillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("skillcuration: create skill dir: %w", err)
	}
	if err := promptdocs.WritePromptDocument(skillDir, skillFileName, candidate.ProposedContent); err != nil {
		return nil, fmt.Errorf("skillcuration: write SKILL.md: %w", err)
	}
	purpose := renderPurpose(*candidate, req.Reviewer, time.Now().UTC())
	if err := promptdocs.WritePromptDocument(skillDir, purposeFileName, purpose); err != nil {
		return nil, fmt.Errorf("skillcuration: write PURPOSE.md: %w", err)
	}

	// A rename-edit supersedes on disk too: the old name leaves the catalog
	// once the new name is written. The prior content is preserved on the
	// row (SupersededContent) — the DB is the versioned lineage, the
	// directory only ever holds the live text. An in-place edit
	// (SupersedesSkillName == SkillName) rewrote the same dir above.
	if candidate.IsEdit && candidate.SupersedesSkillName != candidate.SkillName {
		oldDir := filepath.Join(skillsDir, candidate.SupersedesSkillName)
		if err := os.RemoveAll(oldDir); err != nil {
			return nil, fmt.Errorf("skillcuration: retire superseded skill dir %s: %w", candidate.SupersedesSkillName, err)
		}
	}

	// The row enters probation, then the audit entry lands (D6: the DB row
	// is the system of record; the directory is its projection).
	if err := a.candidates.UpdateStatus(ctx, req.WorkspaceID, candidate.ID, domain.SkillCandidateProvisional, ""); err != nil {
		return nil, fmt.Errorf("skillcuration: move candidate to provisional: %w", err)
	}
	entry := &domain.SkillImpactEntry{
		WorkspaceID: req.WorkspaceID,
		ClusterID:   candidate.ClusterID,
		SkillName:   candidate.SkillName,
		Verdict:     domain.SkillImpactApproved,
		Diff:        renderApprovalDiff(*candidate),
		Reviewer:    req.Reviewer,
	}
	if err := a.candidates.AppendImpactEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("skillcuration: append approval audit entry: %w", err)
	}
	a.log.InfoContext(ctx, "skillcuration: candidate approved and materialized",
		"workspace_id", req.WorkspaceID, "agent_id", candidate.AgentID,
		"candidate_id", candidate.ID, "skill_name", candidate.SkillName,
		"is_edit", candidate.IsEdit, "reviewer", req.Reviewer)

	return a.candidates.Get(ctx, req.WorkspaceID, candidate.ID)
}

// Reject records the reviewer's rejection: the row moves to rejected with
// the reason verbatim and an audit entry carrying the reason and the
// reviewer identity. The reason is required — an empty (or blank) reason is
// domain.ErrInvalid; the four canonical reasons are a UI concern. The wiki
// is untouched by design (spec: "Rejection leaves the wiki intact") — no
// wiki call exists on this path. Only a pending candidate can be rejected;
// anything else is domain.ErrConflict.
func (a *Approver) Reject(ctx context.Context, req ReviewRequest, reason string) (*domain.SkillCandidate, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: a rejection requires a reason", domain.ErrInvalid)
	}

	candidate, err := a.candidates.Get(ctx, req.WorkspaceID, req.CandidateID)
	if err != nil {
		return nil, err
	}
	if candidate.Status != domain.SkillCandidatePending {
		return nil, fmt.Errorf("%w: candidate %s is %s, only a pending candidate can be rejected",
			domain.ErrConflict, candidate.ID, candidate.Status)
	}

	if err := a.candidates.UpdateStatus(ctx, req.WorkspaceID, candidate.ID, domain.SkillCandidateRejected, reason); err != nil {
		return nil, fmt.Errorf("skillcuration: reject candidate: %w", err)
	}
	entry := &domain.SkillImpactEntry{
		WorkspaceID: req.WorkspaceID,
		ClusterID:   candidate.ClusterID,
		SkillName:   candidate.SkillName,
		Verdict:     domain.SkillImpactRejected,
		Diff:        renderApprovalDiff(*candidate),
		Reason:      reason,
		Reviewer:    req.Reviewer,
	}
	if err := a.candidates.AppendImpactEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("skillcuration: append rejection audit entry: %w", err)
	}
	a.log.InfoContext(ctx, "skillcuration: candidate rejected",
		"workspace_id", req.WorkspaceID, "agent_id", candidate.AgentID,
		"candidate_id", candidate.ID, "skill_name", candidate.SkillName,
		"reviewer", req.Reviewer)

	return a.candidates.Get(ctx, req.WorkspaceID, candidate.ID)
}

// ValidateCollisions re-checks one name against the current tier state —
// the dynamic half of ValidateDraft (5.2): slug shape, the reserved system
// names, the workspace registry rows, and the agent-tier skills directory.
// exempt is the superseded skill's name an edit is allowed to carry;
// first-time proposals pass "". Draft failures return a *DraftRejection
// (errors.As-able); store or seam READ failures return plain errors. The
// static checks (description, tools, size) ran at proposal time and the row
// is immutable — approval re-validates only what current state can break.
func (v *DraftValidator) ValidateCollisions(ctx context.Context, workspaceID, agentID, name, exempt string) error {
	var problems []string

	if err := ValidatePageSlug(name); err != nil {
		problems = append(problems, fmt.Sprintf("name: %v", err))
	}
	if slices.Contains(ReservedSystemSkillNames, name) {
		problems = append(problems, fmt.Sprintf("name %q is reserved for a system skill", name))
	}

	rows, err := v.workspaceSkills.List(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("skillcuration: list workspace skills: %w", err)
	}
	for _, row := range rows {
		if row.Name == name && name != exempt {
			problems = append(problems, fmt.Sprintf("name %q collides with a workspace skill", name))
			break
		}
	}

	if _, found, err := v.agentSkillContent(ctx, workspaceID, agentID, name); err != nil {
		return fmt.Errorf("skillcuration: read agent skill %q: %w", name, err)
	} else if found && name != exempt {
		problems = append(problems, fmt.Sprintf("name %q collides with an existing agent skill", name))
	}

	if len(problems) == 0 {
		return nil
	}
	return &DraftRejection{Problems: problems}
}

// renderPurpose writes the curated skill's on-disk provenance file (D4):
// the candidate linkage, the cited wiki patterns, and the evidence runs —
// everything a reviewer needs to trace the skill back to its evidence
// after the store row is long gone.
func renderPurpose(candidate domain.SkillCandidate, reviewer string, approvedAt time.Time) string {
	var sb strings.Builder
	sb.WriteString("# Purpose\n\n")
	sb.WriteString("This skill was curated by the skill-curation pipeline from reviewed run ")
	sb.WriteString("evidence. This file is provenance, not instructions: it maps the skill ")
	sb.WriteString("to the wiki patterns and evidence runs it was distilled from.\n\n")
	fmt.Fprintf(&sb, "- Candidate: %s\n", candidate.ID)
	fmt.Fprintf(&sb, "- Cluster: %s\n", candidate.ClusterID)
	if candidate.IsEdit {
		fmt.Fprintf(&sb, "- Supersedes: %s\n", candidate.SupersedesSkillName)
	}
	if len(candidate.CitedPatternRefs) > 0 {
		fmt.Fprintf(&sb, "- Cited wiki patterns: %s\n", strings.Join(candidate.CitedPatternRefs, ", "))
	} else {
		sb.WriteString("- Cited wiki patterns: (none)\n")
	}
	if len(candidate.EvidenceEventIDs) > 0 {
		fmt.Fprintf(&sb, "- Evidence runs: %s\n", strings.Join(candidate.EvidenceEventIDs, ", "))
	} else {
		sb.WriteString("- Evidence runs: (none)\n")
	}
	by := reviewer
	if by == "" {
		by = "the pipeline"
	}
	fmt.Fprintf(&sb, "\nApproved by %s at %s.\n", by, approvedAt.Format(time.RFC3339))
	return sb.String()
}

// renderApprovalDiff renders the content delta a decision was made on:
// creates render as added lines; edits render the superseded content's
// unique lines as removals and the new content's unique lines as additions.
// Bounded to approvalDiffMaxLines with an ellipsis — the audit is a review
// aid, not an archive (the row carries both contents verbatim).
func renderApprovalDiff(candidate domain.SkillCandidate) string {
	if candidate.IsEdit {
		return renderLineDiff(candidate.SupersededContent, candidate.ProposedContent)
	}
	return prefixLines(strings.Split(strings.TrimRight(candidate.ProposedContent, "\n"), "\n"), "+ ")
}

// renderLineDiff renders old-only lines as removals and new-only lines as
// additions (a set difference, not a minimal edit script — enough for
// review, deliberately simple). Bounded.
func renderLineDiff(oldContent, newContent string) string {
	oldLines := toLineSet(oldContent)
	newLines := toLineSet(newContent)

	var lines []string
	for _, l := range toLineList(oldContent) {
		if _, kept := newLines[l]; !kept {
			lines = append(lines, "- "+l)
		}
	}
	for _, l := range toLineList(newContent) {
		if _, seen := oldLines[l]; !seen {
			lines = append(lines, "+ "+l)
		}
	}
	if len(lines) == 0 {
		return "(no textual delta)"
	}
	return boundDiffLines(lines)
}

func toLineSet(content string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, l := range strings.Split(content, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			set[l] = struct{}{}
		}
	}
	return set
}

func toLineList(content string) []string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func prefixLines(lines []string, prefix string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, prefix+l)
	}
	return boundDiffLines(out)
}

// boundDiffLines caps the rendered diff at approvalDiffMaxLines lines with
// a count-of-the-rest ellipsis.
func boundDiffLines(lines []string) string {
	if len(lines) > approvalDiffMaxLines {
		lines = append(append([]string{}, lines[:approvalDiffMaxLines]...),
			fmt.Sprintf("… (%d more lines)", len(lines)-approvalDiffMaxLines))
	}
	return strings.Join(lines, "\n")
}
