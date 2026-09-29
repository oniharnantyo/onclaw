package agents

// Tests for the shell background lane's execute tool (shell_lane.go): the
// foreground path reaches the shell directly so approval interrupts keep
// working (spec agent-background-shell "Foreground shell unchanged"), a
// background launch joins the run's task space, and a launch of an
// approval-raising command is refused readably (the design's documented
// fallback).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	einofs "github.com/cloudwego/eino/adk/filesystem"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// laneEchoShell is a shell stub that echoes the command, records how it was
// reached, and — optionally — reports commands as approval-raising.
type laneEchoShell struct {
	direct     int // Execute calls (the foreground path)
	wouldDeny  bool
	outputText string
}

func (s *laneEchoShell) Execute(ctx context.Context, req *einofs.ExecuteRequest) (*einofs.ExecuteResponse, error) {
	s.direct++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := s.outputText
	if out == "" {
		out = "ran: " + req.Command
	}
	code := 0
	return &einofs.ExecuteResponse{Output: out, ExitCode: &code}, nil
}

func (s *laneEchoShell) WouldInterrupt(command string) bool {
	return s.wouldDeny
}

// newLaneFixture wires the lane execute tool to one task space and one
// jailed opener.
func newLaneFixture(t *testing.T, shell einofs.Shell) tool.InvokableTool {
	t.Helper()
	agentDir := t.TempDir()
	space, err := newBackgroundTaskSpace(context.Background())
	if err != nil {
		t.Fatalf("task space: %v", err)
	}
	opener, err := backend.NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("opener: %v", err)
	}
	tl, err := newShellLaneExecuteTool(shell, &fsmw.LocalBackgroundConfig{
		Runner:      space.Runner,
		OutputStore: opener,
		OutputDir:   TasksOutputDir,
	}, func(context.Context) (string, error) { return "sess-lane", nil })
	if err != nil {
		t.Fatalf("lane execute tool: %v", err)
	}
	return tl
}

func invokeLane(t *testing.T, tl tool.InvokableTool, args string) string {
	t.Helper()
	out, err := tl.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("InvokableRun(%s): %v", args, err)
	}
	return out
}

// TestShellLane_ForegroundReachesShellDirectly pins the foreground pin: a
// run without run_in_background calls the shell on the caller's context —
// no task, no runner — so a shell approval interrupt surfaces the same way
// it does without the lane.
func TestShellLane_ForegroundReachesShellDirectly(t *testing.T) {
	shell := &laneEchoShell{}
	tl := newLaneFixture(t, shell)

	out := invokeLane(t, tl, `{"command":"echo hi"}`)
	if out != "ran: echo hi" {
		t.Fatalf("foreground result = %q", out)
	}
	if shell.direct != 1 {
		t.Fatalf("foreground run must call the shell directly once, got %d", shell.direct)
	}
}

// TestShellLane_LaunchRefusesApprovalCommand pins the fallback: a command
// the shell would interrupt for approval cannot detach — the launch fails
// with a readable result and never reaches the shell or the task space.
func TestShellLane_LaunchRefusesApprovalCommand(t *testing.T) {
	shell := &laneEchoShell{wouldDeny: true}
	tl := newLaneFixture(t, shell)

	out := invokeLane(t, tl, `{"command":"rm -rf /tmp/x","run_in_background":true}`)
	if !strings.Contains(out, "requires operator approval") || !strings.Contains(out, "foreground") {
		t.Fatalf("refusal must name the approval boundary and the foreground rerun, got %q", out)
	}
	if shell.direct != 0 {
		t.Fatalf("a refused launch must never execute, got %d shell calls", shell.direct)
	}
}

// TestShellLane_LaunchRunsInTaskSpace pins the launch path: run_in_background
// returns promptly with the task id and output location, the command runs to
// completion in the shared space, and the output file lands under the
// canonical tasks directory.
func TestShellLane_LaunchRunsInTaskSpace(t *testing.T) {
	agentDir := t.TempDir()
	shell := &laneEchoShell{outputText: "background output body"}
	space, err := newBackgroundTaskSpace(context.Background())
	if err != nil {
		t.Fatalf("task space: %v", err)
	}
	opener, err := backend.NewTasksAppendOpener(agentDir)
	if err != nil {
		t.Fatalf("opener: %v", err)
	}
	tl, err := newShellLaneExecuteTool(shell, &fsmw.LocalBackgroundConfig{
		Runner:      space.Runner,
		OutputStore: opener,
		OutputDir:   TasksOutputDir,
	}, nil)
	if err != nil {
		t.Fatalf("lane execute tool: %v", err)
	}

	out := invokeLane(t, tl, `{"command":"sleep 0; echo done","run_in_background":true}`)
	if !strings.Contains(out, "running in background with ID:") {
		t.Fatalf("launch must return the task id promptly, got %q", out)
	}
	if !strings.Contains(out, TasksOutputDir) {
		t.Fatalf("launch must name the output file location, got %q", out)
	}
	const idMarker = "ID: "
	start := strings.Index(out, idMarker)
	if start < 0 {
		t.Fatal("no task id in the launch result")
	}
	rest := out[start+len(idMarker):]
	taskID := rest[:strings.Index(rest, ".")]

	// The task reaches Completed inside the shared space; the result and the
	// output file carry the command's output.
	deadline := time.Now().Add(5 * time.Second)
	for {
		task, err := space.Manager.Get(context.Background(), taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if task.Status == bgtask.StatusCompleted {
			if !strings.Contains(string(task.ResultData), "background output body") {
				t.Fatalf("task result = %q, want the command's output", task.ResultData)
			}
			break
		}
		if task.Status == bgtask.StatusFailed || task.Status == bgtask.StatusCanceled {
			t.Fatalf("task ended %q: %s", task.Status, task.ResultError)
		}
		if time.Now().After(deadline) {
			t.Fatalf("task never completed, last status %q", task.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	matches, err := filepath.Glob(filepath.Join(agentDir, TasksOutputDir, "*.output"))
	if err != nil {
		t.Fatalf("glob output dir: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one output file under %s, got %v", TasksOutputDir, matches)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(body), "background output body") {
		t.Fatalf("output file body = %q", body)
	}
}

// TestShellLane_FailedShellFailsTask pins the work path's honesty: a shell
// error fails the task readable through its result error — never swallowed.
func TestShellLane_FailedShellFailsTask(t *testing.T) {
	space, err := newBackgroundTaskSpace(context.Background())
	if err != nil {
		t.Fatalf("task space: %v", err)
	}
	opener, err := backend.NewTasksAppendOpener(t.TempDir())
	if err != nil {
		t.Fatalf("opener: %v", err)
	}
	tl, err := newShellLaneExecuteTool(&failingShell{}, &fsmw.LocalBackgroundConfig{
		Runner:      space.Runner,
		OutputStore: opener,
		OutputDir:   TasksOutputDir,
	}, nil)
	if err != nil {
		t.Fatalf("lane execute tool: %v", err)
	}

	out := invokeLane(t, tl, `{"command":"nope","run_in_background":true}`)
	if !strings.Contains(out, "running in background with ID:") {
		t.Fatalf("launch returns the submit snapshot even when the work fails fast, got %q", out)
	}
	const idMarker = "ID: "
	start := strings.Index(out, idMarker)
	if start < 0 {
		t.Fatal("no task id in the launch result")
	}
	rest := out[start+len(idMarker):]
	taskID := rest[:strings.Index(rest, ".")]

	// The failure surfaces on the task record: readable through the result
	// error, never swallowed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		task, err := space.Manager.Get(context.Background(), taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if task.Status == bgtask.StatusFailed {
			if !strings.Contains(task.ResultError, "shell exploded") {
				t.Fatalf("task error = %q, want the shell's failure", task.ResultError)
			}
			break
		}
		if task.Status == bgtask.StatusCompleted || task.Status == bgtask.StatusCanceled {
			t.Fatalf("task ended %q, want failed", task.Status)
		}
		if time.Now().After(deadline) {
			t.Fatalf("task never failed, last status %q", task.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type failingShell struct{}

func (f *failingShell) Execute(context.Context, *einofs.ExecuteRequest) (*einofs.ExecuteResponse, error) {
	return nil, errors.New("shell exploded")
}
