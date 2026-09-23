package agents

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// The connection gate (add-integration-authority tasks 4.1/4.2): tier default,
// resolution pruning by role, invocation re-check after a mid-run role change,
// canonical block shape, and the service-run escalation interrupt/resume/deny.
// ---------------------------------------------------------------------------

// scriptedGateModel is a scripted tool-call model: first response calls one
// named tool, later responses finish with text.
type scriptedGateModel struct {
	toolName string
	args     string
	calls    int
}

func (m *scriptedGateModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.AgenticMessage{
			Role: schema.AgenticRoleTypeAssistant,
			ContentBlocks: []*schema.ContentBlock{
				{Type: schema.ContentBlockTypeFunctionToolCall, FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-gate-1",
					Name:      m.toolName,
					Arguments: m.args,
				}},
			},
		}, nil
	}
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "done"}},
		},
	}, nil
}

func (m *scriptedGateModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

// stubOriginLookup is the ConnectionOriginLookup test double: a canned
// connection→service table.
type stubOriginLookup struct {
	services map[string]string
	err      error
}

func (l *stubOriginLookup) ConnectionServiceOf(_ context.Context, _, connectionID string) (string, error) {
	if l.err != nil {
		return "", l.err
	}
	return l.services[connectionID], nil
}

// gateTestRecipeSeq mints unique recipe ids (the domain registry panics on
// duplicate registration — recipes are global process state).
var gateTestRecipeSeq atomic.Int64

// registerGateTestRecipe registers a valid http-kind test recipe with one
// read-tier verb and one write-tier verb (or, with tierless, one tierless
// verb — the fail-safe default shape), pinned to the given base URL.
func registerGateTestRecipe(t *testing.T, baseURL string, tierless bool) *domain.Recipe {
	t.Helper()
	id := fmt.Sprintf("gategroup-test-%d", gateTestRecipeSeq.Add(1))
	verbs := []domain.RecipeVerb{
		{
			Tool:   id + ".list_things",
			Method: "GET",
			Path:   "/v1/things",
			Tier:   domain.RecipeToolTierRead,
		},
		{
			Tool:   id + ".create_thing",
			Method: "POST",
			Path:   "/v1/things",
			Tier:   domain.RecipeToolTierWrite,
		},
	}
	if tierless {
		verbs = []domain.RecipeVerb{
			{Tool: id + ".ping_thing", Method: "GET", Path: "/v1/ping"}, // no tier: gates write
		}
	}
	r := domain.Recipe{
		ID:           id,
		Service:      "Gate Test " + id,
		Icon:         "plug",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindHTTP,
		BaseURL:      baseURL,
		TokenHeader:  "X-Test-Token",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Method: "GET", Path: "/ping"},
		Verbs:        verbs,
	}
	domain.RegisterRecipe(r) // panics on an invalid recipe — the test fails loudly
	return domain.RecipeByID(id)
}

// gateHarness is one gate test's assembled world: a workspace with an admin
// (holds integrations.write) and a member (does not), an agent with a test
// connection attached, and a runner wired to the scripted model and the
// connection tool source.
type gateHarness struct {
	st        store.Store
	runner    *Runner
	ws        *domain.Workspace
	ag        *domain.Agent
	admin     *domain.User
	member    *domain.User
	adminRole *domain.Role
	membRole  *domain.Role
	model     *scriptedGateModel
	server    *recordingServer
	recipe    *domain.Recipe
}

// newGateHarness assembles the world. tierless swaps the tiered verb pair for
// a single tierless verb. opts append runner options (the MCP origin-link
// tests rewire the MCP seams on the returned runner).
func newGateHarness(t *testing.T, tierless bool, opts ...RunnerOption) *gateHarness {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	server := newRecordingServer(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	h := &gateHarness{st: st, server: server}
	h.recipe = registerGateTestRecipe(t, server.URL, tierless)

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	h.ws = ws
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	h.admin = &domain.User{Email: "admin@example.com", Name: "Admin"}
	if err := st.Users().Create(ctx, h.admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	h.member = &domain.User{Email: "member@example.com", Name: "Member"}
	if err := st.Users().Create(ctx, h.member); err != nil {
		t.Fatalf("create member: %v", err)
	}

	h.adminRole = &domain.Role{WorkspaceID: ws.ID, Name: "admins", Permissions: domain.AdminPermissions}
	if err := st.Roles().Create(ctx, h.adminRole); err != nil {
		t.Fatalf("create admin role: %v", err)
	}
	h.membRole = &domain.Role{WorkspaceID: ws.ID, Name: "workers", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, h.membRole); err != nil {
		t.Fatalf("create member role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: h.admin.ID, RoleID: h.adminRole.ID}); err != nil {
		t.Fatalf("add admin member: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: h.member.ID, RoleID: h.membRole.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	h.model = &scriptedGateModel{}
	memWorker, memSearch, memGate := newTestMemoryPipeline(st)
	base := []RunnerOption{
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return h.model, nil
		}),
		WithInstructionComposer(stubComposer{}),
		WithConnectionToolSource(NewConnectionToolSource(
			&stubConnectionLister{refs: []HTTPConnectionRef{connectionRef(h.recipe)}},
			&stubConnectionCreds{token: "tok"},
		)),
	}
	h.runner = NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(), st.AgentSessions(),
		st.GatewayLinks(), memWorker, memSearch, memGate,
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		append(base, opts...)...,
	)

	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	h.ag = ag

	// Seed the agent's on-disk workspace directory (the jail root the
	// composition requires at compose time).
	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(h.runner.onClawDir), ws.Slug, ag.Slug)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("seed agent dir: %v", err)
	}
	return h
}

// request builds an ExecRequest for the harness acting as the named user.
func (h *gateHarness) request(userID, origin string) ExecRequest {
	req := ExecRequest{
		WorkspaceID: h.ws.ID,
		AgentID:     h.ag.ID,
		SessionID:   "gate-sess-" + userID,
		UserID:      userID,
		Input:       "drive the connected service",
		Origin:      origin,
	}
	if origin == OriginService {
		req.ConnectionID = "conn-" + h.recipe.ID
		req.ConnectionService = h.recipe.ID
		req.Event = "thing.created"
	}
	return req
}

// resumeRequest builds the resume ExecRequest EXACTLY as the approvals
// endpoint does — no Origin, no attribution, no input (the production resume
// lane carries none of them). Every escalation resume test must use this
// shape: a resume that re-stamps OriginService would silently stop covering
// the origin-restoration contract.
func (h *gateHarness) resumeRequest(userID string) ExecRequest {
	return ExecRequest{
		WorkspaceID: h.ws.ID,
		AgentID:     h.ag.ID,
		SessionID:   "gate-sess-" + userID,
		UserID:      userID,
	}
}

// lastApproval drains the stream and returns the last approval_required
// payload, or nil.
func lastApproval(t *testing.T, stream *EventStream) *ApprovalPayload {
	t.Helper()
	var approval *ApprovalPayload
	for {
		ev, err := stream.Recv()
		if err != nil {
			return approval
		}
		if ev.Kind == TranscriptEventApprovalRequired && ev.Approval != nil {
			approval = ev.Approval
		}
	}
}

// toolResultOf returns the last result text recorded for the named tool, or "".
func toolResultOf(events []TranscriptEvent, name string) string {
	result := ""
	for _, e := range events {
		if e.Kind == TranscriptEventToolCallFinished && e.ToolResult != nil && e.ToolResult.Name == name {
			result = e.ToolResult.Result
		}
	}
	return result
}

// resolvedNames collects the resolved surface's tool names.
func resolvedNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	out := make([]string, 0, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil || info == nil {
			continue
		}
		out = append(out, info.Name)
	}
	return out
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 4.1 — canonical block shape exactness.
// ---------------------------------------------------------------------------

func TestGateBlockResultShape(t *testing.T) {
	got := GateBlockResult("github.merge_pull_request")
	want := `{"blocked_by_hook":true,"hook":"integration_authority","reason":"tool github.merge_pull_request requires the integrations.write permission"}`
	if got != want {
		t.Fatalf("canonical block = %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// 4.1 — tier default (undeclared = write): a tierless verb gates write, so a
// member's surface prunes it while an admin's keeps it.
// ---------------------------------------------------------------------------

func TestConnectionGate_TierlessVerbGatesWrite(t *testing.T) {
	h := newGateHarness(t, true)
	tierlessName := h.recipe.ID + ".ping_thing"

	// Member: the undeclared verb is off the surface (write default).
	_, tools, err := h.runner.resolve(context.Background(), h.request(h.member.ID, OriginUser), h.ws, h.ag, h.membRole)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if containsName(resolvedNames(t, tools), tierlessName) {
		t.Fatalf("tierless verb %q survived member resolution — undeclared must default to write", tierlessName)
	}

	// Admin: the same verb resolves — the default narrows the member surface,
	// never the admin's.
	cfg, tools, err := h.runner.resolve(context.Background(), h.request(h.admin.ID, OriginUser), h.ws, h.ag, h.adminRole)
	if err != nil {
		t.Fatalf("admin resolve: %v", err)
	}
	if !containsName(resolvedNames(t, tools), tierlessName) {
		t.Fatalf("admin surface lost the tierless verb %q", tierlessName)
	}
	if _, ok := cfg.Gate.origins[tierlessName]; !ok {
		t.Fatalf("gate origins missing tierless verb %q", tierlessName)
	}
}

// ---------------------------------------------------------------------------
// 4.1/4.2 — resolution pruning: member resolves read-only, admin both tiers.
// ---------------------------------------------------------------------------

func TestConnectionGate_MemberResolvesReadOnlySurface(t *testing.T) {
	h := newGateHarness(t, false)
	req := h.request(h.member.ID, OriginUser)

	cfg, tools, err := h.runner.resolve(context.Background(), req, h.ws, h.ag, h.membRole)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	readName := h.recipe.ID + ".list_things"
	writeName := h.recipe.ID + ".create_thing"
	names := resolvedNames(t, tools)
	if !containsName(names, readName) {
		t.Fatalf("member surface lost the read-tier tool %q (got %v)", readName, names)
	}
	if containsName(names, writeName) {
		t.Fatalf("member surface kept the write-tier tool %q — it must be pruned", writeName)
	}
	if _, ok := cfg.Gate.origins[writeName]; ok {
		t.Fatal("gate origins kept the pruned write-tier tool")
	}
	if _, ok := cfg.Gate.origins[readName]; !ok {
		t.Fatal("gate origins lost the read-tier tool")
	}
}

func TestConnectionGate_AdminResolvesBothTiers(t *testing.T) {
	h := newGateHarness(t, false)
	req := h.request(h.admin.ID, OriginUser)

	cfg, tools, err := h.runner.resolve(context.Background(), req, h.ws, h.ag, h.adminRole)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	names := resolvedNames(t, tools)
	for _, name := range []string{h.recipe.ID + ".list_things", h.recipe.ID + ".create_thing"} {
		if !containsName(names, name) {
			t.Fatalf("admin surface lost %q (got %v)", name, names)
		}
		if _, ok := cfg.Gate.origins[name]; !ok {
			t.Fatalf("gate origins lost %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 4.1 — invocation re-check AFTER a role change: the tool was on a composed
// surface (admin) but the permission was revoked before the call — the
// CURRENT permission set decides, the canonical block is the outcome, and a
// read-tier call still rides membership alone.
// ---------------------------------------------------------------------------

func TestConnectionGate_InvocationReCheckAfterRoleChange(t *testing.T) {
	h := newGateHarness(t, false)
	writeName := h.recipe.ID + ".create_thing"
	readName := h.recipe.ID + ".list_things"
	origin := connectionToolOrigin{
		ConnectionID: "conn-" + h.recipe.ID,
		Service:      h.recipe.ID,
		ServiceName:  h.recipe.Service,
		Recipe:       h.recipe,
	}

	// Demote the admin, then resolve afresh: the write tool prunes — the
	// demotion is honored at the next resolution.
	if err := h.st.Members().UpdateRole(context.Background(), h.ws.ID, h.admin.ID, h.membRole.ID); err != nil {
		t.Fatalf("demote admin: %v", err)
	}
	_, _, err := h.runner.resolve(context.Background(), h.request(h.admin.ID, OriginUser), h.ws, h.ag, h.membRole)
	if err != nil {
		t.Fatalf("resolve after demotion: %v", err)
	}

	// But a run whose surface was composed BEFORE the demotion (as it happens
	// mid-run) still carries the tool — the invocation re-check consults the
	// CURRENT member + role rows and denies with the canonical block.
	staleGate := &connectionGateConfig{
		origins: map[string]connectionToolOrigin{writeName: origin},
		authority: connectionGateAuthority{
			members:     h.st.Members(),
			roles:       h.st.Roles(),
			workspaceID: h.ws.ID,
			userID:      h.admin.ID, // resolved as admin, currently demoted
		},
	}
	blocked, resultJSON, gateErr := staleGate.evaluate(context.Background(), writeName)
	if gateErr != nil || !blocked {
		t.Fatalf("demoted user's write call must block (blocked=%v, err=%v)", blocked, gateErr)
	}
	if resultJSON != GateBlockResult(writeName) {
		t.Fatalf("denial = %s, want the canonical block", resultJSON)
	}

	// The read tier rides membership alone — still allowed after demotion.
	blocked, resultJSON, gateErr = staleGate.evaluate(context.Background(), readName)
	if blocked || gateErr != nil || resultJSON != "" {
		t.Fatalf("read-tier call after demotion must pass (blocked=%v, result=%q, err=%v)", blocked, resultJSON, gateErr)
	}

	// A store read failure fails closed: the gate never silently disappears.
	failingGate := &connectionGateConfig{
		origins: map[string]connectionToolOrigin{writeName: origin},
		authority: connectionGateAuthority{
			members:     failingMemberLookup{},
			roles:       h.st.Roles(),
			workspaceID: h.ws.ID,
			userID:      h.admin.ID,
		},
	}
	blocked, _, gateErr = failingGate.evaluate(context.Background(), writeName)
	if gateErr != nil || !blocked {
		t.Fatal("an authority read failure must fail closed with the block outcome")
	}
}

// failingMemberLookup models an authority store read failure.
type failingMemberLookup struct{}

func (failingMemberLookup) Get(context.Context, string, string) (*domain.Member, error) {
	return nil, fmt.Errorf("member store down")
}

// ---------------------------------------------------------------------------
// 4.2 — run-level: member read allowed; member write denied before any
// network request; admin both allowed. The write tier is pruned from a
// member's surface entirely (task 2.2), so a direct attempt at the pruned
// name cannot route to the connection at all — the transcript still shows a
// non-execution, the run continues, and the upstream is never touched.
// ---------------------------------------------------------------------------

func TestConnectionGate_MemberRunReadToolExecutes(t *testing.T) {
	h := newGateHarness(t, false)
	h.model.toolName = h.recipe.ID + ".list_things"

	req := h.request(h.member.ID, OriginUser)
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	if hasKind(events, TranscriptEventApprovalRequired) {
		t.Fatal("read-tier member call must not interrupt")
	}
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if got := toolResultOf(events, h.recipe.ID+".list_things"); got != `{"ok":true}` {
		t.Fatalf("read-tier tool result = %q, want the upstream body", got)
	}
	if h.server.hitCount() != 1 {
		t.Fatalf("upstream hit count = %d, want 1", h.server.hitCount())
	}
}

func TestConnectionGate_MemberRunWriteNeverReachesUpstream(t *testing.T) {
	h := newGateHarness(t, false)
	writeName := h.recipe.ID + ".create_thing"
	h.model.toolName = writeName

	req := h.request(h.member.ID, OriginUser)
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	if hasKind(events, TranscriptEventApprovalRequired) {
		t.Fatal("member write call must never escalate — the user path denies outright")
	}
	// The tool was pruned at resolution (off the model's surface), so the
	// direct attempt routes through the unknown-tool seam — which the gate
	// owns: the denial is the canonical block in the transcript, the run
	// continues, and nothing ever reaches the upstream.
	if got := toolResultOf(events, writeName); got != GateBlockResult(writeName) {
		t.Fatalf("direct attempt result = %q, want the canonical block", got)
	}
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("a denied direct attempt must not fail the run: %+v", events)
	}
	if h.server.hitCount() != 0 {
		t.Fatalf("write-tier call reached the upstream %d time(s)", h.server.hitCount())
	}
}

func TestConnectionGate_AdminRunWriteExecutes(t *testing.T) {
	h := newGateHarness(t, false)
	h.model.toolName = h.recipe.ID + ".create_thing"

	req := h.request(h.admin.ID, OriginUser)
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	if hasKind(events, TranscriptEventApprovalRequired) {
		t.Fatal("admin write call must not interrupt")
	}
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if got := toolResultOf(events, h.recipe.ID+".create_thing"); got != `{"ok":true}` {
		t.Fatalf("admin write tool result = %q, want the upstream body", got)
	}
	if h.server.hitCount() != 1 {
		t.Fatalf("upstream hit count = %d, want 1", h.server.hitCount())
	}
}

// ---------------------------------------------------------------------------
// 4.1/4.2 — the service-authority path: webhook runs gate as read tier; a
// write-tier call interrupts with the tool payload, resumes on approval, and
// ends gracefully on denial.
// ---------------------------------------------------------------------------

func TestConnectionGate_ServiceRunWriteEscalationInterrupts(t *testing.T) {
	h := newGateHarness(t, false)
	writeName := h.recipe.ID + ".create_thing"
	h.model.toolName = writeName

	req := h.request(h.admin.ID, OriginService) // service runs act under a mechanical identity
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	approval := lastApproval(t, stream)
	if approval == nil {
		t.Fatal("service-run write call must interrupt for approval")
	}
	if approval.Command != "" {
		t.Fatalf("tool escalation carried a shell command %q", approval.Command)
	}
	if approval.Tool == nil {
		t.Fatal("escalation approval must carry the tool object")
	}
	if approval.Tool.Name != writeName ||
		approval.Tool.Service != h.recipe.ID ||
		approval.Tool.ServiceName != h.recipe.Service ||
		approval.Tool.ConnectionID != "conn-"+h.recipe.ID ||
		approval.Tool.Tier != domain.RecipeToolTierWrite {
		t.Fatalf("tool payload = %+v", approval.Tool)
	}

	// The pending state hydrates identically (the approval card read path).
	pending, err := h.runner.PendingApproval(context.Background(), h.ws.ID, req.SessionID)
	if err != nil {
		t.Fatalf("PendingApproval: %v", err)
	}
	if pending == nil || pending.InterruptID != approval.InterruptID || pending.Tool == nil || pending.Tool.Name != writeName {
		t.Fatalf("pending approval = %+v, want interrupt %q with tool %q", pending, approval.InterruptID, writeName)
	}
}

func TestConnectionGate_ServiceRunEscalationResumeApproved(t *testing.T) {
	h := newGateHarness(t, false)
	writeName := h.recipe.ID + ".create_thing"
	h.model.toolName = writeName

	req := h.request(h.admin.ID, OriginService)
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	approval := lastApproval(t, stream)
	if approval == nil {
		t.Fatal("expected the write escalation interrupt")
	}

	// Owner/Admin approval: the tool call executes and the run resumes. The
	// resume request is the approvals endpoint's shape (no Origin) — the
	// runner must restore the interrupted service run's origin from the
	// pending tool payload, or the rebuilt gate would misclassify the run.
	resumeStream, err := h.runner.Resume(context.Background(), h.resumeRequest(h.admin.ID), approval, true)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	events := collectStream(t, resumeStream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed after approval, got %+v", events)
	}
	if hasKind(events, TranscriptEventError) {
		t.Fatalf("approved resume must not fail: %+v", events)
	}
	if got := toolResultOf(events, writeName); got != `{"ok":true}` {
		t.Fatalf("approved tool result = %q, want the upstream body", got)
	}
	if h.server.hitCount() != 1 {
		t.Fatalf("upstream hit count = %d, want exactly the approved call", h.server.hitCount())
	}
}

func TestConnectionGate_ServiceRunEscalationDenialEndsGracefully(t *testing.T) {
	h := newGateHarness(t, false)
	writeName := h.recipe.ID + ".create_thing"
	h.model.toolName = writeName

	req := h.request(h.admin.ID, OriginService)
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	approval := lastApproval(t, stream)
	if approval == nil {
		t.Fatal("expected the write escalation interrupt")
	}

	// Denial via the approvals endpoint's request shape (no Origin): the
	// resumed turn must NOT execute the write — the runner restores the
	// interrupted run's service origin from the pending tool payload, and
	// the gate blocks resume-target denials regardless of run mode. This is
	// the regression test for the denial fail-open on the production resume
	// lane: without it, the denied call dialed the upstream on the decider's
	// own integrations.write.
	resumeStream, err := h.runner.Resume(context.Background(), h.resumeRequest(h.admin.ID), approval, false)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	events := collectStream(t, resumeStream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("denial must end the run gracefully (turn_completed), got %+v", events)
	}
	if hasKind(events, TranscriptEventError) || hasKind(events, TranscriptEventCancelled) {
		t.Fatalf("denial is a canonical block ending, not a failure: %+v", events)
	}
	if got := toolResultOf(events, writeName); got != GateBlockResult(writeName) {
		t.Fatalf("denial tool result = %q, want the canonical block", got)
	}
	if h.server.hitCount() != 0 {
		t.Fatalf("denied tool reached the upstream %d time(s)", h.server.hitCount())
	}

	// The denial resolves the pending approval.
	pending, err := h.runner.PendingApproval(context.Background(), h.ws.ID, req.SessionID)
	if err != nil {
		t.Fatalf("PendingApproval: %v", err)
	}
	if pending != nil {
		t.Fatalf("denial must resolve the pending approval, got %+v", pending)
	}
}

func TestConnectionGate_ServiceRunReadToolNeverInterrupts(t *testing.T) {
	h := newGateHarness(t, false)
	h.model.toolName = h.recipe.ID + ".list_things"

	req := h.request(h.admin.ID, OriginService)
	stream, err := h.runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)

	if hasKind(events, TranscriptEventApprovalRequired) {
		t.Fatal("read-tier service-run call must not interrupt")
	}
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}
	if h.server.hitCount() != 1 {
		t.Fatalf("upstream hit count = %d, want 1", h.server.hitCount())
	}
}

// ---------------------------------------------------------------------------
// 4.1 — the MCP-kind seam: origin-marked servers annotate through the
// origin-link lookup; members prune write-tier MCP tools; unmarked servers
// stay out of the gate's scope; a failed link degrades fail-safe (write).
// ---------------------------------------------------------------------------

func registerGateMCPTestRecipe(t *testing.T) *domain.Recipe {
	t.Helper()
	id := fmt.Sprintf("gatemcp-test-%d", gateTestRecipeSeq.Add(1))
	r := domain.Recipe{
		ID:           id,
		Service:      "GateTest MCP " + id,
		Icon:         "plug",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://mcp.gatetest.dev/mcp",
		TokenHeader:  "Authorization",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Create a token"}},
		Probe:        domain.RecipeProbe{Tool: "gate_ping"},
		ToolTiers: []domain.RecipeToolTierRule{
			{Tool: "gate_ping", Tier: domain.RecipeToolTierRead},
			{Tool: "gate_create", Tier: domain.RecipeToolTierWrite},
		},
	}
	domain.RegisterRecipe(r)
	return domain.RecipeByID(id)
}

func TestConnectionGate_MCPOriginLinkPrunesForMember(t *testing.T) {
	h := newGateHarness(t, false)
	ctx := context.Background()

	mcpRecipe := registerGateMCPTestRecipe(t)
	// The connection-materialized server: its display name is the recipe's
	// Service (the materializeServer rule) and the origin marker points at
	// the connection the gate resolves through the lookup seam.
	originConn := "conn-" + mcpRecipe.ID
	srv := wsServer(h.ws.ID, "srv-gatemcp", mcpRecipe.Service, true, "", "", 0)
	srv.OriginConnectionID = originConn
	// An ordinary server with the same raw tool names: outside the gate.
	plain := wsServer(h.ws.ID, "srv-plain", "plain-tools", true, "", "", 0)

	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{
		h.ws.ID: {srv, plain},
	}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-gatemcp": {namedStubTool{"gate_ping"}, namedStubTool{"gate_create"}},
		"srv-plain":   {namedStubTool{"gate_ping"}},
	}}
	h.ag.EnabledMCPS = []string{"srv-gatemcp", "srv-plain"}
	h.runner.mcpPolicy, h.runner.mcpManager, h.runner.mcpStatus = policy, manager, &mcpStubStatus{}
	h.runner.connectionOrigins = &stubOriginLookup{services: map[string]string{
		originConn: mcpRecipe.ID,
	}}

	readName := mcp.ToolName(mcpRecipe.Service, "gate_ping")
	writeName := mcp.ToolName(mcpRecipe.Service, "gate_create")

	// Member: the read-tier MCP tool resolves; the write-tier is pruned; the
	// unmarked server's same-named tool stays (not the gate's scope).
	cfg, tools, err := h.runner.resolve(ctx, h.request(h.member.ID, OriginUser), h.ws, h.ag, h.membRole)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	names := resolvedNames(t, tools)
	if !containsName(names, readName) {
		t.Fatalf("member surface lost the read-tier MCP tool %q (got %v)", readName, names)
	}
	if containsName(names, writeName) {
		t.Fatalf("member surface kept the write-tier MCP tool %q", writeName)
	}
	if !containsName(names, "mcp__plain-tools__gate_ping") {
		t.Fatalf("unmarked server's tool must stay on the surface (got %v)", names)
	}
	if _, ok := cfg.Gate.origins[writeName]; ok {
		t.Fatal("gate origins kept the pruned write-tier MCP tool")
	}
	if _, ok := cfg.Gate.origins["mcp__plain-tools__gate_ping"]; ok {
		t.Fatal("an unmarked server's tool must not be gate-scoped")
	}
	origin, ok := cfg.Gate.origins[readName]
	if !ok {
		t.Fatalf("gate origins lost the read-tier MCP tool %q", readName)
	}
	if origin.ConnectionID != originConn || origin.Service != mcpRecipe.ID {
		t.Fatalf("MCP origin annotation = %+v", origin)
	}

	// Admin: both tiers resolve.
	cfg, tools, err = h.runner.resolve(ctx, h.request(h.admin.ID, OriginUser), h.ws, h.ag, h.adminRole)
	if err != nil {
		t.Fatalf("admin resolve: %v", err)
	}
	names = resolvedNames(t, tools)
	if !containsName(names, writeName) || !containsName(names, readName) {
		t.Fatalf("admin surface lost MCP tools (got %v)", names)
	}
	if _, ok := cfg.Gate.origins[writeName]; !ok {
		t.Fatal("gate origins lost the admin's write-tier MCP tool")
	}

	// A failed origin link degrades fail-safe: every tool of the server gates
	// write, so the member's surface prunes them all.
	h.runner.connectionOrigins = &stubOriginLookup{err: context.DeadlineExceeded}
	cfg, _, err = h.runner.resolve(ctx, h.request(h.member.ID, OriginUser), h.ws, h.ag, h.membRole)
	if err != nil {
		t.Fatalf("degraded resolve: %v", err)
	}
	if _, ok := cfg.Gate.origins[readName]; ok {
		t.Fatal("an unresolvable origin link must gate the server's tools as write (fail-safe)")
	}
}

func TestWithConnectionOriginLookupOption(t *testing.T) {
	lookup := &stubOriginLookup{}
	r := &Runner{}
	WithConnectionOriginLookup(lookup)(r)
	if r.connectionOrigins != lookup {
		t.Fatal("option did not wire the lookup")
	}
	WithConnectionOriginLookup(nil)(r)
	if r.connectionOrigins != lookup {
		t.Fatal("nil option replaced a wired lookup")
	}
}
