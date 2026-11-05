package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The browser-attention runtime (add-browser-login-handover, design D3/D6/D7,
// spec chat-approvals "Browser attention requests"): the parent intercepts
// the system server's submit_secret and request_takeover calls, prepares them
// on the child's reserved `__` tools, parks the tool call on a
// KindInputRequired event, and serves the resolution from an in-memory
// registry keyed by interrupt id.
//
// The invariant (design D1) is that the typed value never reaches the model,
// the transcript, or any persisted byte: it arrives through ResolveInput over
// the local bridge, lives in the registry entry the parked tool call waits
// on, rides one __deliver_secret call's arguments, and is dropped at the
// delivery. Events carry receipts only (outcome + redacted length).

// attentionScope is what a turn hands tool resolution so the attention
// wrapper can park and record: the owning session and turn, and the turn's
// record closure (the runTurn/resumeTurn recorder). A scope without a record
// (never production) resolves the tools bare.
type attentionScope struct {
	sessionID string
	turnID    string
	record    recordFn
}

// The child-side contract (design D2/D7 — the browser-use engine's reserved
// tools). The model never sees any of them: the reserved `__` prefix is
// filtered from every offered surface, and the parent alone composes these
// calls.
const (
	childRequestSecret    = "__request_secret"
	childDeliverSecret    = "__deliver_secret"
	childRequestTakeover  = "__request_takeover"
	childBeginTakeover    = "__begin_takeover"
	childResolveAttention = "__resolve_attention"
)

// attentionReasonPhrases maps the fixed reason-code vocabulary of the child's
// __request_secret / __request_takeover / __deliver_secret failures to short
// human phrases (design D2). An unknown code — or a missing one, which is
// what a transport-level failure produces — degrades to the neutral fallback,
// so no page-derived text ever reaches the model (design D1).
// The codes are the child's fixed wire vocabulary (internal/browseruse
// attention.go/secret.go/deliver.go reasonCode values); this map is the
// model-facing presentation of each.
var attentionReasonPhrases = map[string]string{
	// __request_secret prepare refusals.
	"insecure-origin":     "the sign-in page is not a secure HTTPS origin",
	"already-pending":     "this browser is already holding a secret request",
	"session-held":        "this browser is held for a takeover",
	"framed-target":       "the sign-in form sits inside a nested frame",
	"same-site-isolation": "another tab of this site is connected to the page",
	"target-unresolved":   "the sign-in fields could not be resolved",
	"profile-not-found":   "the browser profile is not available",
	"relaunch-failed":     "the browser could not be relaunched for the takeover",
	// __deliver_secret refusals.
	"request-gone":   "the request expired before the secret was delivered",
	"target-changed": "the sign-in form changed before the secret was filled",
	"fill-failed":    "the sign-in form could not be filled",
	"submit-failed":  "the sign-in could not be submitted",
	"scrub-failed":   "the page kept the value after submission; it was reloaded",
}

// attentionReasonFallback is the phrase for a reason code outside the fixed
// vocabulary (and for the child being unreachable outright).
const attentionReasonFallback = "the sign-in form could not be prepared"

// childCallTimeout bounds a parent→child attention invocation that must not
// ride the turn's context alone: the BeginTakeover handshake has to answer
// the user's card even when the wedged child never answers the tool call.
const childCallTimeout = 30 * time.Second

// unavailableAuthText renders the fixed model-facing tool text for an
// unavailable secure-authentication path (design D2): takeover is named as
// the fallback, never a retry.
func unavailableAuthText(reasonCode string) string {
	phrase := attentionReasonPhrases[reasonCode]
	if phrase == "" {
		phrase = attentionReasonFallback
	}
	return "Secure authentication is unavailable. " + phrase + ". …or use browser takeover."
}

// newAttentionID returns a random 128-bit hex interrupt id for an attention
// request, mirroring the turn id convention.
func newAttentionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("agents: generate attention id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// pendingInput is the live registry entry one parked attention tool call
// waits on (design D3). It exists in m.pendingInputs exactly while the tool
// call is parked; the value rides it in memory only and is dropped at the
// delivery. resolved carries the resolution (a buffered single send — the
// resolving guard lets exactly one resolve through); begin/beginResult are
// the BeginTakeover handshake with the parked takeover call.
type pendingInput struct {
	interruptID string
	attention   string // sessions.AttentionSecret or AttentionTakeover
	requestID   string // the child's live request this entry serves
	sessionID   string
	turnID      string

	// beginRequested reports whether BeginTakeover already fired for this
	// entry — the active card must not flip twice. Guarded by the manager's
	// mutex.
	beginRequested bool

	// mu guards the delivery payload below.
	mu           sync.Mutex
	value        string
	savedAs      bool
	identifier   string
	savedLoginID string

	resolved    chan inputResolution // buffered 1: exactly one resolve lands
	begin       chan struct{}        // buffered 1: BeginTakeover's signal
	beginResult chan error           // buffered 1: the child ack's outcome
}

// inputResolution is what unblocks a parked attention tool call.
type inputResolution struct {
	outcome string // sessions.InputCompleted or sessions.InputCancelled
}

// pendingInputRecord is the log-derived pending attention request (design D3
// — no schema, replay-safe): the latest input_required with no matching
// input_resolved after it.
type pendingInputRecord struct {
	interruptID string
	attention   string
	turnID      string
}

// pendingInputFromEvents derives the session's pending attention request from
// its log, the same derivation style as pendingApprovalFromEvents.
func pendingInputFromEvents(events []sessions.Event) *pendingInputRecord {
	resolved := make(map[string]bool)
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Input == nil {
			continue
		}
		switch ev.Kind {
		case sessions.KindInputResolved:
			resolved[ev.Input.InterruptID] = true
		case sessions.KindInputRequired:
			if !resolved[ev.Input.InterruptID] {
				return &pendingInputRecord{
					interruptID: ev.Input.InterruptID,
					attention:   ev.Input.AttentionKind,
					turnID:      ev.TurnID,
				}
			}
		}
	}
	return nil
}

// dropPendingInput removes the registry entry for id, if present. Idempotent:
// both the resolve path and the parked tool call's exits race for it.
func (m *manager) dropPendingInput(id string) {
	m.mu.Lock()
	delete(m.pendingInputs, id)
	m.mu.Unlock()
}

// installPendingInput lands the KindInputRequired event and stocks the
// registry entry the parked tool call will wait on. The entry goes in BEFORE
// the event (a resolve that derives the pending request from the log must
// find a live entry to hand the value to), and comes out again if the record
// fails — a pending request without a parked call would strand the value.
func (m *manager) installPendingInput(at attentionScope, kind attentionInstall) (*pendingInput, error) {
	entry := &pendingInput{
		interruptID: newAttentionID(),
		attention:   kind.attention,
		requestID:   kind.requestID,
		sessionID:   at.sessionID,
		turnID:      at.turnID,
		resolved:    make(chan inputResolution, 1),
		begin:       make(chan struct{}, 1),
		beginResult: make(chan error, 1),
	}
	m.mu.Lock()
	m.pendingInputs[entry.interruptID] = entry
	m.mu.Unlock()

	ev := sessions.Event{
		Kind: sessions.KindInputRequired,
		Input: &sessions.Input{
			InterruptID:   entry.interruptID,
			Prompt:        kind.prompt,
			Method:        kind.method,
			Digits:        kind.digits,
			Origin:        kind.origin,
			AttentionKind: kind.attention,
			Username:      kind.username,
			Identifier:    kind.identifier,
			LoginID:       kind.loginID,
		},
	}
	if err := at.record(&ev, true); err != nil {
		m.dropPendingInput(entry.interruptID)
		return nil, fmt.Errorf("record attention request: %w", err)
	}
	return entry, nil
}

// attentionInstall is the card description installPendingInput persists.
type attentionInstall struct {
	attention string
	requestID string
	prompt    string
	method    string
	digits    int
	origin    string
	username  string
	loginID   string
	// identifier marks that the card also collects a username-or-email
	// (the request declared an identifierTarget); the typed identifier
	// itself rides the delivery, never the event.
	identifier bool
}

// ResolveInput submits the user-typed secret for the session's pending
// browser attention request (design D3). Validation runs against the
// log-derived pending set and the resolving guard closes the
// validate-then-append window — the exact semantics of ResolveApproval. The
// value is stored into the registry entry the parked tool call waits on and
// nowhere else; the input_resolved receipt records outcome, redacted length,
// and the optional saved-login id only.
func (m *manager) ResolveInput(ctx context.Context, sessionID, interruptID, text, identifier string, savedAs bool, savedLoginID string) error {
	events, err := m.sessions.Events(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resolve input: %w", err)
	}
	pend := pendingInputFromEvents(events)
	if pend == nil || pend.interruptID != interruptID {
		return fmt.Errorf("resolve input: %w: %s", ErrInputNotPending, interruptID)
	}
	if pend.attention != sessions.AttentionSecret {
		return fmt.Errorf("resolve input: %w: %s is not a secret request", ErrInputNotPending, interruptID)
	}

	// The resolving guard (design D3): two concurrent resolves can both
	// derive the pending request before either append lands, so the first to
	// take the lock claims the interrupt and the second conflicts exactly
	// like the sequential already-resolved case.
	m.mu.Lock()
	if m.resolvingInputs[interruptID] {
		m.mu.Unlock()
		return fmt.Errorf("resolve input: %w: %s (already resolved)", ErrInputNotPending, interruptID)
	}
	m.resolvingInputs[interruptID] = true
	// A resolve that fails before its input_resolved append leaves the
	// pending state intact — release the claim so a retry can proceed.
	abandon := func() {
		m.mu.Lock()
		delete(m.resolvingInputs, interruptID)
		m.mu.Unlock()
	}
	entry := m.pendingInputs[interruptID]
	m.mu.Unlock()
	if entry == nil {
		// A pending request without a live registry entry: the process (and
		// the value slot with it) was lost — there is nothing to deliver to.
		// The card folds expired client-side (the frontend transcript fold
		// normalizes a still-pending card once the session is no longer busy
		// — expireUnresolvedInputs in frontend/src/features/chat/entries.ts);
		// accepting the value here would land a completed receipt for a fill
		// that can never happen.
		abandon()
		return fmt.Errorf("resolve input: %w: %s (no live request)", ErrInputNotPending, interruptID)
	}

	// The receipt is the durable trace — the value never enters it (design
	// D3). The recording rides a cancellation-proof context like
	// ResolveApproval's: the resolve may outlive the caller's request.
	receipt := sessions.Event{
		TurnID: pend.turnID,
		Kind:   sessions.KindInputResolved,
		Input: &sessions.Input{
			InterruptID:  pend.interruptID,
			Outcome:      sessions.InputCompleted,
			Length:       len(text),
			SavedLoginID: savedLoginID,
		},
	}
	recordCtx := context.WithoutCancel(m.baseCtx)
	batch := []sessions.Event{receipt}
	if err := m.sessions.Append(recordCtx, sessionID, batch); err != nil {
		abandon()
		return fmt.Errorf("resolve input: %w", err)
	}
	m.broadcast(sessionID, batch[0])

	// Hand the value to the parked tool call (in memory only — the channel
	// send makes the writes visible to the delivery), then drop the registry
	// entry: the value's whole lifetime is one __deliver_secret call.
	entry.mu.Lock()
	entry.value = text
	entry.identifier = identifier
	entry.savedAs = savedAs
	entry.mu.Unlock()
	entry.resolved <- inputResolution{outcome: sessions.InputCompleted}
	m.dropPendingInput(interruptID)
	return nil
}

// BeginTakeover approves the session's pending browser takeover request
// (design D8): it signals the parked tool call to hand the profile's browser
// over on the child (headed relaunch) and returns once the child acked — the
// card flips active exactly when this returns nil. A failed ack releases the
// begin claim so the user can retry.
func (m *manager) BeginTakeover(ctx context.Context, sessionID, interruptID string) error {
	events, err := m.sessions.Events(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("begin takeover: %w", err)
	}
	pend := pendingInputFromEvents(events)
	if pend == nil || pend.interruptID != interruptID {
		return fmt.Errorf("begin takeover: %w: %s", ErrInputNotPending, interruptID)
	}
	if pend.attention != sessions.AttentionTakeover {
		return fmt.Errorf("begin takeover: %w: %s is not a takeover request", ErrInputNotPending, interruptID)
	}

	m.mu.Lock()
	entry := m.pendingInputs[interruptID]
	if entry == nil {
		m.mu.Unlock()
		return fmt.Errorf("begin takeover: %w: %s (no live request)", ErrInputNotPending, interruptID)
	}
	if entry.beginRequested {
		m.mu.Unlock()
		return fmt.Errorf("begin takeover: %w: %s (already begun)", ErrInputNotPending, interruptID)
	}
	entry.beginRequested = true
	m.mu.Unlock()

	select {
	case entry.begin <- struct{}{}:
	default:
		return fmt.Errorf("begin takeover: %w: %s (already begun)", ErrInputNotPending, interruptID)
	}
	select {
	case err := <-entry.beginResult:
		if err != nil {
			// The handover did not happen; the card stays pending. Release
			// the begin claim so the user can retry.
			m.mu.Lock()
			entry.beginRequested = false
			m.mu.Unlock()
			return fmt.Errorf("begin takeover: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("begin takeover: %w", ctx.Err())
	}
}

// ResolveAttention ends the session's pending browser attention request with
// an explicit outcome — the takeover card's Done/Cancel path (design D8). The
// receipt lands on the transcript and the parked tool call unblocks to
// return its ending result.
func (m *manager) ResolveAttention(ctx context.Context, sessionID, interruptID, outcome string) error {
	switch outcome {
	case sessions.InputCompleted, sessions.InputCancelled:
	default:
		return fmt.Errorf("resolve attention: invalid outcome %q", outcome)
	}
	events, err := m.sessions.Events(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resolve attention: %w", err)
	}
	pend := pendingInputFromEvents(events)
	if pend == nil || pend.interruptID != interruptID {
		return fmt.Errorf("resolve attention: %w: %s", ErrInputNotPending, interruptID)
	}
	// A secret request completes only through ResolveInput's typed
	// submission — a generic completed resolution here would wake the parked
	// call into delivering an empty value.
	if pend.attention == sessions.AttentionSecret && outcome == sessions.InputCompleted {
		return fmt.Errorf("resolve attention: a secret request completes only through a typed submission")
	}

	m.mu.Lock()
	if m.resolvingInputs[interruptID] {
		m.mu.Unlock()
		return fmt.Errorf("resolve attention: %w: %s (already resolved)", ErrInputNotPending, interruptID)
	}
	m.resolvingInputs[interruptID] = true
	abandon := func() {
		m.mu.Lock()
		delete(m.resolvingInputs, interruptID)
		m.mu.Unlock()
	}
	entry := m.pendingInputs[interruptID]
	m.mu.Unlock()
	if entry == nil {
		abandon()
		return fmt.Errorf("resolve attention: %w: %s (no live request)", ErrInputNotPending, interruptID)
	}

	receipt := sessions.Event{
		TurnID: pend.turnID,
		Kind:   sessions.KindInputResolved,
		Input: &sessions.Input{
			InterruptID: pend.interruptID,
			Outcome:     outcome,
		},
	}
	recordCtx := context.WithoutCancel(m.baseCtx)
	batch := []sessions.Event{receipt}
	if err := m.sessions.Append(recordCtx, sessionID, batch); err != nil {
		abandon()
		return fmt.Errorf("resolve attention: %w", err)
	}
	m.broadcast(sessionID, batch[0])

	entry.resolved <- inputResolution{outcome: outcome}
	m.dropPendingInput(interruptID)
	return nil
}

// invokeChildTool invokes one of the system server's reserved tools by raw
// name over the supervised stdio channel (design D7): the parent already
// holds the connection through the MCP manager, so the internal channel is a
// plain tool call — no network listener, no new transport.
func (m *manager) invokeChildTool(ctx context.Context, ref mcp.Ref, name, argsJSON string) (string, error) {
	tools, err := m.mcpManager.Tools(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("invoke %s: %w", name, err)
	}
	for _, t := range tools {
		info, ierr := t.Info(ctx)
		if ierr != nil || info == nil || info.Name != name {
			continue
		}
		inv, ok := t.(tool.InvokableTool)
		if !ok {
			break
		}
		out, err := inv.InvokableRun(ctx, argsJSON)
		if err != nil {
			return "", err
		}
		return unwrapChildEnvelope(out), nil
	}
	return "", fmt.Errorf("invoke %s: the browser engine does not expose it", name)
}

// unwrapChildEnvelope strips the MCP result envelope the child's stdio
// server wraps every tool result in ({"content":[{"type":"text",...}]}):
// the receipts the parent parses are the inner text. A body that is not
// envelope-shaped passes through unchanged.
func unwrapChildEnvelope(out string) string {
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Content) == 0 {
		return out
	}
	first := env.Content[0].Text
	if first == "" {
		return out
	}
	return first
}

// wrapAttentionTools installs the session-aware interceptor over the system
// server's submit_secret and request_takeover (design D7): the model-facing
// names keep their renamed Info, but every invocation drives the
// prepare-park-deliver flow instead of reaching the child directly. A scope
// without a record (never production) leaves the surface bare.
func (m *manager) wrapAttentionTools(ref mcp.Ref, named []tool.BaseTool, at attentionScope) []tool.BaseTool {
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
		case strings.HasSuffix(info.Name, "__submit_secret"):
			out = append(out, &attentionTool{InvokableTool: inv, serve: m.serveSubmitSecret(ref, at)})
		case strings.HasSuffix(info.Name, "__request_takeover"):
			out = append(out, &attentionTool{InvokableTool: inv, serve: m.serveTakeover(ref, at)})
		default:
			out = append(out, t)
		}
	}
	return out
}

// attentionTool is the interceptor wrapper: an invokable tool whose
// invocations run the attention flow. Info stays promoted — the model sees
// exactly the renamed surface it was offered.
type attentionTool struct {
	tool.InvokableTool
	serve func(ctx context.Context, argumentsInJSON string) (string, error)
}

func (t *attentionTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	return t.serve(ctx, argumentsInJSON)
}

// childReadyResponse is the ready/unavailable answer of the child's
// __request_secret and __request_takeover. The echo fields (method, digits,
// username) are optional: when the child echoes them they win over the
// model's arguments, since they are the post-validation view.
type childReadyResponse struct {
	Status     string `json:"status"`
	ReasonCode string `json:"reasonCode"`
	RequestID  string `json:"requestID"`
	Origin     string `json:"origin"`
	Profile    string `json:"profile"`
	Method     string `json:"method"`
	Digits     int    `json:"digits"`
	Username   string `json:"username"`
}

// childReceiptResponse is the answer of the child's __deliver_secret.
type childReceiptResponse struct {
	Status     string `json:"status"`
	ReasonCode string `json:"reasonCode"`
}

// submitSecretArgs is what the interceptor reads out of the model's
// submit_secret arguments for the attention card. The arguments themselves
// travel to the child verbatim — the value field the schema refuses cannot
// smuggle a secret in here.
type submitSecretArgs struct {
	Method           string `json:"method"`
	Digits           int    `json:"digits"`
	Username         string `json:"username"`
	LoginID          string `json:"loginId"`
	IdentifierTarget string `json:"identifierTarget"`
}

// childRequestArgs builds the JSON args of a child invocation.
func childRequestArgs(pairs ...string) string {
	obj := make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		obj[pairs[i]] = pairs[i+1]
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

// serveSubmitSecret is the intercepted submit_secret flow (design D2/D3):
// prepare on the child, park on the attention card, deliver on resolution.
func (m *manager) serveSubmitSecret(ref mcp.Ref, at attentionScope) func(context.Context, string) (string, error) {
	return func(ctx context.Context, args string) (string, error) {
		// Card fields read out of the model's request (schema-bounded);
		// malformed arguments degrade to zero values — the child re-validates
		// the raw arguments either way.
		var modelArgs submitSecretArgs
		_ = json.Unmarshal([]byte(args), &modelArgs)

		out, err := m.invokeChildTool(ctx, ref, childRequestSecret, args)
		if err != nil {
			// The child is unreachable: the fixed unavailable text, never the
			// transport error (design D1 — fixed reason phrases only).
			return unavailableAuthText(""), nil
		}
		var ready childReadyResponse
		if jerr := json.Unmarshal([]byte(out), &ready); jerr != nil {
			return unavailableAuthText(""), nil
		}
		if ready.Status != "ready" {
			return unavailableAuthText(ready.ReasonCode), nil
		}

		prompt := "The agent asks you to sign in."
		if ready.Origin != "" {
			prompt = "The agent asks you to sign in at " + ready.Origin + "."
		}
		method := ready.Method
		if method == "" {
			method = modelArgs.Method
		}
		digits := ready.Digits
		if digits == 0 {
			digits = modelArgs.Digits
		}
		username := ready.Username
		if username == "" {
			username = modelArgs.Username
		}
		entry, err := m.installPendingInput(at, attentionInstall{
			attention:  sessions.AttentionSecret,
			requestID:  ready.RequestID,
			prompt:     prompt,
			method:     method,
			digits:     digits,
			origin:     ready.Origin,
			username:   username,
			loginID:    modelArgs.LoginID,
			identifier: modelArgs.IdentifierTarget != "",
		})
		if err != nil {
			return "", err
		}

		// Park: the tool call blocks the run here until the user submits the
		// value (the resolution channel), cancels the card, or the turn is
		// cancelled.
		select {
		case <-ctx.Done():
			// A resolution may have landed alongside the cancellation —
			// prefer it so the durable receipt is not followed by a
			// conflicting delivery.
			select {
			case res := <-entry.resolved:
				if res.outcome == sessions.InputCancelled {
					return m.cancelAttention(context.WithoutCancel(ctx), ref, entry), nil
				}
				return m.deliverSecret(context.WithoutCancel(ctx), ref, entry)
			default:
				return m.cancelAttention(ctx, ref, entry), nil
			}
		case res := <-entry.resolved:
			// A person cancel (the card's Cancel button) ends the request
			// without a delivery: the child's freeze is released and the run
			// continues on the cancelled result — the same refusal the
			// model-facing fallback text promises.
			if res.outcome == sessions.InputCancelled {
				return m.cancelAttention(ctx, ref, entry), nil
			}
			return m.deliverSecret(ctx, ref, entry)
		}
	}
}

// deliverSecret serves the parked secret call's resolution: the value leaves
// the registry entry for exactly one __deliver_secret call and is dropped
// from memory at once (design D1 — minimal lifetime, one fill frame).
func (m *manager) deliverSecret(ctx context.Context, ref mcp.Ref, entry *pendingInput) (string, error) {
	entry.mu.Lock()
	value := entry.value
	savedAs := entry.savedAs
	entry.value = ""
	entry.mu.Unlock()
	m.dropPendingInput(entry.interruptID)

	deliverPairs := []string{"requestID", entry.requestID, "value", value}
	if entry.identifier != "" {
		deliverPairs = append(deliverPairs, "identifier", entry.identifier)
	}
	out, err := m.invokeChildTool(ctx, ref, childDeliverSecret,
		childRequestArgs(deliverPairs...))
	if err != nil {
		m.releaseChildAttention(ctx, ref, entry.requestID, sessions.InputFailed)
		return unavailableAuthText(""), nil
	}
	var receipt childReceiptResponse
	if jerr := json.Unmarshal([]byte(out), &receipt); jerr != nil || receipt.Status != "completed" {
		// The child refused the delivery: release its protection freeze
		// best-effort and answer the fixed unavailable text — the agent's
		// fallback is takeover, never a retry (design D2).
		m.releaseChildAttention(ctx, ref, entry.requestID, sessions.InputFailed)
		if jerr == nil {
			return unavailableAuthText(receipt.ReasonCode), nil
		}
		return unavailableAuthText(""), nil
	}
	result, _ := json.Marshal(struct {
		Status       string `json:"status"`
		SavedAsLogin bool   `json:"savedAsLogin"`
	}{Status: "completed", SavedAsLogin: savedAs})
	return string(result), nil
}

// cancelAttention releases a parked attention request whose turn was
// cancelled: the child's protection freeze is released best-effort and the
// tool call answers the cancelled result (the run itself takes the cancelled
// terminal through the normal failure path).
func (m *manager) cancelAttention(ctx context.Context, ref mcp.Ref, entry *pendingInput) string {
	m.dropPendingInput(entry.interruptID)
	m.releaseChildAttention(context.WithoutCancel(ctx), ref, entry.requestID, sessions.InputCancelled)
	return `{"status":"cancelled"}`
}

// releaseChildAttention invokes the child's __resolve_attention best-effort
// (design D8): the child's held state ends at the outcome, and a failure here
// can only cost the child its own timeout — it is never surfaced.
func (m *manager) releaseChildAttention(ctx context.Context, ref mcp.Ref, requestID, outcome string) {
	_, _ = m.invokeChildTool(ctx, ref, childResolveAttention,
		childRequestArgs("requestID", requestID, "outcome", outcome))
}

// serveTakeover is the intercepted request_takeover flow (design D8): prepare
// on the child, park on the takeover card, flip active on BeginTakeover, and
// end on ResolveAttention.
func (m *manager) serveTakeover(ref mcp.Ref, at attentionScope) func(context.Context, string) (string, error) {
	return func(ctx context.Context, args string) (string, error) {
		out, err := m.invokeChildTool(ctx, ref, childRequestTakeover, args)
		if err != nil {
			return unavailableAuthText(""), nil
		}
		var ready childReadyResponse
		if jerr := json.Unmarshal([]byte(out), &ready); jerr != nil {
			return unavailableAuthText(""), nil
		}
		if ready.Status != "ready" {
			return unavailableAuthText(ready.ReasonCode), nil
		}

		profile := ready.Profile
		if profile == "" {
			profile = "browser"
		}
		prompt := "The agent asks to hand over the " + profile + " browser for sign-in."
		entry, err := m.installPendingInput(at, attentionInstall{
			attention: sessions.AttentionTakeover,
			requestID: ready.RequestID,
			prompt:    prompt,
			origin:    ready.Origin,
		})
		if err != nil {
			return "", err
		}

		// Park: pending (Approve/Cancel) until BeginTakeover flips the card
		// active, then Done/Cancel until the resolution ends the held state.
		for {
			select {
			case <-ctx.Done():
				select {
				case res := <-entry.resolved:
					return m.endTakeover(context.WithoutCancel(ctx), ref, entry, res.outcome), nil
				default:
					return m.cancelAttention(ctx, ref, entry), nil
				}
			case <-entry.begin:
				bctx, cancel := context.WithTimeout(ctx, childCallTimeout)
				_, berr := m.invokeChildTool(bctx, ref, childBeginTakeover,
					childRequestArgs("requestID", entry.requestID))
				cancel()
				select {
				case entry.beginResult <- berr:
				default:
				}
				if berr != nil {
					continue // the card stays pending; BeginTakeover surfaced the error
				}
			case res := <-entry.resolved:
				return m.endTakeover(ctx, ref, entry, res.outcome), nil
			}
		}
	}
}

// endTakeover serves the parked takeover call's resolution (design D8): the
// child's held state ends best-effort at the outcome and the ending result
// tells the agent to re-observe the browser.
func (m *manager) endTakeover(ctx context.Context, ref mcp.Ref, entry *pendingInput, outcome string) string {
	m.dropPendingInput(entry.interruptID)
	m.releaseChildAttention(context.WithoutCancel(ctx), ref, entry.requestID, outcome)
	if outcome == sessions.InputCompleted {
		return `{"status":"completed","next":"take a fresh snapshot"}`
	}
	return `{"status":"cancelled"}`
}
