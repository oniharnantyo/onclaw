package skillcuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ---------------------------------------------------------------------------
// The probation lifecycle (6.2, D6): provisional rows tally outcomes, a
// breach auto-disables (the skill dir archived out of the scan with its
// evidence), a full window under the threshold graduates. The fake store
// carries the rows; the skills directory is a real temp tree.
// ---------------------------------------------------------------------------

// probationWorld seeds one provisional candidate WITH its materialized
// skill dir, as approval (6.1) leaves the world.
type probationWorld struct {
	approveWorld
	probation *Probation
	candidate *domain.SkillCandidate
	skillDir  string
}

func newProbationWorld(t *testing.T, cfg Config) *probationWorld {
	t.Helper()
	w := &probationWorld{approveWorld: *newApproveWorld(t)}
	w.probation = NewProbation(
		w.st.SkillCandidates(), w.st.Agents(), w.st.Workspaces(),
		staticCfg(cfg), w.onClawDir, discardLog(),
	)

	// Materialize the skill exactly like an approval would.
	w.candidate = &domain.SkillCandidate{
		WorkspaceID:      fixtureWorkspace,
		AgentID:          fixtureAgent,
		ClusterID:        "cl-probation",
		SkillName:        "deploy-rollback",
		Status:           domain.SkillCandidateProvisional,
		ProposedContent:  "# Deploy Rollback\n\nRoll back the deploy.\n",
		EvidenceEventIDs: []string{"sess-deleted-source"},
		CitedPatternRefs: []string{"deploy-guard"},
	}
	// Approval stamps DecidedAt (the probation start); the world seeds the
	// post-approval state.
	stamp := time.Now().UTC()
	w.candidate.DecidedAt = &stamp
	if err := w.st.SkillCandidates().Save(w.ctx, w.candidate); err != nil {
		t.Fatalf("save provisional candidate: %v", err)
	}
	w.skillDir = filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "deploy-rollback")
	if err := os.MkdirAll(w.skillDir, 0o755); err != nil {
		t.Fatalf("materialize skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.skillDir, skillFileName), []byte(w.candidate.ProposedContent), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.skillDir, purposeFileName), []byte("# Purpose\n\ncited: deploy-guard, sess-deleted-source\n"), 0o644); err != nil {
		t.Fatalf("write PURPOSE.md: %v", err)
	}
	return w
}

// breachConfig makes the breach reachable in two recorded outcomes:
// MinProbationSample 2, threshold 0.5.
func breachConfig() Config {
	cfg := DefaultConfig()
	cfg.MinProbationSample = 2
	cfg.HarmfulRatioThreshold = 0.5
	return cfg
}

func TestProbation_HelpfulOutcomesStayProvisional(t *testing.T) {
	w := newProbationWorld(t, breachConfig())

	for i := 0; i < 5; i++ {
		row, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", true)
		if err != nil {
			t.Fatalf("record helpful outcome %d: %v", i+1, err)
		}
		if row.Status != domain.SkillCandidateProvisional {
			t.Fatalf("helpful outcome %d flipped status to %q", i+1, row.Status)
		}
	}
	row, _ := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, w.candidate.ID)
	if row.HelpfulCount != 5 || row.HarmfulCount != 0 || row.UseCount != 5 {
		t.Errorf("tally drifted: %+v", row)
	}
	if _, err := os.Stat(filepath.Join(w.skillDir, skillFileName)); err != nil {
		t.Errorf("skill dir must stay mounted while provisional: %v", err)
	}
}

func TestProbation_BreachAutoDisablesAndArchives(t *testing.T) {
	w := newProbationWorld(t, breachConfig())

	// Two harmful outcomes pass the minimum sample with ratio 1.0 > 0.5:
	// the second call must auto-disable.
	if _, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", false); err != nil {
		t.Fatalf("record harmful outcome 1: %v", err)
	}
	row, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", false)
	if err != nil {
		t.Fatalf("record harmful outcome 2: %v", err)
	}
	if row.Status != domain.SkillCandidateDisabled {
		t.Fatalf("breach did not disable, status = %q", row.Status)
	}
	if row.HelpfulCount != 0 || row.HarmfulCount != 2 || row.UseCount != 2 {
		t.Errorf("tally must survive the disable: %+v", row)
	}
	if row.Reason == "" || !strings.Contains(row.Reason, "breached threshold") {
		t.Errorf("disable reason must carry the tally, got %q", row.Reason)
	}

	// The skill dir left the scan shape: gone from skills/, archived WITH
	// its evidence provenance next door.
	if _, err := os.Stat(w.skillDir); !os.IsNotExist(err) {
		t.Errorf("skill dir must leave the catalog on disable, stat = %v", err)
	}
	archiveDir := AgentSkillsArchiveDir(w.onClawDir, w.ws.Slug, w.agent.Slug)
	archivedBody, err := os.ReadFile(filepath.Join(archiveDir, "deploy-rollback", skillFileName))
	if err != nil {
		t.Fatalf("archived SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(archivedBody), "Roll back the deploy") {
		t.Errorf("archived SKILL.md drifted: %q", archivedBody)
	}
	archivedPurpose, err := os.ReadFile(filepath.Join(archiveDir, "deploy-rollback", purposeFileName))
	if err != nil || !strings.Contains(string(archivedPurpose), "deploy-guard") {
		t.Errorf("archived PURPOSE.md must travel with the skill: %v %q", err, archivedPurpose)
	}

	// Evidence linkage stays queryable on the row (spec: "Disabled skill
	// keeps its history").
	got, _ := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, w.candidate.ID)
	if len(got.EvidenceEventIDs) != 1 || got.EvidenceEventIDs[0] != "sess-deleted-source" {
		t.Errorf("archived row lost its evidence linkage: %v", got.EvidenceEventIDs)
	}
	if len(got.CitedPatternRefs) != 1 || got.CitedPatternRefs[0] != "deploy-guard" {
		t.Errorf("archived row lost its cited patterns: %v", got.CitedPatternRefs)
	}

	// The disabled row appears on the review surface's disabled listing.
	disabled, err := w.st.SkillCandidates().List(w.ctx, fixtureWorkspace, domain.SkillCandidateDisabled)
	if err != nil || len(disabled) != 1 || disabled[0].ID != w.candidate.ID {
		t.Errorf("disabled listing = (%+v, %v), want the disabled row", disabled, err)
	}
}

func TestProbation_UnderSampledBreachKeepsProvisional(t *testing.T) {
	w := newProbationWorld(t, breachConfig())

	// One harmful outcome: ratio 1.0 over threshold but below the minimum
	// sample of 2 — the tally is not trusted yet.
	row, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", false)
	if err != nil {
		t.Fatalf("record harmful outcome: %v", err)
	}
	if row.Status != domain.SkillCandidateProvisional {
		t.Fatalf("under-sampled breach disabled the skill, status = %q", row.Status)
	}
	if _, err := os.Stat(filepath.Join(w.skillDir, skillFileName)); err != nil {
		t.Errorf("under-sampled breach must keep the skill mounted: %v", err)
	}
}

func TestProbation_UnknownSkillIsNotFound(t *testing.T) {
	w := newProbationWorld(t, breachConfig())

	if _, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "no-such-skill", true); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown skill outcome = %v, want domain.ErrNotFound", err)
	}
	// A foreign agent's same-named skill is equally unknown.
	if _, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, "agt-other", "deploy-rollback", true); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign agent outcome = %v, want domain.ErrNotFound", err)
	}
}

func TestProbation_SweepGraduatesCleanWindow(t *testing.T) {
	w := newProbationWorld(t, breachConfig())

	// A healthy tally: 4 helpful, 1 harmful — ratio 0.2, under 0.5.
	for i := 0; i < 4; i++ {
		if _, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", true); err != nil {
			t.Fatalf("record helpful outcome: %v", err)
		}
	}
	if _, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", false); err != nil {
		t.Fatalf("record harmful outcome: %v", err)
	}

	// Age the approval past the window by sweeping at a now beyond any
	// DecidedAt the store stamps (the sweep takes now as a parameter).
	resolved, err := w.probation.SweepProbation(w.ctx, fixtureWorkspace, time.Now().UTC().Add(30*24*time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Status != domain.SkillCandidateApproved {
		t.Fatalf("sweep resolved = %+v, want the row graduated to approved", resolved)
	}
	got, _ := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, w.candidate.ID)
	if got.Status != domain.SkillCandidateApproved {
		t.Errorf("graduated row status = %q, want approved", got.Status)
	}
}

func TestProbation_SweepDisablesBreachedWindow(t *testing.T) {
	w := newProbationWorld(t, breachConfig())
	cfg := breachConfig()

	// A breaching tally: 0 helpful, 2 harmful.
	for i := 0; i < 2; i++ {
		if _, err := w.probation.RecordSkillOutcome(w.ctx, fixtureWorkspace, fixtureAgent, "deploy-rollback", false); err != nil {
			t.Fatalf("record harmful outcome: %v", err)
		}
	}
	// RecordSkillOutcome disabled it on the second call already; rebuild a
	// provisional row to exercise the SWEEP-side disable specifically.
	aged := &domain.SkillCandidate{
		WorkspaceID:      fixtureWorkspace,
		AgentID:          fixtureAgent,
		ClusterID:        "cl-probation",
		SkillName:        "log-rotation",
		Status:           domain.SkillCandidateProvisional,
		ProposedContent:  "# Log rotation\n\nRotate the logs.\n",
		HelpfulCount:     0,
		HarmfulCount:     3,
		UseCount:         3,
		EvidenceEventIDs: []string{"sess-logs"},
		DecidedAt:        pastTime(cfg),
	}
	if err := w.st.SkillCandidates().Save(w.ctx, aged); err != nil {
		t.Fatalf("save aged breaching row: %v", err)
	}
	logDir := filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "log-rotation")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("materialize log-rotation: %v", err)
	}

	resolved, err := w.probation.SweepProbation(w.ctx, fixtureWorkspace, time.Now().UTC())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Status != domain.SkillCandidateDisabled || resolved[0].SkillName != "log-rotation" {
		t.Fatalf("sweep resolved = %+v, want log-rotation disabled", resolved)
	}
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("sweep-side disable must archive the dir out of the scan, stat = %v", err)
	}
}

// pastTime returns a stamp one day before the config's window closes.
func pastTime(cfg Config) *time.Time {
	t := time.Now().UTC().Add(-time.Duration(cfg.ProbationWindowDays+1) * 24 * time.Hour)
	return &t
}

func TestProbation_SweepKeepsUndersampledProvisional(t *testing.T) {
	w := newProbationWorld(t, breachConfig())
	cfg := breachConfig()

	aged := &domain.SkillCandidate{
		WorkspaceID:      fixtureWorkspace,
		AgentID:          fixtureAgent,
		ClusterID:        "cl-probation",
		SkillName:        "quiet-skill",
		Status:           domain.SkillCandidateProvisional,
		ProposedContent:  "# Quiet skill\n\nDoes its thing.\n",
		HelpfulCount:     0,
		HarmfulCount:     1, // over threshold, below the minimum sample
		UseCount:         1,
		EvidenceEventIDs: []string{"sess-quiet"},
		DecidedAt:        pastTime(cfg),
	}
	if err := w.st.SkillCandidates().Save(w.ctx, aged); err != nil {
		t.Fatalf("save aged under-sampled row: %v", err)
	}

	resolved, err := w.probation.SweepProbation(w.ctx, fixtureWorkspace, time.Now().UTC())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("under-sampled row must stay unresolved, got %+v", resolved)
	}
	got, _ := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, aged.ID)
	if got.Status != domain.SkillCandidateProvisional {
		t.Errorf("under-sampled row status = %q, want provisional", got.Status)
	}
}

func TestProbation_SweepIgnoresInWindowRows(t *testing.T) {
	w := newProbationWorld(t, breachConfig())

	// The freshly approved row sits inside its window even with a breaching
	// tally pattern pending: only RecordSkillOutcome breaches mid-window.
	resolved, err := w.probation.SweepProbation(w.ctx, fixtureWorkspace, time.Now().UTC())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("in-window row must be untouched, got %+v", resolved)
	}
}
