package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
	"github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// DefaultSummarizationMargin is the default safety margin applied to the
// resolved context window before summarization triggers.
const DefaultSummarizationMargin = 0.75

// Runner executes workspace agents and yields domain transcript events.
type Runner struct {
	workspaces    store.WorkspaceStore
	agents        store.AgentStore
	users         store.UserStore
	members       store.MemberStore
	roles         store.RoleStore
	providers     store.ProviderStore
	sessionEvents store.SessionEventStore
	checkpoints   store.SessionCheckpointStore
	memories      store.MemoryStore

	encryptionKey []byte
	onClawDir     string

	agenticFactory      AgenticModelFactory
	instructionComposer InstructionComposer
	toolRegistry        ToolRegistry
	toolPolicy          ToolPolicy
	enabledSkillReader  backend.EnabledSkillReader
	summarizationMargin float64
	maxIterations       int

	mcpPolicy  mcp.MCPPolicy
	mcpManager mcp.ToolSource
	mcpStatus  mcp.StatusWriter

	hooks      *hooks.Dispatcher
	hooksWired bool

	// hooksRuns carries the per-run hook chain from Run to its approval
	// Resume so D4 call-ID dedup spans the interrupt boundary: the Resolved
	// that evaluated a call before the interrupt is the same one consulted on
	// the approved re-execution. Entries are remembered when a run with hooks
	// starts and forgotten when drainAgentEvents reaches a terminal outcome;
	// an interrupted turn keeps its entry until the resume reuses it or the
	// next run on the session replaces it. A resumed turn in a fresh process
	// (restart) resolves a new chain — the decision cache is per-run, and the
	// run did not survive the restart.
	hooksMu   sync.Mutex
	hooksRuns map[RunKey]*hooks.Resolved

	baseCtx context.Context
	runMgr  *runManager
}

// RunnerOption configures the defaultable behavior knobs of a runner.
type RunnerOption func(*Runner)

// WithAgenticModelFactory overrides the agentic model factory (default: the
// provider-type → Eino adapter factory).
func WithAgenticModelFactory(f AgenticModelFactory) RunnerOption {
	return func(r *Runner) {
		if f != nil {
			r.agenticFactory = f
		}
	}
}

// WithInstructionComposer overrides the instruction composer (default: the
// AGENTS/IDENTITY/SOUL/WORKSPACE/USER/BOOTSTRAP composer).
func WithInstructionComposer(c InstructionComposer) RunnerOption {
	return func(r *Runner) {
		if c != nil {
			r.instructionComposer = c
		}
	}
}

// WithToolRegistry supplies the built-in tool surface. Every registered tool is
// exposed by default; the agent's tools denylist filters it at
// composition time (unknown names are inert). Default: the built-in registry.
func WithToolRegistry(reg ToolRegistry) RunnerOption {
	return func(r *Runner) {
		if reg != nil {
			r.toolRegistry = reg
		}
	}
}

// WithToolPolicy supplies the workspace tool gate consulted at resolution:
// the workspace's enabled tool set wins over the agent allowlist and over
// per-turn allowed-tools overrides (design.md D4). Default: a policy that
// enables everything; the composition root wires the settings-backed
// implementation.
func WithToolPolicy(policy ToolPolicy) RunnerOption {
	return func(r *Runner) {
		if policy != nil {
			r.toolPolicy = policy
		}
	}
}

// WithMCPPolicy supplies the MCP server policy consulted at resolution
// (design.md D6): the agent's opt-in workspace servers (enabled only) plus
// its private servers. MCP tools bypass the tools allowlist and this gate —
// the policy is their only authority. Default: a policy that contributes no
// servers; the composition root wires the settings-service-backed
// implementation (a later wave).
func WithMCPPolicy(policy mcp.MCPPolicy) RunnerOption {
	return func(r *Runner) {
		if policy != nil {
			r.mcpPolicy = policy
		}
	}
}

// WithMCPManager supplies the MCP connection cache the resolver draws server
// tools from through its ToolSource seam (design.md D5). Default: a no-op
// source that is never consulted (the default policy yields no servers).
func WithMCPManager(manager mcp.ToolSource) RunnerOption {
	return func(r *Runner) {
		if manager != nil {
			r.mcpManager = manager
		}
	}
}

// WithMCPStatusWriter supplies the best-effort sink for runtime MCP
// connection outcomes (design.md D8): failures flip the stored row status to
// error, successes refresh connected + tool count. A failing write never
// fails a run. Default: a discarding writer.
func WithMCPStatusWriter(w mcp.StatusWriter) RunnerOption {
	return func(r *Runner) {
		if w != nil {
			r.mcpStatus = w
		}
	}
}

// WithHooks supplies the lifecycle-hook dispatcher consulted at the runtime
// seams (design.md D2): prompt submission and tool calls on the run's
// context, observers detached. Default: a no-op dispatcher that resolves no
// hooks and skips resolution entirely; the composition root wires the real
// one.
func WithHooks(d *hooks.Dispatcher) RunnerOption {
	return func(r *Runner) {
		if d != nil {
			r.hooks = d
			r.hooksWired = true
		}
	}
}

// WithEnabledSkillReader supplies the workspace-skill registry reader that
// governs the workspace skills tier (design D2/D3): only skills whose rows
// are enabled attach to agents. The composition root wires the store-backed
// implementation; a nil reader (unit tests) treats every on-disk workspace
// skill as enabled.
func WithEnabledSkillReader(reader backend.EnabledSkillReader) RunnerOption {
	return func(r *Runner) {
		if reader != nil {
			r.enabledSkillReader = reader
		}
	}
}

// WithSummarizationMargin sets the safety factor applied to the resolved
// context window when arming the summarization trigger. Zero or ≥1 selects the
// default margin.
func WithSummarizationMargin(m float64) RunnerOption {
	return func(r *Runner) {
		if m > 0 && m < 1 {
			r.summarizationMargin = m
		}
	}
}

// WithMaxIterations overrides the ADK runner iteration cap. Zero selects the
// package default (DefaultMaxIterations).
func WithMaxIterations(n int) RunnerOption {
	return func(r *Runner) {
		if n > 0 {
			r.maxIterations = n
		}
	}
}

// WithBaseContext sets the context every run derives its lifetime from — the
// server's base context, cancelled on shutdown drain. Run contexts never
// derive from request or stream contexts; the caller's context governs only
// validation, loading, and resolution. Default: context.Background().
func WithBaseContext(ctx context.Context) RunnerOption {
	return func(r *Runner) {
		if ctx != nil {
			r.baseCtx = ctx
		}
	}
}

// NewRunner creates a new production runner instance from explicit per-store
// dependencies. Each granular store sub-interface is a positional parameter;
// pass the aggregate's accessors (e.g. st.Agents(), st.Users()) at the call
// site. Defaultable behaviors are set through RunnerOptions.
func NewRunner(
	workspaces store.WorkspaceStore,
	agents store.AgentStore,
	users store.UserStore,
	members store.MemberStore,
	roles store.RoleStore,
	providers store.ProviderStore,
	sessionEvents store.SessionEventStore,
	checkpoints store.SessionCheckpointStore,
	memories store.MemoryStore,
	encryptionKey []byte,
	onClawDir string,
	opts ...RunnerOption,
) *Runner {
	r := &Runner{
		workspaces:          workspaces,
		agents:              agents,
		users:               users,
		members:             members,
		roles:               roles,
		providers:           providers,
		sessionEvents:       sessionEvents,
		checkpoints:         checkpoints,
		memories:            memories,
		encryptionKey:       encryptionKey,
		onClawDir:           onClawDir,
		agenticFactory:      DefaultAgenticModelFactory,
		instructionComposer: NewInstructionComposer(),
		toolRegistry:        NewDefaultToolRegistry(memories),
		toolPolicy:          allowAllToolPolicy{},
		mcpPolicy:           noopMCPPolicy{},
		mcpManager:          noopMCPTools{},
		mcpStatus:           noopMCPStatus{},
		hooks:               hooks.NewNoopDispatcher(),
		hooksRuns:           make(map[RunKey]*hooks.Resolved),
		summarizationMargin: DefaultSummarizationMargin,
		baseCtx:             context.Background(),
	}
	for _, opt := range opts {
		opt(r)
	}
	r.runMgr = newRunManager(r.baseCtx, DefaultCancelEscalation)
	return r
}

// resolvedContextWindow computes the effective context window for an agent:
// stored value → catalog limit → 200k default. The catalog lookup is deferred
// to the caller-provided resolver to keep the engine catalog-free.
func resolvedContextWindow(agentWindow *int) int {
	return domain.ResolveContextWindow(agentWindow, nil)
}

// load resolves the workspace, agent, user, membership, and role from stores.
func (r *Runner) load(ctx context.Context, req ExecRequest) (
	ws *domain.Workspace, agent *domain.Agent, user *domain.User, role *domain.Role, err error,
) {
	ws, err = r.workspaces.ByID(ctx, req.WorkspaceID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load workspace: %w", err)
	}
	agent, err = r.agents.ByID(ctx, req.WorkspaceID, req.AgentID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load agent: %w", err)
	}
	user, err = r.users.ByID(ctx, req.UserID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load user: %w", err)
	}
	member, err := r.members.Get(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load membership: %w", err)
	}
	role, err = r.roles.ByID(ctx, member.RoleID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load role: %w", err)
	}
	return ws, agent, user, role, nil
}

// agentConfig represents the per-execution composition surface passed through
// an agent turn. It is unexported because it is internal to the agent's Run
// pipeline; callers use [ExecRequest] which carries only identity fields while
// the agent resolves tools, models, and skills internally.
type agentConfig struct {
	// Identity fields populated from ExecRequest + stores.
	WorkspaceID string
	AgentID     string
	SessionID   string
	UserID      string

	// ChatModel reference set by the agentic factory. nil means the model
	// has not been resolved yet. The concrete type is [Model].
	Model Model

	// MaxIterations caps how many turns the ADK runner takes before stopping.
	// Zero uses the package default.
	MaxIterations int

	// Capability fields — nil means the capability is off for this agent.
	// When non-nil, the corresponding middleware is wired into the stack.
	Filesystem    *FilesystemConfig
	Skills        *SkillsConfig
	Summarization *SummarizationConfig

	// Hooks is the run's resolved lifecycle-hook chain (design.md D2); nil or
	// hook-free attaches no hooks middleware. HooksBase carries the per-run
	// event identity every delivery is built from.
	Hooks     *hooks.Resolved
	HooksBase *hooks.Event
}

// validateAgentConfig checks that an agentConfig has all required fields
// after load and resolve phases complete. It returns a descriptive error when
// a required field is empty or otherwise invalid.
func validateAgentConfig(cfg agentConfig) error {
	var parts []string
	if cfg.WorkspaceID == "" {
		parts = append(parts, "workspace_id")
	}
	if cfg.AgentID == "" {
		parts = append(parts, "agent_id")
	}
	if cfg.SessionID == "" {
		parts = append(parts, "session_id")
	}
	if cfg.UserID == "" {
		parts = append(parts, "user_id")
	}
	if cfg.Model == nil {
		parts = append(parts, "model")
	}
	if len(parts) > 0 {
		return fmt.Errorf("agentConfig: missing %s", strings.Join(parts, ", "))
	}
	return nil
}

// resolve builds the agentConfig from loaded entities: tools, model, capabilities.
func (r *Runner) resolve(ctx context.Context, req ExecRequest, ws *domain.Workspace, agent *domain.Agent) (
	cfg agentConfig, resolvedTools []tool.BaseTool, err error,
) {
	cfg = agentConfig{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
		UserID:      req.UserID,
	}

	agentDir := domain.AgentWorkspaceDir(domain.WorkspaceRoot(r.onClawDir), ws.Slug, agent.Slug)

	// Workspace-local clock for tools that resolve dates (the memory tool's
	// MEMORY-TODAY.md). An empty identifier loads as UTC; an invalid one
	// degrades to UTC without failing the run.
	workspaceTZ, tzErr := time.LoadLocation(ws.Timezone)
	if tzErr != nil {
		workspaceTZ = time.UTC
	}

	// Tool resolution order (workspace-tool-catalog 3.2): the per-turn
	// AllowedTools override replaces the agent allowlist, the browser facade
	// alias expands, then the workspace gate filters — so the gate wins over
	// both the allowlist and the override.
	allowlist := agent.Tools
	if req.AllowedTools != nil {
		allowlist = req.AllowedTools
	}
	effective, err := r.applyToolGate(ctx, req.WorkspaceID, allowlist)
	if err != nil {
		return cfg, nil, fmt.Errorf("apply tool policy: %w", err)
	}

	toolConfigs, err := r.toolPolicy.ToolConfigs(ctx, req.WorkspaceID)
	if err != nil {
		return cfg, nil, fmt.Errorf("load tool configs: %w", err)
	}

	_, tools, err := ResolvedTools(ToolContext{
		WorkspaceSlug: ws.Slug,
		AgentSlug:     agent.Slug,
		AgentDir:      agentDir,
		SessionID:     req.SessionID,
		WorkspaceID:   req.WorkspaceID,
		AgentID:       req.AgentID,
		UserID:        req.UserID,
		WorkspaceTZ:   workspaceTZ,
		ToolConfigs:   toolConfigs,
	}, r.toolRegistry, effective)
	if err != nil {
		return cfg, nil, fmt.Errorf("resolve tools: %w", err)
	}
	resolvedTools = tools

	// MCP tools append after built-ins (design.md D6): governed solely by the
	// opt-in allowlist + master switches + private attachment via the policy —
	// deliberately independent of `effective`, the allowlist, and the gate.
	mcpTools, err := r.resolveMCPTools(ctx, req, agent)
	if err != nil {
		return cfg, nil, fmt.Errorf("resolve mcp tools: %w", err)
	}
	resolvedTools = append(resolvedTools, mcpTools...)

	provider, err := r.providers.ByID(ctx, req.WorkspaceID, agent.ProviderID)
	if err != nil {
		return cfg, nil, fmt.Errorf("load provider: %w", err)
	}
	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(r.encryptionKey, []byte(req.WorkspaceID), provider.KeyCiphertext)
		if err != nil {
			return cfg, nil, fmt.Errorf("decrypt provider credentials")
		}
		apiKey = string(plaintext)
	}
	cred := providers.Credential{
		Type:    provider.Type,
		BaseURL: provider.BaseURL,
		APIKey:  apiKey,
	}

	agenticModel, err := r.agenticFactory(ctx, provider.Type, cred, agent.Model)
	if err != nil {
		return cfg, nil, fmt.Errorf("build agentic model: %w", err)
	}
	cfg.Model = agenticModel

	window := resolvedContextWindow(agent.ContextWindow)
	triggerTokens := int(float64(window) * r.summarizationMargin)
	slog.InfoContext(ctx, "agent execution starting",
		"agent", agent.Slug,
		"model", agent.Model,
		"context_window", window,
		"summarization_margin", r.summarizationMargin,
		"summarization_trigger_tokens", triggerTokens,
	)

	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = r.maxIterations
		if cfg.MaxIterations == 0 {
			cfg.MaxIterations = DefaultMaxIterations
		}
	}

	skillsDir := domain.WorkspaceSkillsDir(r.onClawDir, ws.Slug)
	fsCfg := &FilesystemConfig{
		AgentDir:      agentDir,
		DisabledTools: disabledFilesystemTools(effective),
		// The workspace skills tree is readable (bundled references served by
		// absolute path) but never writable (design D6).
		ReadOnlyRoots: []string{skillsDir},
	}
	allowedSet := make(map[string]bool, len(effective))
	for _, t := range effective {
		allowedSet[t] = true
	}
	if allowedSet[ReservedShellTool] {
		fsCfg.Shell = backend.NewJailedShell(agentDir,
			backend.WithSkillsVenvBin(filepath.Join(skillsDir, ".venv", "bin")),
		).WithDecisionLedger(&checkpointDecisionLedger{checkpoints: r.checkpoints})
	}
	cfg.Filesystem = fsCfg
	cfg.Skills = &SkillsConfig{
		OnClawDir:     r.onClawDir,
		TenantSlug:    ws.Slug,
		AgentSlug:     agent.Slug,
		EnabledSkills: r.enabledSkillReader,
	}
	cfg.Summarization = &SummarizationConfig{TriggerTokens: triggerTokens}

	return cfg, resolvedTools, nil
}

// noopMCPPolicy is the default MCP policy: no servers, so resolution
// contributes zero MCP tools. It exists so the runner holds no nil
// dependencies at the point of use (composition-root wiring replaces it).
type noopMCPPolicy struct{}

func (noopMCPPolicy) WorkspaceServers(context.Context, string, []string) ([]domain.WorkspaceMCPServer, error) {
	return nil, nil
}

func (noopMCPPolicy) AgentServers(context.Context, string) ([]domain.AgentMCPServer, error) {
	return nil, nil
}

// noopMCPTools is the default ToolSource. The default policy never yields a
// server, so it is never consulted; it keeps the manager dependency non-nil.
type noopMCPTools struct{}

func (noopMCPTools) Tools(context.Context, mcp.Ref) ([]tool.BaseTool, error) { return nil, nil }

// noopMCPStatus discards best-effort status writes when no writer is wired.
type noopMCPStatus struct{}

func (noopMCPStatus) SetWorkspaceStatus(context.Context, string, string, string, string, int) error {
	return nil
}

func (noopMCPStatus) SetAgentStatus(context.Context, string, string, string, string, int) error {
	return nil
}

// resolveMCPTools appends the agent's MCP tools after the built-ins
// (design.md D6/D8): the policy's opt-in workspace servers first, then the
// agent's private servers, in policy order — the pass-shared [mcp.Namer]
// makes collision suffixes deterministic given that stable order. A
// per-server failure contributes zero tools and best-effort flips the row's
// status to error; the run proceeds. Policy failures (store-level) fail the
// resolution like any other store error.
func (r *Runner) resolveMCPTools(ctx context.Context, req ExecRequest, agent *domain.Agent) ([]tool.BaseTool, error) {
	wsServers, err := r.mcpPolicy.WorkspaceServers(ctx, req.WorkspaceID, agent.EnabledMCPS)
	if err != nil {
		return nil, fmt.Errorf("load workspace mcp servers: %w", err)
	}
	agentServers, err := r.mcpPolicy.AgentServers(ctx, req.AgentID)
	if err != nil {
		return nil, fmt.Errorf("load agent mcp servers: %w", err)
	}

	var (
		tools []tool.BaseTool
		namer = mcp.NewNamer()
	)
	// resolveServer fetches one server's tools through the manager, renames
	// them into the shared pass namespace, and reports the outcome
	// best-effort. Never fails the run (design.md D8).
	resolveServer := func(ref mcp.Ref, prevStatus, prevStatusErr string, prevCount int, report func(status, statusErr string, count int)) []tool.BaseTool {
		serverTools, err := r.mcpManager.Tools(ctx, ref)
		if err != nil {
			slog.WarnContext(ctx, "mcp server unavailable; skipping its tools",
				"workspace_id", req.WorkspaceID,
				"agent_id", req.AgentID,
				"server", ref.Name,
				"error", err,
			)
			if prevStatus != domain.MCPStatusError || prevStatusErr != err.Error() {
				report(domain.MCPStatusError, err.Error(), prevCount)
			}
			return nil
		}
		named := mcp.ApplyNames(ref.Name, serverTools, namer)
		if prevStatus != domain.MCPStatusConnected || prevCount != len(named) {
			report(domain.MCPStatusConnected, "", len(named))
		}
		return named
	}

	for _, s := range wsServers {
		ref := mcp.Ref{WorkspaceID: req.WorkspaceID, ServerID: s.ID, Name: s.Name, Conn: s.MCPConnection}
		ws, wsID := req.WorkspaceID, s.ID
		tools = append(tools, resolveServer(ref, s.Status, s.StatusError, s.ToolCount,
			func(status, statusErr string, count int) {
				reportMCPStatus(ctx, r.mcpStatus.SetWorkspaceStatus(ctx, ws, wsID, status, statusErr, count),
					"workspace", ref.Name)
			})...)
	}
	for _, s := range agentServers {
		ref := mcp.Ref{WorkspaceID: s.WorkspaceID, ServerID: s.ID, Name: s.Name, Conn: s.MCPConnection}
		agentID, srvID := req.AgentID, s.ID
		tools = append(tools, resolveServer(ref, s.Status, s.StatusError, s.ToolCount,
			func(status, statusErr string, count int) {
				reportMCPStatus(ctx, r.mcpStatus.SetAgentStatus(ctx, agentID, srvID, status, statusErr, count),
					"agent", ref.Name)
			})...)
	}
	return tools, nil
}

// reportMCPStatus logs a failed best-effort status write; the run proceeds
// regardless (design.md D8: a failing status write never fails the run).
func reportMCPStatus(ctx context.Context, err error, scope, server string) {
	if err != nil {
		slog.WarnContext(ctx, "mcp status write failed (best-effort)",
			"scope", scope, "server", server, "error", err)
	}
}

// skillMentionPattern matches $name tokens in user input. Skill names are
// DNS-label shaped (lowercase letters, digits, hyphens); anything else —
// shell variables like $HOME, dollar amounts — does not match.
var skillMentionPattern = regexp.MustCompile(`\$([a-z0-9][a-z0-9-]*)`)

// injectSkillInvocations preprocesses user input for explicit skill
// invocation (design D7): every $name token matching an available skill
// (system, enabled workspace, or the owning agent's tier) turns into a
// blocking instruction injected ahead of the user's message, mirroring the
// skill middleware's own "invoke before responding" language. Execution
// still flows through the skill middleware's tool. Non-matching tokens pass
// through untouched; lookup failures degrade to the original input.
func (r *Runner) injectSkillInvocations(ctx context.Context, ws *domain.Workspace, agent *domain.Agent, input string) string {
	if input == "" || ws == nil || agent == nil || !strings.Contains(input, "$") {
		return input
	}

	names, err := backend.AvailableSkillNames(ctx, r.onClawDir, ws.Slug, agent.Slug, r.enabledSkillReader)
	if err != nil {
		slog.WarnContext(ctx, "skill invocation preprocessing skipped: available skills lookup failed", "error", err)
		return input
	}
	available := make(map[string]struct{}, len(names))
	for _, name := range names {
		available[name] = struct{}{}
	}

	var invocations []string
	injected := make(map[string]struct{})
	for _, match := range skillMentionPattern.FindAllStringSubmatch(input, -1) {
		name := match[1]
		if _, done := injected[name]; done {
			continue
		}
		if _, ok := available[name]; !ok {
			continue // unknown or disabled: plain text
		}
		injected[name] = struct{}{}
		invocations = append(invocations, fmt.Sprintf(
			"The user explicitly invoked the skill %q ($%s). Before generating any other response about this task, invoke the skill tool with name %q to load the skill's full instructions, then follow them. Do not answer until the skill is loaded.",
			name, name, name,
		))
	}
	if len(invocations) == 0 {
		return input
	}

	return strings.Join(invocations, "\n") + "\n\n" + input
}

// composeAgent validates config, builds instruction, and delegates composition to Compose.
func (r *Runner) composeAgent(
	ctx context.Context,
	cfg *agentConfig,
	ws *domain.Workspace,
	user *domain.User,
	role *domain.Role,
	resolvedTools []tool.BaseTool,
	domainAgent *domain.Agent,
) (adk.TypedResumableAgent[*schema.AgenticMessage], error) {
	if err := validateAgentConfig(*cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	instruction, err := r.instructionComposer.Compose(ctx, ComposeParams{
		AgentDir:  cfg.Filesystem.AgentDir,
		Workspace: ws,
		User:      user,
		RoleName:  role.Name,
		Memories:  r.memories,
	})
	if err != nil {
		return nil, fmt.Errorf("compose instruction: %w", err)
	}

	adkAgent, err := Compose(ctx, &Config{
		Name:          domainAgent.Name,
		Description:   domainAgent.Description,
		Instruction:   instruction,
		ChatModel:     cfg.Model,
		Tools:         resolvedTools,
		MaxIterations: cfg.MaxIterations,
		Filesystem:    cfg.Filesystem,
		Skills:        cfg.Skills,
		Summarization: cfg.Summarization,
		Hooks:         cfg.Hooks,
		HooksBase:     cfg.HooksBase,
	})
	if err != nil {
		return nil, fmt.Errorf("compose agent: %w", err)
	}
	return adkAgent, nil
}

// execute runs the composed ADK agent and streams events through the given
// session adapter (persistent or ephemeral no-store). The run context comes
// from the manager via handle — the caller's context never reaches the run.
// cancelOpt is the per-run adk.WithCancel option: it arms the ADK agent-level
// cancel state machine so an explicit cancel persists the durable cancel
// marker instead of a session error.
func (r *Runner) execute(
	handle *runHandle,
	cancelOpt adk.AgentRunOption,
	adkAgent adk.TypedResumableAgent[*schema.AgenticMessage],
	req ExecRequest,
	sessionAdapter *ADKSessionAdapter,
	ephemeral bool,
	hookChain *hooks.Resolved,
	hookBase hooks.Event,
) *EventStream {
	var sessionStore adk.SessionEventStore[*schema.AgenticMessage] = sessionAdapter
	var cpStore adk.CheckPointStore = sessionAdapter
	if ephemeral {
		eph := NewEphemeralSessionAdapter()
		sessionStore = eph
		cpStore = eph
	}

	runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           adkAgent,
		EnableStreaming: true,
		CheckPointStore: cpStore,
		SessionID:       req.SessionID,
		SessionStore:    sessionStore,
	})

	stream := NewEventStream(128)
	go r.streamRun(handle, cancelOpt, runner, stream, req, hookChain, hookBase)
	return stream
}

// Run executes an agent turn and streams transcript events to the caller.
// The run's lifetime is detached from the caller's context and from the
// stream: returning early, abandoning the stream, or closing it via
// stream.Cancel() never cancels the run. Explicit cancellation goes through
// CancelRun. A second run on a live session returns an error wrapping
// domain.ErrConflict.
func (r *Runner) Run(ctx context.Context, req ExecRequest) (*EventStream, error) {
	return r.run(ctx, req, false)
}

// RunEphemeral executes an agent turn in an ephemeral session: full-replay
// history is empty and nothing persists (no session events, no checkpoints).
// Used by the /v1 facade for requests with no session binding.
func (r *Runner) RunEphemeral(ctx context.Context, req ExecRequest) (*EventStream, error) {
	return r.run(ctx, req, true)
}

// SubscribeRun attaches a fresh subscriber stream to the run currently
// executing for key. It returns (0, nil, false) when no run is live — the
// caller falls back to committed history. The stream stays live until the run
// finishes, at which point it is closed so Recv drains to EOF; detach early
// via UnsubscribeRun. Slow consumers never stall the run: Send drops when the
// subscriber's buffer is full.
func (r *Runner) SubscribeRun(key RunKey) (uint64, *EventStream, bool) {
	return r.runMgr.Subscribe(key)
}

// UnsubscribeRun detaches a subscriber added by SubscribeRun without
// affecting the run or other subscribers. Unknown keys and sub-IDs are no-ops.
func (r *Runner) UnsubscribeRun(key RunKey, subID uint64) {
	r.runMgr.Unsubscribe(key, subID)
}

// IsRunActive reports whether a run is currently executing for key.
func (r *Runner) IsRunActive(key RunKey) bool {
	return r.runMgr.isLive(key)
}

// CancelRun cancels the live run for the given session. The run unwinds at
// the next safe point and records a cancel marker in the session history.
// It returns false when no run is live for the session.
func (r *Runner) CancelRun(workspaceID, agentID, sessionID string) bool {
	return r.runMgr.cancel(RunKey{
		WorkspaceID: workspaceID,
		AgentID:     agentID,
		SessionID:   sessionID,
	})
}

// DrainRuns stops accepting new runs and waits up to timeout for in-flight
// runs to reach a terminal state, then cancels stragglers. Used by graceful
// shutdown before process exit.
func (r *Runner) DrainRuns(timeout time.Duration) {
	r.runMgr.drain(timeout)
}

func (r *Runner) run(ctx context.Context, req ExecRequest, ephemeral bool) (*EventStream, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	ws, domainAgent, user, role, err := r.load(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	cfg, resolvedTools, err := r.resolve(ctx, req, ws, domainAgent)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	// Hooks are resolved ONCE per run (design.md D2, D7): definitions read
	// fresh, matchers compiled once, per-run state (call-ID dedup, prompt
	// caps) owned by the chain. The chain is shared with the middleware and
	// carried across an approval resume so pre_tool_use evaluates each call
	// exactly once (D4). Runners without a wired dispatcher skip resolution
	// entirely; store-level read failures fail the run — a policy gate must
	// not silently disappear.
	hookChain, err := r.resolveHooksChain(ctx, ws, domainAgent)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}
	hookBase := hookBaseEvent(normalizeOrigin(req.Origin), ws, domainAgent, user, req.SessionID)

	// Run-entry seam (design.md D2/D6): user_prompt_submit evaluates BEFORE
	// the model is ever called. A block ends the turn with the notice + a
	// well-formed turn_completed terminal — zero tokens, live stream
	// terminates cleanly — and the notice persists so a reloaded transcript
	// shows why the turn has no assistant reply.
	if hookChain.HasHooks() {
		blocked, hookName, reason := hookChain.EvaluatePromptSubmission(ctx, hookBase)
		if blocked {
			return r.blockedPromptTurn(ctx, req, ephemeral, hookChain, hookBase, hookName, reason), nil
		}
		hookChain.ObserveRunStarted(ctx, hookBase)
		cfg.Hooks = hookChain
		cfg.HooksBase = &hookBase
		r.rememberHookChain(runKeyOf(req), hookChain)
	}

	adkAgent, err := r.composeAgent(ctx, &cfg, ws, user, role, resolvedTools, domainAgent)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}

	// $name explicit invocation (design D7): one seam ahead of the ADK runner
	// so chats, channels, and cron prompts all honor it.
	req.Input = r.injectSkillInvocations(ctx, ws, domainAgent, req.Input)

	sessionAdapter := NewADKSessionAdapter(r.sessionEvents, r.checkpoints, req.WorkspaceID)

	// The caller's ctx covered validation, loading, and resolution only; the
	// run registers with the manager, which derives its context from base.
	// The per-run ADK cancel option arms the safe-point cancel machine whose
	// AgentCancelFunc the manager invokes on explicit cancel.
	cancelOpt, agentCancel := adk.WithCancel()
	handle, err := r.runMgr.start(RunKey{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
	}, agentCancel)
	if err != nil {
		return nil, fmt.Errorf("agent.Run: %w", err)
	}
	return r.execute(handle, cancelOpt, adkAgent, req, sessionAdapter, ephemeral, hookChain, hookBase), nil
}

// resolveHooksChain resolves the run's hook chain (D2). A runner whose
// dispatcher was never wired (the no-op default) skips resolution entirely;
// a wired dispatcher resolves fresh every run (D7) and store-level read
// failures return an error — the caller owns the run's fate.
func (r *Runner) resolveHooksChain(ctx context.Context, ws *domain.Workspace, agent *domain.Agent) (*hooks.Resolved, error) {
	if !r.hooksWired || r.hooks == nil {
		return hooks.NewNoopDispatcher().Resolve(ctx, ws.ID, agent.ID)
	}
	return r.hooks.Resolve(ctx, ws.ID, agent.ID)
}

// hookBaseEvent builds the per-run hook event identity every delivery clones
// (hooks design.md D1: workspace, agent, session, originating user, origin).
func hookBaseEvent(origin string, ws *domain.Workspace, agent *domain.Agent, user *domain.User, sessionID string) hooks.Event {
	base := hooks.Event{
		Origin:    origin,
		Workspace: hooks.EventRef{ID: ws.ID, Name: ws.Name},
		Agent:     hooks.EventRef{ID: agent.ID, Name: agent.Name},
		SessionID: sessionID,
	}
	if user != nil {
		base.User = &hooks.EventRef{ID: user.ID, Name: user.Name}
	}
	return base
}

// blockedPromptTurn terminates a hook-blocked submission (design.md D6): the
// notice transcript event, a well-formed turn_completed terminal, the durable
// history entry (non-ephemeral sessions), the detached observers, and a
// closed stream — the model is never called, so the turn spends zero tokens
// and live consumers see a clean termination instead of a loading hang.
func (r *Runner) blockedPromptTurn(
	ctx context.Context,
	req ExecRequest,
	ephemeral bool,
	chain *hooks.Resolved,
	base hooks.Event,
	hookName, reason string,
) *EventStream {
	turnID := uuid.NewString()
	now := time.Now().UTC()
	stream := NewEventStream(128)
	stream.Send(&TranscriptEvent{
		Kind:       TranscriptEventTurnStarted,
		OccurredAt: now,
		TurnID:     turnID,
	})
	stream.Send(&TranscriptEvent{
		Kind:          TranscriptEventPromptBlocked,
		OccurredAt:    now,
		TurnID:        turnID,
		PromptBlocked: &PromptBlockedPayload{Hook: hookName, Reason: reason},
	})
	stream.Send(&TranscriptEvent{
		Kind:       TranscriptEventTurnCompleted,
		OccurredAt: time.Now().UTC(),
		TurnID:     turnID,
	})

	// Durable notice (D6), best-effort: the enforcement already happened; a
	// failing history write degrades the reloaded view, never the decision.
	if !ephemeral {
		adapter := NewADKSessionAdapter(r.sessionEvents, r.checkpoints, req.WorkspaceID)
		persistPromptBlocked(ctx, adapter, req.SessionID, turnID, hookName, reason)
	}

	// Observers see the full lifecycle (D1): the run started and finished
	// completed — its terminal event is a well-formed turn_completed.
	chain.ObserveRunStarted(ctx, base)
	chain.RunFinished(ctx, base, hookRunStatusCompleted)

	_ = stream.Close()
	return stream
}

// persistPromptBlocked appends the hook-blocked prompt notice as an
// application-owned session event so hydrated transcripts render it
// identically to the live stream (D6).
func persistPromptBlocked(ctx context.Context, adapter *ADKSessionAdapter, sessionID, turnID, hookName, reason string) {
	ev := &adk.SessionEvent[*schema.AgenticMessage]{
		EventID:   uuid.NewString(),
		TurnID:    turnID,
		Timestamp: time.Now().UTC(),
		Kind:      sessionEventKindPromptBlocked,
		Extension: &adk.SessionExtensionEvent{Data: promptBlockedEvent{Hook: hookName, Reason: reason}},
	}
	if err := adapter.AppendEvents(ctx, sessionID, []*adk.SessionEvent[*schema.AgenticMessage]{ev}); err != nil {
		slog.WarnContext(ctx, "hooks: persist prompt_blocked notice failed (best-effort)",
			"session_id", sessionID, "error", err)
	}
}

// rememberHookChain stores the run's hook chain for a later approval Resume
// of the same session (D4 call-ID dedup across the interrupt boundary).
func (r *Runner) rememberHookChain(key RunKey, chain *hooks.Resolved) {
	if !chain.HasHooks() {
		return
	}
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	r.hooksRuns[key] = chain
}

// reuseHookChain returns the chain remembered by the interrupted run, or nil
// when this resume starts fresh (no interrupted turn, or a restart).
func (r *Runner) reuseHookChain(key RunKey) *hooks.Resolved {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	return r.hooksRuns[key]
}

// forgetHookChain drops the run's hook chain at a terminal outcome: the next
// run on the session resolves fresh per-run state (D4).
func (r *Runner) forgetHookChain(key RunKey) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	delete(r.hooksRuns, key)
}

// run_finished statuses (hooks design.md D1).
const (
	hookRunStatusCompleted = "completed"
	hookRunStatusFailed    = "failed"
	hookRunStatusCancelled = "cancelled"
)

// runKeyOf builds the manager key for a request's session coordinates.
func runKeyOf(req ExecRequest) RunKey {
	return RunKey{WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID}
}

// streamRun drives the ADK runner to completion and maps events onto the EventStream.
func (r *Runner) streamRun(
	handle *runHandle,
	cancelOpt adk.AgentRunOption,
	runner *adk.TypedRunner[*schema.AgenticMessage],
	stream *EventStream,
	req ExecRequest,
	hookChain *hooks.Resolved,
	hookBase hooks.Event,
) {
	key := runKeyOf(req)
	defer handle.finish()
	// Close dynamic subscriber streams before finish() flips lr.done: running
	// here — ahead of the deregistration goroutine that drops the manager
	// entry — guarantees every attached live subscriber is closed and drains
	// to EOF on all exit paths, including the interrupt/cancel short-circuits.
	defer r.runMgr.CloseSubscribers(key)
	defer handle.cancel()
	defer func() { _ = stream.Close() }()
	defer r.logTapDrops(stream, req)
	r.teardownBrowserSession(req)

	turnID := ""
	var partialText string // accumulated text delta for the current streaming message

	// cancelOpt arms the per-run ADK cancel state machine: CancelRun goes
	// through the manager's agentCancel and the run unwinds at a safe point,
	// persisting the durable cancel marker.
	iter := runner.Query(handle.ctx, req.Input, cancelOpt)

	// Emit turn_started.
	now := time.Now().UTC()
	stream.Send(&TranscriptEvent{
		Kind:       TranscriptEventTurnStarted,
		OccurredAt: now,
		TurnID:     turnID,
	})

	r.drainAgentEvents(handle.ctx, iter, stream, key, turnID, partialText, hookChain, hookBase)
}

// logTapDrops emits one debug line when the live tap dropped events because
// no consumer kept up. Persisted history is unaffected.
func (r *Runner) logTapDrops(stream *EventStream, req ExecRequest) {
	if d := stream.Dropped(); d > 0 {
		slog.Debug("live event tap dropped events",
			"dropped", d,
			"workspace_id", req.WorkspaceID,
			"agent_id", req.AgentID,
			"session_id", req.SessionID,
		)
	}
}

// drainAgentEvents consumes the ADK event iterator and maps it onto the
// domain EventStream, including the interrupt short-circuit: an approval
// interrupt ends the stream without a terminal event. Every mapped event is
// additionally fanned out via Broadcast to dynamically attached live
// subscribers; subscriber sends are drop-new, so slow consumers never stall
// the run.
func (r *Runner) drainAgentEvents(
	ctx context.Context,
	iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]],
	stream *EventStream,
	key RunKey,
	turnID string,
	partialText string,
	hookChain *hooks.Resolved,
	hookBase hooks.Event,
) {
	var lastErr error
	var usage UsagePayload
	// partialReasoning accumulates reasoning deltas of the current streaming
	// message so the completed message can carry it alongside the text.
	var partialReasoning string
	// Tool calls are surfaced from model messages (streaming frames carry the
	// call, later message outputs carry the result); span events may also
	// report them, so both paths dedup per call ID.
	startedTools := map[string]bool{}
	finishedTools := map[string]bool{}
	// startedAtTools records the first-seen start time per call ID so
	// emitToolFinished can stamp ToolResultPayload.Latency on both the
	// message-driven and span-driven emit paths (design D3).
	startedAtTools := map[string]time.Time{}

	// emit sends every mapped transcript event to the primary tap and, additively,
	// to all dynamically attached live subscribers of the run.
	emit := func(ev *TranscriptEvent) {
		stream.Send(ev)
		r.runMgr.Broadcast(key, ev)
	}

	// settle is the terminal seam (design.md D2/D5): run_finished fires after
	// the terminal transcript event settles — on the cancel path, after the
	// durable cancel marker drains — with the outcome as status data. The
	// per-run hook chain is forgotten here: the next run on the session
	// resolves fresh per-run state (D4). The approval-interrupt short-circuit
	// below deliberately never settles: a paused turn is not terminal, and
	// the resumed turn keeps evaluating against the same chain. hookChain is
	// nil only for direct drainAgentEvents callers without hooks (tests).
	settle := func(status string) {
		r.forgetHookChain(key)
		if hookChain == nil || !hookChain.HasHooks() {
			return
		}
		hookChain.RunFinished(ctx, hookBase, status)
	}

	recordToolStart := func(callID string, at time.Time) {
		if callID == "" {
			return
		}
		if _, ok := startedAtTools[callID]; !ok {
			startedAtTools[callID] = at
		}
	}
	toolLatency := func(callID string) time.Duration {
		started, ok := startedAtTools[callID]
		if !ok {
			return 0
		}
		return time.Since(started)
	}

	emitToolStarted := func(callID, name, args string) {
		if callID == "" || startedTools[callID] {
			return
		}
		startedTools[callID] = true
		recordToolStart(callID, time.Now().UTC())
		emit(&TranscriptEvent{
			Kind:       TranscriptEventToolCallStarted,
			OccurredAt: time.Now().UTC(),
			TurnID:     turnID,
			ToolCall:   &ToolCallPayload{CallID: callID, Name: name, Arguments: args},
		})
	}
	emitToolFinished := func(callID, name, result string) {
		if callID == "" || finishedTools[callID] {
			return
		}
		finishedTools[callID] = true
		emit(&TranscriptEvent{
			Kind:       TranscriptEventToolCallFinished,
			OccurredAt: time.Now().UTC(),
			TurnID:     turnID,
			ToolResult: &ToolResultPayload{CallID: callID, Name: name, Result: result, Latency: toolLatency(callID)},
		})
	}

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		now := time.Now().UTC()

		// Propagate turn ID from session events.
		if event.SessionEventVariant != nil && event.SessionEventVariant.Event != nil {
			if t := event.SessionEventVariant.Event.TurnID; t != "" && turnID == "" {
				turnID = t
			}
		}

		// Handle errors.
		if event.Err != nil {
			// The ADK agent-level cancel machine ends the run with a
			// *CancelError (safe-point or escalated abort) and persists the
			// durable cancel marker itself; surface the live view as
			// Cancelled, not an error. ErrStreamCanceled rides the model
			// stream when the escalated immediate abort tears it down.
			var cancelErr *adk.CancelError
			var streamCanceled *adk.StreamCanceledError
			if errors.As(event.Err, &cancelErr) || errors.As(event.Err, &streamCanceled) {
				emit(&TranscriptEvent{
					Kind:         TranscriptEventCancelled,
					OccurredAt:   now,
					TurnID:       turnID,
					CancelReason: "execution cancelled",
					Usage:        usageOf(usage),
				})
				drainToEOF(iter)
				settle(hookRunStatusCancelled)
				return
			}
			if errors.Is(event.Err, context.Canceled) {
				emit(&TranscriptEvent{
					Kind:         TranscriptEventCancelled,
					OccurredAt:   now,
					TurnID:       turnID,
					CancelReason: "execution cancelled",
					Usage:        usageOf(usage),
				})
				drainToEOF(iter)
				settle(hookRunStatusCancelled)
				return
			}
			lastErr = event.Err
			break
		}

		// Handle interrupts: a dangerous shell command paused the turn for
		// human approval. Emit the approval event and end the stream without
		// a terminal event — Resume continues the turn from the checkpoint.
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted := event.Action.Interrupted
			approval := &ApprovalPayload{InterruptID: interruptIDOf(interrupted)}
			approval.Command = shellCommandOf(interrupted)
			emit(&TranscriptEvent{
				Kind:       TranscriptEventApprovalRequired,
				OccurredAt: now,
				TurnID:     turnID,
				Approval:   approval,
			})
			// Keep draining the ADK iterator: the interrupt checkpoint is
			// persisted when the underlying turn finalizes, and abandoning the
			// iterator here would skip that save.
			drainToEOF(iter)
			return
		}

		// Handle output events.
		if event.Output != nil && event.Output.MessageOutput != nil {
			mo := event.Output.MessageOutput

			if mo.IsStreaming && mo.MessageStream != nil {
				// Drain stream as text deltas. Usage rides the frames of the
				// current model call; only the last frame carrying usage counts
				// (providers report it once, on the final frame).
				var frameUsage *schema.TokenUsage
				for {
					frame, err := mo.MessageStream.Recv()
					if errors.Is(err, io.EOF) {
						if frameUsage != nil {
							accumulateTokenUsage(&usage, frameUsage)
						}
						break
					}
					if err != nil {
						var streamCanceled *adk.StreamCanceledError
						if errors.As(err, &streamCanceled) {
							// Escalated immediate abort tore down the model
							// stream mid-turn; the durable cancel marker is
							// persisted by the ADK machine.
							emit(&TranscriptEvent{
								Kind:         TranscriptEventCancelled,
								OccurredAt:   time.Now().UTC(),
								TurnID:       turnID,
								CancelReason: "execution cancelled",
								Usage:        usageOf(usage),
							})
							drainToEOF(iter)
							settle(hookRunStatusCancelled)
							return
						}
						if errors.Is(err, context.Canceled) {
							// Send incomplete marker and cancel event.
							if partialText != "" {
								emit(&TranscriptEvent{
									Kind:       TranscriptEventMessageCompleted,
									OccurredAt: time.Now().UTC(),
									TurnID:     turnID,
									Message: &CompletedMessage{
										Role:    "assistant",
										Content: partialText,
									},
								})
							}
							emit(&TranscriptEvent{
								Kind:         TranscriptEventCancelled,
								OccurredAt:   time.Now().UTC(),
								TurnID:       turnID,
								CancelReason: "stream interrupted",
								Usage:        usageOf(usage),
							})
							drainToEOF(iter)
							settle(hookRunStatusCancelled)
							return
						}
						lastErr = err
						break
					}
					// Extract text content from the agentic frame.
					if frame != nil && frame.ResponseMeta != nil && frame.ResponseMeta.TokenUsage != nil {
						frameUsage = frame.ResponseMeta.TokenUsage
					}
					delta := extractAgenticText(frame)
					if delta != "" {
						partialText += delta
						emit(&TranscriptEvent{
							Kind:       TranscriptEventTextDelta,
							OccurredAt: time.Now().UTC(),
							TurnID:     turnID,
							TextDelta:  delta,
						})
					}
					// Eino consolidates reasoning across stream frames; each
					// frame's reasoning chunk is a delta, never persisted.
					reasoning := agenticReasoningText(frame)
					if reasoning != "" {
						partialReasoning += reasoning
						emit(&TranscriptEvent{
							Kind:           TranscriptEventReasoningDelta,
							OccurredAt:     time.Now().UTC(),
							TurnID:         turnID,
							ReasoningDelta: reasoning,
						})
					}
					for _, call := range agenticToolCalls(frame) {
						emitToolStarted(call.CallID, call.Name, call.Arguments)
					}
				}
				if lastErr != nil {
					break
				}
				// Emit completed message.
				if partialText != "" {
					emit(&TranscriptEvent{
						Kind:       TranscriptEventMessageCompleted,
						OccurredAt: time.Now().UTC(),
						TurnID:     turnID,
						Message: &CompletedMessage{
							Role:             "assistant",
							Content:          partialText,
							ReasoningContent: partialReasoning,
						},
					})
					partialText = ""
					partialReasoning = ""
				}
			} else if mo.Message != nil {
				// Non-streaming message (tool result or complete message).
				content := extractAgenticText(mo.Message)
				role := "assistant"
				if mo.AgenticRole == schema.AgenticRoleTypeUser {
					role = "user"
				} else if string(mo.AgenticRole) == "tool" {
					role = "tool"
				}
				if role == "assistant" && mo.Message.ResponseMeta != nil && mo.Message.ResponseMeta.TokenUsage != nil {
					accumulateTokenUsage(&usage, mo.Message.ResponseMeta.TokenUsage)
				}
				for _, call := range agenticToolCalls(mo.Message) {
					emitToolStarted(call.CallID, call.Name, call.Arguments)
				}
				for _, res := range agenticToolResults(mo.Message) {
					emitToolFinished(res.CallID, res.Name, res.Result)
				}
				// Non-streaming assistant messages deliver their reasoning in
				// one chunk with the completed message (design D2).
				reasoning := ""
				if role == "assistant" {
					reasoning = agenticReasoningText(mo.Message)
					if reasoning != "" {
						emit(&TranscriptEvent{
							Kind:           TranscriptEventReasoningDelta,
							OccurredAt:     now,
							TurnID:         turnID,
							ReasoningDelta: reasoning,
						})
					}
				}
				if content != "" {
					emit(&TranscriptEvent{
						Kind:       TranscriptEventMessageCompleted,
						OccurredAt: now,
						TurnID:     turnID,
						Message: &CompletedMessage{
							Role:             role,
							Content:          content,
							ReasoningContent: reasoning,
						},
					})
				}
			}
		}

		// Handle session events for tool call lifecycle.
		if event.SessionEventVariant != nil && event.SessionEventVariant.Event != nil {
			se := event.SessionEventVariant.Event
			switch se.Kind {
			case adk.SessionEventSpanModelRequestEnd:
				// Provider-reported usage rides on the model span end; accumulate
				// it across the turn and stamp the terminal event.
				if se.Span != nil && se.Span.Model != nil && se.Span.Model.Usage != nil {
					mu := se.Span.Model.Usage
					usage.InputTokens += mu.InputTokens
					usage.OutputTokens += mu.OutputTokens
					if mu.Raw != nil {
						usage.TotalTokens += mu.Raw.TotalTokens
					} else {
						usage.TotalTokens += mu.InputTokens + mu.OutputTokens
					}
					// The last model span's input is what the context last held.
					usage.FinalInputTokens = mu.InputTokens
				}
			case adk.SessionEventSpanToolCallStart:
				if se.Span != nil && se.Span.Tool != nil {
					recordToolStart(se.Span.Tool.ToolUseID, now)
					emit(&TranscriptEvent{
						Kind:       TranscriptEventToolCallStarted,
						OccurredAt: now,
						TurnID:     turnID,
						ToolCall: &ToolCallPayload{
							CallID: se.Span.Tool.ToolUseID,
							Name:   se.Span.Tool.Name,
						},
					})
				}
			case adk.SessionEventSpanToolCallEnd:
				if se.Span != nil && se.Span.Tool != nil {
					emit(&TranscriptEvent{
						Kind:       TranscriptEventToolCallFinished,
						OccurredAt: now,
						TurnID:     turnID,
						ToolResult: &ToolResultPayload{
							CallID:  se.Span.Tool.ToolUseID,
							Name:    se.Span.Tool.Name,
							Latency: toolLatency(se.Span.Tool.ToolUseID),
						},
					})
				}
			case adk.SessionEventMessagesReplaced:
				emit(&TranscriptEvent{
					Kind:       TranscriptEventContextCompacted,
					OccurredAt: now,
					TurnID:     turnID,
					Compaction: &CompactionPayload{},
				})
			}
		}
	}

	// Terminal events.
	if lastErr != nil {
		emit(&TranscriptEvent{
			Kind:       TranscriptEventError,
			OccurredAt: time.Now().UTC(),
			TurnID:     turnID,
			Error:      lastErr.Error(),
			Usage:      usageOf(usage),
		})
		settle(hookRunStatusFailed)
		return
	}

	emit(&TranscriptEvent{
		Kind:       TranscriptEventTurnCompleted,
		OccurredAt: time.Now().UTC(),
		TurnID:     turnID,
		Usage:      usageOf(usage),
	})
	settle(hookRunStatusCompleted)
}

// drainToEOF consumes and discards the remaining ADK events so the run's
// asynchronous finalization — interrupt/cancel checkpoint save, the durable
// cancel marker, and the session status rows — lands before the run goroutine
// deregisters. Abandoning the iterator at a terminal event leaves that
// finalization racing history readers: CancelRun could report success while a
// subsequent History call still missed the cancel marker.
func drainToEOF(iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
	for {
		if _, ok := iter.Next(); !ok {
			return
		}
	}
}

// usageOf returns a pointer to the accumulated usage, or nil when the
// provider reported no usage at all.
func usageOf(u UsagePayload) *UsagePayload {
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.TotalTokens == 0 && u.FinalInputTokens == 0 {
		return nil
	}
	return &u
}

// accumulateTokenUsage adds one provider-reported usage block to the
// per-turn accumulator. FinalInputTokens is set, never summed: the
// chronologically last model call's input must win.
func accumulateTokenUsage(acc *UsagePayload, u *schema.TokenUsage) {
	if u == nil {
		return
	}
	acc.InputTokens += u.PromptTokens
	acc.OutputTokens += u.CompletionTokens
	acc.TotalTokens += u.TotalTokens
	acc.FinalInputTokens = u.PromptTokens
}

// teardownBrowserSession closes any browser session the execution opened. It
// is a no-op when the tool registry holds no per-session resources.
func (r *Runner) teardownBrowserSession(req ExecRequest) {
	if td, ok := r.toolRegistry.(SessionTeardown); ok {
		td.CloseSession(req.SessionID)
	}
}

// interruptIDOf returns the root-cause interrupt ID of an InterruptInfo.
func interruptIDOf(info *adk.InterruptInfo) string {
	if info == nil {
		return ""
	}
	for _, ic := range info.InterruptContexts {
		if ic != nil && ic.IsRootCause && ic.ID != "" {
			return ic.ID
		}
	}
	for _, ic := range info.InterruptContexts {
		if ic != nil && ic.ID != "" {
			return ic.ID
		}
	}
	return ""
}

// shellCommandOf extracts the pending shell command from an interrupt's
// user-facing payloads, checking the top-level data and then each context.
func shellCommandOf(info *adk.InterruptInfo) string {
	if info == nil {
		return ""
	}
	if approval, ok := info.Data.(backend.ShellApprovalInfo); ok {
		return approval.Command
	}
	for _, ic := range info.InterruptContexts {
		if ic == nil {
			continue
		}
		if approval, ok := ic.Info.(backend.ShellApprovalInfo); ok {
			return approval.Command
		}
	}
	return ""
}

// ResumeCheckpointID derives the deterministic runner checkpoint ID for a
// session. It mirrors adk's sessionRunnerCheckpointID: interrupts checkpoint
// under one well-known key per runner session.
func ResumeCheckpointID(sessionID string) string {
	return "session/" + sessionID + "/runner_checkpoint"
}

// Resume continues a turn paused by a shell-approval interrupt. It rebuilds
// the agent deterministically from stored config (same load/resolve/compose as
// Run) and resumes the persisted checkpoint, targeting the interrupt with the
// approval decision. Approved executes the command; denied feeds a denial
// notice back to the agent as the tool result.
func (r *Runner) Resume(ctx context.Context, req ExecRequest, approval *ApprovalPayload, approved bool) (*EventStream, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if approval == nil || approval.InterruptID == "" {
		return nil, fmt.Errorf("%w: approval with interrupt_id is required", domain.ErrInvalid)
	}

	// Record the decision durably before resuming: interrupt IDs are
	// regenerated when a checkpoint is reconstructed in a new process, so the
	// shell consults this ledger as the cross-restart decision path.
	if err := recordApprovalDecision(ctx, r.checkpoints, approval.Command, approved); err != nil {
		return nil, fmt.Errorf("agent.Resume: record decision: %w", err)
	}

	ws, domainAgent, user, role, err := r.load(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent.Resume: %w", err)
	}

	cfg, resolvedTools, err := r.resolve(ctx, req, ws, domainAgent)
	if err != nil {
		return nil, fmt.Errorf("agent.Resume: %w", err)
	}

	// D4 call-ID dedup across the approval pause: reuse the interrupted
	// run's hook chain so the approved re-execution returns the decision
	// recorded before the interrupt instead of re-firing pre_tool_use. A
	// fresh resume (no remembered chain — restart or nothing interrupted)
	// resolves a new per-run chain.
	hookBase := hookBaseEvent(normalizeOrigin(req.Origin), ws, domainAgent, user, req.SessionID)
	hookChain := r.reuseHookChain(runKeyOf(req))
	if hookChain == nil {
		hookChain, err = r.resolveHooksChain(ctx, ws, domainAgent)
		if err != nil {
			return nil, fmt.Errorf("agent.Resume: %w", err)
		}
	}
	if hookChain.HasHooks() {
		cfg.Hooks = hookChain
		cfg.HooksBase = &hookBase
		r.rememberHookChain(runKeyOf(req), hookChain)
	}

	adkAgent, err := r.composeAgent(ctx, &cfg, ws, user, role, resolvedTools, domainAgent)
	if err != nil {
		return nil, fmt.Errorf("agent.Resume: %w", err)
	}

	sessionAdapter := NewADKSessionAdapter(
		r.sessionEvents,
		r.checkpoints,
		req.WorkspaceID,
	)

	// The caller's ctx covered the decision ledger, validation, loading, and
	// resolution only; the resumed turn registers with the manager, which
	// derives its context from base — the approval response never cancels it.
	// The per-run ADK cancel option arms the safe-point cancel machine, same
	// as a fresh run.
	cancelOpt, agentCancel := adk.WithCancel()
	handle, err := r.runMgr.start(RunKey{
		WorkspaceID: req.WorkspaceID,
		AgentID:     req.AgentID,
		SessionID:   req.SessionID,
	}, agentCancel)
	if err != nil {
		return nil, fmt.Errorf("agent.Resume: %w", err)
	}

	adkRunner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           adkAgent,
		EnableStreaming: true,
		CheckPointStore: sessionAdapter,
		SessionID:       req.SessionID,
		SessionStore:    sessionAdapter,
	})

	stream := NewEventStream(128)
	go r.streamResume(handle, cancelOpt, adkRunner, stream, req, approval, approved, hookChain, hookBase)
	return stream, nil
}

// streamResume drives the resumed ADK runner and maps events like streamRun,
// resuming the persisted checkpoint with the approval decision as the resume
// target data.
func (r *Runner) streamResume(
	handle *runHandle,
	cancelOpt adk.AgentRunOption,
	runner *adk.TypedRunner[*schema.AgenticMessage],
	stream *EventStream,
	req ExecRequest,
	approval *ApprovalPayload,
	approved bool,
	hookChain *hooks.Resolved,
	hookBase hooks.Event,
) {
	key := runKeyOf(req)
	defer handle.finish()
	// Same subscriber-close ordering as streamRun: ahead of finish() so the
	// manager entry is still registered and every live subscriber drains to
	// EOF, including the ResumeWithParams error short-circuit below.
	defer r.runMgr.CloseSubscribers(key)
	defer handle.cancel()
	defer func() { _ = stream.Close() }()
	defer r.logTapDrops(stream, req)
	r.teardownBrowserSession(req)

	iter, err := runner.ResumeWithParams(handle.ctx, ResumeCheckpointID(req.SessionID), &adk.ResumeParams{
		Targets: map[string]any{approval.InterruptID: approved},
	}, cancelOpt)
	if err != nil {
		stream.Send(&TranscriptEvent{
			Kind:       TranscriptEventError,
			OccurredAt: time.Now().UTC(),
			Error:      err.Error(),
		})
		// Terminal failure before any drain: run_finished still observes the
		// outcome (D1: every terminal outcome fires, status failed).
		r.forgetHookChain(key)
		if hookChain.HasHooks() {
			hookChain.RunFinished(handle.ctx, hookBase, hookRunStatusFailed)
		}
		return
	}
	r.drainAgentEvents(handle.ctx, iter, stream, key, "", "", hookChain, hookBase)
}

// ComposeParams contains the data necessary to compose the execution instruction.
// Memories is always provided by the runner (design.md D7): the composer reads
// the workspace's shared memory and the calling user's own memory per
// execution so the previous turn's appends are visible this turn.
type ComposeParams struct {
	AgentDir  string
	Workspace *domain.Workspace
	User      *domain.User
	RoleName  string
	Memories  store.MemoryStore
}

// InstructionComposer defines the interface for composing agent execution instructions.
type InstructionComposer interface {
	Compose(ctx context.Context, params ComposeParams) (string, error)
}

// DefaultInstructionComposer composes the 6 documents in fixed order.
type DefaultInstructionComposer struct{}

// NewInstructionComposer returns a new DefaultInstructionComposer.
func NewInstructionComposer() *DefaultInstructionComposer {
	return &DefaultInstructionComposer{}
}

// Compose constructs the system instruction from disk prompt files and virtual documents.
func (c *DefaultInstructionComposer) Compose(ctx context.Context, params ComposeParams) (string, error) {
	var docs []string

	// 1. AGENTS.md
	if content := readPromptFile(params.AgentDir, "AGENTS.md"); content != "" {
		docs = append(docs, content)
	}

	// 2. IDENTITY.md
	if content := readPromptFile(params.AgentDir, "IDENTITY.md"); content != "" {
		docs = append(docs, content)
	}

	// 3. SOUL.md
	if content := readPromptFile(params.AgentDir, "SOUL.md"); content != "" {
		docs = append(docs, content)
	}

	// 4. WORKSPACE.md (virtual) — metadata plus the workspace's shared memory
	// subsection, reloaded every execution so last turn's appends are visible
	// this turn (design.md D7).
	var sharedMemory string
	if params.Workspace != nil {
		mem, err := params.Memories.WorkspaceMemory(ctx, params.Workspace.ID)
		if err != nil {
			return "", fmt.Errorf("load workspace memory: %w", err)
		}
		if mem != nil {
			sharedMemory = strings.TrimSpace(mem.Content)
		}
	}
	if wsDoc := renderWorkspaceDoc(params.Workspace, sharedMemory); wsDoc != "" {
		docs = append(docs, wsDoc)
	}

	// 5. USER.md (virtual) — metadata plus the calling user's own memory.
	var userMemory string
	if params.User != nil && params.Workspace != nil {
		mem, err := params.Memories.UserMemory(ctx, params.Workspace.ID, params.User.ID)
		if err != nil {
			return "", fmt.Errorf("load user memory: %w", err)
		}
		if mem != nil {
			userMemory = strings.TrimSpace(mem.Content)
		}
	}
	if userDoc := renderUserDoc(params.User, params.RoleName, userMemory); userDoc != "" {
		docs = append(docs, userDoc)
	}

	// 6. BOOTSTRAP.md
	if content := readPromptFile(params.AgentDir, "BOOTSTRAP.md"); content != "" {
		docs = append(docs, content)
	}

	return strings.Join(docs, "\n\n"), nil
}

func readPromptFile(agentDir, filename string) string {
	if agentDir == "" {
		return ""
	}
	path := filepath.Join(agentDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func renderWorkspaceDoc(ws *domain.Workspace, sharedMemory string) string {
	if ws == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Workspace\n")
	if ws.Name != "" {
		sb.WriteString(fmt.Sprintf("- **Name**: %s\n", ws.Name))
	}
	if ws.Description != "" {
		sb.WriteString(fmt.Sprintf("- **Description**: %s\n", ws.Description))
	}
	if sharedMemory != "" {
		sb.WriteString("\n## Shared memory\n")
		sb.WriteString(sharedMemory)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func renderUserDoc(u *domain.User, roleName, memory string) string {
	if u == nil && roleName == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Current User\n")
	if u != nil {
		if u.Name != "" {
			sb.WriteString(fmt.Sprintf("- **Name**: %s\n", u.Name))
		}
		if u.Email != "" {
			sb.WriteString(fmt.Sprintf("- **Email**: %s\n", u.Email))
		}
	}
	if roleName != "" {
		sb.WriteString(fmt.Sprintf("- **Role**: %s\n", roleName))
	}
	if memory != "" {
		sb.WriteString("\n## Memory\n")
		sb.WriteString(memory)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}
