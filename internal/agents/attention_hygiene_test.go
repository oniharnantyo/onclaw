package agents

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The parent-side secret-hygiene integration proof (add-browser-login-handover
// tasks.md 7.4, design D1/D3): the full interception flow — filtered surface →
// parked submit_secret → ResolveInput with a canary value → one
// __deliver_secret → receipt — asserting the typed value appears ONLY in the
// deliver call's arguments (the positive control) and nowhere else: no session
// event, no transcript byte, no tool result, no log line, and no registry
// entry after the delivery.

// hygieneCanary returns the run-unique canary value the test resolves with.
func hygieneCanary(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "BUDDIES-CANARY-" + hex.EncodeToString(b)
}

// hygieneBuffer is a concurrency-safe byte sink for captured log output.
type hygieneBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *hygieneBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *hygieneBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureHygieneSlog swaps the process default slog logger for one that
// records every record into the returned buffer (restored at cleanup).
func captureHygieneSlog(t *testing.T) *hygieneBuffer {
	t.Helper()
	buf := &hygieneBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// assertNoValueKey walks decoded JSON and refuses any object key named
// "value": the receipt payload must not even have a field a secret could
// occupy (design D3), whatever its content.
func assertNoValueKey(t *testing.T, v any, path string) {
	t.Helper()
	switch node := v.(type) {
	case map[string]any:
		for key, child := range node {
			require.NotEqual(t, "value", key, "key %s.%s must not exist", path, key)
			assertNoValueKey(t, child, path+"."+key)
		}
	case []any:
		for i, child := range node {
			assertNoValueKey(t, child, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

// assertCanaryOnlyInDelivery is the positive control plus the leak check: the
// canary appeared in exactly one child invocation — the __deliver_secret
// arguments — proving the harness observes the value where it legitimately
// flows (a proof without a positive control proves nothing).
func assertCanaryOnlyInDelivery(t *testing.T, deliver *scriptedChildTool, request *scriptedChildTool, requestID, canary string) {
	t.Helper()
	deliveries := deliver.recorded()
	require.Len(t, deliveries, 1, "exactly one __deliver_secret call happened")
	require.Contains(t, deliveries[0], canary,
		"positive control: the harness sees the value riding the one legitimate channel")
	require.JSONEq(t, fmt.Sprintf(`{"requestID":%q,"value":%q}`, requestID, canary), deliveries[0],
		"the delivery arguments are exactly the channel contract plus the value")
	// Every other recorded child invocation (the prepare) is value-free.
	for i, args := range request.recorded() {
		require.NotContains(t, args, canary,
			"child invocation %d besides the delivery must not carry the typed value", i)
	}
}

// TestParentSecretHygiene is the parent-side secret-hygiene proof (tasks.md
// 7.4): one full interception flow, the canary byte-grepped across every
// artifact it could reach.
func TestParentSecretHygiene(t *testing.T) {
	logs := captureHygieneSlog(t)
	canary := hygieneCanary(t)
	const requestID = "req-hygiene-1"
	const turnID = "turn-hygiene-1"
	const savedLoginID = "vault-login-7"

	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"` + requestID + `","origin":"https://example.com","method":"password","username":"sam@example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	// The whole reserved channel is advertised, as the real child does: the
	// surface filter must keep every `__` tool away from the model, while the
	// parent's own channel still reaches the scripted request/deliver tools.
	src.tools[mcp.SystemServerID] = []tool.BaseTool{
		fakeMCPTool{raw: "navigate"},
		fakeMCPTool{raw: "submit_secret"},
		fakeMCPTool{raw: "request_takeover"},
		request,
		deliver,
		fakeMCPTool{raw: "__begin_takeover"},
		fakeMCPTool{raw: "__resolve_attention"},
		fakeMCPTool{raw: "__request_takeover"},
	}

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	sess := f.newSession(t)

	// The filtered surface: submit_secret present, the reserved channel gone.
	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{
		sessionID: sess.ID,
		turnID:    turnID,
		record:    attentionRecord(t, f, sess.ID, turnID),
	})
	require.NoError(t, err)
	names := mcpToolNames(t, tools)
	require.Contains(t, names, "mcp__browser-use__submit_secret",
		"the wrapped submit_secret is offered to the model")
	for _, raw := range []string{"__request_secret", "__deliver_secret", "__begin_takeover", "__resolve_attention", "__request_takeover"} {
		require.NotContains(t, names, "mcp__browser-use__"+raw,
			"the reserved %s must never be offered to the model", raw)
	}
	var wrapper tool.InvokableTool
	for _, tl := range tools {
		info, ierr := tl.Info(context.Background())
		require.NoError(t, ierr)
		if info.Name == "mcp__browser-use__submit_secret" {
			inv, ok := tl.(tool.InvokableTool)
			require.True(t, ok, "submit_secret must stay invokable through the wrapper")
			wrapper = inv
			break
		}
	}
	require.NotNil(t, wrapper, "the wrapped submit_secret found")

	// Park: the intercepted call blocks on the attention card.
	resultCh := invokeAsync(t.Context(), wrapper,
		`{"targets":["password"],"method":"password","username":"sam@example.com"}`)
	pending := awaitInputRequired(t, f, sess.ID)
	require.NotNil(t, pending.Input)
	require.NotEmpty(t, pending.Input.InterruptID)
	require.Equal(t, sessions.AttentionSecret, pending.Input.AttentionKind)
	require.Equal(t, "https://example.com", pending.Input.Origin)
	require.Equal(t, "password", pending.Input.Method)
	require.Equal(t, turnID, pending.TurnID, "the card parks inside the turn")

	// A stale id while a request is genuinely pending refuses and appends
	// nothing.
	before := len(mustEvents(t, f, sess.ID))
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, "no-such-id", canary, "", false, ""),
		ErrInputNotPending, "a stale interrupt id is a conflict")
	require.Len(t, mustEvents(t, f, sess.ID), before, "the stale resolve appended nothing")

	// Resolve: the canary rides to the parked call, never into an event.
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, canary, "", true, savedLoginID))
	res := <-resultCh
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"completed","savedAsLogin":true}`, res.out,
		"the tool result is the closed contract only")
	require.NotContains(t, res.out, canary, "the tool result never carries the typed value")

	// Positive control: the value rode exactly one child invocation.
	assertCanaryOnlyInDelivery(t, deliver, request, requestID, canary)

	// Every appended session event is value-free, structurally and bytewise.
	events := mustEvents(t, f, sess.ID)
	require.GreaterOrEqual(t, len(events), 2, "the park and the receipt are on the log")
	for _, ev := range events {
		raw, merr := json.Marshal(ev)
		require.NoError(t, merr)
		require.NotContains(t, string(raw), canary,
			"event %s (%s) must not carry the typed value", ev.Kind, ev.ID)
		var decoded any
		require.NoError(t, json.Unmarshal(raw, &decoded))
		assertNoValueKey(t, decoded, string(ev.Kind))
	}

	// The receipt: outcome, redacted length, saved-login id — no value field.
	resolved := events[len(events)-1]
	require.Equal(t, sessions.KindInputResolved, resolved.Kind)
	require.NotNil(t, resolved.Input)
	require.Equal(t, sessions.InputCompleted, resolved.Input.Outcome)
	require.Equal(t, len(canary), resolved.Input.Length,
		"the receipt's length is the redacted truth about the typed value")
	require.Equal(t, savedLoginID, resolved.Input.SavedLoginID)
	require.Equal(t, turnID, resolved.TurnID)

	// The full transcript marshals value-free.
	transcript, terr := json.Marshal(events)
	require.NoError(t, terr)
	require.NotContains(t, string(transcript), canary, "the transcript never carries the typed value")

	// The captured slog output is value-free.
	require.NotContains(t, logs.String(), canary, "no log line may carry the typed value")

	// The registry dropped the entry at the delivery: a second resolve (same
	// id, new value) conflicts, no receipt lands, and no second delivery
	// happens.
	require.Empty(t, f.mgr.pendingInputs, "the resolved request left the registry")
	require.Nil(t, pendingInputFromEvents(events), "nothing pending derives from the resolved log")
	before = len(mustEvents(t, f, sess.ID))
	require.ErrorIs(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, canary+"-again", "", true, ""),
		ErrInputNotPending, "a completed request is no longer pending")
	require.Len(t, mustEvents(t, f, sess.ID), before, "the conflicting resolve appended no receipt")
	require.Len(t, deliver.recorded(), 1, "the conflicting resolve delivered nothing")
}
