package skillcuration

import (
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ---------------------------------------------------------------------------
// The extraction-failed card's world (proposer drop path + the manual
// retry): a persistently invalid draft lands as a failed candidate row (the
// drop is no longer silent), and RetryCandidate re-runs the exact same draft
// path for its cluster — one model call, in-place row reset on success, a
// refreshed error message on failure.
// ---------------------------------------------------------------------------

// seedFailed stores one failed candidate in the fixture cluster (the card a
// retry acts on).
func (w *proposalWorld) seedFailed(t *testing.T, skillName, reason string) *domain.SkillCandidate {
	t.Helper()
	row := &domain.SkillCandidate{
		WorkspaceID:      fixtureWorkspace,
		AgentID:          fixtureAgent,
		ClusterID:        proposerCluster,
		SkillName:        skillName,
		Status:           domain.SkillCandidateFailed,
		Reason:           reason,
		EvidenceEventIDs: []string{},
		CitedPatternRefs: []string{},
	}
	if err := w.st.SkillCandidates().Save(w.ctx, row); err != nil {
		t.Fatalf("seed failed candidate: %v", err)
	}
	return row
}

func TestProposeClusterDropRecordsFailedRow(t *testing.T) {
	// A persistently invalid draft is dropped with a log AND recorded as a
	// failed candidate row: the cluster linkage survives so the reviewer can
	// retry the extraction, Reason carries the problems verbatim, and the
	// pending badge count is untouched.
	w := newProposalWorld(t)
	w.model.responses = []string{
		proposalJSON("deploy-guard", "", "k8s.reincarnate"),
		proposalJSON("deploy-guard", "", "k8s.reincarnate"),
	}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("dropped draft must not fail the stage: %v", err)
	}
	rows, err := w.st.SkillCandidates().ListByCluster(w.ctx, fixtureWorkspace, proposerCluster)
	if err != nil {
		t.Fatalf("list cluster candidates: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("candidates stored: %d, want the one failed row", len(rows))
	}
	got := rows[0]
	if got.Status != domain.SkillCandidateFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Reason, `unknown tool "k8s.reincarnate"`) {
		t.Errorf("Reason must carry the validation problems verbatim: %q", got.Reason)
	}
	if got.SkillName != "deploy-guard" {
		t.Errorf("SkillName = %q, want the last attempted draft's name", got.SkillName)
	}
	if got.ProposedContent != "" {
		t.Errorf("a failed row never materialized a draft, got content %q", got.ProposedContent)
	}
	if got.ClusterID != proposerCluster || got.AgentID != fixtureAgent {
		t.Errorf("cluster linkage lost: %+v", got)
	}
	if n, err := p.PendingCount(w.ctx, fixtureWorkspace); err != nil || n != 0 {
		t.Errorf("failed rows must not inflate the pending badge: (%d, %v)", n, err)
	}
}

func TestRetryCandidateHappyPath(t *testing.T) {
	// The failed card's retry: exactly one model call, and the row moves
	// back to pending IN PLACE with the fresh draft's name, content, and
	// evidence — the error message clears.
	w := newProposalWorld(t)
	failed := w.seedFailed(t, "deploy-guard", "response is not a valid proposal object: no JSON object in response")
	w.model.responses = []string{proposalJSON("deploy-guard-procedure", "")}
	p := w.newProposer(t)

	row, err := p.RetryCandidate(w.ctx, fixtureWorkspace, failed.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 1 {
		t.Errorf("model calls: %d, want exactly one", calls)
	}
	if row.ID != failed.ID {
		t.Errorf("retry must update the row in place, got id %s want %s", row.ID, failed.ID)
	}
	if row.Status != domain.SkillCandidatePending {
		t.Errorf("status = %q, want pending", row.Status)
	}
	if row.SkillName != "deploy-guard-procedure" {
		t.Errorf("skill name = %q, want the fresh draft's name", row.SkillName)
	}
	if row.Reason != "" {
		t.Errorf("reason = %q, want cleared on success", row.Reason)
	}
	if len(row.EvidenceEventIDs) != 1 || row.EvidenceEventIDs[0] != fixtureSession {
		t.Errorf("evidence ids = %v, want the fresh draft's citation", row.EvidenceEventIDs)
	}
	if len(row.CitedPatternRefs) != 1 || row.CitedPatternRefs[0] != "deploy-guard" {
		t.Errorf("cited patterns = %v, want [deploy-guard]", row.CitedPatternRefs)
	}
	// The retried row is a real pending candidate: the badge sees it.
	if n, err := p.PendingCount(w.ctx, fixtureWorkspace); err != nil || n != 1 {
		t.Errorf("pending count after retry = (%d, %v), want (1, nil)", n, err)
	}
}

func TestRetryCandidateFailureUpdatesMessage(t *testing.T) {
	// A retry that fails extraction again surfaces the error and refreshes
	// the row's message; the row stays failed (the card stays in the queue).
	w := newProposalWorld(t)
	failed := w.seedFailed(t, "deploy-guard", "earlier failure")
	w.model.responses = []string{proposalJSON("deploy-guard", "", "k8s.reincarnate")}
	p := w.newProposer(t)

	row, err := p.RetryCandidate(w.ctx, fixtureWorkspace, failed.ID)
	if err == nil {
		t.Fatal("retry must surface the extraction failure")
	}
	if row != nil {
		t.Errorf("failed retry must not return a row, got %+v", row)
	}
	if calls := len(w.model.callInputs()); calls != 1 {
		t.Errorf("model calls: %d, want exactly one (no retry loop on the manual path)", calls)
	}
	got, err := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, failed.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.SkillCandidateFailed {
		t.Errorf("status = %q, want still failed", got.Status)
	}
	if got.Reason == "earlier failure" || !strings.Contains(got.Reason, `unknown tool "k8s.reincarnate"`) {
		t.Errorf("failure message not refreshed: %q", got.Reason)
	}
}

func TestRetryCandidateGuards(t *testing.T) {
	// Only a failed candidate can be retried: pending is a conflict, and an
	// unknown or foreign id is ErrNotFound.
	w := newProposalWorld(t)
	p := w.newProposer(t)

	w.seedSiblingPending(t)
	rows := w.pendingCandidates(t)
	if len(rows) != 1 {
		t.Fatalf("pending seed: %d rows", len(rows))
	}
	if _, err := p.RetryCandidate(w.ctx, fixtureWorkspace, rows[0].ID); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("pending retry = %v, want ErrConflict", err)
	}
	if _, err := p.RetryCandidate(w.ctx, fixtureWorkspace, "no-such-candidate"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown retry = %v, want ErrNotFound", err)
	}
	if _, err := p.RetryCandidate(w.ctx, "ws-foreign", rows[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign retry = %v, want ErrNotFound", err)
	}
	if calls := len(w.model.callInputs()); calls != 0 {
		t.Errorf("guarded retries must not consult the model, got %d calls", calls)
	}
}
