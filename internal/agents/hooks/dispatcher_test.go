package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// -------------------------------------------------------------------------
// Fakes
// -------------------------------------------------------------------------

// fakeHookStore is the dispatcher's scripted store: canned tier lists (list
// order = resolution order, mirroring the real adapters' ORDER BY) plus
// recorded audit rows.
type fakeHookStore struct {
	assigned map[string][]Definition // agent ID → definitions in resolution order
	global   []Definition

	mu         sync.Mutex
	executions []Execution
}

func (f *fakeHookStore) AssignedHooks(_ context.Context, agentID string) ([]Definition, error) {
	out := make([]Definition, 0)
	out = append(out, f.assigned[agentID]...)
	return out, nil
}

func (f *fakeHookStore) GlobalHooks(context.Context) ([]Definition, error) {
	out := make([]Definition, 0)
	out = append(out, f.global...)
	return out, nil
}

func (f *fakeHookStore) RecordExecution(_ context.Context, exec Execution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executions = append(f.executions, exec)
	return nil
}

func (f *fakeHookStore) recordedExecutions() []Execution {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Execution(nil), f.executions...)
}

// logFileHook builds a command hook that appends its name to the shared log
// file (through its own env row, so no shell quoting is involved) and exits
// 0 — an allow that still proves execution by side effect.
func logFileHook(t *testing.T, logFile, id, name, event, matcher string) Definition {
	t.Helper()
	raw, err := json.Marshal(commandHookConfig{
		Command: `echo ` + name + ` >> "$HOOK_LOG"`,
		Env:     []configRow{{Name: "HOOK_LOG", Value: logFile}},
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return Definition{
		ID:          id,
		Name:        name,
		Event:       event,
		Matcher:     matcher,
		HandlerType: HandlerCommand,
		Config:      string(raw),
		TimeoutMS:   DefaultTimeoutMS,
		Enabled:     true,
	}
}

// blockingScriptHook builds a script hook that blocks with the given reason.
func blockingScriptHook(id, name, event, matcher, reason string) Definition {
	return Definition{
		ID:          id,
		Name:        name,
		Event:       event,
		Matcher:     matcher,
		HandlerType: HandlerScript,
		Config:      scriptCfg(wrapBody(`return {decision: "block", reason: ` + mustJSONString(reason) + `};`)),
		TimeoutMS:   DefaultTimeoutMS,
		Enabled:     true,
	}
}

// allowScriptHook builds a script hook that returns an explicit allow.
func allowScriptHook(id, name, event, matcher string) Definition {
	return Definition{
		ID:          id,
		Name:        name,
		Event:       event,
		Matcher:     matcher,
		HandlerType: HandlerScript,
		Config:      scriptCfg(wrapBody(`return {decision: "allow"};`)),
		TimeoutMS:   DefaultTimeoutMS,
		Enabled:     true,
	}
}

// logLines waits until the shared log file holds at least n lines (deadline
// 3s) and returns them.
func logLines(t *testing.T, path string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil && strings.Count(strings.TrimRight(string(raw), "\n"), "\n")+1 >= n && strings.TrimSpace(string(raw)) != "" {
			return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		}
		time.Sleep(5 * time.Millisecond)
	}
	raw, _ := os.ReadFile(path)
	t.Fatalf("log file %s never reached %d lines; got %q", path, n, raw)
	return nil
}

func dispatcherEvent() Event {
	return Event{
		Origin:    "user",
		Agent:     EventRef{ID: "ag-1", Name: "Atlas"},
		SessionID: "sess-1",
		TurnID:    "turn-1",
	}
}

func dispatcherTool() EventTool {
	return EventTool{Name: "web_fetch", CallID: "call-1", Args: `{"url":"https://x"}`}
}

// -------------------------------------------------------------------------
// Tests
// -------------------------------------------------------------------------

// TestDispatcher_TierOrder pins the resolution order (design D4, spec
// "Resolution order"): the agent's assigned hooks by position first, then the
// unassigned global tier — each executed, each audited, in dispatch order.
func TestDispatcher_TierOrder(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hooks.log")
	// The fake returns its slice verbatim, so the slice encodes position
	// order (real adapters ORDER BY position, hook name).
	store := &fakeHookStore{
		assigned: map[string][]Definition{
			"ag-1": {
				logFileHook(t, logFile, "a-1", "First", EventPreToolUse, "*"),
				logFileHook(t, logFile, "a-2", "Second", EventPreToolUse, "*"),
			},
		},
		global: []Definition{
			logFileHook(t, logFile, "g-1", "Global", EventPreToolUse, "*"),
		},
	}

	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, blockJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	if blocked || blockJSON != "" {
		t.Fatalf("blocked = %v, json = %q, want clean allow", blocked, blockJSON)
	}

	got := logLines(t, logFile, 3)
	want := []string{"First", "Second", "Global"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("execution %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}

	// One audit row per executed hook, in dispatch order, mapped onto the
	// audit outcomes. The audit row carries the hook ID — the audit trail
	// outlives definitions, so names resolve through (deletable) rows, not
	// the record itself.
	wantIDs := []string{"a-1", "a-2", "g-1"}
	execs := store.recordedExecutions()
	if len(execs) != 3 {
		t.Fatalf("audit rows = %d, want 3", len(execs))
	}
	for i := range want {
		if execs[i].HookID != wantIDs[i] || execs[i].Outcome != OutcomeAllowed {
			t.Errorf("audit[%d] = (%s, %s), want (%s, %s)", i, execs[i].HookID, execs[i].Outcome, wantIDs[i], OutcomeAllowed)
		}
		if execs[i].Event != EventPreToolUse || execs[i].AgentID != "ag-1" || execs[i].SessionID != "sess-1" || execs[i].TurnID != "turn-1" {
			t.Errorf("audit[%d] context = %+v, want the event's agent/session/turn", i, execs[i])
		}
	}
}

// TestDispatcher_FirstBlockWinsShortCircuits pins first block wins and the
// canonical block JSON.
func TestDispatcher_FirstBlockWinsShortCircuits(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hooks.log")
	store := &fakeHookStore{
		global: []Definition{
			blockingScriptHook("g-1", "Blocker", EventPreToolUse, "*", "deployments frozen"),
			logFileHook(t, logFile, "g-2", "Second", EventPreToolUse, "*"),
		},
	}
	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	if !blocked {
		t.Fatal("expected block")
	}

	var payload struct {
		BlockedByHook bool   `json:"blocked_by_hook"`
		Hook          string `json:"hook"`
		Reason        string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &payload); err != nil {
		t.Fatalf("block JSON: %v (%q)", err, resultJSON)
	}
	if !payload.BlockedByHook || payload.Hook != "Blocker" || payload.Reason != "deployments frozen" {
		t.Errorf("block payload = %+v, want blocked_by_hook/hook/reason", payload)
	}
	if _, err := os.Stat(logFile); err == nil {
		t.Fatalf("second hook executed after a block (%q), want short-circuit", mustReadFile(t, logFile))
	}
	execs := store.recordedExecutions()
	if len(execs) != 1 || execs[0].Outcome != OutcomeBlocked || execs[0].HookID != "g-1" {
		t.Fatalf("audit = %+v, want one blocked row for Blocker", execs)
	}

	// The exported builder matches the wire shape byte-for-byte.
	if again := BlockToolResult("Blocker", "deployments frozen"); again != resultJSON {
		t.Errorf("BlockToolResult = %q, dispatch produced %q", again, resultJSON)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

// TestDispatcher_MatcherSkipWritesNoAudit pins the matcher gate: a
// non-matching hook is skipped — no handler execution, no audit row; the
// matching tool name executes and audits.
func TestDispatcher_MatcherSkipWritesNoAudit(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hooks.log")
	store := &fakeHookStore{
		global: []Definition{
			logFileHook(t, logFile, "g-1", "Execute Only", EventPreToolUse, "execute"),
		},
	}
	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	if blocked {
		t.Fatal("unexpected block")
	}
	if _, err := os.Stat(logFile); err == nil {
		t.Fatalf("handler executed on non-matching hook (%q), want 0 executions", mustReadFile(t, logFile))
	}
	if rows := store.recordedExecutions(); len(rows) != 0 {
		t.Fatalf("audit rows = %d, want 0 on matcher skip", len(rows))
	}

	// The matching tool name executes and audits (fresh CallID — the first
	// call's allow outcome is cached per CallID).
	tool := dispatcherTool()
	tool.Name = "execute"
	tool.CallID = "call-exec-1"
	blocked, _ = resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	if blocked {
		t.Fatal("matching occurrence must not block an allow hook")
	}
	if lines := logLines(t, logFile, 1); lines[0] != "Execute Only" {
		t.Fatalf("matching occurrence log = %v, want one execution", lines)
	}
	if rows := store.recordedExecutions(); len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
}

// TestDispatcher_FailurePolicy pins the failure contract: a handler failure
// is recorded as an error outcome and resolved under the hook's on_failure
// policy — the "allow" default continues (fail-open, spec "Failing hook does
// not break the turn"), "block" fails closed with a reason naming the hook
// and the failure.
func TestDispatcher_FailurePolicy(t *testing.T) {
	t.Run("on_failure allow continues", func(t *testing.T) {
		logFile := filepath.Join(t.TempDir(), "hooks.log")
		dead := allowScriptHook("g-0", "Broken", EventPreToolUse, "*")
		dead.HandlerType = HandlerCommand
		dead.Config = mustJSONString(commandHookConfig{Command: "/nonexistent/buddies/hook-bin"})
		store := &fakeHookStore{
			global: []Definition{
				dead,
				logFileHook(t, logFile, "g-1", "Healthy", EventPreToolUse, "*"),
			},
		}
		d := NewDispatcher(store, NewRegistry())
		resolved, err := d.Resolve(context.Background(), "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if blocked {
			t.Fatal("on_failure=allow (the default) must not block")
		}
		if lines := logLines(t, logFile, 1); lines[0] != "Healthy" {
			t.Fatalf("chain stopped after fail-open hook (%v), want continue", lines)
		}
		execs := store.recordedExecutions()
		if len(execs) != 2 || execs[0].Outcome != OutcomeError || execs[0].Error == "" || execs[1].Outcome != OutcomeAllowed {
			t.Fatalf("audit = %+v, want an error row with detail then allow", execs)
		}
	})

	t.Run("on_failure block fails closed", func(t *testing.T) {
		dead := allowScriptHook("g-0", "Safety Gate", EventPreToolUse, "*")
		dead.HandlerType = HandlerCommand
		dead.Config = mustJSONString(commandHookConfig{Command: "/nonexistent/buddies/hook-bin"})
		dead.OnFailure = "block"
		store := &fakeHookStore{global: []Definition{dead}}
		d := NewDispatcher(store, NewRegistry())
		resolved, err := d.Resolve(context.Background(), "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if !blocked {
			t.Fatal("on_failure=block must block on handler failure")
		}
		if want := `"blocked_by_hook":true`; !strings.Contains(resultJSON, want) {
			t.Errorf("block JSON = %q, want it to contain %s", resultJSON, want)
		}
		if want := `"hook":"Safety Gate"`; !strings.Contains(resultJSON, want) {
			t.Errorf("block JSON = %q, want it to name the failed hook", resultJSON)
		}
		execs := store.recordedExecutions()
		if len(execs) != 1 || execs[0].Outcome != OutcomeError || execs[0].Error == "" {
			t.Fatalf("audit = %+v, want one error row with detail", execs)
		}
	})

	t.Run("timeout is a failure under the hook's budget", func(t *testing.T) {
		slow := allowScriptHook("g-0", "Slow Gate", EventPreToolUse, "*")
		slow.HandlerType = HandlerCommand
		slow.Config = mustJSONString(commandHookConfig{Command: `sleep 30`})
		slow.TimeoutMS = 50
		slow.OnFailure = "block"
		store := &fakeHookStore{global: []Definition{slow}}
		d := NewDispatcher(store, NewRegistry())
		resolved, err := d.Resolve(context.Background(), "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		start := time.Now()
		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if !blocked {
			t.Fatal("budget expiry with on_failure=block must block")
		}
		if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
			t.Fatalf("dispatch waited %v, want the 50ms hook budget to bound it", elapsed)
		}
	})
}

// TestDispatcher_PreToolUseDedup pins the per-CallID cache: the same CallID
// evaluates once for the lifetime of the Resolved; the cached outcome
// (including its block JSON) is returned verbatim; a different CallID
// evaluates fresh.
func TestDispatcher_PreToolUseDedup(t *testing.T) {
	store := &fakeHookStore{
		global: []Definition{
			blockingScriptHook("g-1", "Gate", EventPreToolUse, "*", "nope"),
		},
	}
	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	tool := dispatcherTool()
	blocked1, json1 := resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	blocked2, json2 := resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	if !blocked1 || !blocked2 || json1 != json2 {
		t.Fatalf("dedup outcomes differ: (%v,%q) vs (%v,%q)", blocked1, json1, blocked2, json2)
	}
	if rows := len(store.recordedExecutions()); rows != 1 {
		t.Fatalf("audit rows = %d after dedup, want 1", rows)
	}

	// A different CallID is a new logical call — still one audit row per
	// executed evaluation.
	tool.CallID = "call-2"
	resolved.PreToolUse(context.Background(), dispatcherEvent(), tool)
	if rows := len(store.recordedExecutions()); rows != 2 {
		t.Fatalf("audit rows = %d after a fresh CallID, want 2", rows)
	}
}

// TestDispatcher_DetachedObserverSurvivesCancel pins the detached seams:
// observational deliveries run on a cancellation-proof context with their own
// budget and still land when the caller cancels immediately after firing.
func TestDispatcher_DetachedObserverSurvivesCancel(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hooks.log")
	store := &fakeHookStore{
		global: []Definition{
			logFileHook(t, logFile, "g-run", "Run Observer", EventRunStarted, "*"),
			logFileHook(t, logFile, "g-post", "Post Observer", EventPostToolUse, "*"),
			logFileHook(t, logFile, "g-fin", "Finish Observer", EventRunFinished, "failed"),
		},
	}
	// Each observer outlives the caller's cancel by a beat.
	for i := range store.global {
		cfg := mustJSONString(commandHookConfig{
			Command: `sleep 0.15; echo ` + store.global[i].Name + ` >> "$HOOK_LOG"`,
			Env:     []configRow{{Name: "HOOK_LOG", Value: logFile}},
		})
		store.global[i].Config = cfg
	}

	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resolved.ObserveRunStarted(ctx, dispatcherEvent())
	resolved.PostToolUse(ctx, dispatcherEvent(), dispatcherTool())
	resolved.RunFinished(ctx, dispatcherEvent(), "failed")
	cancel() // the run tears down immediately; deliveries must survive

	got := logLines(t, logFile, 3)
	want := map[string]bool{"Run Observer": false, "Post Observer": false, "Finish Observer": false}
	for _, line := range got {
		if _, seen := want[line]; !seen {
			t.Fatalf("unexpected delivery %q", line)
		}
		want[line] = true
	}
	for name, landed := range want {
		if !landed {
			t.Fatalf("observer delivery %q never landed after caller cancel (log: %v)", name, got)
		}
	}

	// Wait for the audit rows to settle, then assert all three recorded.
	deadline := time.Now().Add(3 * time.Second)
	for len(store.recordedExecutions()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	execs := store.recordedExecutions()
	if len(execs) != 3 {
		t.Fatalf("audit rows = %d, want 3", len(execs))
	}
	for _, row := range execs {
		if row.Outcome != OutcomeAllowed {
			t.Errorf("outcome = %q, want %q", row.Outcome, OutcomeAllowed)
		}
	}
}

// TestDispatcher_GracefulSkip pins the uninterpretable-definition rule: the
// hook does not run, the failure resolves under its on_failure policy — allow
// continues, block fails closed.
func TestDispatcher_GracefulSkip(t *testing.T) {
	t.Run("unknown handler type with on_failure allow continues", func(t *testing.T) {
		logFile := filepath.Join(t.TempDir(), "hooks.log")
		broken := logFileHook(t, logFile, "g-broken", "Future Hook", EventPreToolUse, "*")
		broken.HandlerType = "carrier-pigeon"
		store := &fakeHookStore{
			global: []Definition{
				broken,
				logFileHook(t, logFile, "g-1", "Healthy", EventPreToolUse, "*"),
			},
		}
		d := NewDispatcher(store, NewRegistry())
		resolved, err := d.Resolve(context.Background(), "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if blocked {
			t.Fatal("skipped hook with on_failure=allow must not block")
		}
		if lines := logLines(t, logFile, 1); lines[0] != "Healthy" {
			t.Fatalf("chain stopped after skipped hook (%v), want continue", lines)
		}
		execs := store.recordedExecutions()
		if len(execs) != 2 || execs[0].Outcome != OutcomeError || execs[0].HookID != "g-broken" {
			t.Fatalf("audit = %+v, want an error row for the skipped hook then allow", execs)
		}
	})

	t.Run("skipped blocking gate fails closed", func(t *testing.T) {
		broken := logFileHook(t, "/unused/log", "g-gate", "Old Gate", EventPreToolUse, "*")
		broken.HandlerType = "quantum"
		broken.OnFailure = "block"
		store := &fakeHookStore{global: []Definition{broken}}
		d := NewDispatcher(store, NewRegistry())
		resolved, err := d.Resolve(context.Background(), "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, resultJSON := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if !blocked {
			t.Fatal("skipped gate with on_failure=block must fail closed")
		}
		if want := `"hook":"Old Gate"`; !strings.Contains(resultJSON, want) {
			t.Errorf("block JSON = %q, want it to name the skipped hook", resultJSON)
		}
	})

	t.Run("unknown event, uncompilable matcher, and uncompilable script never dispatch", func(t *testing.T) {
		logFile := filepath.Join(t.TempDir(), "hooks.log")
		badEvent := logFileHook(t, logFile, "g-event", "Wrong Event", "wormhole_opened", "*")
		badMatcher := logFileHook(t, logFile, "g-matcher", "Bad Matcher", EventPreToolUse, "(")
		badScript := allowScriptHook("g-script", "Bad Script", EventPreToolUse, "*")
		badScript.Config = scriptCfg(wrapBody(`return {{{`))
		store := &fakeHookStore{global: []Definition{badEvent, badMatcher, badScript}}

		d := NewDispatcher(store, NewRegistry())
		resolved, err := d.Resolve(context.Background(), "ag-1")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
		if blocked {
			t.Fatal("unexpected block from broken hooks with default on_failure")
		}
		if _, err := os.Stat(logFile); err == nil {
			t.Fatalf("handlers executed for broken hooks (%q), want 0 executions", mustReadFile(t, logFile))
		}
		// The unknown-event hook never fires on any known event; the other
		// two fail per on_failure (allow) without a handler call.
		if rows := store.recordedExecutions(); len(rows) != 2 {
			t.Fatalf("audit rows = %d, want 2 (the unknown-event hook is filtered by event)", len(rows))
		}
	})
}

// TestDispatcher_DisabledHooksResolveToNothing: enabled=false rows never join
// the chain.
func TestDispatcher_DisabledHooksResolveToNothing(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hooks.log")
	paused := logFileHook(t, logFile, "g-1", "Paused", EventPreToolUse, "*")
	paused.Enabled = false
	store := &fakeHookStore{global: []Definition{paused}}
	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HasHooks() {
		t.Fatal("disabled hook joined the chain")
	}
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool()); blocked {
		t.Fatal("disabled hook blocked")
	}
	if rows := store.recordedExecutions(); len(rows) != 0 {
		t.Fatalf("audit rows = %d, want 0", len(rows))
	}
}

// TestDispatcher_NoopDispatcher pins the runner default: an empty chain,
// every seam a no-op, no errors.
func TestDispatcher_NoopDispatcher(t *testing.T) {
	resolved, err := NewDispatcher(nil, nil).Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.HasHooks() {
		t.Fatal("noop dispatcher resolved hooks")
	}
	if blocked, _ := resolved.PreToolUse(context.Background(), dispatcherEvent(), dispatcherTool()); blocked {
		t.Fatal("noop dispatcher blocked")
	}
	if blocked, _, _ := resolved.EvaluatePromptSubmission(context.Background(), dispatcherEvent()); blocked {
		t.Fatal("noop dispatcher blocked a prompt")
	}
	resolved.ObserveRunStarted(context.Background(), dispatcherEvent())
	resolved.PostToolUse(context.Background(), dispatcherEvent(), dispatcherTool())
	resolved.RunFinished(context.Background(), dispatcherEvent(), "completed")
}

// TestDispatcher_EvaluatePromptSubmission pins the blocking prompt seam:
// origin matchers gate the evaluation, first block wins with the hook's
// reason, and a non-matching origin never evaluates the hook.
func TestDispatcher_EvaluatePromptSubmission(t *testing.T) {
	store := &fakeHookStore{
		global: []Definition{
			blockingScriptHook("g-1", "Scheduler Gate", EventUserPromptSubmit, "scheduler", "scheduled prompts need review"),
		},
	}
	d := NewDispatcher(store, NewRegistry())
	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	blocked, hookName, reason := resolved.EvaluatePromptSubmission(context.Background(), dispatcherEvent())
	if blocked || hookName != "" || reason != "" {
		t.Fatalf("user-origin prompt: (%v, %q, %q), want no evaluation", blocked, hookName, reason)
	}
	if rows := store.recordedExecutions(); len(rows) != 0 {
		t.Fatalf("non-matching origin executed hooks (rows=%d), want none", len(rows))
	}

	ev := dispatcherEvent()
	ev.Origin = "scheduler"
	blocked, hookName, reason = resolved.EvaluatePromptSubmission(context.Background(), ev)
	if !blocked || hookName != "Scheduler Gate" || reason != "scheduled prompts need review" {
		t.Fatalf("scheduler-origin prompt: (%v, %q, %q), want block by Scheduler Gate", blocked, hookName, reason)
	}
	if rows := store.recordedExecutions(); len(rows) != 1 || rows[0].Outcome != OutcomeBlocked {
		t.Fatalf("audit = %+v, want one blocked row", rows)
	}
}

// TestDispatcher_ScriptHookCompileCachedOnResolved pins the resolver half of
// the compile contract: append compiles the program once per Resolve, a fresh
// Resolve compiles fresh, and an uncompilable script is the graceful skip.
func TestDispatcher_ScriptHookCompileCachedOnResolved(t *testing.T) {
	store := &fakeHookStore{
		global: []Definition{
			allowScriptHook("g-1", "script-gate", EventPreToolUse, "*"),
		},
	}
	d := NewDispatcher(store, NewRegistry())

	resolved, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.hooks) != 1 {
		t.Fatalf("resolved %d hooks, want 1", len(resolved.hooks))
	}
	prog := resolved.hooks[0].scriptProg
	if prog == nil {
		t.Fatal("script hook resolved without a compiled program")
	}

	// Re-resolve: a new run compiles a fresh program.
	resolved2, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve 2: %v", err)
	}
	if resolved2.hooks[0].scriptProg == prog {
		t.Error("second Resolve reused the first run's program; compile cache must be per run")
	}

	// An uncompilable stored script is the graceful skip.
	store.global[0].Config = scriptCfg(wrapBody(`return {{{`))
	resolved3, err := d.Resolve(context.Background(), "ag-1")
	if err != nil {
		t.Fatalf("Resolve 3: %v", err)
	}
	if resolved3.hooks[0].skipErr == "" {
		t.Error("uncompilable script did not produce a skip error")
	}
}

// TestDispatcher_StoreFailureFailsResolve pins the store-error rule: a
// failing tier read is infrastructure failure and Resolve returns the error
// instead of silently running ungated.
func TestDispatcher_StoreFailureFailsResolve(t *testing.T) {
	d := NewDispatcher(failingStore{}, NewRegistry())
	if _, err := d.Resolve(context.Background(), "ag-1"); err == nil {
		t.Fatal("Resolve() err = nil, want the store read failure")
	}
}

type failingStore struct{}

func (failingStore) AssignedHooks(context.Context, string) ([]Definition, error) {
	return nil, context.DeadlineExceeded
}

func (failingStore) GlobalHooks(context.Context) ([]Definition, error) {
	return nil, context.DeadlineExceeded
}

func (failingStore) RecordExecution(context.Context, Execution) error { return nil }
