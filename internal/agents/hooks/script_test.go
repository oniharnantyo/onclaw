package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// wrapBody lifts an author body into the complete stored function expression —
// the D22 contract: the stored/edited script IS `(function(input){ … })` and
// the runtime compiles it as-is.
func wrapBody(body string) string {
	return "(function(input){\n" + body + "\n})"
}

// scriptCfg builds a script handler config from the given JavaScript source.
func scriptCfg(script string) string {
	return `{"script":` + mustJSONString(script) + `}`
}

func jsonRawScript(script string) json.RawMessage {
	return json.RawMessage(scriptCfg(script))
}

func runScript(t *testing.T, reg *Registry, script string, budget time.Duration) (Result, error) {
	t.Helper()
	return reg.Execute(context.Background(), domain.HookHandlerScript,
		jsonRawScript(script), testEvent(), testHook(), budget)
}

// TestScriptHandler_BlockReturn pins the decision contract's block row: the
// D22 gallery script — regex over the serialized tool args — blocks with its
// reason (the same shape the command handler's stdin JSON carries).
func TestScriptHandler_BlockReturn(t *testing.T) {
	reg := NewRegistry()
	script := wrapBody(`if (input.tool && /rm -rf/.test(input.tool.args))
	return { decision: "block", reason: "destructive command" };`)

	res, err := runScript(t, reg, script, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Decision != DecisionBlock {
		t.Errorf("decision = %q, want block", res.Decision)
	}
	if res.Reason != "destructive command" {
		t.Errorf("reason = %q, want destructive command", res.Reason)
	}
}

// TestScriptHandler_DecisionTable covers the rest of the contract: ANY other
// return — undefined, empty object, explicit allow, non-object, or a decision
// value that is not exactly "block" — allows.
func TestScriptHandler_DecisionTable(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"no return", `input.origin;`},
		{"bare undefined", `return undefined;`},
		{"empty object", `return {};`},
		{"explicit allow", `return {decision: "allow"};`},
		{"allow with reason", `return {decision: "allow", reason: "fine"};`},
		{"wrong case decision", `return {decision: "Block", reason: "nope"};`},
		{"string return", `return "block";`},
		{"number return", `return 42;`},
		{"null return", `return null;`},
		{"boolean return", `return true;`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := runScript(t, NewRegistry(), wrapBody(tt.script), 5*time.Second)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Decision != DecisionAllow {
				t.Errorf("decision = %q, want allow", res.Decision)
			}
			if res.Reason != "" {
				t.Errorf("reason = %q, want empty on allow", res.Reason)
			}
		})
	}
}

// TestScriptHandler_BlockReasonTruncated pins the reason cap at the command
// handler's truncation length.
func TestScriptHandler_BlockReasonTruncated(t *testing.T) {
	script := wrapBody(`return {decision: "block", reason: "x".repeat(10000)};`)
	res, err := runScript(t, NewRegistry(), script, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Decision != DecisionBlock {
		t.Fatalf("decision = %q, want block", res.Decision)
	}
	if len(res.Reason) != maxReasonBytes {
		t.Errorf("reason length = %d, want %d", len(res.Reason), maxReasonBytes)
	}
}

// TestScriptHandler_ThrowIsFailure pins the failure row: an uncaught
// exception is a returned error (the caller's on_failure), and the error text
// carries the hook name and the exception — never the script body.
func TestScriptHandler_ThrowIsFailure(t *testing.T) {
	script := wrapBody(`throw new Error("boom");`)
	_, err := runScript(t, NewRegistry(), script, 5*time.Second)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "hook script \"guard\"") {
		t.Errorf("error %q does not name the hook", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not carry the exception message", err)
	}
	if strings.Contains(err.Error(), "throw new Error") {
		t.Errorf("error %q echoes the script body", err)
	}
}

// TestScriptHandler_SyntaxErrorIsFailure: a stored script that never compiled
// (written by an old binary) fails per delivery on the per-delivery compile.
func TestScriptHandler_SyntaxErrorIsFailure(t *testing.T) {
	_, err := runScript(t, NewRegistry(), wrapBody(`return {decision: `), 5*time.Second)
	if err == nil {
		t.Fatal("expected a compile error, got nil")
	}
	if !strings.Contains(err.Error(), "SyntaxError") {
		t.Errorf("error %q is not a syntax error", err)
	}
}

// TestScriptHandler_NonCallableCompletionIsFailure pins the invocation
// contract's callable assert: a stored source that COMPILES but evaluates to
// a non-callable completion value (e.g. a body-only legacy row, since the
// wrapper is what makes the completion a function) fails with the
// keep-the-wrapper message, never echoing the body.
func TestScriptHandler_NonCallableCompletionIsFailure(t *testing.T) {
	_, err := runScript(t, NewRegistry(), `42;`, 5*time.Second)
	if err == nil {
		t.Fatal("expected a non-callable failure, got nil")
	}
	if !strings.Contains(err.Error(), "does not evaluate to the handler function") {
		t.Errorf("error %q lacks the callable-assert message", err)
	}
	if !strings.Contains(err.Error(), "(function(input){ … })") {
		t.Errorf("error %q lacks the keep-the-wrapper hint", err)
	}
	if strings.Contains(err.Error(), "42") {
		t.Errorf("error %q echoes the script body", err)
	}
}

// TestScriptHandler_TimeoutInterrupt pins the budget row: a runaway loop must
// return a timeout error (vm.Interrupt off the delivery context), not hang.
func TestScriptHandler_TimeoutInterrupt(t *testing.T) {
	started := time.Now()
	_, err := runScript(t, NewRegistry(), wrapBody(`while (true) {}`), 250*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "did not finish within its budget") {
		t.Errorf("error %q is not a budget-expiry error", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("interrupt took %v; the loop was not stopped", elapsed)
	}
}

// TestScriptHandler_ConsoleCapturedAndCapped pins the console capture:
// entries accumulate in order, objects render as JSON, entries past the cap
// are dropped, and oversized entries are truncated.
func TestScriptHandler_ConsoleCapturedAndCapped(t *testing.T) {
	script := wrapBody(`
		console.log("first");
		console.error({tool: input.tool.name});
		console.warn("third", 42);
		for (let i = 0; i < 100; i++) console.log("entry", i);
		return {decision: "allow"};`)
	res, err := runScript(t, NewRegistry(), script, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.ConsoleLines) != maxScriptConsoleEntries {
		t.Fatalf("got %d console lines, want %d (capped)", len(res.ConsoleLines), maxScriptConsoleEntries)
	}
	if res.ConsoleLines[0] != "first" {
		t.Errorf("line[0] = %q, want first", res.ConsoleLines[0])
	}
	if !strings.Contains(res.ConsoleLines[1], `"tool":"shell.run"`) {
		t.Errorf("line[1] = %q, want JSON-rendered object", res.ConsoleLines[1])
	}
	if !strings.Contains(res.ConsoleLines[2], "third 42") {
		t.Errorf("line[2] = %q, want spaced join of args", res.ConsoleLines[2])
	}
	for i, line := range res.ConsoleLines {
		if len(line) > maxScriptConsoleEntryBytes {
			t.Errorf("line[%d] is %d bytes, want <= %d", i, len(line), maxScriptConsoleEntryBytes)
		}
	}
}

// TestScriptHandler_ConsoleOnFailure: captured console output rides the
// Result even when the script fails, so the Test panel shows the trail.
func TestScriptHandler_ConsoleOnFailure(t *testing.T) {
	res, err := runScript(t, NewRegistry(), wrapBody(`console.log("before the crash"); nope();`), 5*time.Second)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if len(res.ConsoleLines) != 1 || res.ConsoleLines[0] != "before the crash" {
		t.Errorf("console lines = %v, want the pre-crash entry", res.ConsoleLines)
	}
}

// TestScriptHandler_Sandbox pins the sandbox: the VM binds NOTHING except
// console — no network, filesystem, environment, or process globals exist.
func TestScriptHandler_Sandbox(t *testing.T) {
	reg := NewRegistry()

	t.Run("host globals are undefined", func(t *testing.T) {
		script := wrapBody(`
			const probes = ["fetch", "process", "require", "XMLHttpRequest",
				"WebSocket", "localStorage", "importScripts"];
			for (const p of probes) {
				if (typeof globalThis[p] !== "undefined") {
					return {decision: "block", reason: "leaked global: " + p};
				}
			}
			return {decision: "allow"};`)
		res, err := runScript(t, reg, script, 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("decision = %q (%s), want allow — a host global leaked", res.Decision, res.Reason)
		}
	})

	t.Run("undeclared global reference fails", func(t *testing.T) {
		_, err := runScript(t, reg, wrapBody(`return fetch("https://example.com");`), 5*time.Second)
		if err == nil {
			t.Fatal("expected a ReferenceError failure, got nil")
		}
		if !strings.Contains(err.Error(), "fetch") {
			t.Errorf("error %q does not name the missing global", err)
		}
	})

	t.Run("process env unreachable", func(t *testing.T) {
		// Even through the global object there is no process/env surface.
		script := wrapBody(`return typeof globalThis.process === "undefined" ? {decision: "allow"} : {decision: "block"};`)
		res, err := runScript(t, reg, script, 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("decision = %q, want allow — process is reachable", res.Decision)
		}
	})
}

// TestScriptHandler_InputShape pins the invocation contract's input object:
// the same fields the command handler receives on stdin, with tool.args as
// the SERIALIZED arguments JSON string.
func TestScriptHandler_InputShape(t *testing.T) {
	script := wrapBody(`
		const checks = [
			input.event === "pre_tool_use",
			input.origin === "user",
			input.session_id === "sess-1",
			input.workspace.name === "Acme",
			input.agent.name === "Atlas",
			typeof input.tool.args === "string",
			input.tool.args.indexOf("rm -rf") !== -1,
			input.tool.call_id === "call-1",
			input.user === undefined
		];
		const failed = checks.filter(c => !c).length;
		return failed === 0
			? {decision: "allow"}
			: {decision: "block", reason: "input shape wrong: " + failed};`)
	res, err := runScript(t, NewRegistry(), script, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("decision = %q (%s), want allow — the input shape diverged", res.Decision, res.Reason)
	}
}

// TestScriptHandler_OversizedScriptRejected: the byte cap is enforced at
// runtime too (rows written before the cap existed).
func TestScriptHandler_OversizedScriptRejected(t *testing.T) {
	oversized := wrapBody("//" + strings.Repeat("x", domain.MaxHookScriptBytes))
	_, err := runScript(t, NewRegistry(), oversized, 5*time.Second)
	if err == nil {
		t.Fatal("expected an oversized-script error, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("exceeds %d bytes", domain.MaxHookScriptBytes)) {
		t.Errorf("error %q does not name the cap", err)
	}
}

// TestScriptHandler_KillSwitch pins the D22 operator kill switch: off →
// ErrScriptDisabled, default on, and the option turns it back on.
func TestScriptHandler_KillSwitch(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		reg := NewRegistry(WithScriptEnabled(false))
		_, err := runScript(t, reg, wrapBody(`return {decision: "allow"};`), 5*time.Second)
		if !errors.Is(err, ErrScriptDisabled) {
			t.Errorf("err = %v, want ErrScriptDisabled", err)
		}
		if !strings.Contains(err.Error(), "script hooks are disabled") {
			t.Errorf("error %q lacks the disabled explanation", err)
		}
	})

	t.Run("enabled by default", func(t *testing.T) {
		res, err := runScript(t, NewRegistry(), wrapBody(`return {decision: "allow"};`), 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("decision = %q, want allow", res.Decision)
		}
	})

	t.Run("disabled ignores script content", func(t *testing.T) {
		// The gate fires before any compile: even a broken script gets the
		// disabled error, not a syntax error.
		reg := NewRegistry(WithScriptEnabled(false))
		_, err := runScript(t, reg, wrapBody(`return {{{`), 5*time.Second)
		if !errors.Is(err, ErrScriptDisabled) {
			t.Errorf("err = %v, want ErrScriptDisabled", err)
		}
	})
}

// TestScriptHandler_PrecompiledProgram pins the D22 compile-once-per-run
// threading: a program compiled at resolve time (carried on HookRef) is used
// instead of recompiling per delivery.
func TestScriptHandler_PrecompiledProgram(t *testing.T) {
	prog, err := goja.Compile(scriptFilename, wrapBody(`return {decision: "block", reason: "from cache"};`), false)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	reg := NewRegistry()
	res, err := reg.Execute(context.Background(), domain.HookHandlerScript,
		jsonRawScript(wrapBody(`return {decision: "allow"};`)), // diverges from the cached program on purpose
		testEvent(), HookRef{Name: "guard", Level: domain.HookLevelWorkspace, CompiledScript: prog}, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Decision != DecisionBlock || res.Reason != "from cache" {
		t.Errorf("result = %+v, want the precompiled program's decision", res)
	}
}

// TestDispatcher_ScriptHookCompileCachedOnResolved pins the resolver half of
// D22: append compiles the program once per Resolve, deliveries reuse it, and
// a fresh Resolve compiles fresh.
func TestDispatcher_ScriptHookCompileCachedOnResolved(t *testing.T) {
	hs := &fakeHookStore{
		workspaceHooks: []domain.WorkspaceHook{{
			ID:          "whk-1",
			WorkspaceID: "ws-1",
			HookBase: domain.HookBase{
				Name:        "script-gate",
				Event:       domain.HookEventPreToolUse,
				Matcher:     matchAll(),
				HandlerType: domain.HookHandlerScript,
				Config:      json.RawMessage(scriptCfg(wrapBody(`return {decision: "allow"};`))),
				TimeoutMS:   domain.DefaultHookTimeoutMS,
				Enabled:     true,
			},
		}},
	}
	d := NewDispatcher(hs, NewRegistry())

	resolved, err := d.Resolve(context.Background(), "ws-1", "ag-1")
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
	resolved2, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve 2: %v", err)
	}
	if resolved2.hooks[0].scriptProg == prog {
		t.Error("second Resolve reused the first run's program; compile cache must be per run")
	}

	// An uncompilable stored script is the D7 graceful skip.
	hs.workspaceHooks[0].Config = json.RawMessage(scriptCfg(wrapBody(`return {{{`)))
	resolved3, err := d.Resolve(context.Background(), "ws-1", "ag-1")
	if err != nil {
		t.Fatalf("Resolve 3: %v", err)
	}
	if resolved3.hooks[0].skipErr == "" {
		t.Error("uncompilable script did not produce a skip error")
	}
	if statuses := hs.recordedStatuses(); len(statuses) != 1 || statuses[0].Status != domain.HookStatusError {
		t.Errorf("statuses = %v, want one error status write", statuses)
	}
}

// TestValidateScript pins the save-time helper (D22/D23): the wrapper shape
// gate rejects a deleted or altered `(function(input){ … })` with the pinned
// message, then compile WITHOUT executing reports the first syntax error at
// its TRUE coordinates (the editor displays exactly this text).
func TestValidateScript(t *testing.T) {
	t.Run("good script", func(t *testing.T) {
		if err := ValidateScript(wrapBody(`if (input.origin === "cron") return {decision: "block"};`)); err != nil {
			t.Errorf("ValidateScript: %v", err)
		}
	})

	t.Run("empty script", func(t *testing.T) {
		err := ValidateScript("   ")
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		if !strings.Contains(err.Error(), "config.script: is required") {
			t.Errorf("error %q lacks the required message", err)
		}
	})

	t.Run("missing wrapper is rejected with the pinned message", func(t *testing.T) {
		err := ValidateScript(`return {decision: "block"};`)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		if !strings.Contains(err.Error(), "config.script: keep the (function(input){ … }) wrapper — edit only the body inside") {
			t.Errorf("error %q lacks the pinned wrapper message", err)
		}
	})

	t.Run("arrow function form is rejected with the pinned message", func(t *testing.T) {
		err := ValidateScript(`(input) => { return {decision: "block"}; }`)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		if !strings.Contains(err.Error(), "config.script: keep the (function(input){ … }) wrapper — edit only the body inside") {
			t.Errorf("error %q lacks the pinned wrapper message", err)
		}
	})

	t.Run("trailing semicolon after the wrapper is accepted", func(t *testing.T) {
		if err := ValidateScript(wrapBody(`return {decision: "allow"};`) + ";"); err != nil {
			t.Errorf("ValidateScript: %v", err)
		}
	})

	t.Run("prettier-spaced wrapper is accepted", func(t *testing.T) {
		script := "(function(input) {\n  if (input.origin === \"cron\") return {decision: \"block\"};\n});"
		if err := ValidateScript(script); err != nil {
			t.Errorf("ValidateScript: %v", err)
		}
	})

	t.Run("syntax error carries TRUE line and column", func(t *testing.T) {
		// The editor displays exactly this text, so the bad line reports its
		// raw coordinate — here raw line 3, not shifted by any wrapper offset.
		err := ValidateScript("(function(input){\nconst ok = 1;\nconst bad = ;\n})")
		if err == nil {
			t.Fatal("expected a syntax error, got nil")
		}
		if !strings.Contains(err.Error(), "line 3, column") {
			t.Errorf("error %q does not name line 3", err)
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("err = %v, want ErrInvalid", err)
		}
		if strings.Contains(err.Error(), "const bad") {
			t.Errorf("error %q echoes the script body", err)
		}
	})

	t.Run("syntax error on the first body line", func(t *testing.T) {
		// `const` is always reserved, so the parser fails on raw line 2 — the
		// line right after the opening wrapper line.
		err := ValidateScript("(function(input){\nconst = 1;\n})")
		if err == nil {
			t.Fatal("expected a syntax error, got nil")
		}
		if !strings.Contains(err.Error(), "line 2, column") {
			t.Errorf("error %q does not name line 2", err)
		}
		if strings.Contains(err.Error(), "const = 1") {
			t.Errorf("error %q echoes the script body", err)
		}
	})

	t.Run("oversized script", func(t *testing.T) {
		err := ValidateScript(strings.Repeat("x", domain.MaxHookScriptBytes+1))
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid", err)
		}
		if !strings.Contains(err.Error(), "bytes exceeds maximum") {
			t.Errorf("error %q lacks the cap message", err)
		}
	})
}
