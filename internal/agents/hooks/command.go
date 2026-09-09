package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Command-handler constants (D10).
const (
	// maxStdinStringBytes caps every string value in the stdin payload before
	// it is written to the child.
	maxStdinStringBytes = 64 * 1024
	// maxReasonBytes caps a stderr-derived block reason.
	maxReasonBytes = 256
	// killGraceDelay is how long the child keeps its SIGTERM grace before the
	// forced SIGKILL and pipe teardown.
	killGraceDelay = 2 * time.Second
)

// commandHookConfig is the decrypted command handler config shape (see the
// pinned shapes in secrets.go): exec form only — command plus argument list,
// never a shell string — with name-keyed env rows and an optional working
// directory (never the agent jail; empty means the server's cwd).
type commandHookConfig struct {
	Command string      `json:"command"`
	Args    []string    `json:"args,omitempty"`
	Env     []configRow `json:"env,omitempty"`
	Cwd     string      `json:"cwd,omitempty"`
}

// executeCommand runs the hook program per D10's decision table:
//
//	exit 0                 → allow
//	exit 0 + stdout JSON   → the decision object is honored
//	stdout JSON + exit 2   → exit 2 WINS (Claude Code compatibility)
//	exit 2 + stderr        → block, truncated stderr as the reason
//	exit 2, empty stderr   → block "blocked by hook <name>"
//	any other exit, signal kill, spawn failure, timeout → error
//
// The event payload goes to stdin as JSON, then EOF. The child environment is
// the fixed base (PATH/LANG/TMPDIR) plus the hook's own env rows only — the
// server environment, including DATABASE_URL and ONCLAW_JWT_SECRET, is never
// inherited.
func (r *Registry) executeCommand(ctx context.Context, cfg json.RawMessage, ev Event, hook HookRef, _ time.Duration) (Result, error) {
	var conf commandHookConfig
	if err := json.Unmarshal(cfg, &conf); err != nil {
		return Result{}, fmt.Errorf("hook command: decode config: %w", err)
	}
	if strings.TrimSpace(conf.Command) == "" {
		return Result{}, fmt.Errorf("%w: empty command", ErrSpawnFailed)
	}

	cmd := exec.CommandContext(ctx, conf.Command, conf.Args...)
	// Graceful-then-forced teardown: on budget exhaustion Cancel sends
	// SIGTERM; if the child (or a grandchild holding the pipes) survives
	// killGraceDelay, WaitDelay forces SIGKILL and closes the pipes so no
	// orphan survives the timeout (D10).
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = killGraceDelay

	// Deny-by-default environment: fixed base plus configured rows only.
	cmd.Env = hookChildEnv(conf.Env)
	if conf.Cwd != "" {
		cmd.Dir = conf.Cwd
	}

	// Stdin: the (size-capped) event JSON, then EOF. Stdout/stderr are
	// separate buffers — os/exec drains each pipe in its own goroutine and
	// waits for both, so a chatty child cannot deadlock on a full 64 KB pipe.
	payload, err := json.Marshal(truncateEventForStdin(ev))
	if err != nil {
		return Result{}, fmt.Errorf("hook command: encode event: %w", err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	var exitErr *exec.ExitError
	switch {
	case errors.As(runErr, &exitErr):
		code := exitErr.ExitCode()
		if code == 2 {
			// Exit 2 always blocks; a stdout decision object never
			// overrides it (existing ecosystem scripts keep working).
			reason := truncateReason(strings.TrimSpace(stderrStr))
			if reason == "" {
				reason = fmt.Sprintf("blocked by hook %s", hook.Name)
			}
			return Result{Decision: "block", Reason: reason, ExitCode: &code}, nil
		}
		detail := fmt.Sprintf("hook command %q exited with code %d", conf.Command, code)
		if code < 0 && ctx.Err() != nil {
			detail = fmt.Sprintf("hook command %q terminated on budget expiry (%v)", conf.Command, exitErr)
		}
		return Result{ExitCode: &code}, errors.New(detail)

	case runErr != nil:
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("hook command %q did not finish within its budget: %w", conf.Command, ctx.Err())
		}
		// exec.Error (program not found), permission-denied PathError, and
		// every other start failure: distinguishable so callers can flip the
		// hook's status to error on top of applying on_failure.
		return Result{}, fmt.Errorf("%w: %v", ErrSpawnFailed, runErr)

	default:
		return commandAllowResult(0, stdoutStr), nil
	}
}

// commandAllowResult applies the exit-0 row of the table: a stdout body that
// parses ENTIRELY as a decision object is honored; any other stdout (log
// lines, empty, prose) is not a decision and means allow.
func commandAllowResult(exitCode int, stdout string) Result {
	code := exitCode
	if decision, ok := ParseDecisionJSON([]byte(stdout)); ok {
		return Result{Decision: decision.Decision, Reason: decision.Reason, ExitCode: &code}
	}
	return Result{Decision: "allow", ExitCode: &code}
}

// hookChildEnv builds the child environment: PATH/LANG/TMPDIR from the server
// (only when set) plus the hook's configured rows, which may override the
// base. Nothing else crosses the red line (D10).
func hookChildEnv(rows []configRow) []string {
	env := make([]string, 0, len(rows)+3)
	for _, name := range []string{"PATH", "LANG", "TMPDIR"} {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			env = append(env, name+"="+value)
		}
	}
	for _, row := range rows {
		env = append(env, row.Name+"="+row.Value)
	}
	return env
}

// truncateEventForStdin caps every string value of the payload at
// maxStdinStringBytes before it is written to the child (D10). Refs and the
// tool struct are copied before mutation so the caller's event is untouched.
func truncateEventForStdin(ev Event) Event {
	ev.Event = truncateStdinString(ev.Event)
	ev.DeliveryID = truncateStdinString(ev.DeliveryID)
	ev.Origin = truncateStdinString(ev.Origin)
	ev.Status = truncateStdinString(ev.Status)
	ev.SessionID = truncateStdinString(ev.SessionID)
	ev.Workspace = truncateEventRef(ev.Workspace)
	ev.Agent = truncateEventRef(ev.Agent)
	if ev.User != nil {
		user := truncateEventRef(*ev.User)
		ev.User = &user
	}
	if ev.Tool != nil {
		tool := *ev.Tool
		tool.Name = truncateStdinString(tool.Name)
		tool.CallID = truncateStdinString(tool.CallID)
		tool.Args = truncateStdinString(tool.Args)
		ev.Tool = &tool
	}
	return ev
}

func truncateEventRef(ref EventRef) EventRef {
	ref.ID = truncateStdinString(ref.ID)
	ref.Name = truncateStdinString(ref.Name)
	return ref
}

// truncateStdinString caps s at maxStdinStringBytes, backing off to a valid
// UTF-8 boundary so json.Marshal emits well-formed output.
func truncateStdinString(s string) string {
	return truncateStringBytes(s, maxStdinStringBytes)
}

// truncateReason caps a block reason at maxReasonBytes, same UTF-8 safety.
func truncateReason(s string) string {
	return truncateStringBytes(s, maxReasonBytes)
}

func truncateStringBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
