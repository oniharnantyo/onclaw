package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// The dangerous-command gate (spec chat-approvals "Dangerous-command gate",
// design D4): a shell command matching a safety pattern interrupts the run
// instead of executing. The single approval decision travels through the
// checkpoint ledger — one recorded approval under approval/<sha256(command)>,
// consumed on read, so an identical command seen again interrupts again.

// ShellApprovalInfo is the interrupt payload a gated command carries: the
// translated command the model asked to run. It rides the interrupt context
// into the persisted checkpoint, so it must stay gob-serializable.
type ShellApprovalInfo struct {
	Command string `json:"command"`
}

func init() {
	// The info rides the checkpoint's interface fields; register for gob so
	// checkpoint save/restore round-trips it (the eino RegisterName
	// convention).
	schema.RegisterName[ShellApprovalInfo]("buddies_backend_shell_approval_info")
}

// ApprovalLedger is the consumed-on-read decision store the gate consults:
// one recorded approval under LedgerKey(command), deleted as it is spent.
type ApprovalLedger interface {
	// Consume spends the decision recorded under key, reporting whether one
	// existed. A consumed decision has been spent: the next identical
	// command finds nothing.
	Consume(ctx context.Context, key string) (bool, error)
}

// RecordDecision stores one approval under the command's ledger key. The
// payload names the approved command for auditability; presence is the gate's
// only signal.
func RecordDecision(ctx context.Context, store DecisionRecorder, command string) error {
	payload, err := json.Marshal(map[string]any{"command": command, "approved": true})
	if err != nil {
		return fmt.Errorf("record approval decision: %w", err)
	}
	if err := store.Set(ctx, LedgerKey(command), payload); err != nil {
		return fmt.Errorf("record approval decision: %w", err)
	}
	return nil
}

// DecisionRecorder is the write half of the ledger (the checkpoint store's
// Set).
type DecisionRecorder interface {
	Set(ctx context.Context, key string, data []byte) error
}

// LedgerKey derives the checkpoint KV key one command's decision lives under:
// the approval/ prefix over the SHA-256 of the translated command. The same
// derivation serves the recording side and the gate.
func LedgerKey(command string) string {
	sum := sha256.Sum256([]byte(command))
	return "approval/" + hex.EncodeToString(sum[:])
}

// dangerousCommandPatterns are the safety patterns a command must never run
// without an approval decision (spec chat-approvals: forced recursive
// deletion, pipe-to-shell downloads, privilege escalation, partition or disk
// writes, shutdown/reboot, fork bombs, and equivalent destructive forms).
// Forced recursive deletion (rm with BOTH the recursive and force flags, in
// any spelling and order) is matched by rmForcedRecursive below — flag
// clusters and split flags do not reduce to one regex reliably.
var dangerousCommandPatterns = []*regexp.Regexp{
	// Downloaded script piped straight into a shell.
	regexp.MustCompile(`(?:curl|wget|fetch)\b[^;|&]*\|\s*(?:sudo\s+)?(?:ba|z|da|k)?sh\b`),
	// Privilege escalation.
	regexp.MustCompile(`(?:^|[\s;|(])(?:sudo|doas)\b`),
	// Partition or disk writes: mkfs family, partitioners, raw device writes.
	regexp.MustCompile(`\bmkfs(?:\.\w+)?\b|\bfdisk\b|\bsfdisk\b|\bparted\b|\bpartitiondisk\b|\bdd\b[^;|&]*\bof=/dev/`),
	// Shutdown, reboot, halt, poweroff.
	regexp.MustCompile(`(?:^|[\s;|(])(?:shutdown|reboot|halt|poweroff)\b`),
	// The classic bash fork bomb.
	regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
}

// rmFlags scans one rm invocation's flag tokens: the recursive and force
// flags in any spelling (-r/-R/--recursive, -f/--force), split across tokens
// or merged into clusters (-rf, -fr, -vrf).
var (
	rmInvocation = regexp.MustCompile(`(?:^|[\s;|&])(rm)\s+([^;|&\n]*)`)
	flagToken    = regexp.MustCompile(`(?:^|\s)(--?[^\s]+)`)
)

// rmForcedRecursive reports whether the command runs rm with both the
// recursive and force flags — forced recursive deletion. Non-flag words
// (target paths) are skipped, so file names never trip the match.
func rmForcedRecursive(command string) bool {
	for _, m := range rmInvocation.FindAllStringSubmatch(command, -1) {
		var recursive, force bool
		for _, f := range flagToken.FindAllStringSubmatch(m[2], -1) {
			flag := f[1]
			switch {
			case flag == "--recursive" || flag == "-r" || flag == "-R":
				recursive = true
			case flag == "--force" || flag == "-f":
				force = true
			case len(flag) > 1 && flag[0] == '-' && flag[1] != '-':
				// Short-flag cluster: both letters anywhere in it.
				cluster := flag[1:]
				recursive = recursive || strings.ContainsAny(cluster, "rR")
				force = force || strings.Contains(cluster, "f")
			}
		}
		if recursive && force {
			return true
		}
	}
	return false
}

// matchesDangerousCommand reports whether the command hits a safety pattern.
func matchesDangerousCommand(command string) bool {
	if rmForcedRecursive(command) {
		return true
	}
	for _, re := range dangerousCommandPatterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// approvalGate carries the gate's dependencies; nil disables gating (a shell
// without a ledger has no decision surface to consult, so it must not
// interrupt — a parked run could never resume).
type approvalGate struct {
	ledger ApprovalLedger
}

// DenialResult is the tool result a denied command reports to the model: the
// refusal, in place of any execution output. ResolveApproval passes it back
// as the interrupt's resume data (spec chat-approvals "Deny informs the
// model"), and the gate returns it in place of the execution.
func DenialResult(command string) string {
	return "The user denied approval to run this command; it was not executed: " + command
}

// gate runs the dangerous-command check on a translated command (spec order:
// safety-pattern match → resume-context check → decision-ledger check). It
// returns a response to return in place of execution (a denial result), or
// nil to proceed with the execution. The interrupt itself is raised by the
// caller.
func (g *approvalGate) gate(ctx context.Context, command string) (*einofs.ExecuteResponse, error) {
	if !matchesDangerousCommand(command) {
		return nil, nil
	}
	// A resumed run re-executes this tool from the checkpoint. Targeted with
	// data means the user denied: the data IS the tool result. An implicit
	// resume (approval) falls through to the ledger, which ResolveApproval
	// stocked before resuming.
	wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
	if wasInterrupted {
		isTarget, hasData, denial := tool.GetResumeContext[string](ctx)
		if isTarget && hasData {
			return &einofs.ExecuteResponse{Output: denial}, nil
		}
	}
	// First encounter (or an approval resume): one recorded decision buys one
	// execution — consume it.
	approved, err := g.ledger.Consume(ctx, LedgerKey(command))
	if err == nil && approved {
		return nil, nil
	}
	// No spendable decision: interrupt. The command must not run; the run
	// pauses until the user decides (spec chat-approvals).
	return nil, tool.Interrupt(ctx, ShellApprovalInfo{Command: command})
}
