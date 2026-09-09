package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Dispatcher resolves hook definitions into a per-run execution plan and
// delivers lifecycle events to it (design.md D2, D7, D14). Resolution reads
// all three governance levels on every call — never cached at boot — so
// content changes converge immediately and rolling-deploy skew degrades into
// graceful skips (D7) instead of run failures.
type Dispatcher struct {
	hooks store.HookStore
	reg   *Registry
}

// DispatcherOption configures the dispatcher.
type DispatcherOption func(*Dispatcher)

// NewDispatcher builds the dispatcher over the hook store and handler
// registry. The registry carries the handler dependencies (encryption key,
// MCP invoker, evaluator factory); handler types whose dependencies are not
// wired stay ErrHandlerNotRegistered and are skipped gracefully (D7).
func NewDispatcher(hooks store.HookStore, reg *Registry, _ ...DispatcherOption) *Dispatcher {
	return &Dispatcher{hooks: hooks, reg: reg}
}

// NewNoopDispatcher returns a dispatcher that always resolves an empty chain:
// every delivery is a no-op. It is the runner's default when no dispatcher is
// wired; the composition root injects the real one.
func NewNoopDispatcher() *Dispatcher {
	return &Dispatcher{reg: NewRegistry()}
}

// Resolve reads the three governance levels fresh (D13: instance → workspace
// → agent tier order, list position preserved within a tier — D14) and
// compiles each hook's matcher and if condition once for the run. A hook this
// binary cannot interpret — unknown event, unknown handler type, uncompilable
// matcher, uninterpretable if condition — is skipped gracefully (D7):
// warning logged, hook status persisted as error via SetHookDeliveryStatus,
// and the hook kept in the tier list as a failed execution under its own
// on_failure policy so a safety gate with on_failure = block still fails
// closed. Disabled hooks resolve to nothing.
//
// Store-level read failures return an error; the caller owns the run's fate.
func (d *Dispatcher) Resolve(ctx context.Context, workspaceID, agentID string) (*Resolved, error) {
	r := &Resolved{
		d:            d,
		dedup:        make(map[string]preToolOutcome),
		promptCounts: make(map[string]int),
	}
	if d.hooks == nil {
		// The no-op dispatcher: empty chain, every delivery a no-op.
		return r, nil
	}

	instanceHooks, err := d.hooks.ListInstanceHooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: list instance hooks: %w", err)
	}
	workspaceHooks, err := d.hooks.ListWorkspaceHooks(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: list workspace hooks: %w", err)
	}
	agentHooks, err := d.hooks.ListAgentHooks(ctx, workspaceID, agentID)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: list agent hooks: %w", err)
	}

	for _, h := range instanceHooks {
		r.append(domain.HookLevelInstance, h.ID, h.HookBase)
	}
	for _, h := range workspaceHooks {
		r.append(domain.HookLevelWorkspace, h.ID, h.HookBase)
	}
	for _, h := range agentHooks {
		r.append(domain.HookLevelAgent, h.ID, h.HookBase)
	}
	return r, nil
}

// append adds one hook to the tier list, applying the D7 graceful-skip rule
// to definitions this binary cannot interpret.
func (r *Resolved) append(level domain.HookLevel, id string, def domain.HookBase) {
	if !def.Enabled {
		return
	}
	h := resolvedHook{level: level, id: id, def: def}

	if !isKnownHookEvent(def.Event) {
		h.skipErr = fmt.Sprintf("unknown event %q", def.Event)
		r.skipGracefully(level, id, def, h.skipErr)
		r.hooks = append(r.hooks, h)
		return
	}
	if !isKnownHandlerType(def.HandlerType) {
		h.skipErr = fmt.Sprintf("unknown handler type %q", def.HandlerType)
		r.skipGracefully(level, id, def, h.skipErr)
		r.hooks = append(r.hooks, h)
		return
	}
	matcher, err := CompileMatcher(def.Matcher)
	if err != nil {
		h.skipErr = fmt.Sprintf("uncompilable matcher: %v", err)
		r.skipGracefully(level, id, def, h.skipErr)
		r.hooks = append(r.hooks, h)
		return
	}
	h.matcher = matcher

	// D20: the if gate applies to tool events only — domain validation
	// rejects an if on any other event at save, so the runtime compiles the
	// gate (and can only ever fail to interpret it) for those events and
	// never evaluates it elsewhere.
	if def.If != "" && isToolHookEvent(def.Event) {
		gate, err := compileIfGate(def.If)
		if err != nil {
			h.skipErr = fmt.Sprintf("uninterpretable if condition: %v", err)
			r.skipGracefully(level, id, def, h.skipErr)
		} else {
			h.ifGate = gate
		}
	}

	// D22: script hooks compile ONCE per run, cached on the resolved hook;
	// every delivery in the run reuses the program. An uncompilable script
	// (a row written by an older binary — save validation compiles first)
	// is the D7 graceful skip. Skipped entirely when the handler's kill
	// switch is off: the gate in Registry.Execute reports that instead.
	if def.HandlerType == domain.HookHandlerScript && r.d.reg.scriptEnabled {
		prog, err := compileScriptConfig(def.Config)
		if err != nil {
			h.skipErr = fmt.Sprintf("uncompilable script: %v", err)
			r.skipGracefully(level, id, def, h.skipErr)
		} else {
			h.scriptProg = prog
		}
	}
	r.hooks = append(r.hooks, h)
}

// isToolHookEvent reports whether the event carries a tool call — the only
// events an if condition narrows (D20).
func isToolHookEvent(event domain.HookEvent) bool {
	return event == domain.HookEventPreToolUse || event == domain.HookEventPostToolUse
}

// ifGate is one hook's compiled D20 if condition `ToolName(pattern)`: the
// name part follows the matcher's tool-entry rules (exact or trailing-".*"
// family) and the pattern is an unanchored RE2 regex.
type ifGate struct {
	name    toolEntry
	pattern *regexp.Regexp
}

// allows reports whether a tool occurrence passes the gate: the tool name
// must match the entry and the pattern must hit the serialized tool-input
// JSON (the event's raw call-arguments JSON — the same serialization the
// event delivers to handlers and audit).
func (g *ifGate) allows(toolName, argsJSON string) bool {
	if !g.name.matches(toolName) {
		return false
	}
	return g.pattern.MatchString(argsJSON)
}

// compileIfGate compiles a stored if condition into its runtime gate. Every
// failure mode (malformed rule form, bad name grammar, over-long or
// uncompilable pattern) is a D7 graceful skip upstream — save validation
// (domain.ValidateHookIf) makes this unreachable for rows written by a
// current binary.
func compileIfGate(cond string) (*ifGate, error) {
	name, pattern, ok := domain.SplitHookIf(cond)
	if !ok {
		return nil, fmt.Errorf("%w: if: must be ToolName(pattern), got %q", domain.ErrInvalid, cond)
	}
	entry, err := compileToolEntry(name)
	if err != nil {
		return nil, fmt.Errorf("if: tool name: %w", err)
	}
	if pattern == "" {
		return nil, fmt.Errorf("%w: if: pattern is required", domain.ErrInvalid)
	}
	if len(pattern) > domain.MaxHookPatternLength {
		return nil, fmt.Errorf("%w: if: pattern exceeds %d characters", domain.ErrInvalid, domain.MaxHookPatternLength)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: if: pattern: %v", domain.ErrInvalid, err)
	}
	return &ifGate{name: entry, pattern: re}, nil
}

// skipGracefully is the D7 outcome for an uninterpretable hook: a warning and
// a best-effort status=error write so the broken definition is impossible to
// miss in the settings UI.
func (r *Resolved) skipGracefully(level domain.HookLevel, id string, def domain.HookBase, reason string) {
	slog.WarnContext(context.Background(), "hooks: skipping hook this binary cannot interpret",
		"hook_id", id,
		"hook_name", def.Name,
		"level", level,
		"handler_type", def.HandlerType,
		"reason", reason,
	)
	if err := r.d.hooks.SetHookDeliveryStatus(context.Background(), level, id, domain.HookStatusError, reason); err != nil {
		slog.WarnContext(context.Background(), "hooks: status update failed (best-effort)",
			"hook_id", id, "level", level, "error", err)
	}
}

func isKnownHookEvent(event domain.HookEvent) bool {
	for _, e := range domain.HookEvents {
		if e == event {
			return true
		}
	}
	return false
}

func isKnownHandlerType(handlerType domain.HookHandlerType) bool {
	for _, t := range domain.HookHandlerTypes {
		if t == handlerType {
			return true
		}
	}
	return false
}

// resolvedHook is one hook of a run's resolved chain.
type resolvedHook struct {
	level   domain.HookLevel
	id      string
	def     domain.HookBase
	matcher Matcher
	// ifGate is the compiled D20 input gate; nil when the hook has no if
	// condition (or its event is not a tool event, which never evaluates
	// one). Only set on hooks whose skipErr is empty.
	ifGate *ifGate
	// skipErr is non-empty when the definition is uninterpretable (D7): the
	// hook does not run, but its delivery is a failed execution under its own
	// on_failure policy.
	skipErr string
	// scriptProg is the script handler's compile-once-per-run cache (D22),
	// built in append for script hooks only.
	scriptProg *goja.Program
}

// key identifies the hook within per-run state (cap counters).
func (h *resolvedHook) key() string { return string(h.level) + "/" + h.id }

// preToolOutcome is the cached per-CallID chain outcome (D4): returned
// verbatim when an approved call re-executes through the middleware chain.
type preToolOutcome struct {
	blocked    bool
	resultJSON string
	hookName   string
	reason     string
}

// Resolved is one run's hook execution plan: the three tiers in fixed order,
// matchers compiled once, plus the per-run state (call-ID dedup cache, prompt
// invocation counters). Its methods are the runtime seams' only entry points.
type Resolved struct {
	d     *Dispatcher
	hooks []resolvedHook

	mu           sync.Mutex
	dedup        map[string]preToolOutcome
	promptCounts map[string]int
}

// HasHooks reports whether any hooks resolved for the run — the runtime
// seams' cheap pass-through check.
func (r *Resolved) HasHooks() bool { return len(r.hooks) > 0 }

// -------------------------------------------------------------------------
// Runtime seams (called by the runner's middleware chain)
// -------------------------------------------------------------------------

// EvaluatePromptSubmission runs user_prompt_submit hooks in order on the
// caller's context (blocking, never detached — run cancellation must kill
// them). First block wins; a per executed hook audit row is recorded. The
// returned reason is the blocking hook's reason.
func (r *Resolved) EvaluatePromptSubmission(ctx context.Context, base Event) (blocked bool, hookName, reason string) {
	base.Event = string(domain.HookEventUserPromptSubmit)
	blocked, hookName, reason, _ = r.dispatch(ctx, base)
	return blocked, hookName, reason
}

// PreToolUse runs pre_tool_use hooks in order on the caller's context. The
// decision is cached per tool.CallID for the lifetime of the Resolved (D4):
// an approved call re-executing after human approval returns the cached
// outcome verbatim — hooks evaluate the call exactly once per run, and no
// further audit rows are written. On block, resultJSON is the canonical
// block payload the model reads as the tool result.
func (r *Resolved) PreToolUse(ctx context.Context, base Event, tool EventTool) (blocked bool, resultJSON string) {
	if tool.CallID != "" {
		if out, ok := r.cachedOutcome(tool.CallID); ok {
			return out.blocked, out.resultJSON
		}
	}

	base.Event = string(domain.HookEventPreToolUse)
	base.Tool = &tool
	blocked, hookName, reason, blockJSON := r.dispatch(ctx, base)

	if tool.CallID != "" {
		r.cacheOutcome(tool.CallID, preToolOutcome{
			blocked:    blocked,
			resultJSON: blockJSON,
			hookName:   hookName,
			reason:     reason,
		})
	}
	return blocked, blockJSON
}

// ObserveRunStarted fires run_started hooks detached (D5): a
// cancellation-proof context, the hook's own budget, and panic recovery —
// run teardown never kills an in-flight delivery.
func (r *Resolved) ObserveRunStarted(ctx context.Context, base Event) {
	base.Event = string(domain.HookEventRunStarted)
	r.dispatchDetached(ctx, base)
}

// PostToolUse fires post_tool_use hooks detached (D5).
func (r *Resolved) PostToolUse(ctx context.Context, base Event, tool EventTool) {
	base.Event = string(domain.HookEventPostToolUse)
	base.Tool = &tool
	r.dispatchDetached(ctx, base)
}

// RunFinished fires run_finished hooks detached (D5) with the terminal
// status as data (completed|failed|cancelled). The runtime seam calls it
// after the terminal transcript event settles — on the cancel path, after
// the durable cancel marker drains.
func (r *Resolved) RunFinished(ctx context.Context, base Event, status string) {
	base.Event = string(domain.HookEventRunFinished)
	base.Status = status
	r.dispatchDetached(ctx, base)
}

// BlockToolResult builds the canonical pre_tool_use block payload (D3): the
// tool result the model reads and adapts to. Exported so every seam emits
// byte-identical shapes.
func BlockToolResult(hookName, reason string) string {
	raw, err := json.Marshal(map[string]any{
		"blocked_by_hook": true,
		"hook":            hookName,
		"reason":          reason,
	})
	if err != nil {
		// Marshal of fixed string keys cannot fail; the fallback keeps the
		// contract total.
		return `{"blocked_by_hook":true,"hook":"` + hookName + `","reason":"blocked"}`
	}
	return string(raw)
}

// -------------------------------------------------------------------------
// Dispatch loop
// -------------------------------------------------------------------------

// dispatch runs the ordered chain synchronously on the given context:
// matcher check first, then the D20 if gate on tool events (a non-match on
// either skips entirely — no handler execution, no audit row, no latency),
// then per-hook execution with the hook's own budget, failures resolved under
// the hook's on_failure policy (D9), first block short-circuiting the
// remaining hooks (D14). Returns the first blocking hook's identity and its
// canonical block JSON.
func (r *Resolved) dispatch(ctx context.Context, base Event) (blocked bool, hookName, reason, blockJSON string) {
	matchedValue := MatchedValue(base)
	// The serialized tool-input JSON for the D20 if gate: the event's raw
	// call-arguments JSON, the same serialization handlers and audit receive.
	var toolName, toolArgs string
	if base.Tool != nil {
		toolName, toolArgs = base.Tool.Name, base.Tool.Args
	}

	for i := range r.hooks {
		h := &r.hooks[i]
		if h.def.Event != domain.HookEvent(base.Event) {
			continue
		}

		// Uninterpretable hook (D7): a failed execution under its own
		// on_failure policy — fail closed for on_failure=block safety gates,
		// otherwise continue.
		if h.skipErr != "" {
			err := fmt.Errorf("hook skipped: %s", h.skipErr)
			r.recordExecution(ctx, h, base, DecisionFailure, 0, Result{}, err)
			if h.def.OnFailure == domain.HookFailureBlock {
				return r.blockOutcome(h, err.Error())
			}
			continue
		}

		if !h.matcher.Matches(matchedValue) {
			continue // no handler execution, no audit row, no latency
		}

		// D20 input gate (compiled only for tool events): a non-matching
		// input SKIPS the hook — no execution, no audit row. The if gate
		// narrows; it never blocks by itself.
		if h.ifGate != nil && !h.ifGate.allows(toolName, toolArgs) {
			continue
		}

		// Prompt evaluator cap (D12): per-run per-hook counter. Over cap the
		// evaluator is never called — allow, recorded as capped.
		if h.def.HandlerType == domain.HookHandlerPrompt && r.overPromptCap(h) {
			r.recordExecution(ctx, h, base, DecisionCapped, 0, Result{}, nil)
			continue
		}

		startedAt := time.Now()
		res, err := r.executeHook(ctx, h, base)
		duration := time.Since(startedAt)

		if err != nil {
			r.recordExecution(ctx, h, base, DecisionFailure, duration, res, err)
			r.updateStatus(ctx, h, domain.HookStatusError, err.Error())
			if h.def.OnFailure == domain.HookFailureBlock {
				return r.blockOutcome(h, err.Error())
			}
			continue
		}

		r.recordExecution(ctx, h, base, res.Decision, duration, res, nil)
		r.updateStatus(ctx, h, domain.HookStatusOK, "")

		if res.Decision == DecisionBlock {
			blockReason := res.Reason
			if blockReason == "" {
				blockReason = fmt.Sprintf("blocked by hook %s", h.def.Name)
			}
			return true, h.def.Name, blockReason, BlockToolResult(h.def.Name, blockReason)
		}
		// allow: continue with the remaining hooks.
	}
	return false, "", "", ""
}

// dispatchDetached runs the chain for one observational event on a
// cancellation-proof context in its own goroutine (D5): the run may tear
// down, cancel, or return while the delivery finishes on the hooks' own
// budgets. Panics are contained: recovered, logged, and recorded as a failed
// execution.
func (r *Resolved) dispatchDetached(ctx context.Context, base Event) {
	if !r.HasHooks() {
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(detached, "hooks: observer dispatch panicked",
					"event", base.Event, "panic", rec)
			}
		}()
		r.dispatch(detached, base)
	}()
}

// executeHook runs one hook's handler within its budget, containing panics
// as failures so one broken handler cannot take down the run or the
// observer goroutine.
func (r *Resolved) executeHook(ctx context.Context, h *resolvedHook, base Event) (res Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("hook handler panicked: %v", rec)
		}
	}()

	ev := base
	ev.DeliveryID = uuid.NewString()
	return r.d.reg.Execute(ctx, h.def.HandlerType, h.def.Config, ev,
		HookRef{Name: h.def.Name, Level: h.level, CompiledScript: h.scriptProg}, hookBudget(h.def))
}

// blockOutcome is the uniform failure-turned-block shape (D9): the hook's
// name and a reason naming the failure, delivered as the canonical block
// JSON.
func (r *Resolved) blockOutcome(h *resolvedHook, failure string) (bool, string, string, string) {
	reason := fmt.Sprintf("hook %s failed: %s", h.def.Name, failure)
	return true, h.def.Name, reason, BlockToolResult(h.def.Name, reason)
}

// hookBudget is the hook's own timeout budget; 0 selects the handler-type
// default (5s general, 15s prompt evaluators per D12).
func hookBudget(def domain.HookBase) time.Duration {
	ms := def.TimeoutMS
	if ms <= 0 {
		if def.HandlerType == domain.HookHandlerPrompt {
			ms = domain.DefaultPromptHookTimeoutMS
		} else {
			ms = domain.DefaultHookTimeoutMS
		}
	}
	return time.Duration(ms) * time.Millisecond
}

// overPromptCap reads-and-increments the per-run invocation counter, reporting
// whether the hook is over its configured max_invocations_per_run (default
// defaultPromptHookInvocationCap).
func (r *Resolved) overPromptCap(h *resolvedHook) bool {
	conf := decodePromptCap(h.def.Config)
	r.mu.Lock()
	defer r.mu.Unlock()
	count := r.promptCounts[h.key()]
	if count >= conf {
		return true
	}
	r.promptCounts[h.key()] = count + 1
	return false
}

// decodePromptCap extracts max_invocations_per_run from the stored prompt
// config; absent or malformed selects the D12 default (save validation owns
// rejecting malformed values).
func decodePromptCap(cfg json.RawMessage) int {
	var conf struct {
		MaxInvocationsPerRun int `json:"max_invocations_per_run"`
	}
	if err := json.Unmarshal(cfg, &conf); err != nil || conf.MaxInvocationsPerRun <= 0 {
		return defaultPromptHookInvocationCap
	}
	return conf.MaxInvocationsPerRun
}

// cachedOutcome returns the cached per-CallID chain outcome (D4).
func (r *Resolved) cachedOutcome(callID string) (preToolOutcome, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out, ok := r.dedup[callID]
	return out, ok
}

func (r *Resolved) cacheOutcome(callID string, out preToolOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dedup[callID] = out
}

// -------------------------------------------------------------------------
// Audit and health
// -------------------------------------------------------------------------

// Audit decision values (exactly these four; D16).
const (
	DecisionAllow   = "allow"
	DecisionBlock   = "block"
	DecisionFailure = "failure"
	DecisionCapped  = "capped"
)

// recordExecution writes the audit row for one executed evaluation
// (best-effort: a failing audit write never fails a delivery). Records
// survive hook deletion via the store's denormalized name (D16).
func (r *Resolved) recordExecution(ctx context.Context, h *resolvedHook, ev Event, decision string, duration time.Duration, res Result, execErr error) {
	hookID := h.id
	exec := &domain.HookExecution{
		HookID:      &hookID,
		HookName:    h.def.Name,
		HookLevel:   h.level,
		WorkspaceID: ev.Workspace.ID,
		Event:       domain.HookEvent(ev.Event),
		Decision:    decision,
		DurationMS:  duration.Milliseconds(),
		ExitCode:    res.ExitCode,
		HTTPStatus:  res.HTTPStatus,
		TokenCount:  res.TokenCount,
		Origin:      ev.Origin,
	}
	if execErr != nil {
		exec.Detail = execErr.Error()
	}
	if err := r.d.hooks.RecordHookExecution(ctx, exec); err != nil {
		slog.WarnContext(ctx, "hooks: audit write failed (best-effort)",
			"hook_id", h.id, "hook_name", h.def.Name, "error", err)
	}
}

// updateStatus refreshes the hook's health after one delivery attempt:
// error on failure, ok on success (best-effort write).
func (r *Resolved) updateStatus(ctx context.Context, h *resolvedHook, status domain.HookStatus, statusErr string) {
	if err := r.d.hooks.SetHookDeliveryStatus(ctx, h.level, h.id, status, statusErr); err != nil {
		slog.WarnContext(ctx, "hooks: status update failed (best-effort)",
			"hook_id", h.id, "level", h.level, "error", err)
	}
}
