package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// Store-level coverage for the skill-curation review queue
// (add-skill-curation-from-traces 2.3): the candidate lifecycle (insert
// pending -> approve -> probation -> disable), the append-only impact
// trail, the since-filtered audit listing, the pending count, and the
// cluster reads the suppression check and proposal step ride on.

// seedSkillCurationWorld creates one workspace with one agent and returns
// their ids.
func seedSkillCurationWorld(t *testing.T, s store.Store, suffix string) (wsID, agentID string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{ID: "ws-skcur-" + suffix, Slug: "skcur-" + suffix, Name: "Skill Curation " + suffix}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas-" + suffix, Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, a.ID
}

func newSkillCandidate(wsID, agentID, clusterID, skillName string) *domain.SkillCandidate {
	return &domain.SkillCandidate{
		WorkspaceID:      wsID,
		AgentID:          agentID,
		ClusterID:        clusterID,
		SkillName:        skillName,
		Status:           domain.SkillCandidatePending,
		ProposedContent:  "# " + skillName + "\ndeploy the thing",
		EvidenceEventIDs: []string{"evt-1", "evt-2"},
		CitedPatternRefs: []string{"patterns/retry-backoff.md"},
	}
}

func TestSkillCandidateStore_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, agentID := seedSkillCurationWorld(t, s, "life")
	candidates := s.SkillCandidates()

	// A pending candidate round-trips with store-assigned identity and
	// normalized evidence lists.
	cand := newSkillCandidate(wsID, agentID, "cluster-a", "deploy-rollback")
	cand.EvidenceEventIDs = nil // nil must store as the empty array
	if err := candidates.Save(ctx, cand); err != nil {
		t.Fatalf("save: %v", err)
	}
	if cand.ID == "" || cand.ProposedAt.IsZero() || cand.UpdatedAt.IsZero() {
		t.Fatalf("expected store-assigned id and timestamps, got %+v", cand)
	}
	got, err := candidates.Get(ctx, wsID, cand.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SkillName != "deploy-rollback" || got.Status != domain.SkillCandidatePending {
		t.Fatalf("round-trip drifted: %+v", got)
	}
	if got.EvidenceEventIDs == nil || len(got.EvidenceEventIDs) != 0 {
		t.Errorf("nil evidence list must normalize to the empty array, got %v", got.EvidenceEventIDs)
	}
	if len(got.CitedPatternRefs) != 1 || got.CitedPatternRefs[0] != "patterns/retry-backoff.md" {
		t.Errorf("cited patterns drifted: %v", got.CitedPatternRefs)
	}

	// Pending count sees it.
	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 1 {
		t.Fatalf("count pending = (%d, %v), want (1, nil)", n, err)
	}

	// The review flow: approve, then the probation lifecycle.
	if err := candidates.UpdateStatus(ctx, wsID, cand.ID, domain.SkillCandidateApproved, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, _ = candidates.Get(ctx, wsID, cand.ID)
	if got.Status != domain.SkillCandidateApproved || got.DecidedAt == nil {
		t.Fatalf("approve did not land: %+v", got)
	}
	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 0 {
		t.Fatalf("count pending after approve = (%d, %v), want (0, nil)", n, err)
	}
	if err := candidates.UpdateStatus(ctx, wsID, cand.ID, domain.SkillCandidateProvisional, ""); err != nil {
		t.Fatalf("provisional: %v", err)
	}
	if err := candidates.UpdateStatus(ctx, wsID, cand.ID, domain.SkillCandidateDisabled, "harmful ratio breach"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ = candidates.Get(ctx, wsID, cand.ID)
	if got.Status != domain.SkillCandidateDisabled || got.Reason != "harmful ratio breach" {
		t.Fatalf("disable did not land: %+v", got)
	}

	// Rejection records the reviewer reason.
	rejected := newSkillCandidate(wsID, agentID, "cluster-b", "log-rotation")
	if err := candidates.Save(ctx, rejected); err != nil {
		t.Fatalf("save rejected candidate: %v", err)
	}
	if err := candidates.UpdateStatus(ctx, wsID, rejected.ID, domain.SkillCandidateRejected, "already covered by runbook"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	got, _ = candidates.Get(ctx, wsID, rejected.ID)
	if got.Status != domain.SkillCandidateRejected || got.Reason != "already covered by runbook" {
		t.Fatalf("rejection did not land: %+v", got)
	}

	// The catalog-budget count covers only live curated rows (approved +
	// provisional); pending, rejected, and disabled rows don't occupy it.
	budget := newSkillCandidate(wsID, agentID, "cluster-c", "cache-warm")
	if err := candidates.Save(ctx, budget); err != nil {
		t.Fatalf("save budget candidate: %v", err)
	}
	if n, err := candidates.CountApprovedCuratedByAgent(ctx, wsID, agentID); err != nil || n != 0 {
		t.Fatalf("budget count with nothing live = (%d, %v), want (0, nil)", n, err)
	}
	if err := candidates.UpdateStatus(ctx, wsID, budget.ID, domain.SkillCandidateApproved, ""); err != nil {
		t.Fatalf("approve budget candidate: %v", err)
	}
	if n, err := candidates.CountApprovedCuratedByAgent(ctx, wsID, agentID); err != nil || n != 1 {
		t.Fatalf("budget count after approve = (%d, %v), want (1, nil)", n, err)
	}
	if err := candidates.UpdateStatus(ctx, wsID, budget.ID, domain.SkillCandidateProvisional, ""); err != nil {
		t.Fatalf("provisional budget candidate: %v", err)
	}
	if n, err := candidates.CountApprovedCuratedByAgent(ctx, wsID, agentID); err != nil || n != 1 {
		t.Fatalf("budget count while provisional = (%d, %v), want (1, nil)", n, err)
	}
	if n, err := candidates.CountApprovedCuratedByAgent(ctx, "ws-skcur-partb", agentID); err != nil || n != 0 {
		t.Fatalf("foreign budget count = (%d, %v), want (0, nil)", n, err)
	}

	// List filters by status; an empty status lists everything.
	all, err := candidates.List(ctx, wsID, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("list all = (%d, %v), want (3, nil)", len(all), err)
	}
	onlyPending, err := candidates.List(ctx, wsID, domain.SkillCandidatePending)
	if err != nil || len(onlyPending) != 0 {
		t.Fatalf("list pending after decisions = (%d, %v), want (0, nil)", len(onlyPending), err)
	}
}

func TestSkillCandidateStore_ImpactEntries(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, agentID := seedSkillCurationWorld(t, s, "impact")
	candidates := s.SkillCandidates()
	cand := newSkillCandidate(wsID, agentID, "cluster-imp", "deploy-rollback")
	if err := candidates.Save(ctx, cand); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The audit trail is append-only and ordered oldest first. Explicit
	// CreatedAt values keep the since-filter assertions deterministic (the
	// store honors caller-provided timestamps, stamping only when zero).
	first := &domain.SkillImpactEntry{
		WorkspaceID: wsID,
		ClusterID:   "cluster-imp",
		SkillName:   "deploy-rollback",
		Verdict:     domain.SkillImpactApproved,
		Diff:        "+ deploy-rollback skill",
		Reason:      "",
		Reviewer:    "user-admin",
		CreatedAt:   time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC),
	}
	if err := candidates.AppendImpactEntry(ctx, first); err != nil {
		t.Fatalf("append approved entry: %v", err)
	}
	if first.ID == "" {
		t.Fatalf("expected store-assigned id, got %+v", first)
	}
	second := &domain.SkillImpactEntry{
		WorkspaceID: wsID,
		ClusterID:   "cluster-imp",
		SkillName:   "deploy-rollback",
		Verdict:     domain.SkillImpactSuperseded,
		Diff:        "- old procedure\n+ new procedure",
		CreatedAt:   time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
	}
	if err := candidates.AppendImpactEntry(ctx, second); err != nil {
		t.Fatalf("append superseded entry: %v", err)
	}

	all, err := candidates.ListImpactEntries(ctx, wsID, time.Time{})
	if err != nil || len(all) != 2 {
		t.Fatalf("list impact entries = (%d, %v), want (2, nil)", len(all), err)
	}
	if all[0].Verdict != domain.SkillImpactApproved || all[1].Verdict != domain.SkillImpactSuperseded {
		t.Errorf("entries must come back oldest first, got %s then %s", all[0].Verdict, all[1].Verdict)
	}

	// The since filter is inclusive.
	since := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	window, err := candidates.ListImpactEntries(ctx, wsID, since)
	if err != nil || len(window) != 1 || window[0].Verdict != domain.SkillImpactSuperseded {
		t.Fatalf("since-filtered entries = (%+v, %v), want only the superseded entry", window, err)
	}

	// The suppression check reads the cluster's latest entry.
	latest, err := candidates.LatestImpactEntryForCluster(ctx, wsID, "cluster-imp")
	if err != nil || latest == nil || latest.Verdict != domain.SkillImpactSuperseded {
		t.Fatalf("latest entry for cluster = (%+v, %v), want the superseded entry", latest, err)
	}
	if none, err := candidates.LatestImpactEntryForCluster(ctx, wsID, "cluster-quiet"); err != nil || none != nil {
		t.Fatalf("quiet cluster = (%+v, %v), want (nil, nil)", none, err)
	}

	// Rejection entries require a reason (the domain write shape).
	noReason := &domain.SkillImpactEntry{
		WorkspaceID: wsID,
		ClusterID:   "cluster-imp",
		SkillName:   "deploy-rollback",
		Verdict:     domain.SkillImpactRejected,
	}
	if err := candidates.AppendImpactEntry(ctx, noReason); err == nil {
		t.Error("expected a rejection without a reason to be rejected")
	}
}

func TestSkillCandidateStore_ClusterRuns(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, agentID := seedSkillCurationWorld(t, s, "runs")
	_, agentB := seedSkillCurationWorld(t, s, "runsb")
	candidates := s.SkillCandidates()

	// One failed contrast member and two qualifying members of one family.
	// Explicit IndexedAt values keep the oldest-first assertion
	// deterministic (the store honors caller-provided timestamps, stamping
	// only when zero; same-instant rows fall back to the random-UUID id
	// tie-break, mirroring the postgres adapter).
	failed := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-runs",
		SessionID: "sess-1", TurnID: "turn-1",
		RunStatus: domain.SkillClusterRunFailed, Origin: "user",
		Qualifying: false, ToolCalls: 9, DistinctTools: 4, Recoveries: 0,
		ErrorResults: 2, TotalToolLatencyMS: 18000, WindowEndEventID: "evt-9",
		IndexedAt: time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC),
	}
	if err := candidates.IndexClusterRun(ctx, failed); err != nil {
		t.Fatalf("index failed run: %v", err)
	}
	if failed.ID == "" || failed.IndexedAt.IsZero() {
		t.Fatalf("expected store-assigned id and stamped indexed_at, got %+v", failed)
	}
	for i, turn := range []string{"turn-2", "turn-3"} {
		run := &domain.SkillClusterRun{
			WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-runs",
			SessionID: "sess-1", TurnID: turn,
			RunStatus: domain.SkillClusterRunCompleted, Origin: "user",
			Qualifying: true, ToolCalls: 9, DistinctTools: 4, Recoveries: 1,
			IndexedAt: time.Date(2026, 9, 30, 9, i+1, 0, 0, time.UTC),
		}
		if err := candidates.IndexClusterRun(ctx, run); err != nil {
			t.Fatalf("index qualifying run %d: %v", i, err)
		}
	}

	if n, err := candidates.CountQualifyingByCluster(ctx, wsID, "cluster-runs"); err != nil || n != 2 {
		t.Fatalf("count qualifying = (%d, %v), want (2, nil)", n, err)
	}
	if n, err := candidates.CountQualifyingByCluster(ctx, wsID, "cluster-quiet"); err != nil || n != 0 {
		t.Fatalf("quiet cluster count = (%d, %v), want (0, nil)", n, err)
	}

	// The evidence pool lists every member oldest first; the contrast read
	// is exactly the non-qualifying subset.
	runs, err := candidates.ListClusterRunsByCluster(ctx, wsID, "cluster-runs")
	if err != nil || len(runs) != 3 {
		t.Fatalf("list cluster runs = (%d, %v), want (3, nil)", len(runs), err)
	}
	if runs[0].TurnID != "turn-1" {
		t.Errorf("cluster runs must come back oldest first, got %q first", runs[0].TurnID)
	}
	contrast := 0
	for _, run := range runs {
		if !run.Qualifying {
			contrast++
		}
	}
	if contrast != 1 {
		t.Errorf("contrast members = %d, want exactly the failed run", contrast)
	}

	// Re-indexing the same run rewrites in place: one row per run, the
	// store-assigned id stays stable.
	revisited := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-runs",
		SessionID: "sess-1", TurnID: "turn-1",
		RunStatus: domain.SkillClusterRunCompleted, Origin: "user",
		Qualifying: true, ToolCalls: 9, DistinctTools: 4, Recoveries: 1,
	}
	if err := candidates.IndexClusterRun(ctx, revisited); err != nil {
		t.Fatalf("re-index: %v", err)
	}
	if revisited.ID != failed.ID {
		t.Errorf("re-index kept id %q, want the stable %q", revisited.ID, failed.ID)
	}
	runs, _ = candidates.ListClusterRunsByCluster(ctx, wsID, "cluster-runs")
	if len(runs) != 3 {
		t.Fatalf("cluster holds %d rows after re-index, want exactly 3", len(runs))
	}
	if n, err := candidates.CountQualifyingByCluster(ctx, wsID, "cluster-runs"); err != nil || n != 3 {
		t.Fatalf("count qualifying after re-index = (%d, %v), want (3, nil)", n, err)
	}

	// Write-shape validation runs through the domain layer.
	badStatus := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-runs",
		SessionID: "sess-2", TurnID: "turn-9", RunStatus: "cancelled",
	}
	if err := candidates.IndexClusterRun(ctx, badStatus); err == nil {
		t.Error("expected an unknown run status to be rejected")
	}

	// Tenant partition: another workspace's agent cannot index into this
	// one, and foreign cluster reads are empty — no existence leak.
	crossTenant := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentB, ClusterID: "cluster-runs",
		SessionID: "sess-x", TurnID: "turn-x",
		RunStatus: domain.SkillClusterRunCompleted,
	}
	if err := candidates.IndexClusterRun(ctx, crossTenant); err == nil {
		t.Error("expected a cross-workspace agent reference to be rejected")
	}
	if got, err := candidates.ListClusterRunsByCluster(ctx, "ws-skcur-runsb", "cluster-runs"); err != nil || len(got) != 0 {
		t.Errorf("foreign cluster read = (%d, %v), want (0, nil)", len(got), err)
	}
	if n, err := candidates.CountQualifyingByCluster(ctx, "ws-skcur-runsb", "cluster-runs"); err != nil || n != 0 {
		t.Errorf("foreign qualifying count = (%d, %v), want (0, nil)", n, err)
	}

	// The backstop enumeration: each distinct cluster id once, nothing
	// foreign.
	ids, err := candidates.ListClusterIDs(ctx, wsID)
	if err != nil || len(ids) != 1 || ids[0] != "cluster-runs" {
		t.Errorf("cluster ids = (%v, %v), want [cluster-runs]", ids, err)
	}
	if got, err := candidates.ListClusterIDs(ctx, "ws-skcur-runsb"); err != nil || len(got) != 0 {
		t.Errorf("foreign cluster ids = (%v, %v), want empty", got, err)
	}
}

func TestSkillCandidateStore_WorkspacePartitionAndValidation(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsA, agentA := seedSkillCurationWorld(t, s, "parta")
	_, agentB := seedSkillCurationWorld(t, s, "partb")
	candidates := s.SkillCandidates()

	cand := newSkillCandidate(wsA, agentA, "cluster-x", "deploy-rollback")
	if err := candidates.Save(ctx, cand); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A foreign workspace (and an unknown id) read as ErrNotFound — no
	// existence leak across the tenant boundary.
	if _, err := candidates.Get(ctx, "ws-skcur-partb", cand.ID); err != domain.ErrNotFound {
		t.Errorf("foreign get = %v, want ErrNotFound", err)
	}
	if _, err := candidates.Get(ctx, wsA, "no-such-id"); err != domain.ErrNotFound {
		t.Errorf("unknown get = %v, want ErrNotFound", err)
	}

	// Cluster reads are partitioned too.
	if got, err := candidates.ListByCluster(ctx, "ws-skcur-partb", "cluster-x"); err != nil || len(got) != 0 {
		t.Errorf("foreign cluster read = (%d, %v), want (0, nil)", len(got), err)
	}
	if got, err := candidates.ListByCluster(ctx, wsA, "cluster-x"); err != nil || len(got) != 1 {
		t.Errorf("own cluster read = (%d, %v), want (1, nil)", len(got), err)
	}

	// A candidate whose agent belongs to another workspace is rejected —
	// the fake enforces the same referential shape the FK does.
	foreign := newSkillCandidate(wsA, agentB, "cluster-y", "cross-tenant")
	if err := candidates.Save(ctx, foreign); err == nil {
		t.Error("expected a cross-workspace agent reference to be rejected")
	}

	// Write-shape validation runs through the domain layer.
	emptyName := newSkillCandidate(wsA, agentA, "cluster-z", "")
	if err := candidates.Save(ctx, emptyName); err == nil {
		t.Error("expected an empty skill name to be rejected")
	}
	halfEdit := newSkillCandidate(wsA, agentA, "cluster-z", "edit-proposal")
	halfEdit.IsEdit = true
	if err := candidates.Save(ctx, halfEdit); err == nil {
		t.Error("expected an edit without a superseded skill name to be rejected")
	}
	badStatus := newSkillCandidate(wsA, agentA, "cluster-z", "bad-status")
	badStatus.Status = domain.SkillCandidateStatus("archived")
	if err := candidates.Save(ctx, badStatus); err == nil {
		t.Error("expected an unknown status to be rejected")
	}
	if err := candidates.UpdateStatus(ctx, wsA, cand.ID, domain.SkillCandidateStatus("archived"), ""); err == nil {
		t.Error("expected an unknown status transition to be rejected")
	}
}

func TestSkillCandidateStore_FailedRowAndRetryDraft(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, agentID := seedSkillCurationWorld(t, s, "retry")
	candidates := s.SkillCandidates()

	// A failed row (the extraction-failed card) stores without a skill name
	// or content — the error message rides Reason and the pending badge is
	// untouched.
	failed := &domain.SkillCandidate{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-f",
		SkillName: "", Status: domain.SkillCandidateFailed,
		Reason: "response is not a valid proposal object: no JSON object in response",
	}
	if err := candidates.Save(ctx, failed); err != nil {
		t.Fatalf("save failed row: %v", err)
	}
	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 0 {
		t.Fatalf("failed rows must not count as pending: (%d, %v)", n, err)
	}
	got, err := candidates.Get(ctx, wsID, failed.ID)
	if err != nil {
		t.Fatalf("get failed row: %v", err)
	}
	if got.Status != domain.SkillCandidateFailed || got.ProposedContent != "" {
		t.Fatalf("failed row drifted: %+v", got)
	}

	// The manual retry's success path: UpdateDraft resets the row to pending
	// in place with the fresh draft's payload — Reason clears, DecidedAt
	// clears, ProposedAt re-stamps, the scope tuple stays.
	fresh := newSkillCandidate(wsID, agentID, "cluster-f", "deploy-rollback-v2")
	before := got.ProposedAt
	if err := candidates.UpdateDraft(ctx, wsID, failed.ID, fresh); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	got, _ = candidates.Get(ctx, wsID, failed.ID)
	if got.ID != failed.ID || got.Status != domain.SkillCandidatePending {
		t.Fatalf("retry reset drifted: %+v", got)
	}
	if got.SkillName != "deploy-rollback-v2" || got.Reason != "" || got.DecidedAt != nil {
		t.Errorf("draft payload did not land cleanly: %+v", got)
	}
	if !got.ProposedAt.After(before) {
		t.Errorf("ProposedAt must re-stamp on a retried draft: %v -> %v", before, got.ProposedAt)
	}
	if got.ClusterID != "cluster-f" || got.AgentID != agentID {
		t.Errorf("scope tuple must stay: %+v", got)
	}
	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 1 {
		t.Errorf("retried row must count as pending: (%d, %v)", n, err)
	}

	// Absent and foreign ids are ErrNotFound; an empty draft is invalid.
	if err := candidates.UpdateDraft(ctx, wsID, "no-such-id", fresh); err != domain.ErrNotFound {
		t.Errorf("unknown UpdateDraft = %v, want ErrNotFound", err)
	}
	if err := candidates.UpdateDraft(ctx, "ws-skcur-partb", failed.ID, fresh); err != domain.ErrNotFound {
		t.Errorf("foreign UpdateDraft = %v, want ErrNotFound", err)
	}
	if err := candidates.UpdateDraft(ctx, wsID, failed.ID, nil); err == nil {
		t.Error("expected a nil draft to be rejected")
	}
	// A draft that fails the domain's write shape is rejected.
	bad := newSkillCandidate(wsID, agentID, "cluster-f", "")
	if err := candidates.UpdateDraft(ctx, wsID, failed.ID, bad); err == nil {
		t.Error("expected an empty draft name to be rejected")
	}
}

func TestSkillCandidateStore_IncrementOutcomeCounts(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	wsID, agentID := seedSkillCurationWorld(t, s, "tally")
	candidates := s.SkillCandidates()

	cand := newSkillCandidate(wsID, agentID, "cluster-t", "deploy-rollback")
	if err := candidates.Save(ctx, cand); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Helpful and harmful tallies are additive; used bumps the use counter.
	if err := candidates.IncrementOutcomeCounts(ctx, wsID, cand.ID, true, true); err != nil {
		t.Fatalf("increment helpful: %v", err)
	}
	if err := candidates.IncrementOutcomeCounts(ctx, wsID, cand.ID, true, false); err != nil {
		t.Fatalf("increment helpful again: %v", err)
	}
	if err := candidates.IncrementOutcomeCounts(ctx, wsID, cand.ID, false, true); err != nil {
		t.Fatalf("increment harmful: %v", err)
	}
	got, err := candidates.Get(ctx, wsID, cand.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HelpfulCount != 2 || got.HarmfulCount != 1 || got.UseCount != 2 {
		t.Errorf("tally drifted: helpful=%d harmful=%d used=%d, want 2/1/2",
			got.HelpfulCount, got.HarmfulCount, got.UseCount)
	}
	if got.Status != domain.SkillCandidatePending {
		t.Errorf("tally must not touch the lifecycle status, got %q", got.Status)
	}

	// Absent and foreign-workspace candidates are ErrNotFound — no
	// existence leak, matching Get's convention.
	if err := candidates.IncrementOutcomeCounts(ctx, wsID, "no-such-id", true, true); err != domain.ErrNotFound {
		t.Errorf("unknown increment = %v, want ErrNotFound", err)
	}
	if err := candidates.IncrementOutcomeCounts(ctx, "ws-skcur-partb", cand.ID, true, true); err != domain.ErrNotFound {
		t.Errorf("foreign increment = %v, want ErrNotFound", err)
	}
}
