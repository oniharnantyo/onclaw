package skillcuration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// The cycle's unit world (7.1/7.2): the fake store carries a converged
// cluster (two qualifying runs of the same procedure family) over a seeded
// session; the side-call seam is the scripted model behind the ModelResolver
// (the maintainer_test pattern); the wiki lives in a temp dir.
// ---------------------------------------------------------------------------

const cycleClusterID = "cl-cycle"

// newCycleWorld seeds the workspace and the agent and returns the OnClaw
// root for the wiki directory.
func newCycleWorld(t *testing.T) (context.Context, store.Store, string) {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Agents().Create(ctx, &domain.Agent{ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ctx, st, t.TempDir()
}

// seedConvergedCluster indexes two qualifying runs into one cluster and
// persists their transcript windows on one session — the convergence the
// cluster gate needs. The windows are built with buildWindow (which writes
// to the fixture session) and re-scoped to sessionID: the payloads carry no
// session id, so the re-scope is a field rewrite, and distinct sessions keep
// the store's (workspace, session, turn) membership identity collision-free.
func seedConvergedCluster(t *testing.T, ctx context.Context, st store.Store, clusterID, sessionID string) {
	t.Helper()
	a := buildWindow(t, sessionID+"-a", "turn-a", qualifyingCalls(), true)
	b := buildWindow(t, sessionID+"-b", "turn-b", qualifyingCalls(), true)
	for i := range a {
		a[i].SessionID = sessionID
	}
	for i := range b {
		b[i].SessionID = sessionID
	}
	if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, append(a, b...)); err != nil {
		t.Fatalf("append windows: %v", err)
	}
	indexedAt := time.Now().UTC().Add(-time.Hour)
	for i, turn := range []string{"turn-a", "turn-b"} {
		run := domain.SkillClusterRun{
			WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: clusterID,
			SessionID: sessionID, TurnID: turn, RunStatus: domain.SkillClusterRunCompleted,
			Origin: ingestOriginUser, Qualifying: true,
			ToolCalls: 9, DistinctTools: 4, Recoveries: 1, ErrorResults: 1,
			IndexedAt: indexedAt.Add(time.Duration(i) * time.Minute),
		}
		if err := st.SkillCandidates().IndexClusterRun(ctx, &run); err != nil {
			t.Fatalf("index cluster run: %v", err)
		}
	}
}

// cycleConfig is the test workspace's config source.
func cycleConfig(cfg Config) ConfigSource {
	return func(context.Context, string) Config { return cfg }
}

// newTestCycle builds the cycle over the world: the validator reads the real
// workspace-skill registry with a not-found agent reader and a two-tool
// catalog (the proposals must declare tools from exactly this list).
func newTestCycle(ctx context.Context, st store.Store, onClawDir string, cfg Config, resolver ModelResolver, opts ...CycleOption) *Cycle {
	reader := func(context.Context, string, string, string) (string, bool, error) { return "", false, nil }
	validator := NewDraftValidator(st.WorkspaceSkills(), reader, func() []string {
		return []string{"grafana.query", "files.write"}
	})
	probation := NewProbation(st.SkillCandidates(), st.Agents(), st.Workspaces(), cycleConfig(cfg), onClawDir, discardLog())
	return NewCycle(
		st.Workspaces(), st.SkillCandidates(), st.SessionEvents(),
		cycleConfig(cfg), resolver, validator, probation, onClawDir, discardLog(),
		opts...,
	)
}

// waitStatus polls until the workspace's cycle leaves the running state (or
// the deadline expires) and returns the terminal record.
func waitStatus(t *testing.T, c *Cycle, workspaceID string) CycleStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := c.Status(context.Background(), workspaceID)
		if st.State != CycleStateRunning && st.State != CycleStateIdle {
			return st
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("cycle never reached a terminal state: %+v", c.Status(context.Background(), workspaceID))
	return CycleStatus{}
}

// waitCycleTransition waits for a NEW terminal record after `from` — the
// manual trigger is asynchronous, so a follow-up RunNow must not mistake the
// previous cycle's terminal status for its own.
func waitCycleTransition(t *testing.T, c *Cycle, workspaceID string, from CycleState) CycleStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.Status(context.Background(), workspaceID).State != from {
			return waitStatus(t, c, workspaceID)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("cycle never left %q: %+v", from, c.Status(context.Background(), workspaceID))
	return CycleStatus{}
}

// maintenanceCreateOp is the scripted maintainer answer: one create op
// citing the seeded session.
func maintenanceCreateOp(slug string) string {
	return opsJSON(createOp(slug, "Deploy guard", "Check health before promoting.", fixtureSession))
}

// proposalDraft is the scripted proposer answer: a valid first-time proposal
// citing the created page and the seeded session.
func proposalDraft(name, citedPattern string) string {
	return fmt.Sprintf(`{"name":%q,"description":"Roll back safely after a failed deploy.","tools":["grafana.query"],"cited_patterns":[%q],"cited_runs":[%q],"supersedes":"","content":"# %s\n\nThe procedure.\n"}`,
		name, citedPattern, fixtureSession, name)
}

// TestCycleFullPipelineOverSeededEvents: one full cycle over a converged
// cluster produces wiki pages, a pending candidate, and a succeeded status
// with the health counters (7.2 integration).
func TestCycleFullPipelineOverSeededEvents(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)
	seedConvergedCluster(t, ctx, st, cycleClusterID, fixtureSession)

	mdl := &scriptedModel{responses: []string{
		maintenanceCreateOp("deploy-guard"),
		proposalDraft("deploy-rollback", "deploy-guard"),
	}}
	c := newTestCycle(ctx, st, onClawDir, DefaultConfig(), staticResolver(mdl))

	if err := c.RunNow(ctx, fixtureWorkspace); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	status := waitStatus(t, c, fixtureWorkspace)

	if status.State != CycleStateSucceeded {
		t.Fatalf("cycle state = %q, want succeeded; stages %+v", status.State, status.Stages)
	}
	if status.Trigger != CycleTriggerManual {
		t.Errorf("trigger = %q, want manual", status.Trigger)
	}
	for _, stage := range status.Stages {
		if !stage.OK {
			t.Errorf("stage %s failed: %s", stage.Stage, stage.Error)
		}
	}
	if len(status.Stages) != 4 {
		t.Errorf("expected 4 stage records, got %+v", status.Stages)
	}
	if status.Counters.ProposalsDrafted != 1 {
		t.Errorf("proposals drafted = %d, want 1", status.Counters.ProposalsDrafted)
	}
	if status.Counters.CandidatesPending != 1 {
		t.Errorf("pending after = %d, want 1", status.Counters.CandidatesPending)
	}
	if status.Counters.ClustersProcessed != 1 || status.Counters.ClustersConsidered != 1 {
		t.Errorf("cluster counters drifted: %+v", status.Counters)
	}
	// Qualification health: two qualifying runs of two indexed.
	if status.Counters.QualifyingRuns != 2 || status.Counters.ClusterRuns != 2 {
		t.Errorf("qualification counters drifted: %+v", status.Counters)
	}
	if status.Counters.ApprovedCuratedSkills != 0 || status.Counters.RejectedProposals != 0 {
		t.Errorf("approval counters must start empty: %+v", status.Counters)
	}

	// The wiki page exists.
	wiki := NewWiki(domain.WorkspaceSkillWikiDir(onClawDir, "qual"))
	if _, err := wiki.Page("deploy-guard"); err != nil {
		t.Fatalf("maintained page missing: %v", err)
	}

	// The pending candidate row exists with the drafted linkage.
	rows, err := st.SkillCandidates().List(ctx, fixtureWorkspace, domain.SkillCandidatePending)
	if err != nil || len(rows) != 1 {
		t.Fatalf("pending candidates = %d (%v), want 1", len(rows), err)
	}
	if rows[0].SkillName != "deploy-rollback" || rows[0].ClusterID != cycleClusterID {
		t.Errorf("candidate drifted: %+v", rows[0])
	}
	if len(rows[0].EvidenceEventIDs) != 1 || rows[0].EvidenceEventIDs[0] != fixtureSession {
		t.Errorf("candidate must cite the seeded session: %v", rows[0].EvidenceEventIDs)
	}
	if len(rows[0].CitedPatternRefs) != 1 || rows[0].CitedPatternRefs[0] != "deploy-guard" {
		t.Errorf("candidate must cite the created page: %v", rows[0].CitedPatternRefs)
	}
}

// TestCycleRunNowDoubleTriggerConflicts: the manual trigger and the shared
// in-flight set — a second RunNow while the first cycle is mid-flight is a
// wrapped domain.ErrConflict.
func TestCycleRunNowDoubleTriggerConflicts(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)
	seedConvergedCluster(t, ctx, st, cycleClusterID, fixtureSession)

	entered := make(chan struct{})
	release := make(chan struct{})
	mdl := &gatedModel{
		scriptedModel: scriptedModel{responses: []string{
			maintenanceCreateOp("deploy-guard"),
			proposalDraft("deploy-rollback", "deploy-guard"),
		}},
		entered: entered,
		release: release,
	}
	c := newTestCycle(ctx, st, onClawDir, DefaultConfig(), staticResolver(mdl))

	if err := c.RunNow(ctx, fixtureWorkspace); err != nil {
		t.Fatalf("first RunNow: %v", err)
	}
	<-entered // the first cycle is inside its maintenance side-call

	err := c.RunNow(ctx, fixtureWorkspace)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("double trigger = %v, want wrapped domain.ErrConflict", err)
	}

	close(release)
	status := waitStatus(t, c, fixtureWorkspace)
	if status.State != CycleStateSucceeded {
		t.Fatalf("released cycle state = %q, want succeeded: %+v", status.State, status.Stages)
	}
}

// TestCycleFailingStageDefersAndNextCycleProceeds: a failing side-call stage
// records a failed status (with the error excerpt), stores no candidate, and
// the next cycle — the manual trigger here, the same entry point the next
// tick drives — proceeds and succeeds.
func TestCycleFailingStageDefersAndNextCycleProceeds(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)
	seedConvergedCluster(t, ctx, st, cycleClusterID, fixtureSession)

	mdl := &scriptedModel{err: errors.New("provider exploded")}
	c := newTestCycle(ctx, st, onClawDir, DefaultConfig(), staticResolver(mdl))

	if err := c.RunNow(ctx, fixtureWorkspace); err != nil {
		t.Fatalf("RunNow (failing model): %v", err)
	}
	status := waitStatus(t, c, fixtureWorkspace)
	if status.State != CycleStateFailed {
		t.Fatalf("failing-model cycle state = %q, want failed", status.State)
	}
	byStage := map[string]CycleStageResult{}
	for _, stage := range status.Stages {
		byStage[stage.Stage] = stage
	}
	if byStage[stageMaintenance].OK {
		t.Fatalf("wiki maintenance must be the failed stage: %+v", status.Stages)
	}
	if !strings.Contains(byStage[stageMaintenance].Error, "provider exploded") {
		t.Errorf("stage error must carry the excerpt: %q", byStage[stageMaintenance].Error)
	}
	// The independent stages still ran: the probation sweep passed.
	if !byStage[stageProbationSweep].OK {
		t.Errorf("probation sweep is independent of the side-call failure: %+v", byStage[stageProbationSweep])
	}
	// Nothing pending: the failed cycle drafted nothing.
	rows, _ := st.SkillCandidates().List(ctx, fixtureWorkspace, domain.SkillCandidatePending)
	if len(rows) != 0 {
		t.Fatalf("a failed cycle must draft nothing, got %d rows", len(rows))
	}

	// The next cycle proceeds: the model recovers and the pipeline lands.
	mdl.mu.Lock()
	mdl.err = nil
	mdl.responses = []string{
		maintenanceCreateOp("deploy-guard"),
		proposalDraft("deploy-rollback", "deploy-guard"),
	}
	mdl.mu.Unlock()

	if err := c.RunNow(ctx, fixtureWorkspace); err != nil {
		t.Fatalf("second RunNow: %v", err)
	}
	status = waitCycleTransition(t, c, fixtureWorkspace, CycleStateFailed)
	if status.State != CycleStateSucceeded {
		t.Fatalf("recovered cycle state = %q, want succeeded: %+v", status.State, status.Stages)
	}
	rows, _ = st.SkillCandidates().List(ctx, fixtureWorkspace, domain.SkillCandidatePending)
	if len(rows) != 1 {
		t.Fatalf("recovered cycle must draft the candidate, got %d rows", len(rows))
	}
}

// TestCycleBudgetCapsClusters: the nightly budget caps the selected
// clusters — three converged clusters with K=4 process at most two
// (clusterBudget = K/2), and the status counters report it.
func TestCycleBudgetCapsClusters(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)
	for i, cluster := range []string{"cl-b1", "cl-b2", "cl-b3"} {
		seedConvergedCluster(t, ctx, st, cluster, fmt.Sprintf("sess-b%d", i))
	}

	cfg := DefaultConfig()
	cfg.NightlyBudgetK = 4
	// The scripted answers are cluster-agnostic: maintenance applies no ops,
	// the proposer declines — the counts prove the cap, not the content.
	mdl := &scriptedModel{responses: []string{"[]", "[]", "null", "null"}}
	c := newTestCycle(ctx, st, onClawDir, cfg, staticResolver(mdl))

	if err := c.RunNow(ctx, fixtureWorkspace); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	status := waitStatus(t, c, fixtureWorkspace)
	if status.State != CycleStateSucceeded {
		t.Fatalf("state = %q: %+v", status.State, status.Stages)
	}
	if status.Counters.ClustersConsidered != 3 {
		t.Errorf("considered = %d, want 3", status.Counters.ClustersConsidered)
	}
	if status.Counters.ClustersProcessed != 2 {
		t.Errorf("processed = %d, want 2 (K=4 caps at two clusters)", status.Counters.ClustersProcessed)
	}
	if calls := mdl.callInputs(); len(calls) != 4 {
		t.Errorf("side-calls = %d, want exactly 4 (two per selected cluster)", len(calls))
	}
}

// TestCyclePendingClusterSkipped: a converged cluster holding a pending
// candidate is not re-proposed — the human gate holds (spec: "Pending
// verdicts do not stall maintenance", proposal-side).
func TestCyclePendingClusterSkipped(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)
	seedConvergedCluster(t, ctx, st, cycleClusterID, fixtureSession)
	if err := st.SkillCandidates().Save(ctx, &domain.SkillCandidate{
		WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: cycleClusterID,
		SkillName: "already-pending", Status: domain.SkillCandidatePending,
		ProposedContent: "# already-pending\n\ncontent",
	}); err != nil {
		t.Fatalf("seed pending candidate: %v", err)
	}

	mdl := &scriptedModel{}
	c := newTestCycle(ctx, st, onClawDir, DefaultConfig(), staticResolver(mdl))

	if err := c.RunNow(ctx, fixtureWorkspace); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	status := waitStatus(t, c, fixtureWorkspace)
	if status.State != CycleStateSucceeded {
		t.Fatalf("state = %q: %+v", status.State, status.Stages)
	}
	// The backstop filtered the cluster before any side-call was spent.
	if status.Counters.ClustersProcessed != 0 {
		t.Errorf("processed = %d, want 0 (pending candidate holds the cluster)", status.Counters.ClustersProcessed)
	}
	if calls := mdl.callInputs(); len(calls) != 0 {
		t.Errorf("no side-call may be spent on a pending cluster, got %d", len(calls))
	}
}

// TestCycleDueInterval: the claim semantics — never-run workspaces are due;
// a claimed cycle starts the interval window; the interval boundary flips
// due back on (the white-box counterpart of the ticker test below).
func TestCycleDueInterval(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)

	now := time.Unix(1_700_000_000, 0)
	clock := now
	c := newTestCycle(ctx, st, onClawDir, DefaultConfig(), staticResolver(&scriptedModel{}),
		WithNow(func() time.Time { return clock }))

	if !c.due(ctx, fixtureWorkspace) {
		t.Fatal("a never-run workspace is due")
	}

	// A claimed cycle starts the window: not due inside the interval.
	c.stampCycleStart(fixtureWorkspace, clock)
	clock = clock.Add(DefaultConfig().CycleInterval - time.Second)
	if c.due(ctx, fixtureWorkspace) {
		t.Fatal("inside the interval the workspace is not due")
	}
	clock = clock.Add(2 * time.Second)
	if !c.due(ctx, fixtureWorkspace) {
		t.Fatal("past the interval the workspace is due again")
	}

	// A zero interval is always due (tests drive cycles back to back). A
	// separate service over its own zero-interval config, claimed and then
	// re-checked at the same instant.
	zero := DefaultConfig()
	zero.CycleInterval = 0
	zc := newTestCycle(ctx, st, onClawDir, zero, staticResolver(&scriptedModel{}),
		WithNow(func() time.Time { return clock }))
	zc.stampCycleStart(fixtureWorkspace, clock)
	if !zc.due(ctx, fixtureWorkspace) {
		t.Fatal("a zero interval is always due")
	}
}

// TestCycleTickerRunsDueWorkspaces: the started loop claims due workspaces
// on its own — with a zero interval every tick runs a cycle, and Stop halts
// cleanly (the claim-loop smoke).
func TestCycleTickerRunsDueWorkspaces(t *testing.T) {
	ctx, st, onClawDir := newCycleWorld(t)
	seedConvergedCluster(t, ctx, st, cycleClusterID, fixtureSession)

	mdl := &scriptedModel{responses: []string{
		maintenanceCreateOp("deploy-guard"),
		proposalDraft("deploy-rollback", "deploy-guard"),
	}}
	cfg := DefaultConfig()
	cfg.CycleInterval = 0 // due on every tick

	c := newTestCycle(ctx, st, onClawDir, cfg, staticResolver(mdl),
		WithTick(5*time.Millisecond))
	c.Start(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.Status(ctx, fixtureWorkspace).State == CycleStateSucceeded {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := c.Status(ctx, fixtureWorkspace).State; got != CycleStateSucceeded {
		t.Fatalf("ticker never completed a cycle, state = %q", got)
	}
	if got := c.Status(ctx, fixtureWorkspace).Trigger; got != CycleTriggerScheduled {
		t.Errorf("ticker-triggered cycle trigger = %q, want scheduled", got)
	}

	c.Stop()
	c.Stop() // idempotent

	// After Stop the loop no longer claims: stamp a fresh start and confirm
	// the status never leaves the terminal state within a generous window.
	before := c.Status(ctx, fixtureWorkspace)
	time.Sleep(50 * time.Millisecond)
	if after := c.Status(ctx, fixtureWorkspace); after.State != before.State {
		t.Errorf("status moved after Stop: %q → %q", before.State, after.State)
	}
}

// ---------------------------------------------------------------------------
// The gated model: the double-trigger test's clock. Generate signals entry,
// waits for the release, then answers from the scripted queue.
// ---------------------------------------------------------------------------

type gatedModel struct {
	scriptedModel
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *gatedModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.once.Do(func() { close(m.entered) })
	<-m.release
	return m.scriptedModel.Generate(ctx, input, opts...)
}
