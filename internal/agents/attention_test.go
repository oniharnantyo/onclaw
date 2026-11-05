package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The browser-attention tests (add-browser-login-handover tasks.md 3.1/3.2
// and 2.5's parent half, design D3/D6/D7): the reserved `__` tools never
// reach the model, the intercepted submit_secret/request_takeover park the
// turn on a KindInputRequired card, ResolveInput serves the value to exactly
// one __deliver_secret call while the events carry receipts only, and the
// takeover flows begin and end on the child's reserved channel.

// scriptedChildTool is a fake child (browser-use engine) tool: its raw name
// is what the parent's reserved channel addresses, and every invocation is
// recorded with its exact arguments so tests can assert where the value
// traveled.
type scriptedChildTool struct {
	raw   string
	reply func(args string) (string, error)

	mu    sync.Mutex
	calls []string
}

func (t *scriptedChildTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.raw}, nil
}

func (t *scriptedChildTool) InvokableRun(_ context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	t.mu.Lock()
	t.calls = append(t.calls, argsJSON)
	t.mu.Unlock()
	if t.reply == nil {
		return `{"status":"ok"}`, nil
	}
	return t.reply(argsJSON)
}

func (t *scriptedChildTool) recorded() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

func staticReply(out string) func(string) (string, error) {
	return func(string) (string, error) { return out, nil }
}

// attentionRecord is the turn's record closure, test-grade: durable appends
// through the sessions service on a fixed turn id, then broadcast.
func attentionRecord(t *testing.T, f *fixture, sessionID, turnID string) recordFn {
	t.Helper()
	return func(ev *sessions.Event, durable bool) error {
		ev.TurnID = turnID
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = now()
		}
		if durable {
			batch := []sessions.Event{*ev}
			if err := f.sessions.Append(context.WithoutCancel(t.Context()), sessionID, batch); err != nil {
				return err
			}
			*ev = batch[0]
		}
		f.mgr.broadcast(sessionID, *ev)
		return nil
	}
}

// resolveAttentionSurface resolves the fixture agent's MCP surface with the
// attention scope bound to sessionID/turnID and returns the tool named
// name (the renamed model-facing surface).
func resolveAttentionSurface(t *testing.T, f *fixture, src *systemMCPSource, sessionID, turnID, name string) tool.InvokableTool {
	t.Helper()
	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{
		sessionID: sessionID,
		turnID:    turnID,
		record:    attentionRecord(t, f, sessionID, turnID),
	})
	require.NoError(t, err)
	for _, tl := range tools {
		info, ierr := tl.Info(context.Background())
		require.NoError(t, ierr)
		if info.Name == name {
			inv, ok := tl.(tool.InvokableTool)
			require.True(t, ok, "%s must stay invokable through the wrapper", name)
			return inv
		}
	}
	t.Fatalf("tool %s not found in the resolved surface: %v", name, mcpToolNames(t, tools))
	return nil
}

// awaitInputRequired waits for the session's log to end with the
// input_required event (the install happens on the parked goroutine).
func awaitInputRequired(t *testing.T, f *fixture, sessionID string) sessions.Event {
	t.Helper()
	require.Eventually(t, func() bool {
		events := mustEvents(t, f, sessionID)
		return len(events) > 0 && events[len(events)-1].Kind == sessions.KindInputRequired
	}, 5*time.Second, 10*time.Millisecond, "waiting for a persisted input_required event")
	events := mustEvents(t, f, sessionID)
	return events[len(events)-1]
}

// assertNoValueInEvents byte-greps the session log's encoded events for the
// typed value (design D3: the value never enters any event).
func assertNoValueInEvents(t *testing.T, f *fixture, sessionID, value string) {
	t.Helper()
	for _, ev := range mustEvents(t, f, sessionID) {
		raw, err := json.Marshal(ev)
		require.NoError(t, err)
		require.NotContains(t, string(raw), value,
			"event %s (%s) must not carry the typed value", ev.Kind, ev.ID)
	}
}

// attentionResult carries one parked tool call's outcome across goroutines.
type attentionResult struct {
	out string
	err error
}

// invokeAsync runs a wrapped tool call on its own goroutine (it parks) and
// returns the result channel.
func invokeAsync(ctx context.Context, tl tool.InvokableTool, args string) chan attentionResult {
	resultCh := make(chan attentionResult, 1)
	go func() {
		out, err := tl.InvokableRun(ctx, args)
		resultCh <- attentionResult{out: out, err: err}
	}()
	return resultCh
}

// TestResolveMCPTools_FiltersReservedPrefix pins the reserved-prefix filter
// (tasks.md 2.5, design D7): the model's surface carries no `__` tool, from
// the system server or any user server — even when the child advertises them.
func TestResolveMCPTools_FiltersReservedPrefix(t *testing.T) {
	src := newSystemMCPSource()
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	// The child advertises the whole internal channel beside its real tools.
	src.tools[mcp.SystemServerID] = []tool.BaseTool{
		fakeMCPTool{raw: "navigate"},
		fakeMCPTool{raw: "__request_secret"},
		fakeMCPTool{raw: "__deliver_secret"},
		fakeMCPTool{raw: "__begin_takeover"},
		fakeMCPTool{raw: "__resolve_attention"},
		fakeMCPTool{raw: "__request_takeover"},
		fakeMCPTool{raw: "submit_secret"},
		fakeMCPTool{raw: "request_takeover"},
	}
	// A user server trying the same trick.
	stored := f.seedMCPServer(t, "github", true, false)
	src.tools[stored.ID] = []tool.BaseTool{
		fakeMCPTool{raw: "create_issue"},
		fakeMCPTool{raw: "__exfiltrate"},
	}

	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{})
	require.NoError(t, err)
	names := mcpToolNames(t, tools)
	require.ElementsMatch(t, []string{
		"mcp__browser-use__navigate",
		"mcp__browser-use__submit_secret",
		"mcp__browser-use__request_takeover",
		"mcp__github__create_issue",
	}, names, "the reserved `__` tools must never be offered to the model")
	for _, name := range names {
		require.False(t, strings.Contains(name, "___"),
			"a reserved tool leaked through the filter: %s", name)
	}
}

// TestAttentionSecret_HappyPath drives the full interception (design D3/D7):
// prepare → park on the card → ResolveInput → deliver → receipt, with the
// typed value appearing in exactly one place — the __deliver_secret call's
// arguments — and in no event.
func TestAttentionSecret_HappyPath(t *testing.T) {
	src := newSystemMCPSource()
	const typedValue = "s3cret-value"
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-1","origin":"https://example.com","method":"password","username":"sam@example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)
	const turnID = "turn-attn-1"

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, turnID, "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"],"method":"password","username":"sam@example.com"}`)

	pending := awaitInputRequired(t, f, sess.ID)
	require.NotNil(t, pending.Input)
	require.NotEmpty(t, pending.Input.InterruptID)
	require.Equal(t, sessions.AttentionSecret, pending.Input.AttentionKind)
	require.Equal(t, "https://example.com", pending.Input.Origin)
	require.Equal(t, "The agent asks you to sign in at https://example.com.", pending.Input.Prompt)
	require.Equal(t, "password", pending.Input.Method)
	require.Equal(t, "sam@example.com", pending.Input.Username)
	require.Equal(t, turnID, pending.TurnID, "the card parks inside the turn")

	// The prepare call carried the model's arguments verbatim.
	require.Equal(t, []string{`{"targets":["password"],"method":"password","username":"sam@example.com"}`}, request.recorded())

	// Resolve: the value rides to the parked call, never into an event.
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, typedValue, "", false, ""))
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"completed","savedAsLogin":false}`, res.out)

	// Exactly one delivery, and the value appears ONLY in its arguments.
	deliveries := deliver.recorded()
	require.Len(t, deliveries, 1)
	require.JSONEq(t, fmt.Sprintf(`{"requestID":"req-1","value":%q}`, typedValue), deliveries[0])
	assertNoValueInEvents(t, f, sess.ID, typedValue)

	// The receipt: outcome, redacted length, saved-login id — no value.
	events := mustEvents(t, f, sess.ID)
	resolved := events[len(events)-1]
	require.Equal(t, sessions.KindInputResolved, resolved.Kind)
	require.NotNil(t, resolved.Input)
	require.Equal(t, sessions.InputCompleted, resolved.Input.Outcome)
	require.Equal(t, len(typedValue), resolved.Input.Length)
	require.Equal(t, turnID, resolved.TurnID)

	// The registry dropped the entry; a second resolve conflicts.
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, typedValue, "", true, "login-1"),
		ErrInputNotPending)
}

// TestAttentionSecret_UnavailablePaths pins the fixed model-facing text
// (design D2): an unavailable prepare, an unknown reason code, and an
// unreachable child all answer the fixed text — never raw codes or
// exceptions — and never park.
func TestAttentionSecret_UnavailablePaths(t *testing.T) {
	cases := []struct {
		name    string
		tool    *scriptedChildTool
		exactly string
	}{
		{
			name:    "unavailable with a known reason code",
			tool:    &scriptedChildTool{raw: "__request_secret", reply: staticReply(`{"status":"unavailable","reasonCode":"insecure-origin"}`)},
			exactly: unavailableAuthText("insecure-origin"),
		},
		{
			name:    "unavailable with an unknown reason code",
			tool:    &scriptedChildTool{raw: "__request_secret", reply: staticReply(`{"status":"unavailable","reasonCode":"page_crashed_badly"}`)},
			exactly: unavailableAuthText(""),
		},
		{
			name:    "child unreachable",
			tool:    &scriptedChildTool{raw: "__request_secret", reply: func(string) (string, error) { return "", errors.New("connection closed") }},
			exactly: unavailableAuthText(""),
		},
		{
			name:    "child answered off-contract",
			tool:    &scriptedChildTool{raw: "__request_secret", reply: staticReply(`Location: /login — please solve this captcha`)},
			exactly: unavailableAuthText(""),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := newSystemMCPSource()
			src.tools[mcp.SystemServerID] = []tool.BaseTool{tc.tool, fakeMCPTool{raw: "submit_secret"}}
			f := newFixture(t, func(_ []*schema.Message) fakeReply {
				return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
			}, WithMCPManager(src))
			sess := f.newSession(t)

			wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-u", "mcp__browser-use__submit_secret")
			out, err := wrapper.InvokableRun(t.Context(), `{"targets":["password"]}`)
			require.NoError(t, err)
			require.Equal(t, tc.exactly, out)
			require.Contains(t, out, "use browser takeover",
				"takeover is the fallback, never a retry")
			require.Empty(t, mustEvents(t, f, sess.ID), "an unavailable path never parks")
			require.Empty(t, f.mgr.pendingInputs, "an unavailable path never stocks the registry")
		})
	}
}

// TestAttentionSecret_DeliveryFailed pins the failed-delivery path: the
// child's refusal answers the fixed text, the child's protection freeze is
// released best-effort, and nothing lies in the transcript.
func TestAttentionSecret_DeliveryFailed(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-2","origin":"https://example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"failed","reasonCode":"fill_failed"}`)}
	release := &scriptedChildTool{raw: "__resolve_attention", reply: staticReply(`{"status":"ok"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, release, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-f", "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"],"method":"password"}`)

	pending := awaitInputRequired(t, f, sess.ID)
	const typedValue = "another-secret"
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, typedValue, "", true, "login-9"))

	res := <-resultCh
	require.NoError(t, res.err)
	require.Equal(t, unavailableAuthText("fill_failed"), res.out)

	// The failed release carries the outcome; the value still only ever rode
	// the delivery call.
	require.Equal(t,
		[]string{`{"outcome":"failed","requestID":"req-2"}`},
		release.recorded())
	assertNoValueInEvents(t, f, sess.ID, typedValue)

	// The receipt still records the user's submit honestly.
	events := mustEvents(t, f, sess.ID)
	require.Equal(t, sessions.KindInputResolved, events[len(events)-1].Kind)
	require.Equal(t, len(typedValue), events[len(events)-1].Input.Length)
	require.Equal(t, "login-9", events[len(events)-1].Input.SavedLoginID)
}

// TestResolveInput_StaleAndKindRefusals pins the guard semantics (design D3):
// a stale id, an already-resolved receipt, a takeover-kind pending, and a
// lost registry entry all refuse with ErrInputNotPending and change nothing.
func TestResolveInput_StaleAndKindRefusals(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-3","origin":"https://example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	// Nothing pending at all.
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, "no-such-id", "x", "", false, ""), ErrInputNotPending)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-s", "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"]}`)
	pending := awaitInputRequired(t, f, sess.ID)

	// Simulate the process losing the registry (cold restart): the log still
	// shows the request pending, but there is nothing to deliver to. The
	// parked wrapper keeps waiting on the original entry, which is restored
	// below for the real resolve.
	f.mgr.mu.Lock()
	liveEntry := f.mgr.pendingInputs[pending.Input.InterruptID]
	f.mgr.mu.Unlock()
	require.NotNil(t, liveEntry)
	f.mgr.dropPendingInput(pending.Input.InterruptID)
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "x", "", false, ""),
		ErrInputNotPending, "a pending request without a live registry entry refuses the value")
	require.Empty(t, deliver.recorded(), "no delivery may happen for a lost request")
	derived := pendingInputFromEvents(mustEvents(t, f, sess.ID))
	require.NotNil(t, derived, "the refusal left the pending state intact")

	// A takeover-kind pending refuses ResolveInput.
	f.mgr.mu.Lock()
	f.mgr.pendingInputs["take-pending"] = &pendingInput{
		interruptID: "take-pending",
		attention:   sessions.AttentionTakeover,
		resolved:    make(chan inputResolution, 1),
		begin:       make(chan struct{}, 1),
		beginResult: make(chan error, 1),
	}
	f.mgr.mu.Unlock()
	require.NoError(t, f.sessions.Append(t.Context(), sess.ID, []sessions.Event{
		{TurnID: "turn-attn-s", Kind: sessions.KindInputRequired, Input: &sessions.Input{
			InterruptID: "take-pending", AttentionKind: sessions.AttentionTakeover,
		}},
	}))
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, "take-pending", "x", "", false, ""),
		ErrInputNotPending, "a takeover request takes BeginTakeover, not a value")

	// BeginTakeover handshakes with the parked interceptor (emulated here),
	// a second begin conflicts, and a begin aimed at a secret-kind id
	// conflicts too.
	beginDone := make(chan error, 1)
	go func() { beginDone <- f.mgr.BeginTakeover(t.Context(), sess.ID, "take-pending") }()
	f.mgr.mu.Lock()
	takeEntry := f.mgr.pendingInputs["take-pending"]
	f.mgr.mu.Unlock()
	<-takeEntry.begin
	takeEntry.beginResult <- nil
	require.NoError(t, <-beginDone)
	require.ErrorIs(t, f.mgr.BeginTakeover(t.Context(), sess.ID, "take-pending"), ErrInputNotPending,
		"the card is already active")
	require.ErrorIs(t, f.mgr.BeginTakeover(t.Context(), sess.ID, pending.Input.InterruptID), ErrInputNotPending)

	// ResolveAttention refuses an outcome outside the fixed vocabulary.
	require.Error(t, f.mgr.ResolveAttention(t.Context(), sess.ID, "take-pending", "expired"))

	// End the takeover pending so the secret request is the derivable one
	// again, then resolve it for real and pin the double-resolve conflict.
	require.NoError(t, f.mgr.ResolveAttention(t.Context(), sess.ID, "take-pending", sessions.InputCompleted))
	f.mgr.mu.Lock()
	f.mgr.pendingInputs[pending.Input.InterruptID] = liveEntry
	f.mgr.mu.Unlock()
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "real", "", false, ""))
	res := <-resultCh
	require.NoError(t, res.err)
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "again", "", false, ""),
		ErrInputNotPending, "an already-resolved receipt conflicts")
	require.ErrorIs(t, f.mgr.ResolveAttention(t.Context(), sess.ID, "no-such-id", sessions.InputCompleted), ErrInputNotPending)
}

// TestResolveInput_ConcurrentResolveConflicts pins the resolving guard with
// the deterministic gate store (design D3): a resolve holding a
// pre-resolution snapshot of the log conflicts exactly like the sequential
// case, and exactly one input_resolved lands.
func TestResolveInput_ConcurrentResolveConflicts(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-4","origin":"https://example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-c", "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"]}`)
	pending := awaitInputRequired(t, f, sess.ID)

	gate := &resolveGateStore{
		Service:        f.mgr.sessions,
		eventsEntered:  make(chan struct{}, 1),
		releaseEvents:  make(chan struct{}),
		appendCalls:    make(chan struct{}, 4),
		releaseAppends: make(chan struct{}),
	}
	f.mgr.sessions = gate

	// Racer A reads the log and parks holding the pre-resolution snapshot.
	racing := make(chan error, 1)
	go func() {
		racing <- f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "from-a", "", false, "")
	}()
	<-gate.eventsEntered

	// Racer B runs to its input_resolved append and parks there: its guard is
	// claimed — the exact window the guard closes.
	winner := make(chan error, 1)
	go func() {
		winner <- f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "from-b", "", false, "")
	}()
	<-gate.appendCalls

	// Release A into the window: it must conflict instead of appending a
	// second resolution.
	close(gate.releaseEvents)
	require.ErrorIs(t, <-racing, ErrInputNotPending,
		"the stale-snapshot resolve conflicts instead of appending a second resolution")

	// Let B land its resolution; the parked call receives B's value.
	close(gate.releaseAppends)
	require.NoError(t, <-winner)
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"completed","savedAsLogin":false}`, res.out)

	require.Equal(t,
		[]string{`{"requestID":"req-4","value":"from-b"}`},
		deliver.recorded(), "exactly one delivery happened, with the winning resolve's value")
	resolutions := 0
	for _, ev := range mustEvents(t, f, sess.ID) {
		if ev.Kind == sessions.KindInputResolved {
			resolutions++
		}
	}
	require.Equal(t, 1, resolutions, "exactly one input_resolved lands")
}

// TestTakeover_BeginAndEndings drives the takeover flow (design D8): the card
// parks pending, BeginTakeover hands the browser over (the child ack flips
// the card active), and ResolveAttention ends the held state with the
// completed/cancelled endings.
func TestTakeover_BeginAndEndings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		outcome    string
		wantResult string
	}{
		{
			name:       "completed ending",
			outcome:    sessions.InputCompleted,
			wantResult: `{"status":"completed","next":"take a fresh snapshot"}`,
		},
		{
			name:       "cancelled ending",
			outcome:    sessions.InputCancelled,
			wantResult: `{"status":"cancelled"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newSystemMCPSource()
			request := &scriptedChildTool{raw: "__request_takeover", reply: staticReply(
				`{"status":"ready","requestID":"take-1","profile":"work"}`)}
			begin := &scriptedChildTool{raw: "__begin_takeover", reply: staticReply(`{"status":"started"}`)}
			release := &scriptedChildTool{raw: "__resolve_attention", reply: staticReply(`{"status":"ok"}`)}
			src.tools[mcp.SystemServerID] = []tool.BaseTool{request, begin, release, fakeMCPTool{raw: "request_takeover"}}

			f := newFixture(t, func(_ []*schema.Message) fakeReply {
				return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
			}, WithMCPManager(src))
			sess := f.newSession(t)

			wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-take", "mcp__browser-use__request_takeover")
			resultCh := invokeAsync(t.Context(), wrapper, `{"reason":"sign-in-blocked"}`)

			pending := awaitInputRequired(t, f, sess.ID)
			require.NotNil(t, pending.Input)
			require.Equal(t, sessions.AttentionTakeover, pending.Input.AttentionKind)
			require.Equal(t, "The agent asks to hand over the work browser for sign-in.", pending.Input.Prompt)

			// Approve: the child hands the browser over; BeginTakeover
			// returns nil only after the ack.
			require.NoError(t, f.mgr.BeginTakeover(t.Context(), sess.ID, pending.Input.InterruptID))
			require.Equal(t, []string{`{"requestID":"take-1"}`}, begin.recorded())

			// A second begin conflicts — the card is already active.
			require.ErrorIs(t, f.mgr.BeginTakeover(t.Context(), sess.ID, pending.Input.InterruptID), ErrInputNotPending)

			// Done/Cancel: the receipt lands and the parked call returns the
			// ending result; the child's held state ends at the outcome.
			require.NoError(t, f.mgr.ResolveAttention(t.Context(), sess.ID, pending.Input.InterruptID, tc.outcome))
			res := <-resultCh
			require.NoError(t, res.err)
			require.JSONEq(t, tc.wantResult, res.out)
			require.Equal(t,
				[]string{fmt.Sprintf(`{"outcome":%q,"requestID":"take-1"}`, tc.outcome)},
				release.recorded())

			events := mustEvents(t, f, sess.ID)
			resolved := events[len(events)-1]
			require.Equal(t, sessions.KindInputResolved, resolved.Kind)
			require.Equal(t, tc.outcome, resolved.Input.Outcome)

			// Everything is closed; a second resolution conflicts.
			require.ErrorIs(t, f.mgr.ResolveAttention(t.Context(), sess.ID, pending.Input.InterruptID, tc.outcome),
				ErrInputNotPending)
		})
	}
}

// TestTakeover_BeginChildFailure pins the retry path: a failed begin ack
// surfaces the error, keeps the card pending, and allows a retry.
func TestTakeover_BeginChildFailure(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_takeover", reply: staticReply(
		`{"status":"ready","requestID":"take-2","profile":"work"}`)}
	begin := &scriptedChildTool{raw: "__begin_takeover", reply: func(string) (string, error) {
		return "", errors.New("child is restarting")
	}}
	release := &scriptedChildTool{raw: "__resolve_attention", reply: staticReply(`{"status":"ok"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, begin, release, fakeMCPTool{raw: "request_takeover"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-take-f", "mcp__browser-use__request_takeover")
	resultCh := invokeAsync(t.Context(), wrapper, `{}`)
	pending := awaitInputRequired(t, f, sess.ID)

	require.Error(t, f.mgr.BeginTakeover(t.Context(), sess.ID, pending.Input.InterruptID),
		"the failed ack surfaces to the caller")

	// The failed begin releases the claim: a retry (against a healed child)
	// proceeds and completes.
	begin.reply = staticReply(`{"status":"started"}`)
	require.NoError(t, f.mgr.BeginTakeover(t.Context(), sess.ID, pending.Input.InterruptID))
	require.NoError(t, f.mgr.ResolveAttention(t.Context(), sess.ID, pending.Input.InterruptID, sessions.InputCompleted))
	res := <-resultCh
	require.JSONEq(t, `{"status":"completed","next":"take a fresh snapshot"}`, res.out)
}

// TestAttention_CancelWhileParked pins the cancellation path (design D3):
// cancelling the turn's context while parked releases the child's protection
// best-effort with the cancelled outcome and answers the cancelled result.
func TestAttention_CancelWhileParked(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-9","origin":"https://example.com"}`)}
	release := &scriptedChildTool{raw: "__resolve_attention", reply: staticReply(`{"status":"ok"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, release, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-x", "mcp__browser-use__submit_secret")
	ctx, cancel := context.WithCancel(t.Context())
	resultCh := invokeAsync(ctx, wrapper, `{"targets":["password"]}`)

	awaitInputRequired(t, f, sess.ID)
	cancel()
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"cancelled"}`, res.out)
	require.Equal(t, []string{`{"outcome":"cancelled","requestID":"req-9"}`}, release.recorded())
	require.Empty(t, f.mgr.pendingInputs, "the cancelled park dropped its registry entry")
}

// TestAttention_PersonCancelResolvesParkedSecret pins the secret card's
// Cancel button (ResolveAttention with the cancelled outcome): the parked
// submit_secret call unblocks on the cancelled result — never an empty
// delivery — the child's freeze is released, and the receipt records the
// cancelled outcome only.
func TestAttention_PersonCancelResolvesParkedSecret(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-10","origin":"https://example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	release := &scriptedChildTool{raw: "__resolve_attention", reply: staticReply(`{"status":"ok"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, release, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-cancel-2", "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"]}`)

	pending := awaitInputRequired(t, f, sess.ID)
	require.NotNil(t, pending.Input)

	// The person cancels: the cancelled resolution reaches the parked call.
	require.NoError(t, f.mgr.ResolveAttention(t.Context(), sess.ID, pending.Input.InterruptID, sessions.InputCancelled))
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"cancelled"}`, res.out)

	// No delivery happened, the child's freeze was released, the registry
	// entry is gone, and the receipt carries the cancelled outcome only.
	require.Empty(t, deliver.recorded())
	require.Equal(t, []string{`{"outcome":"cancelled","requestID":"req-10"}`}, release.recorded())
	require.Empty(t, f.mgr.pendingInputs)
	events := mustEvents(t, f, sess.ID)
	resolved := events[len(events)-1]
	require.Equal(t, sessions.KindInputResolved, resolved.Kind)
	require.NotNil(t, resolved.Input)
	require.Equal(t, sessions.InputCancelled, resolved.Input.Outcome)
}

// TestResolveAttention_SecretCompletedRefused pins the guard: a secret
// request never "completes" through the generic resolution — that would
// wake the parked call into delivering an empty value. The park survives
// the refusal and the typed submission still completes it.
func TestResolveAttention_SecretCompletedRefused(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-11","origin":"https://example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-guard", "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"]}`)

	pending := awaitInputRequired(t, f, sess.ID)
	require.Error(t, f.mgr.ResolveAttention(t.Context(), sess.ID, pending.Input.InterruptID, sessions.InputCompleted))

	// The park survives; the typed submission still completes it.
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "s3cret-value", "", false, ""))
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"completed","savedAsLogin":false}`, res.out)
}

// TestAttentionSecret_IdentifierFlow pins the username-or-email extension:
// a request with an identifierTarget flags the card, and the typed identifier
// rides the delivery beside the value — in the deliver arguments only, never
// in an event.
func TestAttentionSecret_IdentifierFlow(t *testing.T) {
	src := newSystemMCPSource()
	const (
		typedValue  = "s3cret-value"
		typedIdent  = "sam@corp.example"
		modelInvoke = `{"targets":["password"],"method":"password","username":"sam","identifierTarget":"Email"}`
	)
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-9","origin":"https://example.com","method":"password","username":"sam"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, fakeMCPTool{raw: "submit_secret"}}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, "turn-attn-2", "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, modelInvoke)

	pending := awaitInputRequired(t, f, sess.ID)
	require.True(t, pending.Input.Identifier, "the card declares the identifier box")
	require.Equal(t, "sam", pending.Input.Username)

	// The prepare call carried the model's arguments — identifierTarget
	// included — verbatim.
	require.Equal(t, []string{modelInvoke}, request.recorded())

	// Resolve with the typed identifier: the deliver call carries both, the
	// events still carry neither.
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, typedValue, typedIdent, false, ""))
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"completed","savedAsLogin":false}`, res.out)

	deliveries := deliver.recorded()
	require.Len(t, deliveries, 1)
	require.JSONEq(t, fmt.Sprintf(`{"requestID":"req-9","value":%q,"identifier":%q}`, typedValue, typedIdent), deliveries[0])
	assertNoValueInEvents(t, f, sess.ID, typedValue)
}
