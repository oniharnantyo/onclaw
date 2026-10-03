package agents

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/authz"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The connection tool gate (add-integration-authority tasks.md 2.1–2.4,
// design.md D1/D2/D3/D6): transport-agnostic user-authority gating for
// connection-sourced tools. Both seams annotate the tools they yield with
// their originating connection's identity (task 2.1) — the HTTP request-tool
// source knows its connection directly, the MCP pass recovers it from the
// origin marker the connections change birth-stamped on materialized servers
// — and the gate consults at resolution (write-tier tools are pruned for
// users without integrations.write, D6) and again at invocation (the CURRENT
// permission set, so a mid-run role change is honored, D6). Read tier rides
// workspace membership; write tier requires domain.IntegrationsWrite (D2).
//
// Deny is the canonical hooks-style block result naming the required
// permission (D3): the tool outcome the model reads and adapts to — soft,
// visible in the transcript, never a run failure. Service-authority runs
// (webhook events, no requesting user) gate as read tier; a write-tier call
// pauses the turn through the same tool.Interrupt machinery the shell
// approval flow uses, rendered as an approval card in the bound thread (D4).

// ConnectionOriginLookup is the runner-owned origin-link seam for the MCP
// pass (task 2.1): a connection-materialized workspace MCP server row
// carries only the connection's id (domain.WorkspaceMCPServer.
// OriginConnectionID); the recipe id — and through it the tier declarations —
// lives on the connection row. The connections service satisfies this
// structurally (the ConnectionCredentials precedent); the composition root
// injects it. A nil lookup (unit constructions) degrades fail-safe: the
// affected tools annotate with no recipe and gate as write.
type ConnectionOriginLookup interface {
	// ConnectionServiceOf returns the connection's recipe id (the
	// domain.Connection.Service value).
	ConnectionServiceOf(ctx context.Context, workspaceID, connectionID string) (string, error)
}

// gateHookName is the blocking-policy name the gate's canonical block
// results carry (agenthooks.BlockToolResult's hook field): the gate is the
// policy that blocked the call, and the reason names the required permission.
const gateHookName = "integration_authority"

// GateBlockResult renders the canonical block outcome for an out-of-tier
// connection tool call (design.md D3): the hooks block shape, reused
// verbatim, with the reason naming the required permission. Exported for the
// test suites that pin the wire shape.
func GateBlockResult(toolName string) string {
	return agenthooks.BlockToolResult(gateHookName,
		"tool "+toolName+" requires the "+domain.IntegrationsWrite+" permission")
}

// connectionToolOrigin is the gate's per-tool annotation (task 2.1): the
// originating connection's identity plus the recipe the tier lookup reads.
// Minimal metadata keyed by the applied tool name — no parallel tool types.
type connectionToolOrigin struct {
	// ConnectionID is the workspace connection row the tool came from.
	ConnectionID string
	// Service is the recipe id (domain.Connection.Service).
	Service string
	// ServiceName is the recipe's display name (domain.Recipe.Service).
	ServiceName string
	// Recipe is the registered recipe behind the connection; nil (unregistered
	// or unresolvable) makes every tool of the connection gate as write —
	// EffectiveToolTier's fail-safe default.
	Recipe *domain.Recipe
}

// tier resolves the tool's tier from its recipe (the frozen stage-A lookup).
// toolName is the applied name the gate sees at resolution or invocation.
func (o connectionToolOrigin) tier(toolName string) string {
	return domain.EffectiveToolTier(o.Recipe, toolName)
}

// approvalOrigin projects the annotation onto the approval payload the
// escalation interrupt carries (task 2.4).
func (o connectionToolOrigin) approvalOrigin(toolName string) *ApprovalToolPayload {
	return &ApprovalToolPayload{
		Name:         toolName,
		Service:      o.Service,
		ServiceName:  o.ServiceName,
		ConnectionID: o.ConnectionID,
		Tier:         o.tier(toolName),
	}
}

// ToolApprovalInfo is the user-facing payload carried by a service-run write
// escalation interrupt (task 2.4). It travels with the interrupt checkpoint
// exactly like backend.ShellApprovalInfo so the approval card and the resume
// path both know what tool awaits a decision; registered with the Eino
// serializer because interrupt payloads are persisted.
type ToolApprovalInfo struct {
	// Name is the applied tool name ("<recipe>.<verb>" or "mcp__<server>__<tool>").
	Name string `json:"name"`
	// Service is the recipe id.
	Service string `json:"service"`
	// ServiceName is the recipe's display name, when registered.
	ServiceName string `json:"service_name,omitempty"`
	// ConnectionID is the owning workspace connection.
	ConnectionID string `json:"connection_id,omitempty"`
	// Tier is the tool's effective tier ("write" — only write-tier calls
	// escalate).
	Tier string `json:"tier"`
}

func init() { schema.Register[ToolApprovalInfo]() }

// toolApprovalOf extracts a tool escalation payload from an interrupt's
// user-facing contexts (the shellCommandOf rule: top-level data first, then
// each context).
func toolApprovalOf(info *adk.InterruptInfo) *ApprovalToolPayload {
	if info == nil {
		return nil
	}
	if approval, ok := info.Data.(ToolApprovalInfo); ok {
		return approvalToPayload(approval)
	}
	for _, ic := range info.InterruptContexts {
		if ic == nil {
			continue
		}
		if approval, ok := ic.Info.(ToolApprovalInfo); ok {
			return approvalToPayload(approval)
		}
	}
	return nil
}

func approvalToPayload(info ToolApprovalInfo) *ApprovalToolPayload {
	return &ApprovalToolPayload{
		Name:         info.Name,
		Service:      info.Service,
		ServiceName:  info.ServiceName,
		ConnectionID: info.ConnectionID,
		Tier:         info.Tier,
	}
}

// ConnectionGateAuthority re-checks the acting user's integrations.write
// permission at invocation time (task 2.3, design.md D6): the role may have
// changed since resolution, so the check reads the CURRENT member + role
// rows on every call. The runner builds one per run over its own stores;
// any read failure fails closed (the gate must not silently disappear).
type ConnectionGateAuthority interface {
	IntegrationsWriteAllowed(ctx context.Context) bool
}

// connectionGateAuthority is the runner's authority implementation: granular
// stores plus the run's coordinates (AGENTS.md: positional, granular deps).
// The authorizer evaluates integrations.write through the rules engine
// (fix-role-permission-audit 1.5); nil — unit constructions only, the
// ConnectionOriginLookup nil precedent — degrades to the resolved role row's
// own permission set. Production always injects it (WithAuthorizer).
type connectionGateAuthority struct {
	authz       authz.Authorizer
	members     memberLookup
	roles       roleLookup
	workspaceID string
	userID      string
}

// memberLookup and roleLookup are the store slices the authority reads; the
// runner's store.MemberStore / store.RoleStore satisfy them structurally
// (narrow interfaces keep the gate testable without the full store).
type memberLookup interface {
	Get(ctx context.Context, workspaceID, userID string) (*domain.Member, error)
}

type roleLookup interface {
	ByID(ctx context.Context, roleID string) (*domain.Role, error)
}

// IntegrationsWriteAllowed implements ConnectionGateAuthority. The CURRENT
// member + role rows decide; the permission itself evaluates through the
// authorizer engine when the runner carries one (production wiring), degrading
// to the role row's own permission set only for unit constructions. Any read
// failure fails closed (the gate must not silently disappear).
func (a connectionGateAuthority) IntegrationsWriteAllowed(ctx context.Context) bool {
	member, err := a.members.Get(ctx, a.workspaceID, a.userID)
	if err != nil || member == nil {
		return false
	}
	role, err := a.roles.ByID(ctx, member.RoleID)
	if err != nil || role == nil {
		return false
	}
	if a.authz != nil {
		allowed, err := a.authz.Enforce(ctx, role.ID, a.workspaceID, domain.IntegrationsWrite)
		if err != nil {
			return false
		}
		return allowed
	}
	return domain.HasPermission(role.Permissions, domain.IntegrationsWrite)
}

// integrationsWriteAllowed resolves the role's integrations.write for the
// resolution-time pruning decision: through the runner's authorizer when
// wired (production — the engine is the single evaluation point,
// fix-role-permission-audit 1.5), degrading to the role row's own permission
// set for unit constructions without the option (the ConnectionOriginLookup
// nil precedent). A nil role or an evaluation failure fails closed.
func (r *Runner) integrationsWriteAllowed(ctx context.Context, workspaceID string, role *domain.Role) bool {
	if role == nil {
		return false
	}
	if r.authorizer != nil {
		allowed, err := r.authorizer.Enforce(ctx, role.ID, workspaceID, domain.IntegrationsWrite)
		if err != nil {
			return false
		}
		return allowed
	}
	return domain.HasPermission(role.Permissions, domain.IntegrationsWrite)
}

// connectionGateConfig is one run's gate: the resolved connection tools'
// origin annotations keyed by applied tool name, the origins pruned at
// resolution (members' write tier — the direct-attempt seam), the run's
// authority, and whether the run executes under service authority (task 2.4).
type connectionGateConfig struct {
	origins map[string]connectionToolOrigin
	// prunedOrigins holds the write-tier tools resolution pruned for the
	// requesting member (task 2.2): they are off the model's surface, but a
	// direct attempt at a pruned name must still produce the canonical block
	// — never the engine's raw not-found run failure (D3).
	prunedOrigins map[string]connectionToolOrigin
	serviceRun    bool
	authority     ConnectionGateAuthority
}

// active reports whether any connection tool is annotated — a run with no
// attached connection tools composes no gate middleware at all.
func (g *connectionGateConfig) active() bool { return g != nil && len(g.origins) > 0 }

// buildConnectionGate assembles the run's gate from the two seams' origin
// annotations (add-integration-authority tasks 2.1/2.2): the merged map keyed
// by applied tool name, the invocation-time authority over the run's acting
// user, and the service-run flag. On a user run whose requesting member's
// CURRENT role (as resolved) holds no integrations.write, write-tier entries
// are dropped from the gate and their tool names returned for surface pruning
// (D6); service-authority runs keep every tier reachable — the write tier
// escalates through the approval flow instead (task 2.4). Runs with no
// connection tools return an inactive gate: no pruning, no middleware,
// byte-identical behavior.
func (r *Runner) buildConnectionGate(ctx context.Context, req ExecRequest, role *domain.Role, originMaps ...map[string]connectionToolOrigin) (*connectionGateConfig, map[string]struct{}) {
	total := 0
	for _, m := range originMaps {
		total += len(m)
	}
	if total == 0 {
		return &connectionGateConfig{}, nil
	}

	origins := make(map[string]connectionToolOrigin, total)
	for _, m := range originMaps {
		for name, origin := range m {
			origins[name] = origin
		}
	}

	gate := &connectionGateConfig{
		origins:    origins,
		serviceRun: normalizeOrigin(req.Origin) == OriginService,
		authority: connectionGateAuthority{
			authz:       r.authorizer,
			members:     r.members,
			roles:       r.roles,
			workspaceID: req.WorkspaceID,
			userID:      req.UserID,
		},
	}
	if gate.serviceRun {
		return gate, nil
	}

	// Resolution-time pruning (task 2.2, design.md D6): a member without
	// integrations.write resolves only read-tier connection tools. A nil role
	// (impossible from load()) prunes everything gate-scoped — fail-safe. The
	// pruned names stay known to the gate so a direct attempt at one returns
	// the canonical block instead of the engine's raw not-found failure.
	var pruned map[string]struct{}
	if !r.integrationsWriteAllowed(ctx, req.WorkspaceID, role) {
		pruned = make(map[string]struct{})
		for name, origin := range origins {
			if origin.tier(name) != domain.RecipeToolTierRead {
				pruned[name] = struct{}{}
				delete(origins, name)
			}
		}
		if len(pruned) == 0 {
			pruned = nil
		} else {
			gate.prunedOrigins = make(map[string]connectionToolOrigin, len(pruned))
			for name := range pruned {
				// Re-derive the pruned origins from the seam maps (the merged
				// map no longer carries them).
				for _, m := range originMaps {
					if o, ok := m[name]; ok {
						gate.prunedOrigins[name] = o
						break
					}
				}
			}
		}
	}
	return gate, pruned
}

// unknownToolHandler serves the ToolsNode's unknown-tool seam for runs whose
// resolution pruned write-tier connection tools: a direct attempt at a pruned
// name returns the canonical block outcome (D3 — the denial teaches, the run
// continues), while any other unknown name keeps the engine's baseline
// not-found error. The CURRENT authority decides, so a user promoted mid-run
// gets the plain unavailability error instead — the tool is genuinely not
// routable on this run's surface. Nil unless resolution pruned something, so
// every other run composes byte-identically.
func (g *connectionGateConfig) unknownToolHandler() func(context.Context, string, string) (string, error) {
	if g == nil || len(g.prunedOrigins) == 0 {
		return nil
	}
	return func(ctx context.Context, name, _ string) (string, error) {
		if _, pruned := g.prunedOrigins[name]; pruned {
			if g.authority != nil && g.authority.IntegrationsWriteAllowed(ctx) {
				return "", fmt.Errorf("tool %s is not available on this session's tool surface", name)
			}
			return GateBlockResult(name), nil
		}
		return "", fmt.Errorf("tool %s not found in toolsNode indexes", name)
	}
}

// pruneResolvedNames drops the pruned write-tier tools from the resolved
// surface (task 2.2): tools are identified by their applied Info name, the
// same key the gate annotates. A tool whose Info fails stays — an
// unidentifiable tool is never speculatively removed.
func pruneResolvedNames(ctx context.Context, tools []tool.BaseTool, pruned map[string]struct{}) []tool.BaseTool {
	if len(pruned) == 0 {
		return tools
	}
	filtered := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if info, err := t.Info(ctx); err == nil && info != nil && info.Name != "" {
			if _, drop := pruned[info.Name]; drop {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// evaluate consults the gate for one tool call. Returns blocked=true with
// the canonical block JSON (the tool result the model reads), or gateErr
// non-nil carrying the escalation interrupt signal (task 2.4). A false/nil
// return passes the call through to the wrapped endpoint.
func (g *connectionGateConfig) evaluate(ctx context.Context, toolName string) (blocked bool, resultJSON string, gateErr error) {
	if !g.active() {
		return false, "", nil
	}
	origin, ok := g.origins[toolName]
	if !ok {
		return false, "", nil // built-in or private MCP tool: not the gate's scope
	}
	if origin.tier(toolName) != domain.RecipeToolTierWrite {
		return false, "", nil // read tier rides membership (D2)
	}

	// Resume-target decisions are mode-independent (defense in depth): a
	// tool-approval interrupt only ever belongs to a service-run escalation,
	// so the human's denial blocks the write regardless of how the rebuilt
	// request classified the run. An approval passes under the service-run
	// gate; on a user-classified rebuild it falls through to the decider's
	// own authority below (which the approvals endpoint already requires for
	// tool approvals).
	isTarget, hasData, approved := tool.GetResumeContext[bool](ctx)
	if isTarget {
		if !hasData || !approved {
			return true, GateBlockResult(toolName), nil
		}
		if g.serviceRun {
			return false, "", nil
		}
	} else if g.serviceRun {
		// A fresh write-tier call on a service-authority run escalates
		// through the shell-approval machinery (task 2.4): the turn pauses,
		// and the decision arrives as the resume target data.
		return false, "", tool.Interrupt(ctx, ToolApprovalInfo{
			Name:         toolName,
			Service:      origin.Service,
			ServiceName:  origin.ServiceName,
			ConnectionID: origin.ConnectionID,
			Tier:         origin.tier(toolName),
		})
	}

	// User runs: the CURRENT permission set decides (task 2.3) — resolution
	// pruning is budget clarity, this is the wall. Read failure fails closed.
	if g.authority == nil || !g.authority.IntegrationsWriteAllowed(ctx) {
		return true, GateBlockResult(toolName), nil
	}
	return false, "", nil
}

// connectionGateMiddleware gates tool calls through the per-run connection
// gate (tasks 2.2–2.4). BOTH endpoint flavors are wrapped — the known eino
// gotcha the hooks middleware documents: the ToolsNode executes through the
// streamable chain, so wrapping only the invokable chain never sees the call.
//
// Ordering: buildMiddlewares appends this middleware AFTER the hooks
// middleware and BEFORE the tool-error-result middleware. The wrappers
// outside it pass interrupt signals through untouched (the approval flow
// depends on them reaching the ADK) and forward the block result as a
// successful tool output; a gated call the gate denies never reaches the
// hooks chain or the tool.
type connectionGateMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	gate *connectionGateConfig
}

func newConnectionGateMiddleware(gate *connectionGateConfig) adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &connectionGateMiddleware{gate: gate}
}

// WrapInvokableToolCall gates the synchronous chain.
func (m *connectionGateMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		blocked, resultJSON, gateErr := m.gate.evaluate(ctx, tCtx.Name)
		if gateErr != nil {
			return "", gateErr
		}
		if blocked {
			m.logDenial(ctx, tCtx.Name)
			return resultJSON, nil
		}
		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// WrapStreamableToolCall gates the streamable chain — the one the ToolsNode
// executes through.
func (m *connectionGateMiddleware) WrapStreamableToolCall(_ context.Context, endpoint adk.StreamableToolCallEndpoint, tCtx *adk.ToolContext) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		blocked, resultJSON, gateErr := m.gate.evaluate(ctx, tCtx.Name)
		if gateErr != nil {
			return nil, gateErr
		}
		if blocked {
			m.logDenial(ctx, tCtx.Name)
			return errorResultStream(resultJSON), nil
		}
		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// logDenial records an out-of-tier denial (D3: denies that teach — the
// decision is visible in the transcript; the log line makes it attributable
// in traces and ops). Never fails the run.
func (m *connectionGateMiddleware) logDenial(ctx context.Context, toolName string) {
	slog.InfoContext(ctx, "connection tool gate: out-of-tier call denied",
		"tool", toolName,
		"required_permission", domain.IntegrationsWrite)
}
