package skillcuration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Probation and catalog hygiene (add-skill-curation-from-traces 6.2, design
// D6). An approved curated skill starts provisional: every recorded usage
// outcome is tallied on the candidate row; a harmful ratio past the minimum
// sample auto-disables the skill (archived out of the catalog with its
// evidence, never deleted, back on the review surface with the tally); a
// full probation window under the threshold graduates the row to approved.
//
// RecordSkillOutcome is the telemetry SEAM: it tallies one classified
// outcome. Wiring it to skill attach/read telemetry joined with terminal run
// outcomes (the runner knows both) is task 7's composition-root concern —
// this package deliberately builds no new telemetry instrumentation.

// skillsArchiveDirName is the archive directory's name, a sibling of the
// agent's skills/ directory: <root>/workspaces/<tenant>/agents/<agent>/
// skills-archive/<name>/. The skill backend scans only <tierDir>/<name>/
// SKILL.md shapes inside skills/, so an archived skill is mounted nowhere,
// while SKILL.md and PURPOSE.md stay on disk for inspection and the
// candidate row keeps the evidence linkage (D6: archived, never deleted).
const skillsArchiveDirName = "skills-archive"

// AgentSkillsArchiveDir returns the archive directory for an agent's
// disabled curated skills: a sibling of AgentSkillsDir, outside every tier
// scan. Both slugs are validated, so the path cannot traverse.
func AgentSkillsArchiveDir(root, tenantSlug, agentSlug string) string {
	return filepath.Join(domain.AgentWorkspaceDir(domain.WorkspaceRoot(root), tenantSlug, agentSlug), skillsArchiveDirName)
}

// Probation owns the tally, breach, graduation, and archive transitions.
// All dependencies are granular stores and seams; none may be nil.
type Probation struct {
	candidates store.SkillCandidateStore
	agents     store.AgentStore
	workspaces store.WorkspaceStore
	config     ConfigSource
	onClawDir  string
	log        *slog.Logger
}

// NewProbation constructs the probation manager from its granular
// dependencies: the candidate store (rows and tallies), the agent and
// workspace stores (slug resolution for the on-disk paths), the workspace
// curation config source (window, sample, threshold), the OnClaw root, and
// the logger.
func NewProbation(
	candidates store.SkillCandidateStore,
	agents store.AgentStore,
	workspaces store.WorkspaceStore,
	config ConfigSource,
	onClawDir string,
	log *slog.Logger,
) *Probation {
	return &Probation{
		candidates: candidates,
		agents:     agents,
		workspaces: workspaces,
		config:     config,
		onClawDir:  onClawDir,
		log:        log,
	}
}

// RecordSkillOutcome tallies one classified usage outcome against the
// live provisional curated skill named (workspace, agent, skillName) —
// helpful or harmful; every outcome is also a use. Breach is checked on
// every call: past the minimum sample, a harmful ratio above the threshold
// auto-disables the skill (archived out of the catalog, row back on the
// review surface with the tally, spec: "Probation breach auto-disables").
//
// This method is the outcome seam: the runner-side join of skill
// attach/read telemetry with terminal run outcomes calls it per terminal
// run — task 7 wires that call site. Unknown skills (nothing provisional by
// that name) return domain.ErrNotFound and are the caller's quiet no-op.
func (p *Probation) RecordSkillOutcome(ctx context.Context, workspaceID, agentID, skillName string, helpful bool) (*domain.SkillCandidate, error) {
	candidate, err := p.provisionalBySkill(ctx, workspaceID, agentID, skillName)
	if err != nil {
		return nil, err
	}
	if err := p.candidates.IncrementOutcomeCounts(ctx, workspaceID, candidate.ID, helpful, true); err != nil {
		return nil, fmt.Errorf("skillcuration: tally outcome: %w", err)
	}
	candidate, err = p.candidates.Get(ctx, workspaceID, candidate.ID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: re-read candidate after tally: %w", err)
	}

	cfg := p.config(ctx, workspaceID)
	if probationBreach(cfg, candidate.HelpfulCount, candidate.HarmfulCount) {
		return p.Disable(ctx, workspaceID, candidate,
			fmt.Sprintf("harmful ratio %.2f breached threshold %.2f over %d outcomes (%d helpful / %d harmful)",
				ratio(candidate.HarmfulCount, candidate.HelpfulCount),
				cfg.HarmfulRatioThreshold,
				candidate.HelpfulCount+candidate.HarmfulCount,
				candidate.HelpfulCount, candidate.HarmfulCount))
	}
	return candidate, nil
}

// SweepProbation resolves every provisional candidate whose probation
// window has closed relative to now: a harmful ratio still over the
// threshold (past the minimum sample) is disabled; anything else
// graduates to approved — a full window without a disqualifying tally is a
// pass, including an under-sampled one (documented choice: the threshold
// evidence never arrived, the window served). Rows inside their window are
// untouched. Task 7's cycle calls this per workspace on each tick; it is
// idempotent and safe to run more often than the window.
func (p *Probation) SweepProbation(ctx context.Context, workspaceID string, now time.Time) ([]domain.SkillCandidate, error) {
	rows, err := p.candidates.List(ctx, workspaceID, domain.SkillCandidateProvisional)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: list provisional candidates: %w", err)
	}

	cfg := p.config(ctx, workspaceID)
	resolved := make([]domain.SkillCandidate, 0)
	for _, row := range rows {
		if row.DecidedAt == nil || now.Before(row.DecidedAt.Add(time.Duration(cfg.ProbationWindowDays)*24*time.Hour)) {
			continue // still inside the window
		}
		switch {
		case probationBreach(cfg, row.HelpfulCount, row.HarmfulCount):
			updated, err := p.Disable(ctx, workspaceID, &row,
				fmt.Sprintf("harmful ratio %.2f still over threshold %.2f at window close (%d outcomes: %d helpful / %d harmful)",
					ratio(row.HarmfulCount, row.HelpfulCount), cfg.HarmfulRatioThreshold,
					row.HelpfulCount+row.HarmfulCount, row.HelpfulCount, row.HarmfulCount))
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, *updated)
		case graduates(cfg, row.HelpfulCount, row.HarmfulCount):
			if err := p.candidates.UpdateStatus(ctx, workspaceID, row.ID, domain.SkillCandidateApproved, ""); err != nil {
				return nil, fmt.Errorf("skillcuration: graduate candidate %s: %w", row.ID, err)
			}
			p.log.InfoContext(ctx, "skillcuration: curated skill graduated",
				"workspace_id", workspaceID, "agent_id", row.AgentID,
				"candidate_id", row.ID, "skill_name", row.SkillName,
				"helpful", row.HelpfulCount, "harmful", row.HarmfulCount)
			updated, err := p.candidates.Get(ctx, workspaceID, row.ID)
			if err != nil {
				return nil, fmt.Errorf("skillcuration: re-read graduated candidate: %w", err)
			}
			resolved = append(resolved, *updated)
		default:
			// Over threshold but below the minimum sample: the tally is not
			// trusted yet — the row stays provisional and review-visible.
			p.log.DebugContext(ctx, "skillcuration: probation window closed under-sampled; keeping provisional",
				"workspace_id", workspaceID, "candidate_id", row.ID,
				"skill_name", row.SkillName,
				"outcomes", row.HelpfulCount+row.HarmfulCount,
				"minimum", cfg.MinProbationSample)
		}
	}
	return resolved, nil
}

// Disable archives one curated skill out of every catalog and flips its row
// to disabled with the reason verbatim (the review surface lists disabled
// rows by status, tally attached — spec: "Disabled skill keeps its
// history"). The skill directory moves to the agent's archive dir — a
// rename, so SKILL.md and PURPOSE.md (the evidence provenance) travel
// intact; a missing source directory is the goal state already (the
// row-only flip still lands). Workspace/agent resolution failures and store
// failures surface as errors; the row stays untouched on failure.
func (p *Probation) Disable(ctx context.Context, workspaceID string, candidate *domain.SkillCandidate, reason string) (*domain.SkillCandidate, error) {
	ws, err := p.workspaces.ByID(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: resolve workspace: %w", err)
	}
	agent, err := p.agents.ByID(ctx, workspaceID, candidate.AgentID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: resolve agent: %w", err)
	}

	skillDir := filepath.Join(domain.AgentSkillsDir(p.onClawDir, ws.Slug, agent.Slug), candidate.SkillName)
	archiveDir := AgentSkillsArchiveDir(p.onClawDir, ws.Slug, agent.Slug)
	if _, err := os.Stat(skillDir); err == nil {
		// Move the dir out of the scan. A stale archive of the same name is
		// cleared first (rename cannot replace a non-empty dir): the DB rows
		// keep every candidate's full evidence lineage, the directory only
		// ever holds the newest archived copy.
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return nil, fmt.Errorf("skillcuration: create archive dir: %w", err)
		}
		if err := os.RemoveAll(filepath.Join(archiveDir, candidate.SkillName)); err != nil {
			return nil, fmt.Errorf("skillcuration: clear stale archive of %s: %w", candidate.SkillName, err)
		}
		if err := os.Rename(skillDir, filepath.Join(archiveDir, candidate.SkillName)); err != nil {
			return nil, fmt.Errorf("skillcuration: archive skill dir %s: %w", candidate.SkillName, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("skillcuration: stat skill dir %s: %w", candidate.SkillName, err)
	} else {
		p.log.DebugContext(ctx, "skillcuration: disabling a skill whose directory is already gone",
			"workspace_id", workspaceID, "agent_id", candidate.AgentID,
			"skill_name", candidate.SkillName)
	}

	if err := p.candidates.UpdateStatus(ctx, workspaceID, candidate.ID, domain.SkillCandidateDisabled, reason); err != nil {
		return nil, fmt.Errorf("skillcuration: disable candidate: %w", err)
	}
	p.log.InfoContext(ctx, "skillcuration: curated skill disabled and archived",
		"workspace_id", workspaceID, "agent_id", candidate.AgentID,
		"candidate_id", candidate.ID, "skill_name", candidate.SkillName,
		"reason", reason)

	return p.candidates.Get(ctx, workspaceID, candidate.ID)
}

// provisionalBySkill finds the newest provisional candidate row for the
// (workspace, agent, skill) triple — the live probation for one curated
// skill. domain.ErrNotFound when none is provisional.
func (p *Probation) provisionalBySkill(ctx context.Context, workspaceID, agentID, skillName string) (*domain.SkillCandidate, error) {
	rows, err := p.candidates.List(ctx, workspaceID, domain.SkillCandidateProvisional)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: list provisional candidates: %w", err)
	}
	// List returns newest proposal first — the first match is the live one.
	for i := range rows {
		if rows[i].AgentID == agentID && rows[i].SkillName == skillName {
			return &rows[i], nil
		}
	}
	return nil, fmt.Errorf("%w: no provisional curated skill %q on agent %s", domain.ErrNotFound, skillName, agentID)
}

// probationBreach is the auto-disable predicate: at least
// MinProbationSample classified outcomes and a harmful ratio strictly above
// HarmfulRatioThreshold (spec: "more harmful than helpful outcomes past the
// minimum sample").
func probationBreach(cfg Config, helpful, harmful int) bool {
	total := helpful + harmful
	if total < cfg.MinProbationSample {
		return false
	}
	return ratio(harmful, helpful) > cfg.HarmfulRatioThreshold
}

// graduates is the window-close predicate: the harmful ratio is at or under
// the threshold. An outcome-free window passes (nothing disqualified it).
func graduates(cfg Config, helpful, harmful int) bool {
	return ratio(harmful, helpful) <= cfg.HarmfulRatioThreshold
}

// ratio is harmful/(helpful+harmful); zero outcomes are a zero ratio.
func ratio(harmful, helpful int) float64 {
	total := helpful + harmful
	if total == 0 {
		return 0
	}
	return float64(harmful) / float64(total)
}
