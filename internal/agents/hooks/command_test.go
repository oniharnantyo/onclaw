package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCommandHandler_DecisionTable(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		wantResult Result
		wantErr    bool
		wantErrIs  error
	}{
		{
			name:       "exit 0, silent stdout: allow",
			script:     `exit 0`,
			wantResult: Result{Decision: DecisionAllow, ExitCode: intPtr(0)},
		},
		{
			name:       "exit 0 with stdout block decision: honored",
			script:     `printf '%s' '{"decision":"block","reason":"no deploys on friday"}'; exit 0`,
			wantResult: Result{Decision: DecisionBlock, Reason: "no deploys on friday", ExitCode: intPtr(0)},
		},
		{
			name:       "exit 0 with stdout allow decision: honored",
			script:     `printf '%s' '{"decision":"allow","reason":"within quota"}'; exit 0`,
			wantResult: Result{Decision: DecisionAllow, Reason: "within quota", ExitCode: intPtr(0)},
		},
		{
			name:       "exit 0 with log lines on stdout: not a decision, allow",
			script:     `echo starting; echo still working; echo done; exit 0`,
			wantResult: Result{Decision: DecisionAllow, ExitCode: intPtr(0)},
		},
		{
			name:       "decision embedded in logs is NOT a decision (strict whole-stdout rule)",
			script:     `echo "starting up"; printf '%s\n' '{"decision":"block","reason":"injected"}'; echo "shutting down"; exit 0`,
			wantResult: Result{Decision: DecisionAllow, ExitCode: intPtr(0)},
		},
		{
			name:    "exit 1: failure",
			script:  `echo boom >&2; exit 1`,
			wantErr: true,
		},
		{
			name:       "exit 2 with stderr: block with truncated stderr",
			script:     `echo denied >&2; exit 2`,
			wantResult: Result{Decision: DecisionBlock, Reason: "denied", ExitCode: intPtr(2)},
		},
		{
			name:       "exit 2 with empty stderr: default reason names the hook",
			script:     `exit 2`,
			wantResult: Result{Decision: DecisionBlock, Reason: "blocked by hook guard", ExitCode: intPtr(2)},
		},
		{
			name:       "stdout decision plus exit 2: exit 2 wins",
			script:     `printf '%s' '{"decision":"allow"}'; echo overridden >&2; exit 2`,
			wantResult: Result{Decision: DecisionBlock, Reason: "overridden", ExitCode: intPtr(2)},
		},
		{
			name:       "stderr block reason is truncated to 256 bytes",
			script:     `printf '%s' "$(head -c 400 /dev/zero | tr '\0' 'x')" >&2; exit 2`,
			wantResult: Result{Decision: DecisionBlock, Reason: strings.Repeat("x", 256), ExitCode: intPtr(2)},
		},
		{
			name:      "spawn failure: distinguishable error",
			script:    "",
			wantErr:   true,
			wantErrIs: ErrSpawnFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var res Result
			var err error
			if tt.wantErrIs == ErrSpawnFailed {
				cfg := json.RawMessage(`{"command":"/nonexistent/buddies/hook-bin","args":["x"]}`)
				res, err = NewRegistry().Execute(context.Background(), HandlerCommand, cfg, testEvent(), testHook(), testBudget)
			} else {
				res, err = runCommandShell(t, tt.script, testBudget)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Execute() err = nil, want error (result %+v)", res)
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("err = %v, want wrapping %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute(): %v", err)
			}
			if res.Decision != tt.wantResult.Decision {
				t.Errorf("decision = %q, want %q", res.Decision, tt.wantResult.Decision)
			}
			if res.Reason != tt.wantResult.Reason {
				t.Errorf("reason = %q, want %q", res.Reason, tt.wantResult.Reason)
			}
			if res.ExitCode == nil || tt.wantResult.ExitCode == nil || *res.ExitCode != *tt.wantResult.ExitCode {
				t.Errorf("exit code = %v, want %v", res.ExitCode, tt.wantResult.ExitCode)
			}
		})
	}
}

func TestCommandHandler_EmptyCommandIsSpawnFailure(t *testing.T) {
	cfg := json.RawMessage(`{"command":"   "}`)
	_, err := NewRegistry().Execute(context.Background(), HandlerCommand, cfg, testEvent(), testHook(), time.Second)
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("err = %v, want ErrSpawnFailed", err)
	}
}

// TestCommandHandler_LegacyShellForm pins the keep-working rule (design D4):
// a config with a bare command line and no args — the shape today's rows
// carry — runs via "sh -c" and keeps its shell semantics (arithmetic and
// redirections work).
func TestCommandHandler_LegacyShellForm(t *testing.T) {
	res, err := runCommandShell(t, `n=$((20+3)); echo shell-formed-$n >&2; exit 2`, testBudget)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if res.Decision != DecisionBlock || res.Reason != "shell-formed-23" {
		t.Errorf("result = %+v, want block/shell-formed-23 from the shell line", res)
	}
	if res.ExitCode == nil || *res.ExitCode != 2 {
		t.Errorf("exit code = %v, want 2", res.ExitCode)
	}
}

// TestCommandHandler_EventJSONOnStdin pins that the child receives the event
// payload as JSON on stdin, closed with EOF.
func TestCommandHandler_EventJSONOnStdin(t *testing.T) {
	script := `payload=$(cat)
case "$payload" in
  *'"delivery_id":"cmd-delivery-7"'*) echo received >&2 ;;
  *) echo corrupted >&2 ;;
esac
exit 2`
	res, err := runCommandShell(t, script, testBudget)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if res.Decision != "block" || res.Reason != "received" {
		t.Errorf("result = %+v, want block/received (stdin payload intact)", res)
	}
}

// TestCommandHandler_StdinStringTruncation pins the 64 KB cap on string
// values: the child counts its stdin bytes and reports the number as the
// block reason, which must equal the size of the truncated payload.
func TestCommandHandler_StdinStringTruncation(t *testing.T) {
	script := `n=$(wc -c | tr -d ' ')
printf '%s' "$n" >&2
exit 2`

	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{
			name:   "200 KB session id truncated to the cap",
			mutate: func(ev *Event) { ev.SessionID = strings.Repeat("a", 200*1024) },
		},
		{
			name:   "oversized multibyte string backs off to a valid boundary",
			mutate: func(ev *Event) { ev.SessionID = strings.Repeat("日", 25000) }, // 75000 bytes of 3-byte runes
		},
		{
			name: "oversized tool args truncated",
			mutate: func(ev *Event) {
				ev.Event = "pre_tool_use"
				ev.Tool = &EventTool{Name: "web_fetch", CallID: "c-1", Args: `{"blob":"` + strings.Repeat("z", 100*1024) + `"}`}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := testEvent()
			tt.mutate(&ev)
			want := len(mustJSONString(truncateEventForStdin(ev)))

			raw, err := json.Marshal(commandHookConfig{Command: "sh", Args: []string{"-c", script}})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			res, err := NewRegistry().Execute(context.Background(), HandlerCommand, raw, ev, testHook(), testBudget)
			if err != nil {
				t.Fatalf("Execute(): %v", err)
			}
			if res.Decision != "block" {
				t.Fatalf("decision = %q, want block", res.Decision)
			}
			if res.Reason != strconv.Itoa(want) {
				t.Errorf("stdin size = %s, want %d (truncation not applied as expected)", res.Reason, want)
			}
		})
	}
}

// TestCommandHandler_EnvContainment pins the red line: the child sees only
// PATH/LANG/TMPDIR plus its configured rows — never the app's environment.
func TestCommandHandler_EnvContainment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:supersecret@db/x")
	t.Setenv("BUDDIES_TEST_SECRET", "signing-hush")
	t.Setenv("BUDDIES_TEST_OTHER", "/some/data/dir") // non-base, non-configured: must not leak either

	script := `leaks=$(env | grep -cE '^(DATABASE_URL|BUDDIES_TEST_SECRET|BUDDIES_TEST_OTHER)=')
own=$(env | grep -c '^HOOK_TOKEN=abc123')
path=$(env | grep -c '^PATH=')
printf '%s_%s_%s' "$leaks" "$own" "$path" >&2
exit 2`

	raw, err := json.Marshal(commandHookConfig{
		Command: "sh",
		Args:    []string{"-c", script},
		Env:     []configRow{{Name: "HOOK_TOKEN", Value: "abc123"}, {Name: "HOOK_MODE", Value: "strict"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := NewRegistry().Execute(context.Background(), HandlerCommand, raw, testEvent(), testHook(), testBudget)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if res.Decision != "block" {
		t.Fatalf("decision = %q, want block", res.Decision)
	}
	if res.Reason != "0_1_1" {
		t.Errorf("env report = %q, want 0_1_1 (no leaks, own row present, PATH present)", res.Reason)
	}
}

// TestCommandHandler_TimeoutTerminatesProcess pins the graceful-then-forced
// teardown: a child that dies on SIGTERM finishes within its budget, and a
// child that ignores SIGTERM is SIGKILLed within the bounded grace period.
func TestCommandHandler_TimeoutTerminatesProcess(t *testing.T) {
	t.Run("graceful SIGTERM", func(t *testing.T) {
		start := time.Now()
		_, err := runCommandShell(t, `sleep 30`, 300*time.Millisecond)
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("timed-out hook must fail")
		}
		if elapsed > 5*time.Second {
			t.Errorf("graceful teardown took %v, want bounded by budget + grace", elapsed)
		}
	})

	t.Run("forced SIGKILL after grace", func(t *testing.T) {
		start := time.Now()
		_, err := runCommandShell(t, `trap '' TERM; while :; do :; done`, 300*time.Millisecond)
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("timed-out hook must fail")
		}
		if elapsed > 10*time.Second {
			t.Errorf("forced teardown took %v, want bounded by budget + %v grace", elapsed, killGraceDelay)
		}
	})
}

// TestCommandHandler_ConcurrentPipeDrain pins the 64 KB pipe-deadlock fix:
// a child filling both pipes well past 64 KB completes and the exit-2 block
// reason still comes from stderr.
func TestCommandHandler_ConcurrentPipeDrain(t *testing.T) {
	script := `head -c 200000 /dev/zero | tr '\0' 'x' >&2
head -c 200000 /dev/zero | tr '\0' 'x'
exit 2`
	res, err := runCommandShell(t, script, testBudget)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if res.Decision != "block" {
		t.Errorf("decision = %q, want block", res.Decision)
	}
	if len(res.Reason) != 256 || strings.Trim(res.Reason, "x") != "" {
		t.Errorf("reason = %.32q..., want 256 x's from drained stderr", res.Reason)
	}
	if res.ExitCode == nil || *res.ExitCode != 2 {
		t.Errorf("exit code = %v, want 2", res.ExitCode)
	}
}

// TestCommandHandler_ExecForm pins the exec form (args present skips the
// shell): the argument list reaches the program's argv verbatim. A bare
// `test` in the shell-form fallback would exit 1 (missing operand), so an
// allow proves the arguments arrived.
func TestCommandHandler_ExecForm(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(commandHookConfig{Command: "test", Args: []string{"-d", dir}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := NewRegistry().Execute(context.Background(), HandlerCommand, raw, testEvent(), testHook(), testBudget)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("decision = %+v, want allow (argv did not arrive intact)", res)
	}
}

// TestCommandHandler_Cwd pins the working-directory field: the child runs
// with cwd as its directory — proven by stat-ing a marker file relative to
// that directory (symlink-proof, unlike a pwd comparison).
func TestCommandHandler_Cwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	raw, err := json.Marshal(commandHookConfig{Command: `test -f marker.txt && exit 0 || exit 1`, Cwd: dir})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := NewRegistry().Execute(context.Background(), HandlerCommand, raw, testEvent(), testHook(), testBudget)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("decision = %+v, want allow (child did not run in the configured cwd)", res)
	}
}
