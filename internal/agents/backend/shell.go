package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// DefaultShellCommandTimeout bounds a command when the caller supplies none.
const DefaultShellCommandTimeout = 60 * time.Second

// DefaultShellOutputCap is the maximum captured output bytes before truncation.
const DefaultShellOutputCap = 64 * 1024

// ShellApprovalInfo is the user-facing payload carried by a shell interrupt.
// It travels with the interrupt checkpoint so the approval UI and the resume
// path both know what command is awaiting a decision. The type is registered
// with the Eino serializer because interrupt payloads are persisted.
type ShellApprovalInfo struct {
	Command string `json:"command"`
}

func init() {
	schema.Register[ShellApprovalInfo]()
}

// DecisionLedger resolves a previously recorded human approval for a command.
// It exists because interrupt IDs are regenerated when a checkpoint is
// reconstructed in a new process, so the resume-target path alone cannot carry
// the decision across restarts; the ledger is durable storage keyed by the
// command itself.
type DecisionLedger interface {
	// Lookup consumes the recorded decision for command: (approved, true)
	// when a decision exists, (false, false) otherwise.
	Lookup(ctx context.Context, command string) (approved bool, ok bool)
}

// JailedShell implements einofs.Shell: commands run with the agent workspace
// directory as working directory and a scrubbed environment. Dangerous
// commands interrupt for human approval instead of executing; on resume, the
// approval decision arrives either as the resume data (same process) or from
// the durable decision ledger (restart).
//
// The jail is a working-directory convention, not an OS sandbox: commands run
// as the server process's user.
type JailedShell struct {
	agentDir   string
	shellPath  string
	timeout    time.Duration
	outputCap  int
	pathPrefix string
	classifier func(string) bool
	ledger     DecisionLedger
}

// WithDecisionLedger attaches a durable approval-decision ledger.
func (s *JailedShell) WithDecisionLedger(l DecisionLedger) *JailedShell {
	s.ledger = l
	return s
}

// ShellOption configures a JailedShell at construction time.
type ShellOption func(*JailedShell)

// WithSkillsVenvBin prepends a workspace skills venv bin directory
// (`workspaces/<slug>/skills/.venv/bin`) to the scrubbed PATH so that, when a
// venv exists, `python3` and installed packages resolve to the shared
// workspace environment (design D6). A missing directory is harmless: PATH
// entries need not exist.
func WithSkillsVenvBin(binDir string) ShellOption {
	return func(s *JailedShell) {
		if binDir != "" {
			s.pathPrefix = binDir
		}
	}
}

// NewJailedShell builds a shell bound to an agent workspace jail root.
func NewJailedShell(agentDir string, opts ...ShellOption) *JailedShell {
	s := &JailedShell{
		agentDir:   agentDir,
		shellPath:  defaultShellPath(),
		timeout:    DefaultShellCommandTimeout,
		outputCap:  DefaultShellOutputCap,
		classifier: IsDangerousCommand,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func defaultShellPath() string {
	if sh, ok := os.LookupEnv("SHELL"); ok && sh != "" {
		return sh
	}
	return "/bin/sh"
}

// scrubbedEnv builds a minimal environment: no provider keys, tokens, or
// instance configuration leak into agent commands. pathPrefix, when set, is
// prepended to PATH (the workspace skills venv bin directory).
func scrubbedEnv(pathPrefix string) []string {
	path := "/usr/local/bin:/usr/bin:/bin"
	if pathPrefix != "" {
		path = pathPrefix + ":" + path
	}
	return []string{
		"PATH=" + path,
		"HOME=" + os.TempDir(),
		"TMPDIR=" + os.TempDir(),
		"LANG=C.UTF-8",
	}
}

// Execute implements einofs.Shell.
func (s *JailedShell) Execute(ctx context.Context, req *einofs.ExecuteRequest) (*einofs.ExecuteResponse, error) {
	command := req.Command

	if s.classifier(command) {
		// Same-process resume: the decision arrives as the resume data.
		isTarget, hasData, approved := tool.GetResumeContext[bool](ctx)
		if !isTarget && s.ledger != nil {
			// Cross-process resume: the decision comes from the durable ledger.
			ledgerApproved, ok := s.ledger.Lookup(ctx, command)
			if ok {
				isTarget, hasData, approved = true, true, ledgerApproved
			}
		}
		if isTarget {
			if !hasData || !approved {
				return &einofs.ExecuteResponse{
					Output:   denialNotice(command),
					ExitCode: intPtr(126),
				}, nil
			}
			// Approved: fall through to execution.
		} else {
			return nil, tool.Interrupt(ctx, ShellApprovalInfo{Command: command})
		}
	}
	return s.exec(ctx, req)
}

func denialNotice(command string) string {
	return fmt.Sprintf("Command blocked: %q was denied by the operator and was not executed.", command)
}

func intPtr(i int) *int { return &i }

// exec runs the command inside the jail.
func (s *JailedShell) exec(ctx context.Context, req *einofs.ExecuteRequest) (*einofs.ExecuteResponse, error) {
	budget := s.timeout
	if req.Timeout != nil && *req.Timeout > 0 {
		budget = *req.Timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	cmd := exec.CommandContext(runCtx, s.shellPath, "-c", req.Command)
	cmd.Dir = s.agentDir
	cmd.Env = scrubbedEnv(s.pathPrefix)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()

	resp := &einofs.ExecuteResponse{}
	output := out.Bytes()
	if len(output) > s.outputCap {
		resp.Output = string(output[:s.outputCap]) + "\n[truncated]"
		resp.Truncated = true
	} else {
		resp.Output = string(output)
	}

	if runCtx.Err() == context.DeadlineExceeded {
		resp.TimedOut = true
		resp.ExitCode = intPtr(124)
		return resp, nil
	}
	if runErr == nil {
		code := 0
		resp.ExitCode = &code
		return resp, nil
	}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		resp.ExitCode = intPtr(exitErr.ExitCode())
		return resp, nil
	}
	return nil, fmt.Errorf("execute: %w", runErr)
}

// dangerPatterns are matched case-insensitively against the full command line.
// The classifier is a deliberate coarse net: it errs toward interrupting.
var dangerPatterns = []*regexp.Regexp{
	// rm -rf and friends
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*\s+)*-[a-zA-Z]*[rR][a-zA-Z]*f`),
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*\s+)*-[a-zA-Z]*f[a-zA-Z]*[rR]`),
	// piping a downloaded script into a shell
	regexp.MustCompile(`\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba|z|da|k)?sh\b`),
	// privilege escalation
	regexp.MustCompile(`\bsudo\b`),
	// filesystem destruction
	regexp.MustCompile(`\bmkfs(\.\w+)?\b`),
	regexp.MustCompile(`\bdd\b[^&|;]*of=/dev/`),
	regexp.MustCompile(`>\s*/dev/sd[a-z]`),
	regexp.MustCompile(`\bchmod\s+(-[a-z]*r[a-z]*\s+)?777\b`),
	// host lifecycle
	regexp.MustCompile(`\b(shutdown|reboot|poweroff|halt|init\s+0|init\s+6)\b`),
	// fork bomb
	regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
}

// IsDangerousCommand reports whether the command matches the built-in
// dangerous-command pattern list and must pause for human approval.
func IsDangerousCommand(command string) bool {
	lower := strings.ToLower(command)
	for _, re := range dangerPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}
