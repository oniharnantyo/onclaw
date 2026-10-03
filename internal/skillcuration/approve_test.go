package skillcuration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// The approver's unit world (6.1, D5/D6): the fake store carries one
// workspace and one agent; the agent-tier reader reads the REAL temp skills
// directory so the collision checks, the materialization, and the archive
// all exercise the same on-disk truth the production wiring sees.
// ---------------------------------------------------------------------------

const reviewerID = "user-reviewer-1"

type approveWorld struct {
	ctx       context.Context
	st        store.Store
	onClawDir string
	ws        *domain.Workspace
	agent     *domain.Agent
}

// newApproveWorld seeds the workspace + agent over the fake store and a
// temp OnClaw root.
func newApproveWorld(t *testing.T) *approveWorld {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	ws := &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	agent := &domain.Agent{ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return &approveWorld{ctx: ctx, st: st, onClawDir: t.TempDir(), ws: ws, agent: agent}
}

// dirContentReader is the agent-tier SkillContentReader over the world's
// real skills directory (the production shape: found=false is absence).
func (w *approveWorld) dirContentReader() SkillContentReader {
	return func(_ context.Context, _, _, skillName string) (string, bool, error) {
		data, err := os.ReadFile(filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), skillName, skillFileName))
		if os.IsNotExist(err) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return string(data), true, nil
	}
}

// newApprover builds the approver over the world.
func (w *approveWorld) newApprover(cfg Config) *Approver {
	return NewApprover(
		w.st.SkillCandidates(), w.st.Agents(), w.st.Workspaces(),
		w.st.WorkspaceSkills(), w.dirContentReader(), staticTools("grafana.query", "files.write"),
		staticCfg(cfg), w.onClawDir, discardLog(),
	)
}

// pendingCandidate stores one pending candidate for the world's agent with
// the given skill name and content.
func (w *approveWorld) pendingCandidate(t *testing.T, skillName, content string) *domain.SkillCandidate {
	t.Helper()
	candidate := &domain.SkillCandidate{
		WorkspaceID:      fixtureWorkspace,
		AgentID:          fixtureAgent,
		ClusterID:        "cl-approve",
		SkillName:        skillName,
		Status:           domain.SkillCandidatePending,
		ProposedContent:  content,
		EvidenceEventIDs: []string{"sess-deleted-source", "sess-other"},
		CitedPatternRefs: []string{"deploy-guard"},
	}
	if err := w.st.SkillCandidates().Save(w.ctx, candidate); err != nil {
		t.Fatalf("save candidate: %v", err)
	}
	return candidate
}

func approveTestConfig() Config {
	cfg := DefaultConfig()
	cfg.CatalogBudgetPerAgent = 2
	return cfg
}

func TestApprover_ApproveMaterializesSkillAndEntersProbation(t *testing.T) {
	w := newApproveWorld(t)
	candidate := w.pendingCandidate(t, "deploy-rollback",
		"# Deploy Rollback\n\nRoll back the deploy.\n\ndescription: Roll back a failed deploy to the last green revision\n")

	got, err := w.newApprover(approveTestConfig()).Approve(w.ctx, ReviewRequest{
		WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID,
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	// The row entered probation, NOT approved — approval materializes AND
	// starts the provisional window (6.2).
	if got.Status != domain.SkillCandidateProvisional {
		t.Fatalf("approved row status = %q, want provisional", got.Status)
	}
	if got.DecidedAt == nil {
		t.Error("expected DecidedAt stamped at approval (the probation start)")
	}

	// The skill materialized atomically under the agent's skills dir with
	// both files.
	skillDir := filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "deploy-rollback")
	body, err := os.ReadFile(filepath.Join(skillDir, skillFileName))
	if err != nil || !strings.Contains(string(body), "Roll back the deploy") {
		t.Fatalf("SKILL.md missing or wrong: %v", err)
	}
	purpose, err := os.ReadFile(filepath.Join(skillDir, purposeFileName))
	if err != nil {
		t.Fatalf("PURPOSE.md missing: %v", err)
	}
	// PURPOSE.md carries the on-disk provenance: candidate id, cited
	// patterns, evidence runs, reviewer (D4).
	for _, want := range []string{candidate.ID, "deploy-guard", "sess-deleted-source", reviewerID} {
		if !strings.Contains(string(purpose), want) {
			t.Errorf("PURPOSE.md missing %q:\n%s", want, purpose)
		}
	}

	// The decision landed in the append-only audit trail.
	entries, err := w.st.SkillCandidates().ListImpactEntries(w.ctx, fixtureWorkspace, time.Time{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("impact entries = (%d, %v), want (1, nil)", len(entries), err)
	}
	entry := entries[0]
	if entry.Verdict != domain.SkillImpactApproved || entry.Reviewer != reviewerID || entry.Diff == "" {
		t.Errorf("approval audit entry drifted: %+v", entry)
	}

	// The materialized skill occupies budget.
	if n, err := w.st.SkillCandidates().CountApprovedCuratedByAgent(w.ctx, fixtureWorkspace, fixtureAgent); err != nil || n != 1 {
		t.Errorf("curated count = (%d, %v), want (1, nil)", n, err)
	}
}

func TestApprover_BudgetRefusalNamesRemedy(t *testing.T) {
	w := newApproveWorld(t)
	cfg := approveTestConfig()
	cfg.CatalogBudgetPerAgent = 1
	approver := w.newApprover(cfg)

	// One live curated skill fills the budget.
	first := w.pendingCandidate(t, "first-skill", "# First\n\ndescription: the first curated skill body\n")
	if _, err := approver.Approve(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: first.ID, Reviewer: reviewerID}); err != nil {
		t.Fatalf("first approve: %v", err)
	}

	second := w.pendingCandidate(t, "second-skill", "# Second\n\ndescription: the second curated skill body\n")
	got, err := approver.Approve(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: second.ID, Reviewer: reviewerID})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("budget refusal = %v, want domain.ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "at capacity (1/1)") || !strings.Contains(err.Error(), "disable or merge existing curated skills first") {
		t.Errorf("refusal must name the remedy, got: %v", err)
	}

	// The refused candidate stays pending for re-review.
	row, err := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, second.ID)
	if err != nil || row.Status != domain.SkillCandidatePending {
		t.Errorf("refused candidate = (%+v, %v), want still pending", row, err)
	}
	if got != nil {
		t.Errorf("a refused approve must not return a candidate, got %+v", got)
	}
}

func TestApprover_RaceCollisionStaysPending(t *testing.T) {
	w := newApproveWorld(t)
	approver := w.newApprover(approveTestConfig())

	candidate := w.pendingCandidate(t, "deploy-rollback",
		"# Deploy Rollback\n\ndescription: roll back a failed deploy to the last green revision\n")

	// A same-named skill reached the agent's directory between proposal and
	// approval (the race the human gate closes).
	skillDir := filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "deploy-rollback")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("seed colliding dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte("# hand-installed\n"), 0o644); err != nil {
		t.Fatalf("seed colliding skill: %v", err)
	}

	if _, err := approver.Approve(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("race collision = %v, want domain.ErrConflict", err)
	}

	row, err := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, candidate.ID)
	if err != nil || row.Status != domain.SkillCandidatePending {
		t.Fatalf("collision-blocked candidate = (%+v, %v), want still pending", row, err)
	}
	// The colliding skill's on-disk content was left alone.
	body, err := os.ReadFile(filepath.Join(skillDir, skillFileName))
	if err != nil || string(body) != "# hand-installed\n" {
		t.Errorf("collision check must not touch the existing skill: %v %q", err, body)
	}
}

func TestApprover_EditExemptFromOwnSupersededName(t *testing.T) {
	w := newApproveWorld(t)
	approver := w.newApprover(approveTestConfig())

	// The superseded skill lives on disk; the edit rewrites it in place —
	// the collision check exempts exactly the superseded name.
	oldContent := "# Deploy guard\n\nOld body.\n"
	skillDir := filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "deploy-guard")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("seed superseded dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(oldContent), 0o644); err != nil {
		t.Fatalf("seed superseded skill: %v", err)
	}

	candidate := &domain.SkillCandidate{
		WorkspaceID:         fixtureWorkspace,
		AgentID:             fixtureAgent,
		ClusterID:           "cl-edit",
		SkillName:           "deploy-guard",
		Status:              domain.SkillCandidatePending,
		ProposedContent:     "# Deploy guard\n\nNew corrected body.\n",
		IsEdit:              true,
		SupersedesSkillName: "deploy-guard",
		SupersededContent:   oldContent,
		EvidenceEventIDs:    []string{"sess-deleted-source"},
	}
	if err := w.st.SkillCandidates().Save(w.ctx, candidate); err != nil {
		t.Fatalf("save edit candidate: %v", err)
	}

	got, err := approver.Approve(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID})
	if err != nil {
		t.Fatalf("edit approve: %v", err)
	}
	if got.Status != domain.SkillCandidateProvisional {
		t.Fatalf("edit row status = %q, want provisional", got.Status)
	}
	body, err := os.ReadFile(filepath.Join(skillDir, skillFileName))
	if err != nil || !strings.Contains(string(body), "New corrected body") {
		t.Fatalf("edit did not rewrite the skill in place: %v %q", err, body)
	}
	if _, err := os.Stat(filepath.Join(skillDir, purposeFileName)); err != nil {
		t.Errorf("PURPOSE.md missing after edit approve: %v", err)
	}

	// An edit replaces a live skill one-for-one: it never consumes budget.
	if n, err := w.st.SkillCandidates().CountApprovedCuratedByAgent(w.ctx, fixtureWorkspace, fixtureAgent); err != nil || n != 1 {
		t.Errorf("edit must not grow the catalog count, got (%d, %v), want (1, nil)", n, err)
	}
}

func TestApprover_EditRenameRetiresOldDir(t *testing.T) {
	w := newApproveWorld(t)
	approver := w.newApprover(approveTestConfig())

	oldContent := "# Deploy guard\n\nOld body.\n"
	oldDir := filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "deploy-guard")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("seed superseded dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, skillFileName), []byte(oldContent), 0o644); err != nil {
		t.Fatalf("seed superseded skill: %v", err)
	}

	candidate := &domain.SkillCandidate{
		WorkspaceID:         fixtureWorkspace,
		AgentID:             fixtureAgent,
		ClusterID:           "cl-edit",
		SkillName:           "deploy-guard-v2",
		Status:              domain.SkillCandidatePending,
		ProposedContent:     "# Deploy guard v2\n\nNew corrected body.\n",
		IsEdit:              true,
		SupersedesSkillName: "deploy-guard",
		SupersededContent:   oldContent,
		EvidenceEventIDs:    []string{"sess-deleted-source"},
	}
	if err := w.st.SkillCandidates().Save(w.ctx, candidate); err != nil {
		t.Fatalf("save rename-edit candidate: %v", err)
	}

	if _, err := approver.Approve(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID}); err != nil {
		t.Fatalf("rename-edit approve: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("superseded dir must leave the catalog, stat = %v", err)
	}
	newDir := filepath.Join(domain.AgentSkillsDir(w.onClawDir, w.ws.Slug, w.agent.Slug), "deploy-guard-v2")
	if _, err := os.Stat(filepath.Join(newDir, skillFileName)); err != nil {
		t.Errorf("renamed skill missing: %v", err)
	}
}

func TestApprover_DeletedSourceDoesNotBlock(t *testing.T) {
	w := newApproveWorld(t)
	approver := w.newApprover(approveTestConfig())

	// The evidence ids name sessions that exist NOWHERE — the ids are
	// opaque strings by contract (D5); approval must never look them up.
	candidate := w.pendingCandidate(t, "deploy-rollback",
		"# Deploy Rollback\n\ndescription: roll back a failed deploy to the last green revision\n")

	if _, err := approver.Approve(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID}); err != nil {
		t.Fatalf("approve with deleted source = %v, want success", err)
	}
}

func TestApprover_RejectRequiresReasonAndLeavesWikiIntact(t *testing.T) {
	w := newApproveWorld(t)
	approver := w.newApprover(approveTestConfig())

	candidate := w.pendingCandidate(t, "deploy-rollback",
		"# Deploy Rollback\n\ndescription: roll back a failed deploy to the last green revision\n")

	// A blank reason is a 400-class refusal; nothing is recorded.
	if _, err := approver.Reject(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID}, "   "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank-reason reject = %v, want domain.ErrInvalid", err)
	}
	if n, err := w.st.SkillCandidates().CountPending(w.ctx, fixtureWorkspace); err != nil || n != 1 {
		t.Fatalf("blank reason must not consume the candidate, pending = (%d, %v)", n, err)
	}

	// The wiki exists with one page; rejection must not touch it (spec:
	// "Rejection leaves the wiki intact").
	wiki := NewWiki(filepath.Join(w.onClawDir, "skillwiki"))
	if err := wiki.Create(testPage("deploy-guard", "Deploy guard", "Check health first.", fixtureSession)); err != nil {
		t.Fatalf("seed wiki: %v", err)
	}
	before, err := wiki.Page("deploy-guard")
	if err != nil {
		t.Fatalf("read wiki before reject: %v", err)
	}
	logsBefore, err := wiki.ReadLog()
	if err != nil {
		t.Fatalf("read log before reject: %v", err)
	}

	got, err := approver.Reject(w.ctx, ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID},
		"already covered by the runbook")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got.Status != domain.SkillCandidateRejected || got.Reason != "already covered by the runbook" {
		t.Fatalf("rejected row drifted: %+v", got)
	}

	entries, err := w.st.SkillCandidates().ListImpactEntries(w.ctx, fixtureWorkspace, time.Time{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("impact entries = (%d, %v), want (1, nil)", len(entries), err)
	}
	entry := entries[0]
	if entry.Verdict != domain.SkillImpactRejected || entry.Reason != "already covered by the runbook" || entry.Reviewer != reviewerID {
		t.Errorf("rejection audit entry drifted: %+v", entry)
	}

	after, err := wiki.Page("deploy-guard")
	if err != nil {
		t.Fatalf("read wiki after reject: %v", err)
	}
	if after.Title != before.Title || after.Status != before.Status || after.SupersededBy != before.SupersededBy ||
		after.Body != before.Body || strings.Join(after.EvidenceRuns, ",") != strings.Join(before.EvidenceRuns, ",") {
		t.Errorf("wiki page changed across a rejection: before=%+v after=%+v", before, after)
	}
	logsAfter, err := wiki.ReadLog()
	if err != nil || len(logsAfter) != len(logsBefore) {
		t.Errorf("wiki log changed across a rejection: %v -> %v (%v)", logsBefore, logsAfter, err)
	}
}

func TestApprover_AlreadyDecidedIsConflict(t *testing.T) {
	w := newApproveWorld(t)
	approver := w.newApprover(approveTestConfig())

	candidate := w.pendingCandidate(t, "deploy-rollback",
		"# Deploy Rollback\n\ndescription: roll back a failed deploy to the last green revision\n")
	req := ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: candidate.ID, Reviewer: reviewerID}

	if _, err := approver.Approve(w.ctx, req); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	// A second approve (and any reject) on a decided row conflicts.
	if _, err := approver.Approve(w.ctx, req); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("double approve = %v, want domain.ErrConflict", err)
	}
	if _, err := approver.Reject(w.ctx, req, "changed my mind"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("reject after approve = %v, want domain.ErrConflict", err)
	}

	unknown := ReviewRequest{WorkspaceID: fixtureWorkspace, CandidateID: "no-such-id", Reviewer: reviewerID}
	if _, err := approver.Approve(w.ctx, unknown); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown candidate approve = %v, want domain.ErrNotFound", err)
	}
	if _, err := approver.Reject(w.ctx, unknown, "reason"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown candidate reject = %v, want domain.ErrNotFound", err)
	}
}
