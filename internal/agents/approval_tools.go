package agents

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
)

// The computer-use approval gate (add-computer-use design D7, spec
// chat-approvals "Computer-use destructive actions require approval"):
// shell-gate semantics at the tool boundary. wrapApprovalTools intercepts a
// fixed destructive subset of the computer-use surface — killing an app,
// writing the clipboard, and typing into credential fields — and reuses the
// shell gate's whole park machinery unchanged: the gated call raises the
// same tool interrupt carrying a [backend.ShellApprovalInfo] (its Command
// names the requested action and its target), the runner's drain parks the
// run on a persisted approval_required event, approving stocks the decision
// ledger and resumes the exact call, and denying feeds the fixed refusal
// back as the resume data. No new event kinds, no new card renderer.
//
// Credential typing is gated conditionally: the adapter annotates secure
// elements in observe/list results as `<token> [secure]` (add-computer-use
// coordination contract C2), the recording wrapper below collects those
// tokens into a per-session bounded set, and a `type`/`set_value` call
// targeting a token in the calling session's set is gated. Undetectable
// targets are not gated — the design's documented limitation (open question:
// credential-field detection quality). Read-only tools and `stop` are never
// gated: the permission warm-up gate is their consent boundary, and `stop`
// is the kill switch.

// computerUseServerID is the computer-use system server's reserved id (the
// tool namespace segment mcp__computer-use__<tool>). The mcp package will
// export it as ComputerUseServerID when the adapter's spec lands; until then
// the literal here is the coordination contract's spelling.
const computerUseServerID = "computer-use"

// computerUseNamePrefix is the renamed tool name prefix the naming pass
// assigns the computer-use server's surface (ApplyNames with the spec's ID).
const computerUseNamePrefix = "mcp__" + computerUseServerID + "__"

// The gated computer-use tool names (add-computer-use design D4's surface,
// design D7's tiers): kill_app-class destruction and clipboard writes are
// ALWAYS gated; type and set_value are gated only for secure-field targets.
const (
	cuKillApp        = "kill_app"
	cuWriteClipboard = "write_clipboard"
	cuType           = "type"
	cuSetValue       = "set_value"
)

// cuObserveTools are the observation/listing tools whose results the
// recording wrapper parses for [secure] annotations (C2). Their results are
// the only channel that surfaces addressable element tokens.
var cuObserveTools = map[string]struct{}{
	"observe_window":  {},
	"observe_desktop": {},
	"list_apps":       {},
	"list_windows":    {},
}

// The argument keys each gated target is read from. The adapter's argument
// structs are the other side of this seam (add-computer-use design D4); the
// lists cover the plausible spellings so a shape drift degrades to an
// unnamed target (or an ungated typing call) rather than a mismatched gate.
// A value is taken only when it is a non-empty string.
var (
	cuKillAppTargetKeys = []string{"app", "application", "appName", "app_name", "name", "target"}
	cuElementTargetKeys = []string{"element", "elementToken", "element_token", "token", "ref", "target", "field", "name", "label"}
)

// secureAnnotation matches one secure-element annotation in a computer-use
// observe/list result (C2): a non-whitespace opaque token followed by the
// literal " [secure]".
var secureAnnotation = regexp.MustCompile(`(\S+) \[secure\]`)

// secureAnnotatedTokens extracts the secure tokens one result annotates.
func secureAnnotatedTokens(result string) []string {
	matches := secureAnnotation.FindAllStringSubmatch(result, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// The per-session secure-token store. Lifecycle choice (documented): no
// turn-end hook is reachable from the tool-wrap seam — the runner owns the
// turn lifecycle paths — so the store is BOUNDED instead of session-scoped:
// an LRU over sessions and a FIFO cap per session keep the memory flat, and
// the caps sit far above any one session's observation surface. Eviction
// only re-opens the gating question for later typing calls (they proceed
// ungated, the design's fail-open for undetectable fields); the tokens
// themselves are opaque element references, never values.
const (
	maxSecureTokenSessions    = 64
	maxSecureTokensPerSession = 256
)

// secureTokenSet is one session's recorded tokens: the membership set plus
// the insertion order the per-session FIFO cap evicts over.
type secureTokenSet struct {
	tokens map[string]struct{}
	fifo   []string
}

// secureTokenStore remembers the tokens each session's observations
// annotated. Guarded by mu; sessions evict LRU-oldest first.
type secureTokenStore struct {
	mu       sync.Mutex
	sessions map[string]*secureTokenSet
	order    []string // LRU order, oldest first
}

// sessionSecureTokens is the process-wide store the wraps share.
var sessionSecureTokens = newSecureTokenStore()

func newSecureTokenStore() *secureTokenStore {
	return &secureTokenStore{sessions: make(map[string]*secureTokenSet)}
}

// touch moves sessionID to the back of the LRU order. Callers hold mu.
func (s *secureTokenStore) touch(sessionID string) {
	for i, other := range s.order {
		if other == sessionID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, sessionID)
}

// record adds tokens to the session's set, evicting the oldest token at the
// per-session cap and the least-recently-used session at the session cap.
func (s *secureTokenStore) record(sessionID string, tokens ...string) {
	if sessionID == "" || len(tokens) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.sessions[sessionID]
	if !ok {
		set = &secureTokenSet{tokens: make(map[string]struct{})}
		s.sessions[sessionID] = set
	}
	s.touch(sessionID)
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if _, dup := set.tokens[tok]; dup {
			continue
		}
		if len(set.fifo) >= maxSecureTokensPerSession {
			oldest := set.fifo[0]
			set.fifo = set.fifo[1:]
			delete(set.tokens, oldest)
		}
		set.tokens[tok] = struct{}{}
		set.fifo = append(set.fifo, tok)
	}
	for len(s.order) > maxSecureTokenSessions {
		evicted := s.order[0]
		s.order = s.order[1:]
		delete(s.sessions, evicted)
	}
}

// contains reports whether the session's set holds the token. A hit counts
// as a use for the LRU.
func (s *secureTokenStore) contains(sessionID, token string) bool {
	if sessionID == "" || token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.sessions[sessionID]
	if !ok {
		return false
	}
	_, hit := set.tokens[token]
	if hit {
		s.touch(sessionID)
	}
	return hit
}

// computerUseToolSegment extracts the tool segment of a renamed computer-use
// tool ("mcp__computer-use__kill_app" → "kill_app"), trimming the naming
// pass's numeric collision suffix ("type_2" → "type") so a collision cannot
// slip a gated tool past the gate.
func computerUseToolSegment(name string) string {
	seg := name
	if i := strings.LastIndex(seg, "__"); i >= 0 {
		seg = seg[i+2:]
	}
	if i := strings.LastIndex(seg, "_"); i > 0 && isAllDigits(seg[i+1:]) {
		return seg[:i]
	}
	return seg
}

// isAllDigits reports whether s is a non-empty digit run.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// computerUseGatedTool reports whether the named tool sits in design D7's
// gated set (always-gated or conditionally-gated alike): such tools are
// wrapped with the approval gate, which decides per call.
func computerUseGatedTool(name string) bool {
	switch computerUseToolSegment(name) {
	case cuKillApp, cuWriteClipboard, cuType, cuSetValue:
		return true
	}
	return false
}

// computerUseObserves reports whether the named tool is an observation or
// listing tool whose result the secure-token recording parses.
func computerUseObserves(name string) bool {
	_, ok := cuObserveTools[computerUseToolSegment(name)]
	return ok
}

// argsString reads the first non-empty string value under one of keys from
// the call's arguments object. Malformed arguments degrade to "" — the
// child re-validates the raw arguments either way.
func argsString(argsJSON string, keys ...string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &obj); err != nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// computerUseApproval derives one gated call's approval payload — the
// requested action and its target, the string the approval card renders and
// the ledger keys the decision under (so it must be deterministic for the
// same call). gated reports whether THIS call parks: kill_app and
// write_clipboard always; type and set_value only when the call's target
// token is in the calling session's secure-token set. A tool outside the
// computer-use namespace never gates — the gate is the computer-use server's
// seam only. The payload carries the target's identity only — never a typed
// value or clipboard content.
func computerUseApproval(toolName, argsJSON, sessionID string) (payload string, gated bool) {
	if !strings.HasPrefix(toolName, computerUseNamePrefix) {
		return "", false
	}
	switch seg := computerUseToolSegment(toolName); seg {
	case cuKillApp:
		target := argsString(argsJSON, cuKillAppTargetKeys...)
		if target == "" {
			return seg, true
		}
		return seg + " " + target, true
	case cuWriteClipboard:
		return seg + " clipboard", true
	case cuType, cuSetValue:
		token := argsString(argsJSON, cuElementTargetKeys...)
		if !sessionSecureTokens.contains(sessionID, token) {
			return "", false
		}
		return seg + " " + token, true
	default:
		return "", false
	}
}

// wrapApprovalTools installs the computer-use interceptor over the named
// surface (design D7, beside wrapAttentionTools): observation/listing tools
// gain the secure-token recording pass-through, the gated subset gains the
// approval flow, and everything else — read-only tools and `stop` — passes
// through untouched. A scope without a record (never production) leaves the
// surface bare, exactly like the attention wrap.
func (m *manager) wrapApprovalTools(named []tool.BaseTool, at attentionScope) []tool.BaseTool {
	if at.record == nil {
		return named
	}
	out := make([]tool.BaseTool, 0, len(named))
	for _, t := range named {
		info, err := t.Info(context.Background())
		if err != nil || info == nil {
			out = append(out, t)
			continue
		}
		inv, ok := t.(tool.InvokableTool)
		if !ok {
			out = append(out, t)
			continue
		}
		switch {
		case computerUseObserves(info.Name):
			out = append(out, &approvalTool{InvokableTool: inv, serve: m.serveSecureTokenRecording(at, inv)})
		case computerUseGatedTool(info.Name):
			out = append(out, &approvalTool{InvokableTool: inv, serve: m.serveComputerUseApproval(inv, info.Name, at)})
		default:
			out = append(out, t)
		}
	}
	return out
}

// approvalTool is the interceptor wrapper: an invokable tool whose
// invocations run the approval or recording flow. Info stays promoted — the
// model sees exactly the renamed surface it was offered.
type approvalTool struct {
	tool.InvokableTool
	serve func(ctx context.Context, argumentsInJSON string) (string, error)
}

func (t *approvalTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	return t.serve(ctx, argumentsInJSON)
}

// serveSecureTokenRecording passes an observe/list result through unchanged
// and records the [secure] tokens it annotates (C2) into the calling
// session's bounded set. A failed call records nothing.
func (m *manager) serveSecureTokenRecording(at attentionScope, inner tool.InvokableTool) func(context.Context, string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		out, err := inner.InvokableRun(ctx, argsJSON)
		if err != nil {
			return out, err
		}
		if tokens := secureAnnotatedTokens(out); len(tokens) > 0 {
			sessionSecureTokens.record(at.sessionID, tokens...)
		}
		return out, nil
	}
}

// serveComputerUseApproval is the intercepted gated flow (design D7): derive
// the payload, follow the resume decision, consume one ledger decision to
// execute, or interrupt the run into the approval park. The gate mirrors the
// shell gate's ordering discipline (backend/shell_approval.go) with one
// difference: the resume context is read BEFORE the gating condition, so a
// bounded-set eviction between the park and the resume cannot turn a denial
// into an execution.
func (m *manager) serveComputerUseApproval(inner tool.InvokableTool, name string, at attentionScope) func(context.Context, string) (string, error) {
	ledger := checkpointLedger{checkpoints: m.store.Checkpoints()}
	return func(ctx context.Context, argsJSON string) (string, error) {
		payload, gated := computerUseApproval(name, argsJSON, at.sessionID)

		// A resumed run re-executes this tool from the checkpoint. Targeted
		// with data means the user denied: the data IS the tool result (spec
		// chat-approvals "Deny informs the model"). An implicit resume
		// (approval) falls through to the ledger, which ResolveApproval
		// stocked before resuming.
		wasInterrupted, _, _ := tool.GetInterruptState[any](ctx)
		if wasInterrupted {
			if isTarget, hasData, denial := tool.GetResumeContext[string](ctx); isTarget && hasData {
				return denial, nil
			}
		}
		if !gated {
			return inner.InvokableRun(ctx, argsJSON)
		}
		// First encounter (or an approval resume): one recorded decision buys
		// one execution — consume it. An identical action interrupts again.
		approved, err := ledger.Consume(ctx, backend.LedgerKey(payload))
		if err == nil && approved {
			return inner.InvokableRun(ctx, argsJSON)
		}
		// No spendable decision: interrupt. The action must not run; the run
		// pauses until the user decides (spec chat-approvals).
		return "", tool.Interrupt(ctx, backend.ShellApprovalInfo{Command: payload})
	}
}
