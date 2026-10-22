package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

func newTestShell(t *testing.T) *JailedShell {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return NewJailedShell(resolved)
}

func TestJailedShell_CwdConfinement(t *testing.T) {
	s := newTestShell(t)
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{Command: "pwd"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code := *resp.ExitCode; code != 0 {
		t.Fatalf("exit code %d, output %q", code, resp.Output)
	}
	if got := strings.TrimSpace(resp.Output); got != s.agentDir && !strings.HasSuffix(s.agentDir, got) && got != filepath.Clean(s.agentDir) {
		t.Errorf("pwd = %q, want jail root %q", got, s.agentDir)
	}
}

func TestJailedShell_EnvScrubbing(t *testing.T) {
	t.Setenv("BUDDIES_SECRET", "leak-me")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak-me-too")
	s := newTestShell(t)
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{
		Command: "env | grep -E 'BUDDIES_SECRET|AWS_SECRET' || true",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(resp.Output, "leak-me") {
		t.Errorf("secrets leaked into shell environment: %q", resp.Output)
	}
	// A minimal PATH is still present so ordinary commands work.
	resp, err = s.Execute(context.Background(), &einofs.ExecuteRequest{Command: "ls /"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code := *resp.ExitCode; code != 0 {
		t.Errorf("expected PATH to allow basic commands, exit %d", code)
	}
}

func TestJailedShell_Timeout(t *testing.T) {
	s := newTestShell(t)
	budget := 300 * time.Millisecond
	start := time.Now()
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{
		Command: "sleep 30",
		Timeout: &budget,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.TimedOut {
		t.Error("expected TimedOut")
	}
	if code := *resp.ExitCode; code != 124 {
		t.Errorf("expected exit code 124 on timeout, got %d", code)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout not enforced, ran %v", elapsed)
	}
}

func TestJailedShell_Truncation(t *testing.T) {
	s := newTestShell(t)
	s.outputCap = 1024
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{
		Command: "yes hello | head -c 4096",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.Truncated {
		t.Error("expected Truncated")
	}
	if len(resp.Output) > 1024+len("\n[truncated]") {
		t.Errorf("output not capped: %d bytes", len(resp.Output))
	}
}

func TestJailedShell_ExitCodeAndStderr(t *testing.T) {
	s := newTestShell(t)
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{
		Command: "echo out; echo err >&2; exit 3",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code := *resp.ExitCode; code != 3 {
		t.Errorf("exit code %d, want 3", code)
	}
	if !strings.Contains(resp.Output, "out") || !strings.Contains(resp.Output, "err") {
		t.Errorf("stdout+stderr expected in output, got %q", resp.Output)
	}
}

func TestJailedShell_WritesIntoJail(t *testing.T) {
	s := newTestShell(t)
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{
		Command: "touch jailed-file",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code := *resp.ExitCode; code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(s.agentDir, "jailed-file")); err != nil {
		t.Errorf("file not created in jail root: %v", err)
	}
}

// TestJailedShell_ExecuteRunsTranslatedCommand pins the mount contract end to
// end: the model-facing /workspace prefix must be the command the host shell
// actually runs. Execute translating only for judgment but exec spawning the
// shell with the raw command would split the model-facing view between the
// fs tools and the shell (the command would die on /workspace while
// write_file to /workspace/x resolved fine in the same turn).
func TestJailedShell_ExecuteRunsTranslatedCommand(t *testing.T) {
	s := newTestShell(t)
	resp, err := s.Execute(context.Background(), &einofs.ExecuteRequest{
		Command: `cd /workspace && pwd; ls /workspace`,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if code := *resp.ExitCode; code != 0 {
		t.Fatalf("exit %d, output %q", code, resp.Output)
	}
	if strings.Contains(resp.Output, "/workspace") && !strings.Contains(resp.Output, s.agentDir) {
		t.Errorf("raw mount prefix reached the host shell: output %q", resp.Output)
	}
	if got := strings.TrimSpace(strings.SplitN(resp.Output, "\n", 2)[0]); got != s.agentDir {
		t.Errorf("cd /workspace && pwd = %q, want jail root %q", got, s.agentDir)
	}
}
