package hooks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type handlerKey struct {
	id     string
	config string
}

type circuitBreaker struct {
	mu      sync.Mutex
	history map[string][]time.Time // hookID -> timestamps of blocks/timeouts
}

func (cb *circuitBreaker) Record(hookID string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.history == nil {
		cb.history = make(map[string][]time.Time)
	}
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	times := cb.history[hookID]
	var active []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			active = append(active, t)
		}
	}
	active = append(active, now)
	cb.history[hookID] = active

	return len(active) >= 5
}

// Dispatcher coordinates hook resolution, execution, safeguards, and auditing.
type Dispatcher struct {
	hookStore     store.HookStore
	execStore     store.HookExecutionStore
	cb            *circuitBreaker
	handlersMu    sync.Mutex
	handlersCache map[handlerKey]Handler
}

// NewDispatcher creates a new Dispatcher instance.
func NewDispatcher(hs store.HookStore, es store.HookExecutionStore) *Dispatcher {
	return &Dispatcher{
		hookStore:     hs,
		execStore:     es,
		cb:            &circuitBreaker{},
		handlersCache: make(map[handlerKey]Handler),
	}
}

// GetHookStore returns the hook store.
func (d *Dispatcher) GetHookStore() store.HookStore {
	return d.hookStore
}

// GetExecStore returns the execution store.
func (d *Dispatcher) GetExecStore() store.HookExecutionStore {
	return d.execStore
}

func (d *Dispatcher) getHandler(h *store.Hook) (Handler, error) {
	d.handlersMu.Lock()
	defer d.handlersMu.Unlock()
	key := handlerKey{id: h.ID, config: h.Config}
	if handler, ok := d.handlersCache[key]; ok {
		return handler, nil
	}
	handler, err := New(h.HandlerType, []byte(h.Config))
	if err != nil {
		return nil, err
	}
	d.handlersCache[key] = handler
	return handler, nil
}

// Fire executes all matching hooks for the given event and payload.
func (d *Dispatcher) Fire(ctx context.Context, event Event, payload Payload) (Decision, error) {
	payload.Event = event

	// Resolve active hooks for this agent and event
	hooks, err := d.resolveHooks(ctx, payload.Agent, event)
	if err != nil {
		return DecisionAllow, fmt.Errorf("resolve hooks: %w", err)
	}

	if len(hooks) == 0 {
		return DecisionAllow, nil
	}

	startTime := time.Now()
	const chainBudget = 10 * time.Second

	for _, h := range hooks {
		// Run tool name regex matcher if applicable
		if event == EventPreToolUse || event == EventPostToolUse {
			if !MatchToolName(h.Matcher, payload.ToolName) {
				continue
			}
		}

		// Check if chain budget is exceeded
		if time.Since(startTime) > chainBudget {
			if isBlockingEvent(event) {
				return DecisionBlock, fmt.Errorf("chain budget exceeded: total execution exceeded %v", chainBudget)
			}
			break
		}

		decision, err := d.runHook(ctx, h, payload, false)

		if isBlockingEvent(event) && decision == DecisionBlock {
			// Short-circuit on first block
			return DecisionBlock, err
		}
	}

	return DecisionAllow, nil
}

// TestHook runs a single hook for dry-run/testing purposes (no audit logs, no breaker).
func (d *Dispatcher) TestHook(ctx context.Context, h *store.Hook, payload Payload) (Decision, error) {
	return d.runHook(ctx, h, payload, true)
}

func (d *Dispatcher) runHook(ctx context.Context, h *store.Hook, payload Payload, dryRun bool) (Decision, error) {
	handler, err := d.getHandler(h)
	if err != nil {
		// Fail closed on resolution/initialization error
		d.audit(ctx, h.ID, h.HandlerType, payload, DecisionBlock, 0, err, dryRun)
		return DecisionBlock, fmt.Errorf("get handler for hook %s: %w", h.ID, err)
	}

	timeout := time.Duration(h.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	} else if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	dec, err := handler.Run(ctxWithTimeout, payload)
	duration := time.Since(start)

	var execErr error
	finalDecision := dec

	if err != nil {
		execErr = err
		if errors.Is(err, context.DeadlineExceeded) {
			execErr = fmt.Errorf("hook execution timed out after %s", timeout)
			if h.OnTimeout == "allow" {
				finalDecision = DecisionAllow
			} else {
				finalDecision = DecisionBlock
			}
		} else {
			finalDecision = DecisionBlock
		}
	}

	// For non-blocking events, decision is always allow (ignore block)
	if !isBlockingEvent(payload.Event) {
		finalDecision = DecisionAllow
	}

	// Update circuit breaker if not a dry-run and hook blocked, timed out, or errored
	if !dryRun && (finalDecision == DecisionBlock || execErr != nil) {
		if d.cb.Record(h.ID) {
			// Disable hook
			_ = d.hookStore.ToggleHook(ctx, h.ID, false)
		}
	}

	d.audit(ctx, h.ID, h.HandlerType, payload, finalDecision, duration.Milliseconds(), execErr, dryRun)

	return finalDecision, execErr
}

func (d *Dispatcher) audit(ctx context.Context, hookID string, handlerType string, payload Payload, dec Decision, durationMs int64, err error, dryRun bool) {
	if dryRun {
		return
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	exec := &store.HookExecution{
		HookID:      hookID,
		Event:       string(payload.Event),
		HandlerType: handlerType,
		Decision:    string(dec),
		DurationMS:  durationMs,
		Error:       errStr,
	}
	_ = d.execStore.AppendExecution(ctx, exec)
}

func (d *Dispatcher) resolveHooks(ctx context.Context, agent string, event Event) ([]*store.Hook, error) {
	globalHooks, err := d.hookStore.ListHooksByScopeAndEvent(ctx, "global", string(event))
	if err != nil {
		return nil, err
	}

	var agentHooks []*store.Hook
	if agent != "" && agent != "global" {
		agentHooks, err = d.hookStore.ListHooksByScopeAndEvent(ctx, agent, string(event))
		if err != nil {
			return nil, err
		}
	}

	merged := append(globalHooks, agentHooks...)

	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Priority != merged[j].Priority {
			return merged[i].Priority > merged[j].Priority
		}
		return merged[i].CreatedAt < merged[j].CreatedAt
	})

	var active []*store.Hook
	for _, h := range merged {
		if h.Enabled == 1 {
			active = append(active, h)
		}
	}

	return active, nil
}

func isBlockingEvent(event Event) bool {
	return event == EventUserPromptSubmit || event == EventPreToolUse
}
