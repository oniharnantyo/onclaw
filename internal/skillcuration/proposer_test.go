package skillcuration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// The proposer's unit world (5.1–5.3): the fake store carries one session
// with qualifying runs of one procedure family indexed into one cluster; a
// wiki page matches the cluster; the model seam is the maintainer's
// scriptedModel behind the ModelResolver seam. The code gates — cluster
// convergence, rejection suppression, one-pending-per-cluster,
// edit-over-sibling — are each proven to act BEFORE the model is consulted
// (zero scripted calls consumed on a gate hit).
// ---------------------------------------------------------------------------

const proposerCluster = "cl-proposer"

// staticCfg pins the workspace's curation config.
func staticCfg(cfg Config) ConfigSource {
	return func(context.Context, string) Config { return cfg }
}

// siblingContent is the on-disk SKILL.md the fake agent-tier reader returns
// for the curated sibling.
const siblingContent = "# Deploy guard\n\nCurrent content.\n"

// proposalWorld is the proposer's fixture.
type proposalWorld struct {
	ctx    context.Context
	st     store.Store
	wiki   *Wiki
	model  *scriptedModel
	cfg    Config
	chips  []SkillCandidatePayload
	reader SkillContentReader
}

// newProposalWorld seeds workspace, agent, one session holding two
// qualifying turns (turn-a at T-3h, turn-b at T-2.5h), the cluster
// membership rows, and a wiki page whose evidence matches the cluster.
func newProposalWorld(t *testing.T) *proposalWorld {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Agents().Create(ctx, &domain.Agent{ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var events []domain.SessionEvent
	events = append(events, buildWindow(t, "pw-a", "turn-a", qualifyingCalls(), true)...)
	events = append(events, buildWindow(t, "pw-b", "turn-b", qualifyingCalls(), true)...)
	if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, events); err != nil {
		t.Fatalf("append windows: %v", err)
	}

	w := &proposalWorld{
		ctx:   ctx,
		st:    st,
		wiki:  NewWiki(t.TempDir()),
		model: &scriptedModel{},
		cfg:   DefaultConfig(),
		reader: mapContentReader(map[string]string{
			"deploy-guard": siblingContent,
		}),
	}
	w.indexQualifying(t, "turn-a", time.Now().UTC().Add(-3*time.Hour))
	w.indexQualifying(t, "turn-b", time.Now().UTC().Add(-150*time.Minute))

	mustCreate(t, w.wiki, testPage("deploy-guard", "Deploy guard", "Check health before promoting.", fixtureSession))
	return w
}

// indexQualifying inserts one qualifying membership row at the given time.
func (w *proposalWorld) indexQualifying(t *testing.T, turnID string, indexedAt time.Time) {
	t.Helper()
	if err := w.st.SkillCandidates().IndexClusterRun(w.ctx, &domain.SkillClusterRun{
		WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: proposerCluster,
		SessionID: fixtureSession, TurnID: turnID, RunStatus: domain.SkillClusterRunCompleted,
		Origin: "user", Qualifying: true,
		ToolCalls: 9, DistinctTools: 4, Recoveries: 2, ErrorResults: 1,
		IndexedAt: indexedAt,
	}); err != nil {
		t.Fatalf("index cluster run %s: %v", turnID, err)
	}
}

// newProposer builds the proposer over the world with the scripted model
// and a capture chip sink.
func (w *proposalWorld) newProposer(t *testing.T, opts ...ProposerOption) *Proposer {
	t.Helper()
	validator := NewDraftValidator(w.st.WorkspaceSkills(), w.reader, staticTools("grafana.query", "files.write", "http.request"))
	opts = append([]ProposerOption{WithProposalChipSink(func(_ context.Context, _ ingest.Job, payload SkillCandidatePayload) {
		w.chips = append(w.chips, payload)
	})}, opts...)
	return NewProposer(w.st.SessionEvents(), w.st.SkillCandidates(), staticCfg(w.cfg), staticResolver(w.model), w.wiki, validator, discardLog(), opts...)
}

// propose runs one proposal attempt for the fixture cluster.
func (w *proposalWorld) propose(t *testing.T, p *Proposer) error {
	t.Helper()
	return p.ProposeCluster(w.ctx, ProposeRequest{WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: proposerCluster})
}

// pendingCandidates lists the cluster's PENDING candidates (the queue the
// cycle and the review surface act on; the seeded approved sibling is
// lifecycle state, not queue content).
func (w *proposalWorld) pendingCandidates(t *testing.T) []domain.SkillCandidate {
	t.Helper()
	rows, err := w.st.SkillCandidates().ListByCluster(w.ctx, fixtureWorkspace, proposerCluster)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	pending := make([]domain.SkillCandidate, 0, len(rows))
	for _, row := range rows {
		if row.Status == domain.SkillCandidatePending {
			pending = append(pending, row)
		}
	}
	return pending
}

// userPrompts returns the scripted model's user-turn texts, one per call.
func (w *proposalWorld) userPrompts() []string {
	var prompts []string
	for _, in := range w.model.callInputs() {
		if len(in) > 1 {
			prompts = append(prompts, in[1])
		}
	}
	return prompts
}

// proposalJSON renders a scripted proposer response.
func proposalJSON(name, supersedes string, tools ...string) string {
	if len(tools) == 0 {
		tools = []string{"grafana.query"}
	}
	quoted := make([]string, 0, len(tools))
	for _, tool := range tools {
		quoted = append(quoted, fmt.Sprintf("%q", tool))
	}
	return fmt.Sprintf(`{"name":%q,"description":"Guard deployments with a health check before promoting.",`+
		`"tools":[%s],"cited_patterns":["deploy-guard"],"cited_runs":[%q],"supersedes":%q,`+
		`"content":"# %s\n\nPurpose: guard deploys.\n\n1. Query health."}`,
		name, strings.Join(quoted, ","), fixtureSession, supersedes, name)
}

// reject seeds a rejection impact entry for the fixture cluster.
func (w *proposalWorld) reject(t *testing.T, skillName string, at time.Time) {
	t.Helper()
	if err := w.st.SkillCandidates().AppendImpactEntry(w.ctx, &domain.SkillImpactEntry{
		WorkspaceID: fixtureWorkspace, ClusterID: proposerCluster, SkillName: skillName,
		Verdict: domain.SkillImpactRejected, Reason: "wrong scope",
		CreatedAt: at,
	}); err != nil {
		t.Fatalf("append rejection: %v", err)
	}
}

// seedSibling stores an approved curated skill in the fixture cluster.
func (w *proposalWorld) seedSibling(t *testing.T) *domain.SkillCandidate {
	t.Helper()
	sibling := &domain.SkillCandidate{
		WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: proposerCluster,
		SkillName: "deploy-guard", Status: domain.SkillCandidateApproved,
		ProposedContent: "# Deploy guard\n\nOld content.",
		ProposedAt:      time.Now().UTC().Add(-time.Hour),
	}
	if err := w.st.SkillCandidates().Save(w.ctx, sibling); err != nil {
		t.Fatalf("seed sibling: %v", err)
	}
	return sibling
}

func TestProposeClusterGateClosedDraftsNothing(t *testing.T) {
	// The cluster gate is code, not model: with the workspace's minimum
	// raised to three, a two-run cluster stays closed and the model is
	// never consulted.
	w := newProposalWorld(t)
	w.cfg.ClusterMinimum = 3
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 0 {
		t.Errorf("model called %d times below the cluster minimum, want 0", calls)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("candidates stored below the cluster minimum: %d", len(rows))
	}
}

func TestProposeClusterFirstRunOpensClusterWithoutProposing(t *testing.T) {
	// A first-ever qualifying run opens the cluster without proposing:
	// count 1 < default minimum 2.
	w := newProposalWorld(t)
	if err := w.st.SkillCandidates().IndexClusterRun(w.ctx, &domain.SkillClusterRun{
		WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: "cl-first",
		SessionID: fixtureSession, TurnID: "turn-first", RunStatus: domain.SkillClusterRunCompleted,
		Origin: "user", Qualifying: true,
		ToolCalls: 9, DistinctTools: 4, Recoveries: 2, ErrorResults: 1,
		IndexedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("index first run: %v", err)
	}
	validator := NewDraftValidator(w.st.WorkspaceSkills(), w.reader, staticTools("grafana.query"))
	p := NewProposer(w.st.SessionEvents(), w.st.SkillCandidates(), staticCfg(w.cfg), staticResolver(w.model), w.wiki, validator, discardLog())
	if err := p.ProposeCluster(w.ctx, ProposeRequest{WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: "cl-first"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 0 {
		t.Errorf("model called %d times for a first-ever qualifying run, want 0", calls)
	}
	rows, err := w.st.SkillCandidates().ListByCluster(w.ctx, fixtureWorkspace, "cl-first")
	if err != nil {
		t.Fatalf("list cl-first candidates: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("candidates stored for a first-ever qualifying run: %d", len(rows))
	}
}

func TestProposeClusterSuppressedWithoutNewEvidence(t *testing.T) {
	// Rejected last week (here: an hour ago) with no qualifying run joining
	// after the rejection — the cycle drafts nothing.
	w := newProposalWorld(t)
	w.reject(t, "deploy-guard", time.Now().UTC().Add(-time.Hour))
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 0 {
		t.Errorf("model called %d times on a suppressed cluster, want 0", calls)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("candidates stored on a suppressed cluster: %d", len(rows))
	}
}

func TestProposeClusterNewEvidenceReopensCluster(t *testing.T) {
	// A qualifying run joining AFTER the rejection is materially new
	// evidence: drafting is allowed again.
	w := newProposalWorld(t)
	w.reject(t, "deploy-guard", time.Now().UTC().Add(-time.Hour))
	w.indexQualifying(t, "turn-c", time.Now().UTC().Add(-30*time.Minute))
	w.model.responses = []string{proposalJSON("deploy-guard-procedure", "")}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 1 {
		t.Errorf("model called %d times, want 1", calls)
	}
	if rows := w.pendingCandidates(t); len(rows) != 1 {
		t.Fatalf("candidates stored: %d, want 1", len(rows))
	}
}

func TestProposeClusterPendingCandidateSkipsCycle(t *testing.T) {
	// One atomic create-or-edit per cluster per cycle: a pending candidate
	// awaits the human gate — no drafting behind it.
	w := newProposalWorld(t)
	w.seedSiblingPending(t)
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 0 {
		t.Errorf("model called %d times with a candidate already pending, want 0", calls)
	}
	if rows := w.pendingCandidates(t); len(rows) != 1 {
		t.Errorf("candidate rows: %d, want just the seeded pending one", len(rows))
	}
}

// seedSiblingPending stores a pending curated candidate in the cluster.
func (w *proposalWorld) seedSiblingPending(t *testing.T) {
	t.Helper()
	if err := w.st.SkillCandidates().Save(w.ctx, &domain.SkillCandidate{
		WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: proposerCluster,
		SkillName: "deploy-guard", Status: domain.SkillCandidatePending,
		ProposedContent: "# Deploy guard\n\nAwaiting review.",
	}); err != nil {
		t.Fatalf("seed pending candidate: %v", err)
	}
}

func TestProposeClusterEditOverSibling(t *testing.T) {
	// An approved sibling in the cluster forces the edit shape: the stored
	// candidate is a superseding edit carrying the sibling's current
	// content as the superseded lineage.
	w := newProposalWorld(t)
	w.seedSibling(t)
	w.model.responses = []string{proposalJSON("deploy-guard", "deploy-guard")}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	rows := w.pendingCandidates(t)
	if len(rows) != 1 {
		t.Fatalf("candidates stored: %d, want 1", len(rows))
	}
	got := rows[0]
	if !got.IsEdit {
		t.Error("candidate is not an edit")
	}
	if got.SupersedesSkillName != "deploy-guard" {
		t.Errorf("SupersedesSkillName = %q, want deploy-guard", got.SupersedesSkillName)
	}
	if got.SupersededContent != siblingContent {
		t.Errorf("SupersededContent = %q, want the on-disk sibling content", got.SupersededContent)
	}
	// The prompt showed the sibling and its current content.
	prompts := w.userPrompts()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "already encodes this procedure") || !strings.Contains(prompts[0], siblingContent) {
		t.Errorf("prompt did not present the sibling skill: %v", prompts)
	}
}

func TestProposeClusterSecondSkillRejectedInCode(t *testing.T) {
	// With a live sibling, a create-shaped draft is rejected in code — not
	// just prompted: the retry names the rule, and a persistent create is
	// dropped without a candidate row.
	w := newProposalWorld(t)
	w.seedSibling(t)
	w.model.responses = []string{
		proposalJSON("deploy-helper", ""), // a second skill — forbidden
		proposalJSON("deploy-helper", ""), // still forbidden
	}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose (must drop, not fail): %v", err)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("second-skill candidate stored: %d rows", len(rows))
	}
	if calls := len(w.model.callInputs()); calls != 2 {
		t.Errorf("model calls: %d, want the initial attempt plus one retry", calls)
	}
	prompts := w.userPrompts()
	if len(prompts) != 2 || !strings.Contains(prompts[1], "second skill for the same procedure is forbidden") {
		t.Errorf("retry prompt did not carry the sibling rule: %v", prompts)
	}
}

func TestProposeClusterRetrySatisfiesSiblingRule(t *testing.T) {
	// The create is rejected, the retry's edit is accepted: one edit row.
	w := newProposalWorld(t)
	w.seedSibling(t)
	w.model.responses = []string{
		proposalJSON("deploy-helper", ""),
		proposalJSON("deploy-guard", "deploy-guard"),
	}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	rows := w.pendingCandidates(t)
	if len(rows) != 1 || !rows[0].IsEdit {
		t.Fatalf("candidates: %d rows (is_edit=%v), want one edit", len(rows), len(rows) == 1 && rows[0].IsEdit)
	}
}

func TestProposeClusterSupersedesUnknownSkillRejected(t *testing.T) {
	// With no live sibling, a draft naming a skill to supersede is rejected
	// in code: there is nothing to edit.
	w := newProposalWorld(t)
	w.model.responses = []string{
		proposalJSON("deploy-guard", "ghost-skill"),
		proposalJSON("deploy-guard", "ghost-skill"),
	}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("candidate stored despite unknown supersedes target: %d", len(rows))
	}
	prompts := w.userPrompts()
	if len(prompts) != 2 || !strings.Contains(prompts[1], `"supersedes" must be empty`) {
		t.Errorf("retry prompt did not carry the supersedes rule: %v", prompts)
	}
}

func TestProposeClusterRetryThenValidDraft(t *testing.T) {
	// First draft names an unknown tool; the retry carries the validation
	// problems and the corrected draft stores.
	w := newProposalWorld(t)
	w.model.responses = []string{
		proposalJSON("deploy-guard-procedure", "", "k8s.reincarnate"),
		proposalJSON("deploy-guard-procedure", ""),
	}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	rows := w.pendingCandidates(t)
	if len(rows) != 1 {
		t.Fatalf("candidates stored: %d, want 1", len(rows))
	}
	prompts := w.userPrompts()
	if len(prompts) != 2 || !strings.Contains(prompts[1], `unknown tool "k8s.reincarnate"`) {
		t.Errorf("retry prompt did not carry the validation problems: %v", prompts)
	}
}

func TestProposeClusterDropsAfterRetries(t *testing.T) {
	// A persistently invalid draft is dropped with a log after the bounded
	// retries (default 1) — the cycle continues (nil error).
	w := newProposalWorld(t)
	w.model.responses = []string{
		proposalJSON("deploy-guard", "", "k8s.reincarnate"),
		proposalJSON("deploy-guard", "", "k8s.reincarnate"),
	}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("dropped draft must not fail the stage: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 2 {
		t.Errorf("model calls: %d, want initial attempt plus default one retry", calls)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("candidates stored despite invalid drafts: %d", len(rows))
	}

	// WithDraftRetries(0) bounds the attempt to a single call.
	w2 := newProposalWorld(t)
	w2.model.responses = []string{proposalJSON("deploy-guard", "", "k8s.reincarnate")}
	p2 := w2.newProposer(t, WithDraftRetries(0))
	if err := w2.propose(t, p2); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w2.model.callInputs()); calls != 1 {
		t.Errorf("model calls with zero retries: %d, want 1", calls)
	}
}

func TestProposeClusterUnknownCitationsRejected(t *testing.T) {
	// The model can only cite evidence it was shown: unknown patterns and
	// unknown runs reject the draft.
	w := newProposalWorld(t)
	ghost := `{"name":"deploy-guard","description":"Guard deployments with a health check before promoting.",` +
		`"tools":["grafana.query"],"cited_patterns":["ghost-pattern"],"cited_runs":["sess-ghost"],` +
		`"supersedes":"","content":"# deploy-guard\n\nBody."}`
	w.model.responses = []string{ghost, ghost}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("candidate stored with unknown citations: %d", len(rows))
	}
	prompts := w.userPrompts()
	if len(prompts) != 2 {
		t.Fatalf("model calls: %d, want 2", len(prompts))
	}
	for _, sub := range []string{`cites unknown pattern "ghost-pattern"`, `cites unknown evidence run "sess-ghost"`} {
		if !strings.Contains(prompts[1], sub) {
			t.Errorf("retry prompt missing %q: %s", sub, prompts[1])
		}
	}
}

func TestProposeClusterNothingProposed(t *testing.T) {
	// An explicit null is the legitimate "windows do not converge": quiet
	// no-op, one side-call spent.
	w := newProposalWorld(t)
	w.model.responses = []string{"```json\nnull\n```"}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 1 {
		t.Errorf("model calls: %d, want 1", calls)
	}
	if rows := w.pendingCandidates(t); len(rows) != 0 {
		t.Errorf("candidates stored from a null proposal: %d", len(rows))
	}
}

func TestProposeClusterRoundTripAndChip(t *testing.T) {
	// The stored candidate carries the full evidence chain; the chip
	// payload links the transcript to the review row; PendingCount is the
	// badge number.
	w := newProposalWorld(t)
	w.model.responses = []string{proposalJSON("deploy-guard-procedure", "")}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}

	rows := w.pendingCandidates(t)
	if len(rows) != 1 {
		t.Fatalf("candidates stored: %d, want 1", len(rows))
	}
	got, err := w.st.SkillCandidates().Get(w.ctx, fixtureWorkspace, rows[0].ID)
	if err != nil {
		t.Fatalf("round-trip get: %v", err)
	}
	if got.Status != domain.SkillCandidatePending {
		t.Errorf("status = %q, want pending", got.Status)
	}
	if got.ClusterID != proposerCluster {
		t.Errorf("cluster linkage = %q, want %q", got.ClusterID, proposerCluster)
	}
	if got.SkillName != "deploy-guard-procedure" {
		t.Errorf("skill name = %q", got.SkillName)
	}
	if len(got.EvidenceEventIDs) != 1 || got.EvidenceEventIDs[0] != fixtureSession {
		t.Errorf("evidence ids = %v, want [%s]", got.EvidenceEventIDs, fixtureSession)
	}
	if len(got.CitedPatternRefs) != 1 || got.CitedPatternRefs[0] != "deploy-guard" {
		t.Errorf("cited patterns = %v, want [deploy-guard]", got.CitedPatternRefs)
	}
	if got.IsEdit || got.SupersedesSkillName != "" {
		t.Errorf("first-time proposal marked as edit: %+v", got)
	}
	if strings.Contains(got.ProposedContent, "deploy-guard-procedure") == false {
		t.Errorf("proposed content lost: %q", got.ProposedContent)
	}

	// The chip rides the newest qualifying run's coordinates with the
	// candidate's identity.
	if len(w.chips) != 1 {
		t.Fatalf("chips emitted: %d, want 1", len(w.chips))
	}
	chip := w.chips[0]
	if chip.CandidateID != got.ID || chip.SkillName != got.SkillName {
		t.Errorf("chip = %+v, want candidate id %s name %s", chip, got.ID, got.SkillName)
	}
	if chip.Cluster != proposerCluster || chip.SessionID != fixtureSession || chip.RunID != "turn-b" {
		t.Errorf("chip coordinates = %+v, want the newest qualifying run (turn-b)", chip)
	}

	count, err := p.PendingCount(w.ctx, fixtureWorkspace)
	if err != nil {
		t.Fatalf("pending count: %v", err)
	}
	if count != 1 {
		t.Errorf("PendingCount = %d, want 1", count)
	}
}

func TestProposeClusterResolverFailureFailsStage(t *testing.T) {
	// A model-resolution failure is a stage error the cycle defers on —
	// not a silent drop.
	w := newProposalWorld(t)
	validator := NewDraftValidator(w.st.WorkspaceSkills(), w.reader, staticTools("grafana.query"))
	p := NewProposer(w.st.SessionEvents(), w.st.SkillCandidates(), staticCfg(w.cfg), failingResolver(), w.wiki, validator, discardLog())
	if err := w.propose(t, p); err == nil {
		t.Fatal("resolver failure returned nil")
	}
}

func TestProposeClusterUnknownClusterQuietNoop(t *testing.T) {
	// A cluster with no membership rows is a quiet no-op — no model call.
	w := newProposalWorld(t)
	validator := NewDraftValidator(w.st.WorkspaceSkills(), w.reader, staticTools("grafana.query"))
	p := NewProposer(w.st.SessionEvents(), w.st.SkillCandidates(), staticCfg(w.cfg), staticResolver(w.model), w.wiki, validator, discardLog())
	if err := p.ProposeCluster(w.ctx, ProposeRequest{WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: "cl-unknown"}); err != nil {
		t.Fatalf("unknown cluster: %v", err)
	}
	if calls := len(w.model.callInputs()); calls != 0 {
		t.Errorf("model called %d times on an empty cluster, want 0", calls)
	}
}

func TestProposeClusterPromptCarriesWikiAndAudit(t *testing.T) {
	// The prompt renders the wiki index with the matched page's content and
	// the cluster's audit entries (D3: the DB rendered into the prompt).
	w := newProposalWorld(t)
	w.reject(t, "deploy-guard", time.Now().UTC().Add(-2*time.Hour))
	w.indexQualifying(t, "turn-c", time.Now().UTC().Add(-30*time.Minute)) // reopen
	w.model.responses = []string{proposalJSON("deploy-guard-procedure", "")}
	p := w.newProposer(t)
	if err := w.propose(t, p); err != nil {
		t.Fatalf("propose: %v", err)
	}
	prompts := w.userPrompts()
	if len(prompts) != 1 {
		t.Fatalf("model calls: %d, want 1", len(prompts))
	}
	prompt := prompts[0]
	for _, want := range []string{
		"## Tool catalog",
		"grafana.query",
		"- deploy-guard — Deploy guard [active]",
		"Check health before promoting.", // matched page body
		"## Review audit for this cluster",
		`rejected "deploy-guard"`,
		"wrong scope",
		"## Sampled run windows",
		"qualifying",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
