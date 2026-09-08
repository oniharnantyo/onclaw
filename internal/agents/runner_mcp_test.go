package agents

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// namedStubTool is a minimal invokable tool standing in for an eino-ext mcp
// tool: its Info carries the server-assigned raw name; the naming pass is
// expected to rename it.
type namedStubTool struct{ raw string }

func (t namedStubTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.raw}, nil
}

func (t namedStubTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "{}", nil
}

// mcpStubManager records the refs it is asked for and answers from a
// per-server tool table or a per-server error table.
type mcpStubManager struct {
	mu    sync.Mutex
	refs  []mcp.Ref
	tools map[string][]tool.BaseTool // key: serverID
	errs  map[string]error           // key: serverID
}

func (m *mcpStubManager) Tools(_ context.Context, ref mcp.Ref) ([]tool.BaseTool, error) {
	m.mu.Lock()
	m.refs = append(m.refs, ref)
	m.mu.Unlock()
	if err := m.errs[ref.ServerID]; err != nil {
		return nil, err
	}
	return m.tools[ref.ServerID], nil
}

func (m *mcpStubManager) seenServerIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.refs))
	for _, ref := range m.refs {
		out = append(out, ref.ServerID)
	}
	return out
}

// mcpStubPolicy records the opt-in set it was consulted with and models the
// MCPPolicy contract (enabled master switch ∧ opt-in; enabled private rows)
// over canned rows, exactly like the settings-service-backed implementation
// must (workspace-mcp + agent-runtime deltas).
type mcpStubPolicy struct {
	workspace map[string][]domain.WorkspaceMCPServer
	agents    map[string][]domain.AgentMCPServer

	mu          sync.Mutex
	lastOptIns  []string
	lastWSID    string
	lastAgentID string
	policyErr   error
}

func (p *mcpStubPolicy) WorkspaceServers(_ context.Context, workspaceID string, optInIDs []string) ([]domain.WorkspaceMCPServer, error) {
	p.mu.Lock()
	p.lastWSID, p.lastOptIns = workspaceID, slices.Clone(optInIDs)
	p.mu.Unlock()
	if p.policyErr != nil {
		return nil, p.policyErr
	}
	var out []domain.WorkspaceMCPServer
	for _, s := range p.workspace[workspaceID] {
		if s.Enabled && slices.Contains(optInIDs, s.ID) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (p *mcpStubPolicy) AgentServers(_ context.Context, agentID string) ([]domain.AgentMCPServer, error) {
	p.mu.Lock()
	p.lastAgentID = agentID
	p.mu.Unlock()
	if p.policyErr != nil {
		return nil, p.policyErr
	}
	var out []domain.AgentMCPServer
	for _, s := range p.agents[agentID] {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

// mcpStubStatus records best-effort writes and can fail them.
type mcpStubStatus struct {
	workspace []mcpStatusWrite
	agent     []mcpStatusWrite
	writeErr  error
}

type mcpStatusWrite struct {
	scopeID, serverID, status, statusErr string
	toolCount                            int
}

func (s *mcpStubStatus) SetWorkspaceStatus(_ context.Context, workspaceID, serverID, status, statusErr string, toolCount int) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.workspace = append(s.workspace, mcpStatusWrite{workspaceID, serverID, status, statusErr, toolCount})
	return nil
}

func (s *mcpStubStatus) SetAgentStatus(_ context.Context, agentID, serverID, status, statusErr string, toolCount int) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.agent = append(s.agent, mcpStatusWrite{agentID, serverID, status, statusErr, toolCount})
	return nil
}

// lastWorkspaceFor returns the most recent write for a workspace server.
func (s *mcpStubStatus) lastWorkspaceFor(serverID string) (mcpStatusWrite, bool) {
	for i := len(s.workspace) - 1; i >= 0; i-- {
		if s.workspace[i].serverID == serverID {
			return s.workspace[i], true
		}
	}
	return mcpStatusWrite{}, false
}

// lastAgentFor returns the most recent write for an agent-private server.
func (s *mcpStubStatus) lastAgentFor(serverID string) (mcpStatusWrite, bool) {
	for i := len(s.agent) - 1; i >= 0; i-- {
		if s.agent[i].serverID == serverID {
			return s.agent[i], true
		}
	}
	return mcpStatusWrite{}, false
}

// setupMCPRunner seeds a minimal workspace/agent and returns a runner whose
// MCP seams are stubs. Callers then wire policy/manager/status through the
// WithMCP* options and drive resolve directly.
func setupMCPRunner(t *testing.T, agentTools []string) (*Runner, *domain.Workspace, *domain.Agent, ExecRequest) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()

	ws := &domain.Workspace{Slug: "acme", Name: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "prov"}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	user := &domain.User{Email: "u@example.com", Name: "U"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	role := &domain.Role{WorkspaceID: ws.ID, Name: "owner"}
	if err := st.Roles().Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{
		WorkspaceID: ws.ID, UserID: user.ID, RoleID: role.ID,
	}); err != nil {
		t.Fatalf("create member: %v", err)
	}
	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
		Temperature: 1.0,
		Tools:       agentTools,
	}
	if err := st.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	runner := NewRunner(
		st.Workspaces(), st.Agents(), st.Users(), st.Members(), st.Roles(),
		st.Providers(), st.SessionEvents(), st.SessionCheckpoints(),
		st.Memories(),
		[]byte("test-key-32-bytes-long-12345678"),
		t.TempDir(),
		WithAgenticModelFactory(func(context.Context, string, providers.Credential, string) (Model, error) {
			return newScriptedToolCallModel(), nil
		}),
		WithInstructionComposer(stubComposer{}),
	)

	req := ExecRequest{
		WorkspaceID: ws.ID,
		AgentID:     ag.ID,
		SessionID:   "sess-1",
		UserID:      user.ID,
		Input:       "hello",
	}
	return runner, ws, ag, req
}

// toolNamesOf reads the resolved (post-naming) names of the tool set.
func toolNamesOf(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	out := make([]string, 0, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		out = append(out, info.Name)
	}
	return out
}

// wsServer builds a workspace MCP row with canned status fields.
func wsServer(wsID, id, name string, enabled bool, status, statusErr string, toolCount int) domain.WorkspaceMCPServer {
	return domain.WorkspaceMCPServer{
		ID:            id,
		WorkspaceID:   wsID,
		Name:          name,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "/bin/true"},
		Enabled:       enabled,
		Status:        status,
		StatusError:   statusErr,
		ToolCount:     toolCount,
	}
}

// agentServer builds an agent-private MCP row.
func agentServer(wsID, agentID, id, name string, enabled bool) domain.AgentMCPServer {
	return domain.AgentMCPServer{
		ID:            id,
		WorkspaceID:   wsID,
		AgentID:       agentID,
		Name:          name,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "/bin/true"},
		Enabled:       enabled,
	}
}

func TestRunnerResolveMCPOptInFiltering(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	opted := wsServer(ws.ID, "srv-opted", "github", true, "", "", 0)
	notOpted := wsServer(ws.ID, "srv-other", "linear", true, "", "", 0)
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {opted, notOpted}}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-opted": {namedStubTool{"create_issue"}},
		"srv-other": {namedStubTool{"list_rows"}},
	}}
	status := &mcpStubStatus{}

	ag.EnabledMCPS = []string{"srv-opted"} // opts in only to github; linear is registered but not opted
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// The policy was consulted with the agent's full opt-in allowlist…
	if !slices.Equal(policy.lastOptIns, []string{"srv-opted"}) {
		t.Fatalf("policy opt-ins = %v", policy.lastOptIns)
	}
	if policy.lastWSID != ws.ID {
		t.Fatalf("policy workspace = %q", policy.lastWSID)
	}
	// …but only the opted-in server's tools resolve: the non-opted server
	// never reaches the connection layer.
	if got := manager.seenServerIDs(); !slices.Equal(got, []string{"srv-opted"}) {
		t.Fatalf("manager saw %v, want [srv-opted]", got)
	}
	names := toolNamesOf(t, tools)
	if len(names) != 1 || names[0] != "mcp__github__create_issue" {
		t.Fatalf("resolved = %v", names)
	}
}

func TestRunnerResolveMCPMasterSwitchWins(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	paused := wsServer(ws.ID, "srv-paused", "github", false, "", "", 0) // opted in, but paused
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {paused}}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-paused": {namedStubTool{"create_issue"}},
	}}
	status := &mcpStubStatus{}

	ag.EnabledMCPS = []string{"srv-paused"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := manager.seenServerIDs(); len(got) != 0 {
		t.Fatalf("paused server reached the resolver: %v", got)
	}
	if len(tools) != 0 {
		t.Fatalf("paused server contributed tools: %v", toolNamesOf(t, tools))
	}
	if len(status.workspace) != 0 {
		t.Fatalf("unexpected status writes: %v", status.workspace)
	}
}

func TestRunnerResolveMCPDeadServerSkipsAndMarks(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	alive := wsServer(ws.ID, "srv-alive", "github", true, "", "", 0)
	dead := wsServer(ws.ID, "srv-dead", "linear", true, "", "", 0)
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {alive, dead}}}
	manager := &mcpStubManager{
		tools: map[string][]tool.BaseTool{"srv-alive": {namedStubTool{"create_issue"}}},
		errs:  map[string]error{"srv-dead": errors.New("connection refused")},
	}
	status := &mcpStubStatus{}

	ag.EnabledMCPS = []string{"srv-alive", "srv-dead"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("dead server must not fail the run: %v", err)
	}
	names := toolNamesOf(t, tools)
	if len(names) != 1 || names[0] != "mcp__github__create_issue" {
		t.Fatalf("resolved = %v; dead server should contribute nothing", names)
	}
	write, ok := status.lastWorkspaceFor("srv-dead")
	if !ok {
		t.Fatalf("dead server status not recorded: %+v", status.workspace)
	}
	if write.status != domain.MCPStatusError || write.statusErr == "" {
		t.Fatalf("dead server status = %+v", write)
	}
	if write.toolCount != 0 {
		t.Fatalf("dead server tool count = %d", write.toolCount)
	}
}

func TestRunnerResolveMCPStatusWriteIsBestEffort(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	alive := wsServer(ws.ID, "srv-alive", "github", true, "", "", 0)
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {alive}}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-alive": {namedStubTool{"create_issue"}},
	}}
	// The status sink itself is down; the run must not notice.
	status := &mcpStubStatus{writeErr: errors.New("db unavailable")}

	ag.EnabledMCPS = []string{"srv-alive"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("failing status write must not fail the run: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("resolved = %v", toolNamesOf(t, tools))
	}
}

func TestRunnerResolveMCPSuccessRefreshesStatus(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	// Row was previously errored by a probe; a successful runtime connection
	// refreshes the stored status and tool count.
	stale := wsServer(ws.ID, "srv-1", "github", true, domain.MCPStatusError, "stale probe failure", 0)
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {stale}}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-1": {namedStubTool{"a"}, namedStubTool{"b"}},
	}}
	status := &mcpStubStatus{}

	ag.EnabledMCPS = []string{"srv-1"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	if _, _, err := runner.resolve(context.Background(), req, ws, ag); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	write, ok := status.lastWorkspaceFor("srv-1")
	if !ok || write.status != domain.MCPStatusConnected || write.statusErr != "" || write.toolCount != 2 {
		t.Fatalf("success status = %+v (ok=%v)", write, ok)
	}

	// A second resolve over an already-consistent row writes nothing (no
	// amplification). The policy re-reads rows, so simulate the persisted
	// write landing in the store-backed view first.
	policy.workspace[ws.ID][0].Status = domain.MCPStatusConnected
	policy.workspace[ws.ID][0].StatusError = ""
	policy.workspace[ws.ID][0].ToolCount = 2
	if _, _, err := runner.resolve(context.Background(), req, ws, ag); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if len(status.workspace) != 1 {
		t.Fatalf("status written %d times, want 1", len(status.workspace))
	}
}

func TestRunnerResolveMCPPrivateServers(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	mine := agentServer(ws.ID, ag.ID, "srv-mine", "private-one", true)
	other := agentServer(ws.ID, "agent-B", "srv-other-agent", "private-two", true)
	paused := agentServer(ws.ID, ag.ID, "srv-paused", "private-three", false)
	policy := &mcpStubPolicy{
		agents: map[string][]domain.AgentMCPServer{
			ag.ID:     {mine, paused},
			"agent-B": {other},
		},
	}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-mine":        {namedStubTool{"do_private"}},
		"srv-other-agent": {namedStubTool{"do_elsewhere"}},
	}}
	status := &mcpStubStatus{}

	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Only this agent's enabled private server resolves; the other agent's
	// server and the paused one never reach the connection layer.
	if got := manager.seenServerIDs(); !slices.Equal(got, []string{"srv-mine"}) {
		t.Fatalf("manager saw %v, want [srv-mine]", got)
	}
	names := toolNamesOf(t, tools)
	if len(names) != 1 || names[0] != "mcp__private-one__do_private" {
		t.Fatalf("resolved = %v", names)
	}
	// Status writes ride the agent-scoped port.
	if _, ok := status.lastAgentFor("srv-mine"); !ok {
		t.Fatalf("agent status not recorded: %+v", status.agent)
	}
}

func TestRunnerResolveMCPNamingCollisions(t *testing.T) {
	runner, ws, ag, req := setupMCPRunner(t, nil)

	// Two servers whose names collide after sanitization ("GH 1" / "GH_1"
	// both → gh_1), plus a clean one.
	a := wsServer(ws.ID, "srv-a", "GH 1", true, "", "", 0)
	b := wsServer(ws.ID, "srv-b", "GH_1", true, "", "", 0)
	clean := wsServer(ws.ID, "srv-c", "github", true, "", "", 0)
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {a, b, clean}}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-a": {namedStubTool{"do.thing"}},
		"srv-b": {namedStubTool{"do.thing"}},
		"srv-c": {namedStubTool{"create_issue"}},
	}}
	status := &mcpStubStatus{}

	ag.EnabledMCPS = []string{"srv-a", "srv-b", "srv-c"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	names := toolNamesOf(t, tools)
	want := []string{"mcp__gh_1__do_thing", "mcp__gh_1__do_thing_2", "mcp__github__create_issue"}
	if !slices.Equal(names, want) {
		t.Fatalf("resolved = %v, want %v", names, want)
	}
}

func TestRunnerResolveMCPIgnoresToolsAllowlist(t *testing.T) {
	// The agent's tools allowlist is empty: no built-in registry tools may
	// resolve, yet the opted-in MCP server's tools still do (agent-runtime
	// delta: MCP exposure is independent of the allowlist and the gate).
	runner, ws, ag, req := setupMCPRunner(t, nil)

	opted := wsServer(ws.ID, "srv-opted", "github", true, "", "", 0)
	policy := &mcpStubPolicy{workspace: map[string][]domain.WorkspaceMCPServer{ws.ID: {opted}}}
	manager := &mcpStubManager{tools: map[string][]tool.BaseTool{
		"srv-opted": {namedStubTool{"create_issue"}},
	}}
	status := &mcpStubStatus{}

	ag.EnabledMCPS = []string{"srv-opted"}
	runner.mcpPolicy, runner.mcpManager, runner.mcpStatus = policy, manager, status

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for _, name := range toolNamesOf(t, tools) {
		if name != "mcp__github__create_issue" {
			t.Fatalf("built-in tool %q leaked through an empty allowlist", name)
		}
	}
	if len(tools) != 1 {
		t.Fatalf("resolved = %v; want exactly the MCP tool", toolNamesOf(t, tools))
	}
}

func TestRunnerResolveMCPDefaultsContributeNothing(t *testing.T) {
	// No MCP options wired: resolution must yield zero MCP tools with no
	// nil-dereference, exactly as before the MCP integration.
	runner, ws, ag, req := setupMCPRunner(t, nil)

	_, tools, err := runner.resolve(context.Background(), req, ws, ag)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("default runner resolved %v; want none", toolNamesOf(t, tools))
	}
}

func TestRunnerResolveMCPPolicyErrorFailsResolution(t *testing.T) {
	// A store-level policy failure is infrastructure failure: unlike a dead
	// server it fails the resolution, like every other store error.
	runner, ws, ag, req := setupMCPRunner(t, nil)

	policy := &mcpStubPolicy{policyErr: errors.New("db down")}
	runner.mcpPolicy = policy

	if _, _, err := runner.resolve(context.Background(), req, ws, ag); err == nil {
		t.Fatal("expected policy failure to fail resolution")
	}
}

func TestNewRunnerMCPOptions(t *testing.T) {
	// Defaults: non-nil no-op seams (no nil guards at use sites).
	r := NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/x")
	if _, ok := r.mcpPolicy.(noopMCPPolicy); !ok {
		t.Fatalf("default policy = %T, want noopMCPPolicy", r.mcpPolicy)
	}
	if _, ok := r.mcpManager.(noopMCPTools); !ok {
		t.Fatalf("default manager = %T, want noopMCPTools", r.mcpManager)
	}
	if _, ok := r.mcpStatus.(noopMCPStatus); !ok {
		t.Fatalf("default status = %T, want noopMCPStatus", r.mcpStatus)
	}

	// Options wire the seams; nil arguments keep the defaults.
	policy := &mcpStubPolicy{}
	manager := &mcpStubManager{}
	status := &mcpStubStatus{}
	r = NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/x",
		WithMCPPolicy(policy), WithMCPManager(manager), WithMCPStatusWriter(status))
	if r.mcpPolicy != mcp.MCPPolicy(policy) || r.mcpManager != mcp.ToolSource(manager) || r.mcpStatus != mcp.StatusWriter(status) {
		t.Fatal("WithMCP* options did not wire the seams")
	}
	r = NewRunner(nil, nil, nil, nil, nil, nil, nil, nil, nil, []byte("k"), "/tmp/x",
		WithMCPPolicy(nil), WithMCPManager(nil), WithMCPStatusWriter(nil))
	if _, ok := r.mcpPolicy.(noopMCPPolicy); !ok {
		t.Fatalf("nil policy overrode the default: %T", r.mcpPolicy)
	}
}
