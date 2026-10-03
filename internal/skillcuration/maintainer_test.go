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
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// The maintainer side-call's unit world (4.2): the fake store carries one
// session with two runs of the same procedure family — turn-a qualifying,
// turn-b failing contrast — indexed into one cluster; the model seam is a
// scripted stub (mirroring internal/memory's worker_test scriptedModel
// pattern) behind the ModelResolver seam.
// ---------------------------------------------------------------------------

const maintainerCluster = "cl-maintainer"

// scriptedModel answers Generates from a scripted response queue, records
// every call's input texts, and can be pointed at a permanent error.
type scriptedModel struct {
	mu        sync.Mutex
	responses []string
	err       error
	inputs    [][]string
}

func (m *scriptedModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	texts := make([]string, 0, len(input))
	for _, msg := range input {
		if msg == nil {
			continue
		}
		var sb strings.Builder
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			if block.AssistantGenText != nil {
				sb.WriteString(block.AssistantGenText.Text)
			}
			if block.UserInputText != nil {
				sb.WriteString(block.UserInputText.Text)
			}
		}
		texts = append(texts, sb.String())
	}
	m.inputs = append(m.inputs, texts)
	if m.err != nil {
		return nil, m.err
	}
	resp := ""
	if len(m.responses) > 0 {
		resp = m.responses[0]
		m.responses = m.responses[1:]
	}
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: resp}}}}, nil
}

func (m *scriptedModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("scriptedModel: stream not supported")
}

func (m *scriptedModel) callInputs() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]string, len(m.inputs))
	copy(out, m.inputs)
	return out
}

func staticResolver(m Model) ModelResolver {
	return func(context.Context, string, string) (Model, error) { return m, nil }
}

func failingResolver() ModelResolver {
	return func(context.Context, string, string) (Model, error) { return nil, errors.New("model provider down") }
}

// opsJSON renders a scripted maintainer response.
func opsJSON(ops ...string) string {
	return "```json\n[" + strings.Join(ops, ",") + "]\n```"
}

func createOp(slug, title, body, citedRun string) string {
	return fmt.Sprintf(`{"op":"create","slug":%q,"title":%q,"cited_runs":[%q],"body":%q}`, slug, title, citedRun, body)
}

func updateOp(slug, title, body, citedRun string) string {
	return fmt.Sprintf(`{"op":"update","slug":%q,"title":%q,"cited_runs":[%q],"body":%q}`, slug, title, citedRun, body)
}

func supersedeOp(oldSlug, newSlug, title, body, citedRun string) string {
	return fmt.Sprintf(`{"op":"supersede","slug":%q,"successor_slug":%q,"title":%q,"cited_runs":[%q],"body":%q}`, oldSlug, newSlug, title, citedRun, body)
}

// failingCalls is a 9-call window with two errors and no recovery — the
// contrast evidence (failed run, indexed not qualifying).
func failingCalls() []toolCall {
	ok := func(name, id string) toolCall { return toolCall{name: name, callID: id} }
	return []toolCall{
		ok("grafana.query", "d1"),
		{name: "http.request", callID: "d2", err: true},
		ok("files.write", "d3"),
		{name: "http.request", callID: "d4", err: true},
		ok("http.request", "d5"),
		ok("grafana.annotate", "d6"),
		ok("grafana.query", "d7"),
		ok("files.write", "d8"),
		ok("grafana.query", "d9"),
	}
}

// newMaintainerWorld seeds the store: workspace, agent, one session holding
// the qualifying run (turn-a) and the failing run (turn-b), and the cluster
// membership rows. Returns the store and the wiki over a temp dir.
func newMaintainerWorld(t *testing.T) (context.Context, store.Store, *Wiki) {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Agents().Create(ctx, &domain.Agent{ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	qualifying := buildWindow(t, "mw-a", "turn-a", qualifyingCalls(), true)
	failing := buildWindow(t, "mw-b", "turn-b", failingCalls(), false)
	if err := st.SessionEvents().AppendEvents(ctx, fixtureWorkspace, append(qualifying, failing...)); err != nil {
		t.Fatalf("append windows: %v", err)
	}

	indexedAt := time.Now().UTC().Add(-time.Hour)
	for _, run := range []domain.SkillClusterRun{
		{
			WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: maintainerCluster,
			SessionID: fixtureSession, TurnID: "turn-a", RunStatus: domain.SkillClusterRunCompleted,
			Origin: ingestOriginUser, Qualifying: true,
			ToolCalls: 9, DistinctTools: 4, Recoveries: 1, ErrorResults: 1,
			WindowEndEventID: "mw-a-evt-11", IndexedAt: indexedAt,
		},
		{
			WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: maintainerCluster,
			SessionID: fixtureSession, TurnID: "turn-b", RunStatus: domain.SkillClusterRunFailed,
			Origin: ingestOriginUser, Qualifying: false,
			ToolCalls: 9, DistinctTools: 4, Recoveries: 0, ErrorResults: 2,
			WindowEndEventID: "mw-b-evt-11", IndexedAt: indexedAt.Add(time.Minute),
		},
	} {
		if err := st.SkillCandidates().IndexClusterRun(ctx, &run); err != nil {
			t.Fatalf("index cluster run: %v", err)
		}
	}

	wiki := NewWiki(t.TempDir())
	return ctx, st, wiki
}

// ingestOriginUser mirrors the ingest package's origin literal without
// importing it twice in this file's fixtures (score.go already imports it).
const ingestOriginUser = "user"

// newTestMaintainer builds the maintainer over the world with the scripted
// model.
func newTestMaintainer(st store.Store, wiki *Wiki, resolver ModelResolver) *Maintainer {
	return NewMaintainer(st.SessionEvents(), st.SkillCandidates(), resolver, wiki, discardLog())
}

func maintainReq() MaintainRequest {
	return MaintainRequest{WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: maintainerCluster}
}

// wikiState snapshots the pages and log lines for unchanged assertions.
func wikiState(t *testing.T, w *Wiki) (pages []string, log []string) {
	t.Helper()
	list, err := w.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range list {
		pages = append(pages, p.Slug+":"+string(p.Status))
	}
	log, err = w.ReadLog()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return pages, log
}

// TestMaintainerAppliesOps: the happy path — the scripted response updates
// the existing page and creates a new one, both citing the sampled session;
// pages land, citations persist, and the log records both operations.
func TestMaintainerAppliesOps(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	mustCreate(t, wiki, testPage("deploy-guard", "Deploy guard", "Old body.", "sess-old"))

	model := &scriptedModel{responses: []string{opsJSON(
		updateOp("deploy-guard", "Deploy guard", "Check health before promoting.", fixtureSession),
		createOp("rollback-first", "Rollback first", "Always prepare the rollback path.", fixtureSession),
	)}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	if err := m.MaintainCluster(ctx, maintainReq()); err != nil {
		t.Fatalf("maintain: %v", err)
	}

	page, err := wiki.Page("deploy-guard")
	if err != nil {
		t.Fatalf("read updated page: %v", err)
	}
	if page.Body != "Check health before promoting." || page.Status != PageActive {
		t.Fatalf("update must rewrite the page, got %+v", page)
	}
	created, err := wiki.Page("rollback-first")
	if err != nil {
		t.Fatalf("read created page: %v", err)
	}
	if len(created.EvidenceRuns) != 1 || created.EvidenceRuns[0] != fixtureSession {
		t.Fatalf("created page must cite the sampled session, got %v", created.EvidenceRuns)
	}

	// The prompt showed the model the wiki index and the sampled windows.
	inputs := model.callInputs()
	if len(inputs) != 1 {
		t.Fatalf("expected exactly one side-call, got %d", len(inputs))
	}
	prompt := inputs[0][0] + "\n" + inputs[0][1]
	for _, want := range []string{"deploy-guard", "Deploy guard", fixtureSession, "run the procedure", "grafana.query", "qualifying", "failing"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt must contain %q:\n%s", want, prompt)
		}
	}

	// The log recorded the pre-seed, the update, and the create.
	log, err := wiki.ReadLog()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(log) != 3 || !strings.Contains(log[1], "update deploy-guard") || !strings.Contains(log[2], "create rollback-first") {
		t.Fatalf("unexpected log lines:\n%s", strings.Join(log, "\n"))
	}
}

// TestMaintainerSupersedeLeavesOldReadable: a supersede op retires the old
// page behind a readable successor pointer (spec: "Stale pattern is
// superseded, not erased").
func TestMaintainerSupersedeLeavesOldReadable(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	mustCreate(t, wiki, testPage("old-pattern", "Old pattern", "Outdated advice.", fixtureSession))

	model := &scriptedModel{responses: []string{opsJSON(
		supersedeOp("old-pattern", "new-pattern", "New pattern", "Current advice.", fixtureSession),
	)}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	if err := m.MaintainCluster(ctx, maintainReq()); err != nil {
		t.Fatalf("maintain: %v", err)
	}

	old, err := wiki.Page("old-pattern")
	if err != nil {
		t.Fatalf("superseded page must remain readable: %v", err)
	}
	if old.Status != PageSuperseded || old.SupersededBy != "new-pattern" || old.Body != "Outdated advice." {
		t.Fatalf("superseded page must carry pointer and body, got %+v", old)
	}
	successor, err := wiki.Page("new-pattern")
	if err != nil {
		t.Fatalf("read successor: %v", err)
	}
	if successor.Status != PageActive {
		t.Fatalf("successor must be active, got %+v", successor)
	}
}

// TestMaintainerMalformedOutputFailsSoft: an unparseable response fails the
// stage — the error returns and the wiki is untouched.
func TestMaintainerMalformedOutputFailsSoft(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	mustCreate(t, wiki, testPage("kept", "Kept", "body", fixtureSession))
	before, logBefore := wikiState(t, wiki)

	model := &scriptedModel{responses: []string{"I could not produce JSON today, sorry."}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	if err := m.MaintainCluster(ctx, maintainReq()); err == nil {
		t.Fatal("expected a malformed response to error")
	}
	after, logAfter := wikiState(t, wiki)
	if strings.Join(after, "|") != strings.Join(before, "|") || strings.Join(logAfter, "|") != strings.Join(logBefore, "|") {
		t.Fatalf("a malformed response must leave the wiki unchanged, before=%v after=%v", before, after)
	}
}

// TestMaintainerUnknownEvidenceRunRejected: an op citing a run the model was
// never shown is rejected before anything is written — the whole stage fails
// with the wiki unchanged (strict parsing; pages always cite real evidence).
func TestMaintainerUnknownEvidenceRunRejected(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	mustCreate(t, wiki, testPage("kept", "Kept", "body", fixtureSession))
	before, logBefore := wikiState(t, wiki)

	model := &scriptedModel{responses: []string{opsJSON(
		updateOp("kept", "Kept", "New body.", fixtureSession),        // valid…
		createOp("invented", "Invented", "body", "sess-never-shown"), // …but cites an unknown run
	)}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	if err := m.MaintainCluster(ctx, maintainReq()); err == nil {
		t.Fatal("expected an op citing an unknown run to fail the stage")
	}
	after, logAfter := wikiState(t, wiki)
	if strings.Join(after, "|") != strings.Join(before, "|") || strings.Join(logAfter, "|") != strings.Join(logBefore, "|") {
		t.Fatalf("a rejected op must leave the wiki unchanged (no partial apply), before=%v after=%v", before, after)
	}
}

// TestMaintainerMalformedOpAmongValidAppliesNothing: strict parsing — one
// malformed op in an otherwise valid batch fails the whole stage with the
// wiki untouched.
func TestMaintainerMalformedOpAmongValidAppliesNothing(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	before, logBefore := wikiState(t, wiki)

	model := &scriptedModel{responses: []string{opsJSON(
		createOp("good-page", "Good", "body", fixtureSession),
		createOp("Bad_Slug", "Bad", "body", fixtureSession), // invalid slug
	)}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	if err := m.MaintainCluster(ctx, maintainReq()); err == nil {
		t.Fatal("expected the malformed op to fail the stage")
	}
	after, logAfter := wikiState(t, wiki)
	if len(after) != len(before) || strings.Join(logAfter, "|") != strings.Join(logBefore, "|") {
		t.Fatalf("no op may apply when a sibling op is malformed, before=%v after=%v", before, after)
	}
}

// TestMaintainerModelErrorFailsSoft: a resolver or model failure fails the
// stage; the wiki stays consistent.
func TestMaintainerModelErrorFailsSoft(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	mustCreate(t, wiki, testPage("kept", "Kept", "body", fixtureSession))
	before, logBefore := wikiState(t, wiki)

	m := newTestMaintainer(st, wiki, failingResolver())
	if err := m.MaintainCluster(ctx, maintainReq()); err == nil {
		t.Fatal("expected a failing resolver to error")
	}

	model := &scriptedModel{err: errors.New("provider exploded")}
	m = newTestMaintainer(st, wiki, staticResolver(model))
	if err := m.MaintainCluster(ctx, maintainReq()); err == nil {
		t.Fatal("expected a failing model to error")
	}

	after, logAfter := wikiState(t, wiki)
	if strings.Join(after, "|") != strings.Join(before, "|") || strings.Join(logAfter, "|") != strings.Join(logBefore, "|") {
		t.Fatalf("model failures must leave the wiki consistent, before=%v after=%v", before, after)
	}
}

// TestMaintainerUnreadableWindowsQuietSkip: a cluster whose windows are all
// gone (deleted sessions) is a quiet no-op — no model call, no error.
func TestMaintainerUnreadableWindowsQuietSkip(t *testing.T) {
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := st.Agents().Create(ctx, &domain.Agent{ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	// The membership row survives; its session events do not.
	run := domain.SkillClusterRun{
		WorkspaceID: fixtureWorkspace, AgentID: fixtureAgent, ClusterID: maintainerCluster,
		SessionID: "sess-vanished", TurnID: "turn-x", RunStatus: domain.SkillClusterRunCompleted,
		Qualifying: true, IndexedAt: time.Now().UTC(),
	}
	if err := st.SkillCandidates().IndexClusterRun(ctx, &run); err != nil {
		t.Fatalf("index: %v", err)
	}

	wiki := NewWiki(t.TempDir())
	model := &scriptedModel{responses: []string{opsJSON(createOp("never", "N", "b", "sess-vanished"))}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	if err := m.MaintainCluster(ctx, maintainReq()); err != nil {
		t.Fatalf("no readable windows must be a quiet skip, got %v", err)
	}
	if calls := model.callInputs(); len(calls) != 0 {
		t.Fatalf("the model must not be called without windows, got %d calls", len(calls))
	}
}

// TestMaintainerEmptyClusterQuietSkip: an unknown cluster has no membership
// rows — quiet no-op.
func TestMaintainerEmptyClusterQuietSkip(t *testing.T) {
	ctx, st, wiki := newMaintainerWorld(t)
	model := &scriptedModel{responses: []string{opsJSON(createOp("never", "N", "b", fixtureSession))}}
	m := newTestMaintainer(st, wiki, staticResolver(model))

	req := maintainReq()
	req.ClusterID = "cl-unknown"
	if err := m.MaintainCluster(ctx, req); err != nil {
		t.Fatalf("empty cluster must be a quiet skip, got %v", err)
	}
	if calls := model.callInputs(); len(calls) != 0 {
		t.Fatalf("the model must not be called for an empty cluster, got %d calls", len(calls))
	}
}

// ---------------------------------------------------------------------------
// The curation model resolver (D7 four-step fallback).
// ---------------------------------------------------------------------------

type factoryRecord struct {
	providerType string
	modelName    string
	apiKey       string
}

func resolverWorld(t *testing.T) (context.Context, store.Store, *[]factoryRecord, ModelFactory) {
	t.Helper()
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{ID: fixtureWorkspace, Slug: "qual", Name: "Qual"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	for _, p := range []domain.ProviderConfig{
		{ID: "prov-curation", WorkspaceID: fixtureWorkspace, Type: "openai", Name: "Curation"},
		{ID: "prov-memory", WorkspaceID: fixtureWorkspace, Type: "anthropic", Name: "Memory"},
		{ID: "prov-agent", WorkspaceID: fixtureWorkspace, Type: "google", Name: "Agent"},
	} {
		cfg := p
		if err := st.Providers().Create(ctx, &cfg); err != nil {
			t.Fatalf("create provider %s: %v", cfg.ID, err)
		}
	}
	if err := st.Agents().Create(ctx, &domain.Agent{
		ID: fixtureAgent, WorkspaceID: fixtureWorkspace, Slug: "atlas", Name: "Atlas",
		ProviderID: "prov-agent", Model: "agent-model",
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	records := &[]factoryRecord{}
	factory := ModelFactory(func(_ context.Context, providerType string, cred providers.Credential, modelName string) (Model, error) {
		*records = append(*records, factoryRecord{providerType: providerType, modelName: modelName, apiKey: cred.APIKey})
		return &scriptedModel{}, nil
	})
	return ctx, st, records, factory
}

func setMemorySettings(t *testing.T, ctx context.Context, st store.Store, providerID, modelName string) {
	t.Helper()
	config := map[string]any{}
	if providerID != "" {
		config["sidecall_provider_id"] = providerID
	}
	if modelName != "" {
		config["sidecall_model"] = modelName
	}
	if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
		WorkspaceID: fixtureWorkspace, ToolKey: "memory", Enabled: true, Config: config,
	}); err != nil {
		t.Fatalf("upsert memory settings: %v", err)
	}
}

func agentWith(t *testing.T, ctx context.Context, st store.Store, mut func(*domain.Agent)) {
	t.Helper()
	agent, err := st.Agents().ByID(ctx, fixtureWorkspace, fixtureAgent)
	if err != nil {
		t.Fatalf("read agent: %v", err)
	}
	mut(agent)
	if err := st.Agents().Update(ctx, agent); err != nil {
		t.Fatalf("update agent: %v", err)
	}
}

func lastModel(t *testing.T, records *[]factoryRecord) factoryRecord {
	t.Helper()
	if len(*records) == 0 {
		t.Fatal("expected the factory to be called")
	}
	return (*records)[len(*records)-1]
}

// TestCurationModelResolverFallback walks the D7 order end to end.
func TestCurationModelResolverFallback(t *testing.T) {
	ctx, st, records, factory := resolverWorld(t)

	// 1. The agent's curation pair wins over everything.
	agentWith(t, ctx, st, func(a *domain.Agent) {
		a.SkillCurationProviderID, a.SkillCurationModel = "prov-curation", "cur-model"
		a.MemorySidecallProviderID, a.MemorySidecallModel = "prov-memory", "mem-model"
	})
	setMemorySettings(t, ctx, st, "prov-memory", "ws-mem-model")
	resolver := CurationModelResolver(st.Providers(), st.Agents(), st.ToolSettings(), nil, factory, Config{
		SidecallProviderID: "prov-agent", SidecallModel: "ws-cfg-model",
	})
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec := lastModel(t, records); rec.modelName != "cur-model" || rec.providerType != "openai" {
		t.Fatalf("step 1 must resolve the curation pair, got %+v", rec)
	}

	// 2. Curation pair unset → the agent's memory side-call pair.
	agentWith(t, ctx, st, func(a *domain.Agent) {
		a.SkillCurationProviderID, a.SkillCurationModel = "", ""
	})
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec := lastModel(t, records); rec.modelName != "mem-model" || rec.providerType != "anthropic" {
		t.Fatalf("step 2 must resolve the memory pair, got %+v", rec)
	}

	// 3. Both agent pairs unset → the workspace default: the curation
	// config's pair.
	agentWith(t, ctx, st, func(a *domain.Agent) {
		a.MemorySidecallProviderID, a.MemorySidecallModel = "", ""
	})
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec := lastModel(t, records); rec.modelName != "ws-cfg-model" || rec.providerType != "google" {
		t.Fatalf("step 3 must resolve the curation config pair, got %+v", rec)
	}

	// 3b. Curation config pair unset too → the memory settings' pair.
	resolver = CurationModelResolver(st.Providers(), st.Agents(), st.ToolSettings(), nil, factory, Config{})
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec := lastModel(t, records); rec.modelName != "ws-mem-model" || rec.providerType != "anthropic" {
		t.Fatalf("step 3b must resolve the memory settings pair, got %+v", rec)
	}

	// 4. Nothing pinned anywhere → the model the agent itself runs.
	setMemorySettings(t, ctx, st, "", "") // clears the row's pair (absent keys)
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec := lastModel(t, records); rec.modelName != "agent-model" || rec.providerType != "google" {
		t.Fatalf("step 4 must resolve the agent's own model, got %+v", rec)
	}
}

// TestCurationModelResolverHalfSetPairsDegrade: half-set pairs (agent or
// settings) degrade to the next tier instead of failing.
func TestCurationModelResolverHalfSetPairsDegrade(t *testing.T) {
	ctx, st, records, factory := resolverWorld(t)
	agentWith(t, ctx, st, func(a *domain.Agent) {
		a.SkillCurationProviderID, a.SkillCurationModel = "prov-curation", "" // half
		a.MemorySidecallProviderID, a.MemorySidecallModel = "", "mem-model"   // half
	})
	setMemorySettings(t, ctx, st, "prov-memory", "") // half

	resolver := CurationModelResolver(st.Providers(), st.Agents(), st.ToolSettings(), nil, factory, Config{})
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if rec := lastModel(t, records); rec.modelName != "agent-model" || rec.providerType != "google" {
		t.Fatalf("half-set pairs must fall through to the agent's own model, got %+v", rec)
	}
}

// TestCurationModelResolverErrors: an unknown agent and a pair naming an
// unknown provider both error — no silent default provider exists.
func TestCurationModelResolverErrors(t *testing.T) {
	ctx, st, _, factory := resolverWorld(t)
	agentWith(t, ctx, st, func(a *domain.Agent) {
		a.SkillCurationProviderID, a.SkillCurationModel = "prov-missing", "cur-model"
	})
	resolver := CurationModelResolver(st.Providers(), st.Agents(), st.ToolSettings(), nil, factory, Config{})
	if _, err := resolver(ctx, fixtureWorkspace, fixtureAgent); err == nil {
		t.Fatal("a curation pair naming an unknown provider must error")
	}
	if _, err := resolver(ctx, fixtureWorkspace, "agt-ghost"); err == nil {
		t.Fatal("an unknown agent must error")
	}
	if _, err := resolver(ctx, fixtureWorkspace, ""); err == nil {
		t.Fatal("an empty agent id must error (curation side-calls are agent-anchored)")
	}
}
