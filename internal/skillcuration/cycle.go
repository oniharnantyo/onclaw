package skillcuration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The curation cycle (add-skill-curation-from-traces 7.1, design D7): a
// claim-loop cloned from the scheduler (internal/scheduler/service.go). A
// ticker enumerates the workspaces and, for each whose workspace-configured
// CycleInterval has elapsed since its last cycle, runs one cycle; RunNow is
// the manual trigger and shares the SAME in-flight set — a double trigger is
// a wrapped domain.ErrConflict the HTTP layer maps to 409. One cycle is:
//
//	① probation sweep — graduate or disable provisional skills whose window
//	   closed (SweepProbation);
//	② qualify backstop — enumerate the clusters the membership index knows,
//	   keep those with the convergence gate open and no pending candidate
//	   (the qualifier already indexes runs per ingest job; the backstop
//	   covers the clusters whose convergence matured between cycles);
//	③ wiki maintenance — the maintainer side-call for the selected clusters;
//	④ proposals — the proposer side-call for the same clusters;
//	⑤ cycle status — the in-memory record the status API reads.
//
// Fail-soft at EVERY stage (D7): a stage error is recorded on the status
// (name + error excerpt) and logged; the remaining stages still run (an
// empty backstop makes maintenance and proposals quiet no-ops), the cycle
// always reaches a recorded status, and the ticker keeps firing — the next
// tick retries once the interval elapses, and RunNow retries immediately.
//
// The budget (3.2): NightlyBudgetK caps the side-calls one cycle spends.
// Each selected cluster costs up to two (one maintenance call, one proposal
// attempt), so the cluster selection is capped at K/2 (K=1 still selects one
// cluster — the rounding is documented here) and the selection order is the
// deterministic soft score (SelectForCycle) over the clusters' qualifying
// evidence.

// House defaults and bounds.
const (
	// defaultCycleTick is the claim-loop cadence. The workspace cadence
	// (Config.CycleInterval) is evaluated per tick against the in-memory
	// last-cycle stamp; a minute of claim latency is invisible on a nightly
	// schedule.
	defaultCycleTick = time.Minute
	// maxCycleRuntime bounds one workspace cycle. Each side-call self-bounds
	// at DefaultSideCallBudget, so the runtime cap only stops a pathologically
	// large selection (or a wedged store) from pinning Stop forever.
	maxCycleRuntime = 30 * time.Minute
	// cycleStopGrace extends Stop's wait past the runtime cap.
	cycleStopGrace = time.Minute
	// stageErrorExcerpt bounds one stage's recorded error excerpt.
	stageErrorExcerpt = 300
)

// Stage names — the cycle status's per-stage record.
const (
	stageProbationSweep = "probation_sweep"
	stageBackstop       = "qualify_backstop"
	stageMaintenance    = "wiki_maintenance"
	stageProposals      = "proposals"
)

// Cycle triggers.
const (
	CycleTriggerScheduled = "scheduled"
	CycleTriggerManual    = "manual"
)

// CycleState is one workspace cycle's lifecycle state (spec: "Cycle cadence
// and manual trigger" — running, succeeded, failed; idle is the
// never-run-since-start state the status API reports).
type CycleState string

const (
	CycleStateIdle      CycleState = "idle"
	CycleStateRunning   CycleState = "running"
	CycleStateSucceeded CycleState = "succeeded"
	CycleStateFailed    CycleState = "failed"
)

// CycleStageResult records one stage's outcome: ok, or the first error
// excerpt (fail-soft — a failed stage never aborts the cycle).
type CycleStageResult struct {
	Stage string `json:"stage"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// CycleCounters carries the cycle's health numbers (add-skill-curation-
// from-traces 7.2, design D6). The task-1 consolidator's StatsFunc seam is
// deliberately NOT extended — memory's MorningReport struct stays untouched
// (the memory specs must not move), so this status record carries the
// curation counters instead: the documented deviation from D6's "land in
// the morning report" phrasing, consistent with the planner's
// recommendation. The rates are computable from the counts:
// qualification rate = qualifying_runs / cluster_runs; approval rate =
// approved_curated_skills / (approved_curated_skills + rejected_proposals).
type CycleCounters struct {
	// ClustersConsidered counts every enumerated cluster; ClustersProcessed
	// counts those the budget selected for side-calls.
	ClustersConsidered int `json:"clusters_considered"`
	ClustersProcessed  int `json:"clusters_processed"`
	// ProposalsDrafted is the pending-count delta across the proposal stage.
	ProposalsDrafted int `json:"proposals_drafted"`
	// PatternsChanged is the wiki page-set delta across the maintenance
	// stage (pages created, superseded, merged, or status-flipped — the
	// maintainer logs op counts internally; the page-set delta is the
	// cycle-side observable).
	PatternsChanged int `json:"patterns_changed"`
	PatternCount    int `json:"pattern_count"`
	// CandidatesPending is the workspace's pending-candidate count after
	// the cycle — the review-badge number.
	CandidatesPending int `json:"candidates_pending"`
	// Probation outcomes from the sweep.
	ProbationGraduated int `json:"probation_graduated"`
	ProbationDisabled  int `json:"probation_disabled"`
	// Qualification health (D6 alarm: rate > cfg.QualificationRateAlarm):
	// qualifying runs over every indexed run across the enumerated clusters.
	QualifyingRuns int `json:"qualifying_runs"`
	ClusterRuns    int `json:"cluster_runs"`
	// Approval health (D6 alarm: rate < cfg.ApprovalRateAlarm): live curated
	// skills (approved + provisional) vs rejected proposals.
	ApprovedCuratedSkills int `json:"approved_curated_skills"`
	RejectedProposals     int `json:"rejected_proposals"`
}

// CycleStatus is one workspace's cycle record — the last (or current) run
// the status API serves, and the manual trigger's "running" acknowledgment.
type CycleStatus struct {
	WorkspaceID string             `json:"workspace_id"`
	State       CycleState         `json:"state"`
	Trigger     string             `json:"trigger,omitempty"`
	StartedAt   time.Time          `json:"started_at"`
	FinishedAt  time.Time          `json:"finished_at"`
	Stages      []CycleStageResult `json:"stages"`
	Counters    CycleCounters      `json:"counters"`
}

// cycleCluster is one gate-open, pending-free cluster the cycle selected.
// The cluster is agent-anchored (ClusterKey hashes the agent), so the first
// member's agent id anchors both side-calls.
type cycleCluster struct {
	ClusterID string
	AgentID   string
	members   []domain.SkillClusterRun
}

// Cycle runs the per-workspace curation cycles: a claim-loop ticker plus
// the manual trigger, both through one in-flight set and one runWorkspace
// path. The zero value is not usable; construct with NewCycle.
type Cycle struct {
	workspaces store.WorkspaceStore
	clusters   store.SkillCandidateStore
	sessions   store.SessionEventStore
	config     ConfigSource
	resolver   ModelResolver
	validator  *DraftValidator
	probation  *Probation
	onClawDir  string
	// chip is the one optional capability (the qualifier's
	// WithCandidateChipSink precedent): wired, the proposers the cycle builds
	// emit the drafted-skill chip through it; unwired, candidates still
	// store — only the transcript signal is dropped.
	chip CandidateChipSink
	log  *slog.Logger

	tick time.Duration
	now  func() time.Time

	mu sync.Mutex
	// inFlight guards one cycle per workspace across BOTH triggers (the
	// scheduler's markInFlight pattern).
	inFlight map[string]struct{}
	// lastStart stamps each claimed cycle's start — the claim (the
	// scheduler's claim-advances-next_run_at analog): the interval is
	// measured from the start, so a long cycle cannot shorten the gap.
	// In-memory by design (7.1): a restart re-runs one cycle per workspace,
	// which the stages' idempotence (probation sweeps, gate + pending
	// checks, suppression) makes harmless.
	lastStart map[string]time.Time
	statuses  map[string]*CycleStatus

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// CycleOption tunes a defaultable Cycle knob; invalid values keep the
// default.
type CycleOption func(*Cycle)

// WithTick sets the claim-loop cadence (the scheduler's WithTick analog).
func WithTick(d time.Duration) CycleOption {
	return func(c *Cycle) {
		if d > 0 {
			c.tick = d
		}
	}
}

// WithNow injects the clock (tests anchor the due math).
func WithNow(now func() time.Time) CycleOption {
	return func(c *Cycle) {
		if now != nil {
			c.now = now
		}
	}
}

// WithChipSink wires the drafted-skill chip emission for the proposers the
// cycle builds (the memory ChipSink precedent: an optional capability, never
// a nil-able dependency).
func WithChipSink(fn CandidateChipSink) CycleOption {
	return func(c *Cycle) {
		if fn != nil {
			c.chip = fn
		}
	}
}

// NewCycle constructs the cycle service from its granular dependencies,
// resolved non-nil by the composition root: the workspace store the ticker
// enumerates, the candidate store the cluster memberships and candidates
// read through, the session-event store the side-calls' windows load from,
// the workspace config source (interval, cluster minimum, budget), the model
// resolver (CurationModelResolver in production), the draft validator
// (workspace-agnostic — every method is workspace-scoped), the probation
// manager, the OnClaw root (the per-workspace wiki directory), and the
// logger.
func NewCycle(
	workspaces store.WorkspaceStore,
	clusters store.SkillCandidateStore,
	sessions store.SessionEventStore,
	config ConfigSource,
	resolver ModelResolver,
	validator *DraftValidator,
	probation *Probation,
	onClawDir string,
	log *slog.Logger,
	opts ...CycleOption,
) *Cycle {
	c := &Cycle{
		workspaces: workspaces,
		clusters:   clusters,
		sessions:   sessions,
		config:     config,
		resolver:   resolver,
		validator:  validator,
		probation:  probation,
		onClawDir:  onClawDir,
		log:        log,
		tick:       defaultCycleTick,
		now:        time.Now,
		inFlight:   make(map[string]struct{}),
		lastStart:  make(map[string]time.Time),
		statuses:   make(map[string]*CycleStatus),
		stopCh:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Start launches the claim-loop ticker. The loop exits when ctx is cancelled
// or Stop is called; in-flight cycles are not interrupted — Stop waits for
// them.
func (c *Cycle) Start(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(c.tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-ticker.C:
				c.tickOnce(ctx)
			}
		}
	}()
}

// Stop halts the ticker and waits for in-flight cycles, bounded by the
// cycle runtime cap plus the stop grace (every cycle self-bounds at
// maxCycleRuntime, so the wait converges). Idempotent.
func (c *Cycle) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(maxCycleRuntime + cycleStopGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		c.log.Warn("skillcuration: stop deadline exceeded with cycles still in flight")
	}
}

// RunNow is the manual trigger (spec: "Manual trigger runs a cycle now"):
// one cycle for the workspace executes immediately, bypassing the interval
// check, through the SAME runWorkspace path and in-flight set as the
// ticker. A cycle already running conflicts (the handler maps the wrapped
// domain.ErrConflict to 409); an unknown workspace surfaces the store's
// ErrNotFound. The cycle runs asynchronously — the caller reads its state
// through Status.
func (c *Cycle) RunNow(ctx context.Context, workspaceID string) error {
	ws, err := c.workspaces.ByID(ctx, workspaceID)
	if err != nil {
		return err
	}
	if !c.markInFlight(workspaceID) {
		return fmt.Errorf("skillcuration cycle still running for workspace %s: %w", workspaceID, domain.ErrConflict)
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer c.unmarkInFlight(workspaceID)
		cycleCtx, cancel := c.cycleContext(ctx)
		defer cancel()
		c.runWorkspace(cycleCtx, ws, CycleTriggerManual)
	}()
	return nil
}

// Status returns the workspace's last (or current) cycle record; a
// workspace that never ran one reports the idle state.
func (c *Cycle) Status(_ context.Context, workspaceID string) CycleStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if st, ok := c.statuses[workspaceID]; ok {
		return *st
	}
	return CycleStatus{WorkspaceID: workspaceID, State: CycleStateIdle}
}

// tickOnce claims every due workspace into a cycle goroutine (the
// scheduler's claim-dispatch pass). Enumeration or per-workspace failures
// log and never wedge the loop.
func (c *Cycle) tickOnce(ctx context.Context) {
	workspaces, err := c.workspaces.ListAll(ctx)
	if err != nil {
		c.log.WarnContext(ctx, "skillcuration: enumerate workspaces failed", "error", err)
		return
	}
	for _, ws := range workspaces {
		ws := ws
		if !c.due(ctx, ws.ID) {
			continue
		}
		if !c.markInFlight(ws.ID) {
			c.log.DebugContext(ctx, "skillcuration: cycle tick skipped; cycle still running", "workspace_id", ws.ID)
			continue
		}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			defer c.unmarkInFlight(ws.ID)
			cycleCtx, cancel := c.cycleContext(ctx)
			defer cancel()
			c.runWorkspace(cycleCtx, &ws, CycleTriggerScheduled)
		}()
	}
}

// due reports whether the workspace's CycleInterval elapsed since its last
// claimed cycle (never-run workspaces are due immediately — the first tick
// after start runs one cycle per workspace).
func (c *Cycle) due(ctx context.Context, workspaceID string) bool {
	c.mu.Lock()
	last, ran := c.lastStart[workspaceID]
	c.mu.Unlock()
	if !ran {
		return true
	}
	interval := c.config(ctx, workspaceID).CycleInterval
	if interval <= 0 {
		return true
	}
	return c.now().Sub(last) >= interval
}

// cycleContext derives a cycle's context: detached from the caller (a
// server shutdown or an HTTP request returning must not kill a cycle's tail
// work — the scheduler's fireContext reasoning) and bounded by the runtime
// cap so Stop's wait converges.
func (c *Cycle) cycleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), maxCycleRuntime)
}

// markInFlight registers a workspace as cycling; false means a cycle is
// already live (the loop skips; RunNow conflicts).
func (c *Cycle) markInFlight(workspaceID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, live := c.inFlight[workspaceID]; live {
		return false
	}
	c.inFlight[workspaceID] = struct{}{}
	return true
}

func (c *Cycle) unmarkInFlight(workspaceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inFlight, workspaceID)
}

// stampCycleStart claims the interval window at cycle start (the scheduler's
// claim-advances-next_run_at analog).
func (c *Cycle) stampCycleStart(workspaceID string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastStart[workspaceID] = at
}

func (c *Cycle) setStatus(workspaceID string, status *CycleStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := *status
	c.statuses[workspaceID] = &cp
}

// runWorkspace executes one workspace's cycle through the five stages. It
// NEVER returns an error: every stage failure is recorded on the status and
// logged (D7 fail-soft), and the roll-up state is failed only when a stage
// failed.
func (c *Cycle) runWorkspace(ctx context.Context, ws *domain.Workspace, trigger string) {
	started := c.now().UTC()
	status := &CycleStatus{
		WorkspaceID: ws.ID,
		State:       CycleStateRunning,
		Trigger:     trigger,
		StartedAt:   started,
	}
	c.setStatus(ws.ID, status)
	c.stampCycleStart(ws.ID, started)
	c.log.InfoContext(ctx, "skillcuration: cycle started",
		"workspace_id", ws.ID, "trigger", trigger)

	stages := make([]CycleStageResult, 0, 4)
	counters := CycleCounters{}

	// ① Probation sweep — graduate or disable provisional skills whose
	// window closed. Idempotent; safe on every tick.
	graduated, disabled, err := c.stageProbationSweep(ctx, ws.ID)
	stages = append(stages, stageResult(stageProbationSweep, err))
	counters.ProbationGraduated = graduated
	counters.ProbationDisabled = disabled

	// ② Qualify backstop — the gate-open, pending-free clusters.
	refs, err := c.stageBackstop(ctx, ws.ID, &counters)
	stages = append(stages, stageResult(stageBackstop, err))

	// The budget: rank the gate-open clusters and cap the selection.
	selected := c.selectWithinBudget(ctx, ws.ID, refs)
	counters.ClustersProcessed = len(selected)

	// ③ + ④ share the workspace's wiki (one Wiki per workspace, D3).
	wiki := NewWiki(domain.WorkspaceSkillWikiDir(c.onClawDir, ws.Slug))

	// ③ Wiki maintenance — every cycle, regardless of pending verdicts
	// (spec: "Pending verdicts do not stall maintenance").
	changed, patternCount, err := c.stageMaintenance(ctx, ws.ID, wiki, selected)
	stages = append(stages, stageResult(stageMaintenance, err))
	counters.PatternsChanged = changed
	counters.PatternCount = patternCount

	// ④ Proposals — at most one atomic create-or-edit per cluster per
	// cycle; human materialization waits on the review gate.
	drafted, pending, err := c.stageProposals(ctx, ws.ID, wiki, selected)
	stages = append(stages, stageResult(stageProposals, err))
	counters.ProposalsDrafted = drafted
	counters.CandidatesPending = pending

	// Health numbers from the candidate store (D6 alarms are computable
	// from the counts; see CycleCounters).
	c.recordHealth(ctx, ws.ID, &counters)

	// ⑤ Cycle status — the roll-up: failed when any stage failed, with the
	// per-stage detail and the counters preserved either way.
	status.FinishedAt = c.now().UTC()
	status.Stages = stages
	status.Counters = counters
	status.State = CycleStateSucceeded
	for _, stage := range stages {
		if !stage.OK {
			status.State = CycleStateFailed
			break
		}
	}
	c.setStatus(ws.ID, status)
	c.log.InfoContext(ctx, "skillcuration: cycle finished",
		"workspace_id", ws.ID, "trigger", trigger, "state", status.State,
		"clusters_processed", counters.ClustersProcessed,
		"proposals_drafted", counters.ProposalsDrafted,
		"patterns_changed", counters.PatternsChanged,
		"probation_graduated", counters.ProbationGraduated,
		"probation_disabled", counters.ProbationDisabled)
}

// stageProbationSweep runs stage ①. Graduated and disabled classify the
// resolved rows by their post-sweep status; a sweep failure is the stage's
// error (the counts stay zero — nothing resolved).
func (c *Cycle) stageProbationSweep(ctx context.Context, workspaceID string) (graduated, disabled int, err error) {
	resolved, err := c.probation.SweepProbation(ctx, workspaceID, c.now().UTC())
	if err != nil {
		return 0, 0, err
	}
	for _, row := range resolved {
		switch row.Status {
		case domain.SkillCandidateApproved:
			graduated++
		case domain.SkillCandidateDisabled:
			disabled++
		}
	}
	return graduated, disabled, nil
}

// stageBackstop runs stage ②: enumerate the workspace's clusters and keep
// those with the convergence gate open and no pending candidate. The gate
// and pending checks are the proposer's own gates mirrored cheaply here so
// the budget only ever funds workable clusters; the proposer re-checks
// internally (authoritative). A per-cluster read failure logs and skips
// that cluster (fail-soft); only the enumeration read fails the stage.
func (c *Cycle) stageBackstop(ctx context.Context, workspaceID string, counters *CycleCounters) ([]cycleCluster, error) {
	cfg := c.config(ctx, workspaceID)
	clusterIDs, err := c.clusters.ListClusterIDs(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: enumerate clusters: %w", err)
	}

	refs := make([]cycleCluster, 0, len(clusterIDs))
	for _, id := range clusterIDs {
		members, err := c.clusters.ListClusterRunsByCluster(ctx, workspaceID, id)
		if err != nil {
			c.log.WarnContext(ctx, "skillcuration: cluster membership read failed; skipping cluster",
				"workspace_id", workspaceID, "cluster_id", id, "error", err)
			continue
		}
		counters.ClusterRuns += len(members)
		qualifying := 0
		for _, run := range members {
			if run.Qualifying {
				qualifying++
			}
		}
		counters.QualifyingRuns += qualifying
		counters.ClustersConsidered++

		if len(members) == 0 || !ClusterGateOpen(qualifying, cfg.ClusterMinimum) {
			continue
		}
		candidates, err := c.clusters.ListByCluster(ctx, workspaceID, id)
		if err != nil {
			c.log.WarnContext(ctx, "skillcuration: cluster candidates read failed; skipping cluster",
				"workspace_id", workspaceID, "cluster_id", id, "error", err)
			continue
		}
		pending := false
		for _, cand := range candidates {
			if cand.Status == domain.SkillCandidatePending {
				pending = true
				break
			}
		}
		if pending {
			continue // the human gate holds this cluster; the next cycle re-checks
		}
		refs = append(refs, cycleCluster{ClusterID: id, AgentID: members[0].AgentID, members: members})
	}
	return refs, nil
}

// selectWithinBudget caps the cluster selection at the nightly budget: each
// selected cluster costs up to two side-calls (maintenance + proposal), so
// the cap is NightlyBudgetK/2 (a positive K under 2 still selects one
// cluster — the rounding documented on the package comment). Beyond the
// cap, the selection order is the deterministic soft score (SelectForCycle,
// 3.2) over the clusters' qualifying evidence. budget <= 0 selects nothing.
func (c *Cycle) selectWithinBudget(ctx context.Context, workspaceID string, refs []cycleCluster) []cycleCluster {
	if len(refs) == 0 {
		return nil
	}
	cfg := c.config(ctx, workspaceID)
	if cfg.NightlyBudgetK <= 0 {
		return nil
	}
	clusterBudget := cfg.NightlyBudgetK / 2
	if clusterBudget == 0 {
		clusterBudget = 1
	}
	if len(refs) <= clusterBudget {
		return refs
	}

	qualified := make([]domain.SkillClusterRun, 0)
	for _, ref := range refs {
		for _, run := range ref.members {
			if run.Qualifying {
				qualified = append(qualified, run)
			}
		}
	}
	rank := make(map[string]int, len(refs))
	order := make([]string, 0, len(refs))
	for _, q := range SelectForCycle(qualified, cfg.NightlyBudgetK) {
		if _, seen := rank[q.ClusterID]; !seen {
			rank[q.ClusterID] = len(order)
			order = append(order, q.ClusterID)
		}
	}
	byID := make(map[string]cycleCluster, len(refs))
	for _, ref := range refs {
		byID[ref.ClusterID] = ref
	}
	selected := make([]cycleCluster, 0, clusterBudget)
	for _, id := range order {
		if len(selected) >= clusterBudget {
			break
		}
		if ref, ok := byID[id]; ok {
			selected = append(selected, ref)
		}
	}
	return selected
}

// stageMaintenance runs stage ③: the maintainer side-call per selected
// cluster, each fail-soft (one cluster's model blip defers that cluster,
// never the stage's remaining clusters). The page-set delta across the
// stage is the PatternsChanged counter; the post-stage page count is
// PatternCount. The stage errors only when every selected cluster failed
// (or the wiki itself is unreadable — the stage's input).
func (c *Cycle) stageMaintenance(ctx context.Context, workspaceID string, wiki *Wiki, selected []cycleCluster) (changed, count int, err error) {
	before, err := wiki.List()
	if err != nil {
		return 0, 0, fmt.Errorf("skillcuration: read wiki before maintenance: %w", err)
	}

	maintainer := NewMaintainer(c.sessions, c.clusters, c.resolver, wiki, c.log)
	failures := 0
	firstErr := ""
	for _, ref := range selected {
		merr := maintainer.MaintainCluster(ctx, MaintainRequest{
			WorkspaceID: workspaceID,
			AgentID:     ref.AgentID,
			ClusterID:   ref.ClusterID,
		})
		if merr != nil {
			failures++
			if firstErr == "" {
				firstErr = merr.Error()
			}
			c.log.WarnContext(ctx, "skillcuration: wiki maintenance failed; deferring cluster",
				"workspace_id", workspaceID, "cluster_id", ref.ClusterID, "error", merr)
		}
	}

	after, err := wiki.List()
	if err != nil {
		return 0, 0, fmt.Errorf("skillcuration: read wiki after maintenance: %w", err)
	}
	changed = diffPageSets(before, after)
	count = len(after)
	if failures > 0 {
		return changed, count, fmt.Errorf("%d/%d maintenance side-calls failed; first: %s",
			failures, len(selected), excerpt(firstErr, stageErrorExcerpt))
	}
	return changed, count, nil
}

// stageProposals runs stage ④: the proposer side-call per selected cluster,
// each fail-soft (the proposer's own gates and bounded retries absorb the
// per-cluster paths). ProposalsDrafted is the pending-count delta — the
// store's CountPending is the system of record.
func (c *Cycle) stageProposals(ctx context.Context, workspaceID string, wiki *Wiki, selected []cycleCluster) (drafted, pending int, err error) {
	pendingBefore, err := c.clusters.CountPending(ctx, workspaceID)
	if err != nil {
		return 0, 0, fmt.Errorf("skillcuration: count pending before proposals: %w", err)
	}

	proposer := NewProposer(c.sessions, c.clusters, c.config, c.resolver, wiki, c.validator, c.log,
		WithProposalChipSink(c.chip))
	failures := 0
	firstErr := ""
	for _, ref := range selected {
		perr := proposer.ProposeCluster(ctx, ProposeRequest{
			WorkspaceID: workspaceID,
			AgentID:     ref.AgentID,
			ClusterID:   ref.ClusterID,
		})
		if perr != nil {
			failures++
			if firstErr == "" {
				firstErr = perr.Error()
			}
			c.log.WarnContext(ctx, "skillcuration: proposal failed; deferring cluster",
				"workspace_id", workspaceID, "cluster_id", ref.ClusterID, "error", perr)
		}
	}

	pendingAfter, err := c.clusters.CountPending(ctx, workspaceID)
	if err != nil {
		return 0, pendingBefore, fmt.Errorf("skillcuration: count pending after proposals: %w", err)
	}
	drafted = pendingAfter - pendingBefore
	if drafted < 0 {
		drafted = 0 // a reviewer decided mid-cycle; never report negative work
	}
	if failures > 0 {
		return drafted, pendingAfter, fmt.Errorf("%d/%d proposal side-calls failed; first: %s",
			failures, len(selected), excerpt(firstErr, stageErrorExcerpt))
	}
	return drafted, pendingAfter, nil
}

// recordHealth folds the approval-side health numbers into the counters
// (D6: approval rate is alarmed; the counts let the status API compute it).
func (c *Cycle) recordHealth(ctx context.Context, workspaceID string, counters *CycleCounters) {
	rows, err := c.clusters.List(ctx, workspaceID, "")
	if err != nil {
		c.log.WarnContext(ctx, "skillcuration: candidate health read failed",
			"workspace_id", workspaceID, "error", err)
		return
	}
	for _, row := range rows {
		switch row.Status {
		case domain.SkillCandidateApproved, domain.SkillCandidateProvisional:
			counters.ApprovedCuratedSkills++
		case domain.SkillCandidateRejected:
			counters.RejectedProposals++
		}
	}
}

// stageResult folds a stage error into its status record (nil error = ok).
func stageResult(stage string, err error) CycleStageResult {
	if err == nil {
		return CycleStageResult{Stage: stage, OK: true}
	}
	return CycleStageResult{Stage: stage, OK: false, Error: excerpt(err.Error(), stageErrorExcerpt)}
}

// diffPageSets counts the wiki pages added, removed, or status-flipped
// across the maintenance stage (the cycle-side observable of the
// maintainer's create/supersede/merge/update work).
func diffPageSets(before, after []Page) int {
	beforeSet := make(map[string]string, len(before))
	for _, page := range before {
		beforeSet[page.Slug] = string(page.Status)
	}
	changed := 0
	for _, page := range after {
		if status, ok := beforeSet[page.Slug]; !ok || status != string(page.Status) {
			changed++
			continue
		}
		delete(beforeSet, page.Slug)
	}
	return changed + len(beforeSet) // body rewrites land under the same slug+status; removals count too
}

// excerpt bounds an error string for the status record.
func excerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
