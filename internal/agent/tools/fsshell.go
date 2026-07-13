package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk/filesystem"

	"github.com/oniharnantyo/onclaw/internal/shellpolicy"
)

// fsShell implements filesystem.Shell, executing commands inside the workspace
// under the configured execution policy. It is a verbatim port of the prior
// hand-rolled `shell` tool's policy; the Eino `execute` tool is a pass-through
// to this, so no policy behaviour is lost.
type fsShell struct {
	workspace string
	policy    string
	allowlist []string
	denylist  []string
	// stdin is the confirmation reader for the `ask` policy. It defaults to
	// os.Stdin but is injectable so the policy can be tested without a TTY.
	stdin io.Reader
}

// NewFSShell constructs a Shell confined to workspace with the given policy.
// An optional stdin reader may be supplied (used by tests for the `ask`
// policy); when omitted it defaults to os.Stdin.
func NewFSShell(workspace, policy string, allowlist, denylist []string, stdin ...io.Reader) filesystem.Shell {
	in := io.Reader(os.Stdin)
	if len(stdin) > 0 && stdin[0] != nil {
		in = stdin[0]
	}
	return &fsShell{
		workspace: workspace,
		policy:    policy,
		allowlist: allowlist,
		denylist:  denylist,
		stdin:     in,
	}
}

// CappedBuffer bounds command output to a fixed number of bytes.
type CappedBuffer struct {
	Cap  int
	Buf  strings.Builder
	Size int
}

func (cb *CappedBuffer) Write(p []byte) (n int, err error) {
	if cb.Size >= cb.Cap {
		return len(p), nil
	}
	available := cb.Cap - cb.Size
	toWrite := len(p)
	if toWrite > available {
		toWrite = available
	}
	n, err = cb.Buf.Write(p[:toWrite])
	cb.Size += n
	if cb.Size >= cb.Cap && len(p) > toWrite {
		cb.Buf.WriteString("\n[Output truncated due to size limit]")
	}
	return len(p), err
}

func isAllowedCommand(command string, allowlist []string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	var binary string
	for _, part := range parts {
		if strings.Contains(part, "=") {
			continue // skip env variables like CGO_ENABLED=0
		}
		binary = part
		break
	}

	binary = filepath.Base(binary)

	for _, allowed := range allowlist {
		if binary == allowed {
			return true
		}
	}
	return false
}

// denylistReCache compiles each denylist pattern once.
var denylistReCache sync.Map // map[string]*regexp.Regexp

// catastrophicCategory maps a known floor pattern to a human-readable category.
var catastrophicCategory = func() map[string]string {
	m := make(map[string]string, len(shellpolicy.CatastrophicFloor))
	for _, e := range shellpolicy.CatastrophicFloor {
		m[e.Pattern] = e.Category
	}
	return m
}()

// matchesCatastrophic evaluates the FULL command string against the denylist
// patterns, returning the matched category (or raw pattern) for the result.
func matchesCatastrophic(command string, denylist []string) (bool, string) {
	for _, pattern := range denylist {
		if pattern == "" {
			continue
		}
		var re *regexp.Regexp
		if cached, ok := denylistReCache.Load(pattern); ok {
			re = cached.(*regexp.Regexp)
		} else {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			re = compiled
			denylistReCache.Store(pattern, re)
		}
		if re.MatchString(command) {
			reason := pattern
			if cat, ok := catastrophicCategory[pattern]; ok {
				reason = cat
			}
			return true, reason
		}
	}
	return false, ""
}

// Execute runs the command under the configured policy, redacting output.
func (s *fsShell) Execute(ctx context.Context, req *filesystem.ExecuteRequest) (*filesystem.ExecuteResponse, error) {
	policy := strings.ToLower(strings.TrimSpace(s.policy))
	if policy == "" {
		policy = "denylist"
	}

	if policy == "deny" {
		return &filesystem.ExecuteResponse{Output: "Command blocked by execution policy: deny"}, nil
	}

	if policy == "denylist" {
		if matched, reason := matchesCatastrophic(req.Command, s.denylist); matched {
			slog.Warn("shell command blocked by denylist",
				"command", req.Command,
				"pattern", reason,
			)
			return &filesystem.ExecuteResponse{Output: fmt.Sprintf("Command blocked (catastrophic-pattern: %s)", reason)}, nil
		}
	}

	if policy == "allowlist" {
		if !isAllowedCommand(req.Command, s.allowlist) {
			return &filesystem.ExecuteResponse{Output: "Command blocked: binary is not in the allowed commands list"}, nil
		}
	}

	if policy == "ask" {
		fmt.Printf("\n[Shell tool] Agent wants to execute: %s\nConfirm execution? (y/n): ", req.Command)
		var response string
		_, err := fmt.Fscanln(s.stdin, &response)
		if err != nil {
			return &filesystem.ExecuteResponse{Output: "Command blocked: failed to get user confirmation"}, nil
		}
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			return &filesystem.ExecuteResponse{Output: "Command blocked: user rejected execution"}, nil
		}
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	cmd.Dir = s.workspace

	var outBuf CappedBuffer
	outBuf.Cap = 32 * 1024 // 32KB output cap
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	outputStr := Redact(outBuf.Buf.String())
	truncated := outBuf.Size >= outBuf.Cap

	resp := &filesystem.ExecuteResponse{
		Output:    outputStr,
		Truncated: truncated,
	}

	if err != nil {
		if ctx.Err() != nil {
			resp.Output = outputStr + "\nCommand interrupted (context cancelled)"
			return resp, nil
		}
		resp.Output = fmt.Sprintf("%s\nCommand failed: %v", outputStr, err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			resp.ExitCode = &code
		}
		return resp, nil
	}

	code := 0
	resp.ExitCode = &code
	return resp, nil
}
