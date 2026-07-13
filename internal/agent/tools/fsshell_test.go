package tools_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/shellpolicy"
)

func TestFSShellDeny(t *testing.T) {
	s := tools.NewFSShell(t.TempDir(), "deny", nil, nil)
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(resp.Output, "deny") {
		t.Errorf("deny policy should block: %q", resp.Output)
	}
}

func TestFSShellAllowlist(t *testing.T) {
	s := tools.NewFSShell(t.TempDir(), "allowlist", []string{"echo"}, nil)
	// allowed
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(resp.Output) != "hi" {
		t.Errorf("allowed command should run, got %q", resp.Output)
	}
	// blocked
	resp, err = s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "ls /"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(resp.Output, "not in the allowed") {
		t.Errorf("non-allowlisted command should be blocked: %q", resp.Output)
	}
}

func TestFSShellDenylistCatastrophic(t *testing.T) {
	s := tools.NewFSShell(t.TempDir(), "denylist", nil, shellpolicy.FloorPatterns())
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "rm -rf /"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(resp.Output, "catastrophic-pattern") {
		t.Errorf("catastrophic command should be blocked: %q", resp.Output)
	}
	if !strings.Contains(resp.Output, "mass-destruction") {
		t.Errorf("catastrophic reason should name category: %q", resp.Output)
	}

	// a benign command runs
	resp, err = s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo ok"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(resp.Output) != "ok" {
		t.Errorf("benign command should run, got %q", resp.Output)
	}
}

func TestFSShellExitCode(t *testing.T) {
	s := tools.NewFSShell(t.TempDir(), "denylist", nil, nil)
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "exit 3"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.ExitCode == nil || *resp.ExitCode != 3 {
		t.Errorf("expected exit code 3, got %v", resp.ExitCode)
	}

	resp, err = s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo done"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.ExitCode == nil || *resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %v", resp.ExitCode)
	}
}

func TestFSShellOutputCap(t *testing.T) {
	s := tools.NewFSShell(t.TempDir(), "denylist", nil, nil)
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "head -c 40000 /dev/zero | tr '\\0' 'A'"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Truncated {
		t.Error("expected output to be flagged truncated")
	}
	if len(resp.Output) > 32*1024+100 {
		t.Errorf("output exceeded cap: %d bytes", len(resp.Output))
	}
}

func TestFSShellRedaction(t *testing.T) {
	s := tools.NewFSShell(t.TempDir(), "denylist", nil, nil)
	secret := "sk-ABCDEFGHIJKLMNOPQRSTUVW"
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo " + secret})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(resp.Output, secret) {
		t.Errorf("secret not redacted in shell output: %q", resp.Output)
	}
	if !strings.Contains(resp.Output, "[REDACTED]") {
		t.Errorf("expected redaction marker: %q", resp.Output)
	}
}

func TestFSShellAsk(t *testing.T) {
	// "n" rejects execution without running the command.
	s := tools.NewFSShell(t.TempDir(), "ask", nil, nil, strings.NewReader("n\n"))
	resp, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(resp.Output, "user rejected") {
		t.Errorf("ask=n should block execution, got %q", resp.Output)
	}

	// "y" runs the command.
	s = tools.NewFSShell(t.TempDir(), "ask", nil, nil, strings.NewReader("y\n"))
	resp, err = s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "echo hi"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(resp.Output) != "hi" {
		t.Errorf("ask=y should run the command, got %q", resp.Output)
	}
}

func TestFSShellDenylistLogged(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	s := tools.NewFSShell(t.TempDir(), "denylist", nil, shellpolicy.FloorPatterns())
	if _, err := s.Execute(context.Background(), &filesystem.ExecuteRequest{Command: "rm -rf /"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(buf.String(), "shell command blocked by denylist") {
		t.Errorf("expected denylist block to be logged, got %q", buf.String())
	}
}
