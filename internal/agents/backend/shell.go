package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	einofs "github.com/cloudwego/eino/adk/filesystem"
)

// DefaultShellCommandTimeout bounds a command when the caller supplies none.
const DefaultShellCommandTimeout = 60 * time.Second

// DefaultShellOutputCap is the maximum captured output bytes before truncation.
const DefaultShellOutputCap = 64 * 1024

// JailedShell implements einofs.Shell: commands run with the agent workspace
// directory as working directory and a scrubbed environment.
//
// The jail is a working-directory convention, not an OS sandbox: commands run
// as the app process's user. The guarded posture is cwd confinement, the
// bounded timeout, the output cap, the scrubbed child env, and — when built
// WithApprovalGate — the dangerous-command gate (spec chat-approvals: a
// safety-pattern match interrupts the run for a human decision instead of
// executing).
type JailedShell struct {
	agentDir  string
	shellPath string
	timeout   time.Duration
	outputCap int
	mount     *mountBinding
	gate      *approvalGate
}

// ShellOption configures a JailedShell at construction.
type ShellOption func(*JailedShell)

// WithApprovalGate attaches the dangerous-command gate backed by ledger: a
// command matching a safety pattern interrupts the run for a human decision
// (spec chat-approvals) instead of executing.
func WithApprovalGate(ledger ApprovalLedger) ShellOption {
	return func(s *JailedShell) { s.gate = &approvalGate{ledger: ledger} }
}

// mountBinding rewrites the model-facing mount prefix (DefaultMountPoint,
// the fs tools' contract) to the host directory it maps onto. The shell has
// no chroot, so an absolute model path like /workspace cannot resolve on its
// own — without the rewrite, any command the model writes against the mount
// contract ("cd /workspace && ...") dies on the host filesystem.
type mountBinding struct {
	prefix string
	dir    string
	re     *regexp.Regexp
}

func newMountBinding(prefix, dir string) *mountBinding {
	// The prefix must open at a command boundary (start of string, or a
	// character that cannot continue a path or URL: whitespace, quote, or
	// opening bracket) and end at one (end of string, slash, or a character
	// that closes a path argument). "x/workspace", "//workspace", and
	// "/workspacefoo" therefore stay untouched — URLs especially. RE2 has no
	// lookaround, so both boundary characters join the match and the apply
	// step re-emits them around the host path.
	b := &mountBinding{prefix: prefix, dir: dir}
	b.re = regexp.MustCompile(`(^|[\s"'=(\[])` + regexp.QuoteMeta(prefix) + `($|/|[\s"')\],;:&|<>])`)
	return b
}

func (b *mountBinding) apply(command string) string {
	return b.re.ReplaceAllStringFunc(command, func(match string) string {
		i := strings.Index(match, b.prefix)
		return match[:i] + b.dir + match[i+len(b.prefix):]
	})
}

// NewJailedShell builds a shell bound to an agent workspace jail root.
// The workspace is pre-bound to the fs tools' mount contract: the model is
// told its workspace lives at DefaultMountPoint, so shell commands using
// that prefix resolve to the same directory. Options apply construction-time
// configuration (WithApprovalGate).
func NewJailedShell(agentDir string, opts ...ShellOption) *JailedShell {
	s := &JailedShell{
		agentDir:  agentDir,
		shellPath: defaultShellPath(),
		timeout:   DefaultShellCommandTimeout,
		outputCap: DefaultShellOutputCap,
		mount:     newMountBinding(DefaultMountPoint, agentDir),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// translateMounts rewrites the model-facing mount prefix to its host path —
// the same mapping the fs jail applies to tool paths, so both views of the
// workspace agree.
func (s *JailedShell) translateMounts(command string) string {
	return s.mount.apply(command)
}

func defaultShellPath() string {
	if sh, ok := os.LookupEnv("SHELL"); ok && sh != "" {
		return sh
	}
	return "/bin/sh"
}

// scrubbedEnv builds a minimal environment: no provider keys, tokens, or
// instance configuration leak into agent commands — the child env is built
// from scratch, not inherited, so nothing else can slip through. PATH is the
// one exception: it extends the process's own PATH (childPath) so
// user-installed CLIs resolve by name, and HOME resolves to the real home so
// their caches (~/.cache/…) are reachable; the jail is a cwd convention, not
// a sandbox (see JailedShell), so this only fixes default resolution.
func scrubbedEnv() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	return []string{
		"PATH=" + childPath(home),
		"HOME=" + home,
		"TMPDIR=" + os.TempDir(),
		"LANG=C.UTF-8",
	}
}

// childPath assembles the jailed shell's PATH. The process's own PATH leads
// — whatever launched buddies already resolved the user's tool homes (a
// terminal, Explorer, a service manager), covering pipx/uv/npm/cargo installs
// wherever they landed — and the platform's user-local tool directories are
// appended, so even a spartan GUI-launch PATH (macOS launchd) still reaches
// the well-known homes. Duplicates collapse; directories that do not exist
// are simply skipped by the OS lookup. PATH carries no secrets — the
// scrub's guarantee (a built-from-scratch env) is about everything else.
func childPath(home string) string {
	sep := string(os.PathListSeparator)
	seen := make(map[string]bool)
	var dirs []string
	add := func(dirsToAdd ...string) {
		for _, d := range dirsToAdd {
			if d == "" || seen[d] {
				continue
			}
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	add(filepath.SplitList(os.Getenv("PATH"))...)
	switch runtime.GOOS {
	case "windows":
		// Explorer-launched processes carry the user's full PATH; unix
		// system directories would be dead weight, and the shell
		// resolution itself is still unix-only (defaultShellPath).
		if len(dirs) > 0 {
			return strings.Join(dirs, sep)
		}
		return filepath.Join(home, ".local", "bin")
	case "darwin":
		// Apple Silicon Homebrew; Intel installs into /usr/local/bin below.
		add("/opt/homebrew/bin", "/opt/homebrew/sbin")
	case "linux":
		add("/home/linuxbrew/.linuxbrew/bin")
	}
	add(filepath.Join(home, ".local", "bin"))
	add("/usr/local/bin", "/usr/bin", "/bin")
	return strings.Join(dirs, sep)
}

// Execute implements einofs.Shell.
func (s *JailedShell) Execute(ctx context.Context, req *einofs.ExecuteRequest) (*einofs.ExecuteResponse, error) {
	// The translated form is the one that must run. Writing it back into the
	// request keeps exec single-sourced; without this, the model-facing mount
	// contract (`cd /workspace && …`) reached the host shell raw and died on a
	// path the fs tools resolve fine.
	req.Command = s.translateMounts(req.Command)
	// Dangerous-command gate (spec chat-approvals): a safety-pattern match
	// never executes directly — either a spendable recorded decision buys
	// this one execution, or the run interrupts for a human decision.
	if s.gate != nil {
		denial, err := s.gate.gate(ctx, req.Command)
		if err != nil {
			return nil, err
		}
		if denial != nil {
			return denial, nil
		}
	}
	return s.exec(ctx, req)
}

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
	cmd.Env = scrubbedEnv()

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

func intPtr(i int) *int { return &i }
