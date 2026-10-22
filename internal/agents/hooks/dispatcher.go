package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"
)

// Dispatcher resolves hook definitions into a per-run execution plan and
// delivers lifecycle events to it (design D4, spec agent-hooks). Resolution
// reads both assignment tiers fresh on every call — never cached at boot —
// so content changes converge immediately.
type Dispatcher struct {
	store Store
	reg   *Registry
}

// NewDispatcher builds the dispatcher over the store seam and the handler
// registry. A nil store is the no-op dispatcher (see NewNoopDispatcher).
func NewDispatcher(store Store, reg *Registry) *Dispatcher {
	return &Dispatcher{store: store, reg: reg}
}

// NewNoopDispatcher returns a dispatcher that always resolves an empty chain:
// every delivery is a no-op. It is the runner's default when no dispatcher is
// wired; the composition root injects the real one.
func NewNoopDispatcher() *Dispatcher {
	return &Dispatcher{reg: NewRegistry()}
}

// Resolve reads the two tiers fresh (design D4: assigned hooks by position
// first, then the unassigned global tier) and compiles each hook's matcher —
// and each script hook's program — once for the run. A hook this binary
// cannot interpret — unknown event, unknown handler type, uncompilable
// matcher, uncompilable script — is skipped gracefully: warning logged, the
// hook kept in the tier list as a failed execution under its own on_failure
// policy so a safety gate with on_failure = block still fails closed.
// Disabled hooks resolve to nothing.
//
// Store-level read failures return an error; the caller owns the run's fate.
func (d *Dispatcher) Resolve(ctx context.Context, agentID string) (*Resolved, error) {
	r := &Resolved{
		d:     d,
		dedup: make(map[string]preToolOutcome),
	}
	if d.store == nil {
		// The no-op dispatcher: empty chain, every delivery a no-op.
		return r, nil
	}

	assigned, err := d.store.AssignedHooks(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: list assigned hooks: %w", err)
	}
	global, err := d.store.GlobalHooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks: list global hooks: %w", err)
	}

	for _, def := range assigned {
		r.append(TierAssigned, def)
	}
	for _, def := range global {
		r.append(TierGlobal, def)
	}
	return r, nil
}

// append adds one hook to the tier list, applying the graceful-skip rule to
// definitions this binary cannot interpret.
func (r *Resolved) append(tier string, def Definition) {
	if !def.Enabled {
		return
	}
	h := resolvedHook{tier: tier, id: def.ID, def: def}

	if !isKnownEvent(def.Event) {
		h.skipErr = fmt.Sprintf("unknown event %q", def.Event)
		r.skipGracefully(tier, def, h.skipErr)
		r.hooks = append(r.hooks, h)
		return
	}
	if !isKnownHandlerType(def.HandlerType) {
		h.skipErr = fmt.Sprintf("unknown handler type %q", def.HandlerType)
		r.skipGracefully(tier, def, h.skipErr)
		r.hooks = append(r.hooks, h)
		return
	}
	matcher, err := CompileMatcher(def.Matcher)
	if err != nil {
		h.skipErr = fmt.Sprintf("uncompilable matcher: %v", err)
		r.skipGracefully(tier, def, h.skipErr)
		r.hooks = append(r.hooks, h)
		return
	}
	h.matcher = matcher

	// Script hooks compile ONCE per run, cached on the resolved hook; every
	// delivery in the run reuses the program. An uncompilable script (a row
	// written by an older binary — save validation compiles first) is the
	// graceful skip. Skipped entirely when the handler's kill switch is off:
	// the gate in Registry.Execute reports that instead.
	if def.HandlerType == HandlerScript && r.d.reg.scriptEnabled {
		prog, err := compileScriptConfig(json.RawMessage(def.Config))
		if err != nil {
			h.skipErr = fmt.Sprintf("uncompilable script: %v", err)
			r.skipGracefully(tier, def, h.skipErr)
		} else {
			h.scriptProg = prog
		}
	}
	r.hooks = append(r.hooks, h)
}

// skipGracefully is the outcome for an uninterpretable hook: a warning so the
// broken definition is impossible to miss in the logs. The hook stays in the
// tier list; its delivery becomes a failed execution under its own
// on_failure policy at dispatch time.
func (r *Resolved) skipGracefully(tier string, def Definition, reason string) {
	slog.WarnContext(context.Background(), "hooks: skipping hook this binary cannot interpret",
		"hook_id", def.ID,
		"hook_name", def.Name,
		"tier", tier,
		"handler_type", def.HandlerType,
		"reason", reason,
	)
}

// resolvedHook is one hook of a run's resolved chain.
type resolvedHook struct {
	tier string
	id   string
	def  Definition
	// matcher is nil only on hooks whose skipErr is set (the graceful skip
	// short-circuits before any matcher use).
	matcher Matcher
	// skipErr is non-empty when the definition is uninterpretable: the hook
	// does not run, but its delivery is a failed execution under its own
	// on_failure policy.
	skipErr string
	// scriptProg is the script handler's compile-once-per-run cache, built
	// in append for script hooks only.
	scriptProg *goja.Program
}

// key identifies the hook within per-run state (dedup counters).
func (h *resolvedHook) key() string { return h.tier + "/" + h.id }

// preToolOutcome is the cached per-CallID chain outcome: returned verbatim
// when an approved call re-executes through the middleware chain.
type preToolOutcome struct {
	blocked    bool
	resultJSON string
	hookName   string
	reason     string
}

// Resolved is one run's hook execution plan: the two tiers in fixed order,
// matchers compiled once, plus the per-run state (call-ID dedup cache). Its
// methods are the runtime seams' only entry points.
type Resolved struct {
	d     *Dispatcher
	hooks []resolvedHook

	mu    sync.Mutex
	dedup map[string]preToolOutcome
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
	base.Event = EventUserPromptSubmit
	blocked, hookName, reason, _ = r.dispatch(ctx, base)
	return blocked, hookName, reason
}

// PreToolUse runs pre_tool_use hooks in order on the caller's context. The
// decision is cached per tool.CallID for the lifetime of the Resolved: an
// approved call re-executing after human approval returns the cached outcome
// verbatim — hooks evaluate the call exactly once per run, and no further
// audit rows are written. On block, resultJSON is the canonical block
// payload the model reads as the tool result.
func (r *Resolved) PreToolUse(ctx context.Context, base Event, tool EventTool) (blocked bool, resultJSON string) {
	if tool.CallID != "" {
		if out, ok := r.cachedOutcome(tool.CallID); ok {
			return out.blocked, out.resultJSON
		}
	}

	base.Event = EventPreToolUse
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

// ObserveRunStarted fires run_started hooks detached: a cancellation-proof
// context, the hook's own budget, and panic recovery — run teardown never
// kills an in-flight delivery.
func (r *Resolved) ObserveRunStarted(ctx context.Context, base Event) {
	base.Event = EventRunStarted
	r.dispatchDetached(ctx, base)
}

// PostToolUse fires post_tool_use hooks detached.
func (r *Resolved) PostToolUse(ctx context.Context, base Event, tool EventTool) {
	base.Event = EventPostToolUse
	base.Tool = &tool
	r.dispatchDetached(ctx, base)
}

// RunFinished fires run_finished hooks detached with the terminal status as
// data (completed|failed|cancelled). The runtime seam calls it after the
// terminal transcript event settles — on the cancel path, after the durable
// cancel marker drains.
func (r *Resolved) RunFinished(ctx context.Context, base Event, status string) {
	base.Event = EventRunFinished
	base.Status = status
	r.dispatchDetached(ctx, base)
}

// BlockToolResult builds the canonical pre_tool_use block payload: the tool
// result the model reads and adapts to. Exported so every seam emits
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
// event filter first, then the matcher (a non-match skips entirely — no
// handler execution, no audit row, no latency), then per-hook execution with
// the hook's own budget, failures resolved under the hook's on_failure
// policy, first block short-circuiting the remaining hooks. Returns the
// first blocking hook's identity and its canonical block JSON.
func (r *Resolved) dispatch(ctx context.Context, base Event) (blocked bool, hookName, reason, blockJSON string) {
	matchedValue := MatchedValue(base)

	for i := range r.hooks {
		h := &r.hooks[i]
		if h.def.Event != base.Event {
			continue
		}

		// Uninterpretable hook: a failed execution under its own on_failure
		// policy — fail closed for on_failure=block safety gates, otherwise
		// continue.
		if h.skipErr != "" {
			err := fmt.Errorf("hook skipped: %s", h.skipErr)
			r.recordExecution(ctx, h, base, DecisionFailure, 0, Result{}, err)
			if failsClosed(h.def) {
				return r.blockOutcome(h, err.Error())
			}
			continue
		}

		if !h.matcher.Matches(matchedValue) {
			continue // no handler execution, no audit row, no latency
		}

		startedAt := time.Now()
		res, err := r.executeHook(ctx, h, base)
		duration := time.Since(startedAt)

		if err != nil {
			r.recordExecution(ctx, h, base, DecisionFailure, duration, res, err)
			if failsClosed(h.def) {
				return r.blockOutcome(h, err.Error())
			}
			continue
		}

		r.recordExecution(ctx, h, base, res.Decision, duration, res, nil)

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

// failsClosed reports whether the definition's on_failure policy turns a
// handler failure into a block. The empty policy is the "allow" default.
func failsClosed(def Definition) bool { return def.OnFailure == "block" }

// dispatchDetached runs the chain for one observational event on a
// cancellation-proof context in its own goroutine: the run may tear down,
// cancel, or return while the delivery finishes on the hooks' own budgets.
// Panics are contained: recovered, logged, and recorded as a failed
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
	return r.d.reg.Execute(ctx, h.def.HandlerType, json.RawMessage(h.def.Config), ev,
		HookRef{Name: h.def.Name, Tier: h.tier, CompiledScript: h.scriptProg}, hookBudget(h.def))
}

// blockOutcome is the uniform failure-turned-block shape: the hook's name
// and a reason naming the failure, delivered as the canonical block JSON.
func (r *Resolved) blockOutcome(h *resolvedHook, failure string) (bool, string, string, string) {
	reason := fmt.Sprintf("hook %s failed: %s", h.def.Name, failure)
	return true, h.def.Name, reason, BlockToolResult(h.def.Name, reason)
}

// hookBudget is the hook's own timeout budget; <= 0 selects the default and
// an over-cap value is clamped to MaxTimeoutMS.
func hookBudget(def Definition) time.Duration {
	ms := def.TimeoutMS
	if ms <= 0 {
		ms = DefaultTimeoutMS
	}
	if ms > MaxTimeoutMS {
		ms = MaxTimeoutMS
	}
	return time.Duration(ms) * time.Millisecond
}

// cachedOutcome returns the cached per-CallID chain outcome.
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
// Audit
// -------------------------------------------------------------------------

// recordExecution writes the audit row for one executed evaluation
// (best-effort: a failing audit write never fails a delivery). Records
// survive hook deletion — the audit trail outlives the definition.
func (r *Resolved) recordExecution(ctx context.Context, h *resolvedHook, ev Event, decision string, duration time.Duration, res Result, execErr error) {
	if r.d.store == nil {
		return
	}
	exec := Execution{
		HookID:     h.id,
		AgentID:    ev.Agent.ID,
		SessionID:  ev.SessionID,
		TurnID:     ev.TurnID,
		Event:      ev.Event,
		Outcome:    outcomeOf(decision, execErr),
		DurationMS: duration.Milliseconds(),
	}
	if execErr != nil {
		exec.Error = execErr.Error()
	}
	if err := r.d.store.RecordExecution(ctx, exec); err != nil {
		slog.WarnContext(ctx, "hooks: audit write failed (best-effort)",
			"hook_id", h.id, "hook_name", h.def.Name, "error", err)
	}
}
