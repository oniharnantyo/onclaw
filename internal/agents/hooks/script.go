package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The script handler (D22): author-written JavaScript run in-process through
// goja — pure computation over the event, the decide lane beside command's do
// lane. Config shape (pinned alongside the shapes in secrets.go — script
// carries no secret rows, no cwd, no interpreter):
//
//	script: {"script":"(function(input){ return {decision:'block', reason:'…'} })"}
//
// The VM binds NOTHING except a console (log/error/warn into a capped
// buffer): goja ships no network, filesystem, environment, or process
// globals and none are added — the server environment never crosses in.
// Budget: the hook's timeout drives vm.Interrupt off the delivery context
// (Registry.Execute bounds that context by the budget), so a runaway loop
// errors instead of hanging; run cancellation kills the VM the same way.
// The vendored goja exposes no runtime memory-limit API, so D22's allocation
// backstop is not implementable on this version — the 64 KB source cap
// (domain.MaxHookScriptBytes) and the time interrupt are the enforced
// bounds. Audit and error text carry hook name + error only: goja renders
// positions, never source text, so the script body cannot leak into a
// message.
const (
	// scriptFilename labels stack frames and compile errors in place of any
	// real file — neutral, never the hook name or the script.
	scriptFilename = "hook-script.js"

	// maxScriptConsoleEntries and maxScriptConsoleEntryBytes bound the
	// captured console buffer surfaced by the D18 Test panel.
	maxScriptConsoleEntries    = 16
	maxScriptConsoleEntryBytes = 512
)

// The invocation contract (D22): the stored/edited script IS the complete
// handler function — the invoked function expression `(function(input){ … })`,
// wrapper visible in the editor. The runtime compiles the stored source
// as-is, evaluates the program, asserts the completion value is callable
// (goja.AssertFunction), and calls it with the event object. A non-callable
// completion (a deleted or altered wrapper, an expression form) is a handler
// failure; save-time validation (ValidateScript) requires the wrapper shape so
// the common case fails at save, never silently allows.

// scriptHookConfig is the decrypted script handler config shape.
type scriptHookConfig struct {
	Script string `json:"script"`
}

// The wrapper shape gate (D22 save validation): the source must open with
// `(function(input){` and close with `})` — tolerant of surrounding
// whitespace, prettier spacing inside the parens/braces, and an optional
// trailing semicolon after the closing paren. The editor displays exactly
// this text, so a deleted or altered wrapper is rejected at save with the
// pinned message before any parse runs.
var (
	scriptWrapperPrefixRe = regexp.MustCompile(`^\s*\(function\s*\(\s*input\s*\)\s*\{`)
	scriptWrapperSuffixRe = regexp.MustCompile(`\}\s*\)\s*;?\s*$`)
)

// decodeScriptConfig extracts the configured JavaScript source.
func decodeScriptConfig(cfg json.RawMessage) (string, error) {
	var conf scriptHookConfig
	if err := json.Unmarshal(cfg, &conf); err != nil {
		return "", fmt.Errorf("hook script: decode config: %w", err)
	}
	if strings.TrimSpace(conf.Script) == "" {
		return "", fmt.Errorf("hook script: config.script is required")
	}
	if len(conf.Script) > domain.MaxHookScriptBytes {
		return "", fmt.Errorf("hook script: config.script exceeds %d bytes", domain.MaxHookScriptBytes)
	}
	return conf.Script, nil
}

// compileScriptConfig compiles a stored script config to its program — the
// per-run cache built by the dispatcher (D22 compile once per run). The
// stored source is the complete `(function(input){ … })` function expression
// and is compiled as-is.
func compileScriptConfig(cfg json.RawMessage) (*goja.Program, error) {
	script, err := decodeScriptConfig(cfg)
	if err != nil {
		return nil, err
	}
	return goja.Compile(scriptFilename, script, false)
}

// ValidateScript validates script WITHOUT executing it — the save-time
// counterpart of the runtime contract (D22, D23). The wrapper shape gate runs
// first so a deleted or altered `(function(input){ … })` wrapper fails at
// save, never silently allows. Syntax errors are reported with TRUE
// coordinates: the editor displays exactly this text, so no offset is
// applied.
func ValidateScript(script string) error {
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("%w: config.script: is required", domain.ErrInvalid)
	}
	if len(script) > domain.MaxHookScriptBytes {
		return fmt.Errorf("%w: config.script: %d bytes exceeds maximum of %d", domain.ErrInvalid, len(script), domain.MaxHookScriptBytes)
	}
	if !scriptWrapperPrefixRe.MatchString(script) || !scriptWrapperSuffixRe.MatchString(script) {
		return fmt.Errorf("%w: config.script: keep the (function(input){ … }) wrapper — edit only the body inside", domain.ErrInvalid)
	}

	// Parse first: this phase carries the structured first-error position
	// (goja.Compile flattens the parser's error list into a string).
	if _, err := parser.ParseFile(nil, scriptFilename, script, 0); err != nil {
		var list parser.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			first := list[0]
			return fmt.Errorf("%w: config.script: line %d, column %d: %s", domain.ErrInvalid,
				first.Position.Line, first.Position.Column, first.Message)
		}
		return fmt.Errorf("%w: config.script: %v", domain.ErrInvalid, err)
	}

	// Compile catches the rarer compiler-phase syntax errors.
	if _, err := goja.Compile(scriptFilename, script, false); err != nil {
		var syntaxErr *goja.CompilerSyntaxError
		if errors.As(err, &syntaxErr) && syntaxErr.File != nil {
			pos := syntaxErr.File.Position(syntaxErr.Offset)
			return fmt.Errorf("%w: config.script: line %d, column %d: %s", domain.ErrInvalid, pos.Line, pos.Column, syntaxErr.Message)
		}
		return fmt.Errorf("%w: config.script: %v", domain.ErrInvalid, err)
	}
	return nil
}

// executeScript runs the hook script per the D22 contract: the stored source
// is the complete `(function(input){ … })` function expression, compiled
// as-is; the program's completion value must be callable and is called with
// the (size-capped) event object; a return of {decision: "block", reason}
// blocks, ANY other return allows, and an uncaught exception is a failure
// for the hook's on_failure policy.
func (r *Registry) executeScript(ctx context.Context, cfg json.RawMessage, ev Event, hook HookRef, _ time.Duration) (Result, error) {
	script, err := decodeScriptConfig(cfg)
	if err != nil {
		return Result{}, err
	}

	prog := hook.CompiledScript // D22: compile once per run when dispatched
	if prog == nil {
		if prog, err = goja.Compile(scriptFilename, script, false); err != nil {
			return Result{}, fmt.Errorf("hook script %q: %v", hook.Name, err)
		}
	}

	vm := goja.New()
	console := &scriptConsole{}
	if err := installScriptConsole(vm, console); err != nil {
		return Result{}, fmt.Errorf("hook script %q: install console: %v", hook.Name, err)
	}

	// input = the same object the command handler puts on stdin (D22):
	// truncated event, marshaled, unmarshaled into a VM value.
	payload, err := json.Marshal(truncateEventForStdin(ev))
	if err != nil {
		return Result{}, fmt.Errorf("hook script %q: encode event: %w", hook.Name, err)
	}
	var data interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return Result{}, fmt.Errorf("hook script %q: decode event: %w", hook.Name, err)
	}
	input := vm.ToValue(data)

	// The interrupt watcher: budget expiry (the ctx is budget-bounded
	// upstream) and run cancellation both stop a runaway loop at the next
	// loop back-edge or call.
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(errScriptInterrupted)
		case <-stop:
		}
	}()
	defer close(stop)
	defer vm.ClearInterrupt()

	ret, runErr := vm.RunProgram(prog)
	if runErr != nil {
		return Result{ConsoleLines: console.lines}, fmt.Errorf("hook script %q failed: %v", hook.Name, runErr)
	}
	fn, ok := goja.AssertFunction(ret)
	if !ok {
		return Result{ConsoleLines: console.lines}, fmt.Errorf("hook script %q does not evaluate to the handler function (keep the (function(input){ … }) wrapper)", hook.Name)
	}
	ret, callErr := fn(goja.Undefined(), input)

	if callErr != nil {
		var interrupted *goja.InterruptedError
		if errors.As(callErr, &interrupted) && ctx.Err() != nil {
			return Result{ConsoleLines: console.lines}, fmt.Errorf("hook script %q did not finish within its budget: %w", hook.Name, ctx.Err())
		}
		// goja exception text carries message + file:line:col positions,
		// never source text (verified against goja's StackFrame renderer) —
		// safe to surface without echoing the script body.
		return Result{ConsoleLines: console.lines}, fmt.Errorf("hook script %q failed: %v", hook.Name, callErr)
	}

	return scriptDecisionResult(ret, console.lines)
}

// errScriptInterrupted is the value handed to vm.Interrupt; it surfaces only
// through InterruptedError, which the timeout path translates into a
// budget-expiry error.
var errScriptInterrupted = errors.New("script execution interrupted")

// scriptDecisionResult applies the decision contract to the script's return
// value: decision exactly "block" (with an optional string reason) blocks;
// undefined, non-objects, other decisions, and other shapes all allow.
func scriptDecisionResult(ret goja.Value, consoleLines []string) (Result, error) {
	obj, ok := ret.Export().(map[string]interface{})
	if !ok {
		return Result{Decision: DecisionAllow, ConsoleLines: consoleLines}, nil
	}
	decision, ok := obj["decision"].(string)
	if !ok || decision != DecisionBlock {
		return Result{Decision: DecisionAllow, ConsoleLines: consoleLines}, nil
	}
	reason, _ := obj["reason"].(string)
	return Result{Decision: DecisionBlock, Reason: truncateReason(reason), ConsoleLines: consoleLines}, nil
}

// scriptConsole is the capped in-memory console buffer (D22): the only global
// the VM gains. Entries past the cap are dropped; each entry is truncated.
type scriptConsole struct {
	lines []string
}

// installScriptConsole binds console.log/error/warn to the buffer. goja
// exposes no other host globals, so this is the entire sandbox surface.
func installScriptConsole(vm *goja.Runtime, console *scriptConsole) error {
	obj := vm.NewObject()
	for _, name := range []string{"log", "error", "warn"} {
		if err := obj.Set(name, func(call goja.FunctionCall) goja.Value {
			console.write(call.Arguments)
			return goja.Undefined()
		}); err != nil {
			return err
		}
	}
	vm.Set("console", obj)
	return nil
}

// write formats one console call into the buffer: objects as JSON, other
// values via their string form, joined with spaces, then truncated. The VM
// is single-threaded, so no synchronization is needed.
func (c *scriptConsole) write(args []goja.Value) {
	if len(c.lines) >= maxScriptConsoleEntries {
		return
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, scriptConsoleValue(arg))
	}
	c.lines = append(c.lines, truncateStringBytes(strings.Join(parts, " "), maxScriptConsoleEntryBytes))
}

// scriptConsoleValue renders one console argument: exported objects and
// arrays as JSON (functions and anything unexportable fall back to the JS
// string form).
func scriptConsoleValue(arg goja.Value) string {
	if obj, ok := arg.(*goja.Object); ok && obj.ClassName() != "Function" {
		if b, err := json.Marshal(obj.Export()); err == nil {
			return truncateStringBytes(string(b), maxScriptConsoleEntryBytes)
		}
	}
	return truncateStringBytes(arg.String(), maxScriptConsoleEntryBytes)
}
