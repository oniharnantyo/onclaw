//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Integration coverage for the skill-curation review queue
// (add-skill-curation-from-traces 2.3): the candidate lifecycle, impact
// trail, pending count, and cluster/audit reads run against real PostgreSQL
// — the same contract the in-memory fake passes.

// seedSkillCandidateFixtures creates one workspace with one agent (the FK
// targets the candidates ride on).
func seedSkillCandidateFixtures(t *testing.T, s store.Store) (wsID, agentID string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "pg-skcur", Name: "PG Skill Curation"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, a.ID
}

func newPGSkillCandidate(wsID, agentID, clusterID, skillName string) *domain.SkillCandidate {
	return &domain.SkillCandidate{
		WorkspaceID:      wsID,
		AgentID:          agentID,
		ClusterID:        clusterID,
		SkillName:        skillName,
		Status:           domain.SkillCandidatePending,
		ProposedContent:  "# " + skillName + "\ndeploy the thing",
		EvidenceEventIDs: []string{"evt-pg-1", "evt-pg-2"},
		CitedPatternRefs: []string{"patterns/retry-backoff.md"},
	}
}

func TestSkillCandidateStorePG_Lifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedSkillCandidateFixtures(t, s)
	candidates := s.SkillCandidates()

	// A pending candidate round-trips with store-assigned identity.
	cand := newPGSkillCandidate(wsID, agentID, "cluster-a", "deploy-rollback")
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
	if len(got.EvidenceEventIDs) != 2 || got.EvidenceEventIDs[0] != "evt-pg-1" {
		t.Errorf("evidence ids drifted: %v", got.EvidenceEventIDs)
	}
	if len(got.CitedPatternRefs) != 1 || got.CitedPatternRefs[0] != "patterns/retry-backoff.md" {
		t.Errorf("cited patterns drifted: %v", got.CitedPatternRefs)
	}

	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 1 {
		t.Fatalf("count pending = (%d, %v), want (1, nil)", n, err)
	}

	// The review flow: approve, probation, disable (archive keeps evidence).
	if err := candidates.UpdateStatus(ctx, wsID, cand.ID, domain.SkillCandidateApproved, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, _ = candidates.Get(ctx, wsID, cand.ID)
	if got.Status != domain.SkillCandidateApproved || got.DecidedAt == nil {
		t.Fatalf("approve did not land: %+v", got)
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
	if len(got.EvidenceEventIDs) != 2 {
		t.Errorf("archived row must retain its evidence linkage, got %v", got.EvidenceEventIDs)
	}
	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 0 {
		t.Fatalf("count pending after decisions = (%d, %v), want (0, nil)", n, err)
	}

	// The catalog-budget count covers only live curated rows (approved +
	// provisional); disabled and rejected rows don't occupy it.
	budget := newPGSkillCandidate(wsID, agentID, "cluster-c", "cache-warm")
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

	// List filters by status; an empty status lists everything.
	rejected := newPGSkillCandidate(wsID, agentID, "cluster-b", "log-rotation")
	if err := candidates.Save(ctx, rejected); err != nil {
		t.Fatalf("save rejected candidate: %v", err)
	}
	if err := candidates.UpdateStatus(ctx, wsID, rejected.ID, domain.SkillCandidateRejected, "already covered by runbook"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	all, err := candidates.List(ctx, wsID, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("list all = (%d, %v), want (3, nil)", len(all), err)
	}
	onlyRejected, err := candidates.List(ctx, wsID, domain.SkillCandidateRejected)
	if err != nil || len(onlyRejected) != 1 || onlyRejected[0].Reason != "already covered by runbook" {
		t.Fatalf("list rejected = (%+v, %v), want the rejected candidate", onlyRejected, err)
	}
}

func TestSkillCandidateStorePG_FailedRowAndRetryDraft(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedSkillCandidateFixtures(t, s)
	candidates := s.SkillCandidates()

	// A failed row (the extraction-failed card) stores without a skill name
	// or content — the error message rides Reason — and the status CHECK
	// accepts it. The pending badge is untouched.
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
	if got.Status != domain.SkillCandidateFailed || got.Reason == "" {
		t.Fatalf("failed row drifted: %+v", got)
	}

	// The manual retry's success path: UpdateDraft resets the row to pending
	// in place — Reason clears, DecidedAt clears, ProposedAt re-stamps, the
	// scope tuple stays.
	fresh := newPGSkillCandidate(wsID, agentID, "cluster-f", "deploy-rollback-v2")
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
	if len(got.EvidenceEventIDs) != 2 || len(got.CitedPatternRefs) != 1 {
		t.Errorf("retried evidence chain drifted: %v / %v", got.EvidenceEventIDs, got.CitedPatternRefs)
	}
	if got.ClusterID != "cluster-f" || got.AgentID != agentID {
		t.Errorf("scope tuple must stay: %+v", got)
	}
	if n, err := candidates.CountPending(ctx, wsID); err != nil || n != 1 {
		t.Errorf("retried row must count as pending: (%d, %v)", n, err)
	}

	// Absent and foreign ids are ErrNotFound; a nil draft is invalid.
	if err := candidates.UpdateDraft(ctx, wsID, "00000000-0000-0000-0000-000000000000", fresh); err != domain.ErrNotFound {
		t.Errorf("unknown UpdateDraft = %v, want ErrNotFound", err)
	}
	if err := candidates.UpdateDraft(ctx, "00000000-0000-0000-0000-000000000000", failed.ID, fresh); err != domain.ErrNotFound {
		t.Errorf("foreign UpdateDraft = %v, want ErrNotFound", err)
	}
	if err := candidates.UpdateDraft(ctx, wsID, failed.ID, nil); err == nil {
		t.Error("expected a nil draft to be rejected")
	}
}

func TestSkillCandidateStorePG_ClusterRuns(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedSkillCandidateFixtures(t, s)
	candidates := s.SkillCandidates()

	// One failed contrast member and two qualifying members of one family.
	failed := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-pg-runs",
		SessionID: "sess-1", TurnID: "turn-1",
		RunStatus: domain.SkillClusterRunFailed, Origin: "user",
		Qualifying: false, ToolCalls: 9, DistinctTools: 4, Recoveries: 0,
		ErrorResults: 2, TotalToolLatencyMS: 18000, WindowEndEventID: "evt-9",
	}
	if err := candidates.IndexClusterRun(ctx, failed); err != nil {
		t.Fatalf("index failed run: %v", err)
	}
	if failed.ID == "" || failed.IndexedAt.IsZero() {
		t.Fatalf("expected store-assigned id and stamped indexed_at, got %+v", failed)
	}
	for _, turn := range []string{"turn-2", "turn-3"} {
		run := &domain.SkillClusterRun{
			WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-pg-runs",
			SessionID: "sess-1", TurnID: turn,
			RunStatus: domain.SkillClusterRunCompleted, Origin: "user",
			Qualifying: true, ToolCalls: 9, DistinctTools: 4, Recoveries: 1,
		}
		if err := candidates.IndexClusterRun(ctx, run); err != nil {
			t.Fatalf("index qualifying run: %v", err)
		}
	}

	if n, err := candidates.CountQualifyingByCluster(ctx, wsID, "cluster-pg-runs"); err != nil || n != 2 {
		t.Fatalf("count qualifying = (%d, %v), want (2, nil)", n, err)
	}

	// The evidence pool lists every member oldest first.
	runs, err := candidates.ListClusterRunsByCluster(ctx, wsID, "cluster-pg-runs")
	if err != nil || len(runs) != 3 {
		t.Fatalf("list cluster runs = (%d, %v), want (3, nil)", len(runs), err)
	}
	if runs[0].TurnID != "turn-1" || runs[0].TotalToolLatencyMS != 18000 || runs[0].WindowEndEventID != "evt-9" {
		t.Errorf("failed run's row drifted: %+v", runs[0])
	}

	// Re-indexing the same run rewrites in place over the unique key: one
	// row per run, the store-assigned id stays stable.
	revisited := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-pg-runs",
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
	runs, _ = candidates.ListClusterRunsByCluster(ctx, wsID, "cluster-pg-runs")
	if len(runs) != 3 {
		t.Fatalf("cluster holds %d rows after re-index, want exactly 3", len(runs))
	}
	if n, err := candidates.CountQualifyingByCluster(ctx, wsID, "cluster-pg-runs"); err != nil || n != 3 {
		t.Fatalf("count qualifying after re-index = (%d, %v), want (3, nil)", n, err)
	}

	// Write-shape validation runs through the domain layer before SQL.
	badStatus := &domain.SkillClusterRun{
		WorkspaceID: wsID, AgentID: agentID, ClusterID: "cluster-pg-runs",
		SessionID: "sess-2", TurnID: "turn-9", RunStatus: "cancelled",
	}
	if err := candidates.IndexClusterRun(ctx, badStatus); err == nil {
		t.Error("expected an unknown run status to be rejected")
	}

	// Tenant partition: foreign cluster reads are empty — no existence leak.
	ws2 := &domain.Workspace{Slug: "pg-skcur-runs", Name: "PG Skill Curation Runs"}
	if err := s.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	if got, err := candidates.ListClusterRunsByCluster(ctx, ws2.ID, "cluster-pg-runs"); err != nil || len(got) != 0 {
		t.Errorf("foreign cluster read = (%d, %v), want (0, nil)", len(got), err)
	}
	if n, err := candidates.CountQualifyingByCluster(ctx, ws2.ID, "cluster-pg-runs"); err != nil || n != 0 {
		t.Errorf("foreign qualifying count = (%d, %v), want (0, nil)", n, err)
	}
}

func TestSkillCandidateStorePG_ListClusterIDs(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedSkillCandidateFixtures(t, s)
	candidates := s.SkillCandidates()

	// Two clusters hold membership; the enumeration returns each id once.
	for _, cluster := range []string{"cluster-ids-a", "cluster-ids-a", "cluster-ids-b"} {
		run := &domain.SkillClusterRun{
			WorkspaceID: wsID, AgentID: agentID, ClusterID: cluster,
			SessionID: "sess-" + cluster, TurnID: "turn-1",
			RunStatus: domain.SkillClusterRunCompleted, Origin: "user",
			Qualifying: true, ToolCalls: 9, DistinctTools: 4, Recoveries: 1,
		}
		if err := candidates.IndexClusterRun(ctx, run); err != nil {
			t.Fatalf("index run in %s: %v", cluster, err)
		}
	}
	ids, err := candidates.ListClusterIDs(ctx, wsID)
	if err != nil {
		t.Fatalf("list cluster ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("cluster ids = %v, want exactly the two distinct ids", ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen["cluster-ids-a"] || !seen["cluster-ids-b"] {
		t.Errorf("enumeration drifted: %v", ids)
	}

	// Tenant partition: a foreign workspace enumerates nothing.
	ws2 := &domain.Workspace{Slug: "pg-skcur-ids", Name: "PG Skill Curation Ids"}
	if err := s.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	got, err := candidates.ListClusterIDs(ctx, ws2.ID)
	if err != nil || len(got) != 0 {
		t.Errorf("foreign enumeration = (%v, %v), want empty", got, err)
	}
}

func TestSkillCandidateStorePG_ImpactEntriesAndClusters(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedSkillCandidateFixtures(t, s)
	candidates := s.SkillCandidates()
	cand := newPGSkillCandidate(wsID, agentID, "cluster-imp", "deploy-rollback")
	if err := candidates.Save(ctx, cand); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The audit trail is append-only, oldest first, and stamped app-side
	// when CreatedAt is zero.
	first := &domain.SkillImpactEntry{
		WorkspaceID: wsID,
		ClusterID:   "cluster-imp",
		SkillName:   "deploy-rollback",
		Verdict:     domain.SkillImpactApproved,
		Diff:        "+ deploy-rollback skill",
		Reviewer:    "user-admin",
	}
	if err := candidates.AppendImpactEntry(ctx, first); err != nil {
		t.Fatalf("append approved entry: %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("expected store-assigned id and created_at, got %+v", first)
	}
	second := &domain.SkillImpactEntry{
		WorkspaceID: wsID,
		ClusterID:   "cluster-imp",
		SkillName:   "deploy-rollback",
		Verdict:     domain.SkillImpactSuperseded,
		Diff:        "- old procedure\n+ new procedure",
		CreatedAt:   first.CreatedAt.Add(time.Hour),
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
	window, err := candidates.ListImpactEntries(ctx, wsID, second.CreatedAt)
	if err != nil || len(window) != 1 || window[0].Verdict != domain.SkillImpactSuperseded {
		t.Fatalf("since-filtered entries = (%+v, %v), want only the superseded entry", window, err)
	}

	// The suppression check and proposal step's cluster reads.
	latest, err := candidates.LatestImpactEntryForCluster(ctx, wsID, "cluster-imp")
	if err != nil || latest == nil || latest.Verdict != domain.SkillImpactSuperseded {
		t.Fatalf("latest entry for cluster = (%+v, %v), want the superseded entry", latest, err)
	}
	if none, err := candidates.LatestImpactEntryForCluster(ctx, wsID, "cluster-quiet"); err != nil || none != nil {
		t.Fatalf("quiet cluster = (%+v, %v), want (nil, nil)", none, err)
	}
	if got, err := candidates.ListByCluster(ctx, wsID, "cluster-imp"); err != nil || len(got) != 1 || got[0].ID != cand.ID {
		t.Fatalf("list by cluster = (%+v, %v), want the saved candidate", got, err)
	}

	// A rejection entry without a reason is rejected by the domain write
	// shape before any SQL runs.
	noReason := &domain.SkillImpactEntry{
		WorkspaceID: wsID,
		ClusterID:   "cluster-imp",
		SkillName:   "deploy-rollback",
		Verdict:     domain.SkillImpactRejected,
	}
	if err := candidates.AppendImpactEntry(ctx, noReason); err == nil {
		t.Error("expected a rejection without a reason to be rejected")
	}

	// Tenant partition: a foreign workspace's candidate and audit trail are
	// invisible — no existence leak.
	ws2 := &domain.Workspace{Slug: "pg-skcur-2", Name: "PG Skill Curation 2"}
	if err := s.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	if _, err := candidates.Get(ctx, ws2.ID, cand.ID); err != domain.ErrNotFound {
		t.Errorf("foreign get = %v, want ErrNotFound", err)
	}
	if got, err := candidates.ListByCluster(ctx, ws2.ID, "cluster-imp"); err != nil || len(got) != 0 {
		t.Errorf("foreign cluster read = (%d, %v), want (0, nil)", len(got), err)
	}
	if n, err := candidates.CountPending(ctx, ws2.ID); err != nil || n != 0 {
		t.Errorf("foreign pending count = (%d, %v), want (0, nil)", n, err)
	}
}

func TestSkillCandidateStorePG_IncrementOutcomeCounts(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, agentID := seedSkillCandidateFixtures(t, s)
	candidates := s.SkillCandidates()

	cand := newPGSkillCandidate(wsID, agentID, "cluster-pg-tally", "deploy-rollback")
	if err := candidates.Save(ctx, cand); err != nil {
		t.Fatalf("save: %v", err)
	}

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

	if err := candidates.IncrementOutcomeCounts(ctx, wsID, "no-such-id", true, true); err != domain.ErrNotFound {
		t.Errorf("unknown increment = %v, want ErrNotFound", err)
	}
}
