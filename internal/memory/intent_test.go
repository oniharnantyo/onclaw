package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// intentModel is a scripted intent side-call model: fixed response, records
// the received prompt texts, and honors the context deadline (a real
// provider's HTTP call does too) so the per-call budget is testable.
type intentModel struct {
	response string
	err      error
	delay    time.Duration

	prompts []string
}

func (m *intentModel) Generate(ctx context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	var sb strings.Builder
	for _, msg := range input {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			if block.AssistantGenText != nil {
				sb.WriteString(block.AssistantGenText.Text)
				sb.WriteByte('\n')
			}
			if block.UserInputText != nil {
				sb.WriteString(block.UserInputText.Text)
				sb.WriteByte('\n')
			}
		}
	}
	m.prompts = append(m.prompts, sb.String())
	if m.err != nil {
		return nil, m.err
	}
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: m.response}}},
	}, nil
}

func (m *intentModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("intentModel: stream not supported")
}

func newTestIntentGate(m Model) *IntentGate {
	return NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), WithModelResolver(staticResolver(m)))
}

func TestIntentGate_HitRoutesBuckets(t *testing.T) {
	m := &intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide about the vendor renewal?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected notes-routed hit, got %+v", verdict)
	}
	if len(m.prompts) != 1 || !strings.Contains(m.prompts[0], "vendor renewal") {
		t.Fatalf("the turn text must reach the side-call prompt: %q", m.prompts)
	}
}

func TestIntentGate_BothBucketsAndFences(t *testing.T) {
	m := &intentModel{response: "Sure!\n```json\n{\"needs_memory\": true, \"buckets\": [\"notes\", \"events\"]}\n```"}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "Recap last week's incident and what we changed since.", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || !verdict.Events {
		t.Fatalf("expected both buckets, got %+v", verdict)
	}
}

func TestIntentGate_SelfContainedQuiet(t *testing.T) {
	m := &intentModel{response: `{"needs_memory":false,"buckets":[]}`}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "Write me a haiku about deploy pipelines.", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("expected the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_EmptyTextSkipsModel: nothing to classify is quietly
// self-contained and never spends the side-call.
func TestIntentGate_EmptyTextSkipsModel(t *testing.T) {
	m := &intentModel{response: `{}`}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "   ", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if verdict.NeedsDeepMemory || len(m.prompts) != 0 {
		t.Fatalf("empty turn must skip the model, got verdict %+v and %d calls", verdict, len(m.prompts))
	}
}

// TestIntentGate_ModelErrorFailsOpen (spec: gate failure fails open): a dead
// side-call model classifies as self-contained with the error surfaced.
func TestIntentGate_ModelErrorFailsOpen(t *testing.T) {
	m := &intentModel{err: errors.New("provider down")}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide about the vendor renewal?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the model error to surface")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_UndecodableFailsOpen: prose without a verdict JSON object
// fails open.
func TestIntentGate_UndecodableFailsOpen(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: "I think it needs memory, probably."})

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the undecodable-output error")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_HardTimeout pins the hard-deadline contract: the caller's
// budget is enforced with a context deadline, so a model that honors its
// context is abandoned at it and the gate fails open. If Classify skipped the
// deadline the slow model would answer and the test would see a verdict
// instead of the deadline error.
func TestIntentGate_HardTimeout(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`, delay: 3 * time.Second})

	start := time.Now()
	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 100*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the context deadline error, got %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if elapsed > time.Second {
		t.Fatalf("the gate must abort at the budget, took %v", elapsed)
	}
}

// TestIntentGate_MissingBucketsRoutesBoth: a needs_memory verdict whose
// bucket list is unreadable routes both buckets — a bucket typo must never
// starve the retrieval the model asked for.
func TestIntentGate_MissingBucketsRoutesBoth(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: `{"needs_memory":true,"buckets":["everything"]}`})

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || !verdict.Events {
		t.Fatalf("expected the both-buckets fallback, got %+v", verdict)
	}
}

// TestIntentGate_AssociativeBucketRoutesTraversal (wave3 task 5.3, D8): the
// associative bucket routes the entity-shaped query to the traversal-bearing
// prefetch, carrying the model's entity spelling as the advisory seed; the
// "entity" bucket spelling routes identically (fail-open generosity), and a
// null entity leaves the seed empty for the searcher's own resolution.
func TestIntentGate_AssociativeBucketRoutesTraversal(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: `{"needs_memory":true,"buckets":["associative"],"entity":"ProjectX"}`})

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What is the status of ProjectX?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Associative {
		t.Fatalf("expected the associative route, got %+v", verdict)
	}
	if verdict.Entity != "ProjectX" {
		t.Fatalf("the verdict must carry the entity seed, got %+v", verdict)
	}

	gate = newTestIntentGate(&intentModel{response: `{"needs_memory":true,"buckets":["entity"]}`})
	verdict, err = gate.Classify(context.Background(), "ws-1", "", "What about Beta?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.Associative || verdict.Entity != "" {
		t.Fatalf("the entity bucket spelling must route associative with an empty seed, got %+v", verdict)
	}
}

// TestIntentGate_ResolverFailureFailsOpen covers the unwired side-call tier:
// a resolution failure is a fail-open classification, not a panic.
func TestIntentGate_ResolverFailureFailsOpen(t *testing.T) {
	gate := NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), WithModelResolver(failingResolver("no cheap tier")))

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the resolver error")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_HonorsConfiguredBudget (fix-memory-retrieval-lane D1/D2,
// tasks 1.4): the classification budget is the caller's per-turn value, not a
// package pin. The same ~2s side-call is abandoned under a 100ms budget
// (fail-open with the deadline error) and answers under the 4s default-sized
// budget.
func TestIntentGate_HonorsConfiguredBudget(t *testing.T) {
	m := &intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`, delay: 2 * time.Second}
	gate := newTestIntentGate(m)

	start := time.Now()
	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 100*time.Millisecond)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the deadline error under the 100ms budget, got %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if elapsed >= time.Second {
		t.Fatalf("the 100ms budget must abort early, took %v", elapsed)
	}

	verdict, err = gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify under the 4s budget: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected the notes-routed verdict under the 4s budget, got %+v", verdict)
	}
}

// ---------------------------------------------------------------------------
// Decision-branch tests (add-configurable-decision-backend 3.2): a stored
// decision configuration routes Classify through the systemone decision
// client; absent configuration, no settings source, or a settings read error
// keeps the LLM path byte-identical; any decision-path failure fails open
// self-contained and never falls back to the LLM path.
// ---------------------------------------------------------------------------

// decisionTestKey is the instance encryption key the decision-path tests
// encrypt the provider credential with (AES-256, KeySize bytes).
var decisionTestKey = bytes.Repeat([]byte{0x2f}, secrets.KeySize)

// captureLogHandler records every log record so the fail-open contract's warn
// line is assertable.
type captureLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureLogHandler) WithGroup(string) slog.Handler      { return h }

// assertOneFailOpenWarn asserts exactly one fail-open warn was logged and
// returns its stage attribute.
func (h *captureLogHandler) assertOneFailOpenWarn(t *testing.T) string {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) != 1 {
		t.Fatalf("expected exactly one fail-open warn record, got %d", len(h.records))
	}
	if h.records[0].Message != "memory: intent gate failed open; turn proceeds without retrieval" {
		t.Fatalf("warn message = %q, want the gate fail-open line", h.records[0].Message)
	}
	stage := ""
	h.records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "stage" {
			stage = a.Value.String()
			return false
		}
		return true
	})
	return stage
}

// recordingResolver counts LLM-path resolutions and always errors: the
// decision-branch tests use it to prove the LLM path is never touched when
// decision mode is stored and that decision failures never fall back to it.
func recordingResolver(calls *atomic.Int64) ModelResolver {
	return func(context.Context, string, string) (Model, error) {
		calls.Add(1)
		return nil, errors.New("the LLM path must not run")
	}
}

// decisionSystemoneStub serves scripted systemone answers, recording the auth
// header and decoded request body of the single call it expects.
type decisionSystemoneStub struct {
	server *httptest.Server
	hits   atomic.Int64

	mu      sync.Mutex
	auth    string
	gotBody decisionRequest
}

// newDecisionSystemoneStub starts a stub systemone endpoint serving the given
// body/status for every request.
func newDecisionSystemoneStub(t *testing.T, status int, body string) *decisionSystemoneStub {
	t.Helper()
	stub := &decisionSystemoneStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		stub.mu.Lock()
		stub.auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&stub.gotBody); err != nil {
			t.Errorf("decode systemone request: %v", err)
		}
		stub.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *decisionSystemoneStub) requestSnapshot() (auth string, body decisionRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth, s.gotBody
}

// seedDecisionWorkspace creates the fake workspace the decision tests scope
// to, plus the typesafe provider config the settings record references. The
// stub URL is the provider's base_url (the full systemone endpoint for the
// type); a non-empty apiKey is encrypted with the test instance key and the
// workspace AAD (empty is the keyless config the keyless test exercises). The
// returned id is the generated workspace id every scoping argument must use.
func seedDecisionWorkspace(t *testing.T, baseURL, apiKey string) (store.ProviderStore, *domain.ProviderConfig, string) {
	t.Helper()
	s := fake.New()
	ctx := context.Background()
	ws := &domain.Workspace{Slug: "decision-ws", Name: "Decision WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	ciphertext := ""
	if apiKey != "" {
		ct, err := secrets.Encrypt(decisionTestKey, []byte(ws.ID), []byte(apiKey))
		if err != nil {
			t.Fatalf("encrypt provider key: %v", err)
		}
		ciphertext = ct
	}
	provider := &domain.ProviderConfig{
		WorkspaceID:   ws.ID,
		Type:          providers.TypeTypeSafe,
		Name:          "TypeSafe",
		BaseURL:       baseURL,
		KeyCiphertext: ciphertext,
	}
	if err := s.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return s.Providers(), provider, ws.ID
}

// TestIntentGate_DecisionBackendClassifies (3.2): a stored decision pair
// routes the classification through the decision client — the stub systemone
// is hit once with the decrypted key, the raw turn, and the configured model,
// the LLM path never runs, and a clean classification logs no warn.
func TestIntentGate_DecisionBackendClassifies(t *testing.T) {
	stub := newDecisionSystemoneStub(t, 200, systemoneAnswers(0.9, 0.9, 0.1))

	providersStore, provider, wsID := seedDecisionWorkspace(t, stub.server.URL, "ts-key")

	settings := stubSettingsStore{row: &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			decisionProviderIDKey: provider.ID,
			decisionModelKey:      "jev-latest",
		},
	}}

	log := &captureLogHandler{}
	llmCalls := &atomic.Int64{}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(log),
		WithWorkspaceModelSource(settings), WithModelResolver(recordingResolver(llmCalls)))

	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide about the vendor renewal?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected the notes-routed decision verdict, got %+v", verdict)
	}
	if verdict.Associative || verdict.Entity != "" {
		t.Fatalf("decision verdict must never carry the associative route, got %+v", verdict)
	}
	if n := stub.hits.Load(); n != 1 {
		t.Fatalf("decision backend hit %d times, want exactly 1", n)
	}
	auth, body := stub.requestSnapshot()
	if auth != "Bearer ts-key" {
		t.Fatalf("Authorization = %q, want the decrypted provider key", auth)
	}
	if body.Model != "jev-latest" || body.State != "What did we decide about the vendor renewal?" {
		t.Fatalf("decision request state/model = %q/%q, want the turn text and configured model", body.State, body.Model)
	}
	if n := llmCalls.Load(); n != 0 {
		t.Fatalf("the LLM path ran %d times under decision mode, want 0", n)
	}
	// A clean decision classification logs exactly one info record — the
	// per-decision line naming the backend, model, verdict, and raw
	// probabilities — and nothing else (no warn).
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.records) != 1 {
		t.Fatalf("a clean decision classification must log exactly one record, got %d", len(log.records))
	}
	rec := log.records[0]
	if rec.Level != slog.LevelInfo || rec.Message != "memory: intent gate classified turn" {
		t.Fatalf("decision log = %s/%q, want info %q", rec.Level, rec.Message, "memory: intent gate classified turn")
	}
	got := map[string]string{}
	rec.Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	if got["source"] != "decision" || got["model"] != "jev-latest" {
		t.Fatalf("decision log source/model = %q/%q, want decision/jev-latest", got["source"], got["model"])
	}
	if got["needs_memory"] != "true" || got["notes"] != "true" || got["events"] != "false" {
		t.Fatalf("decision log verdict = %v/%v/%v, want the notes-routed verdict", got["needs_memory"], got["notes"], got["events"])
	}
	for _, key := range []string{"probability_needs_memory", "probability_bucket_notes", "probability_bucket_events"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("decision log missing %s", key)
		}
	}
}

// TestIntentGate_ClearedDecisionConfigKeepsLLMPath: a settings record without
// the decision pair keeps the LLM path and never touches the decision
// endpoint (absence-is-defaults).
func TestIntentGate_ClearedDecisionConfigKeepsLLMPath(t *testing.T) {
	stub := newDecisionSystemoneStub(t, 200, systemoneAnswers(0.9, 0.9, 0.9))

	providersStore, provider, wsID := seedDecisionWorkspace(t, stub.server.URL, "")

	// The record carries only the side-call pair — decision mode is cleared.
	settings := stubSettingsStore{row: &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			"sidecall_provider_id": provider.ID,
			"sidecall_model":       "m-sidecall",
		},
	}}

	m := &intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithWorkspaceModelSource(settings), WithModelResolver(staticResolver(m)))

	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected the LLM path's notes-routed verdict, got %+v", verdict)
	}
	if len(m.prompts) != 1 {
		t.Fatalf("the LLM side-call ran %d times, want 1", len(m.prompts))
	}
	if n := stub.hits.Load(); n != 0 {
		t.Fatalf("the decision endpoint was hit %d times with no stored decision config, want 0", n)
	}
}

// TestIntentGate_SettingsReadErrorKeepsLLMPath: a failed settings read behaves
// as absent — the LLM path classifies and the decision endpoint is untouched.
func TestIntentGate_SettingsReadErrorKeepsLLMPath(t *testing.T) {
	stub := newDecisionSystemoneStub(t, 200, systemoneAnswers(0.9, 0.9, 0.9))

	providersStore, _, wsID := seedDecisionWorkspace(t, stub.server.URL, "")
	settings := stubSettingsStore{err: errors.New("db down")}

	m := &intentModel{response: `{"needs_memory":true,"buckets":["events"]}`}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithWorkspaceModelSource(settings), WithModelResolver(staticResolver(m)))

	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Events || verdict.Notes {
		t.Fatalf("expected the LLM path's events-routed verdict, got %+v", verdict)
	}
	if len(m.prompts) != 1 {
		t.Fatalf("the LLM side-call ran %d times, want 1", len(m.prompts))
	}
	if n := stub.hits.Load(); n != 0 {
		t.Fatalf("the decision endpoint was hit %d times on a settings read error, want 0", n)
	}
}

// TestIntentGate_DecisionProviderResolutionFailsOpen: decision mode with an
// unresolvable provider fails open self-contained with the warn — never a
// fallback to the LLM path and never a decision request.
func TestIntentGate_DecisionProviderResolutionFailsOpen(t *testing.T) {
	stub := newDecisionSystemoneStub(t, 200, systemoneAnswers(0.9, 0.9, 0.9))

	providersStore, _, wsID := seedDecisionWorkspace(t, stub.server.URL, "")
	settings := stubSettingsStore{row: &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			decisionProviderIDKey: "missing-provider",
			decisionModelKey:      "jev-latest",
		},
	}}
	log := &captureLogHandler{}
	llmCalls := &atomic.Int64{}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(log),
		WithWorkspaceModelSource(settings), WithModelResolver(recordingResolver(llmCalls)))

	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the provider resolution error to surface")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if stage := log.assertOneFailOpenWarn(t); stage != "decision call" {
		t.Fatalf("fail-open warn stage = %q, want %q", stage, "decision call")
	}
	if n := stub.hits.Load(); n != 0 {
		t.Fatalf("the decision endpoint was hit %d times on a provider resolution failure, want 0", n)
	}
	if n := llmCalls.Load(); n != 0 {
		t.Fatalf("a decision failure must never fall back to the LLM path, which ran %d times", n)
	}
}

// TestIntentGate_DecisionKeylessProviderFailsOpen: a stored decision pair
// referencing a provider without a stored key is a decision-path failure —
// self-contained, warn, no request, no LLM fallback.
func TestIntentGate_DecisionKeylessProviderFailsOpen(t *testing.T) {
	stub := newDecisionSystemoneStub(t, 200, systemoneAnswers(0.9, 0.9, 0.9))

	providersStore, provider, wsID := seedDecisionWorkspace(t, stub.server.URL, "")
	settings := stubSettingsStore{row: &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			decisionProviderIDKey: provider.ID,
			decisionModelKey:      "jev-latest",
		},
	}}

	log := &captureLogHandler{}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(log),
		WithWorkspaceModelSource(settings), WithModelResolver(recordingResolver(&atomic.Int64{})))

	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the keyless-config error to surface")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if stage := log.assertOneFailOpenWarn(t); stage != "decision call" {
		t.Fatalf("fail-open warn stage = %q, want %q", stage, "decision call")
	}
	if n := stub.hits.Load(); n != 0 {
		t.Fatalf("the decision endpoint was hit %d times for a keyless provider, want 0", n)
	}
}

// TestIntentGate_DecisionTimeoutFailsOpenUnderBudget: the decision call runs
// under the caller's budget — a slow endpoint is abandoned at it, fails open
// with the warn, and issues exactly one attempt.
func TestIntentGate_DecisionTimeoutFailsOpenUnderBudget(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(systemoneAnswers(0.9, 0.9, 0.9)))
	}))
	defer srv.Close()

	providersStore, provider, wsID := seedDecisionWorkspace(t, srv.URL, "ts-key")
	settings := stubSettingsStore{row: &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			decisionProviderIDKey: provider.ID,
			decisionModelKey:      "jev-latest",
		},
	}}

	log := &captureLogHandler{}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(log),
		WithWorkspaceModelSource(settings))

	start := time.Now()
	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide?", 80*time.Millisecond)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the budget deadline error, got %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if elapsed >= time.Second {
		t.Fatalf("the decision call must abort at the budget, took %v", elapsed)
	}
	if stage := log.assertOneFailOpenWarn(t); stage != "decision call" {
		t.Fatalf("fail-open warn stage = %q, want %q", stage, "decision call")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("the timed-out decision call issued %d requests, want exactly 1", n)
	}
}

// TestIntentGate_DecisionMalformedBodyFailsOpen: a 200 with a body missing the
// answers map is equivalent to self-contained.
func TestIntentGate_DecisionMalformedBodyFailsOpen(t *testing.T) {
	stub := newDecisionSystemoneStub(t, 200, `{"usage": {"total": 3}}`)

	providersStore, provider, wsID := seedDecisionWorkspace(t, stub.server.URL, "ts-key")
	settings := stubSettingsStore{row: &domain.WorkspaceToolSetting{
		WorkspaceID: wsID,
		ToolKey:     "memory",
		Enabled:     true,
		Config: map[string]any{
			decisionProviderIDKey: provider.ID,
			decisionModelKey:      "jev-latest",
		},
	}}

	log := &captureLogHandler{}
	gate := NewIntentGate(providersStore, decisionTestKey, nil, slog.New(log),
		WithWorkspaceModelSource(settings))

	verdict, err := gate.Classify(context.Background(), wsID, "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the malformed-body error to surface")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if stage := log.assertOneFailOpenWarn(t); stage != "decision call" {
		t.Fatalf("fail-open warn stage = %q, want %q", stage, "decision call")
	}
	if n := stub.hits.Load(); n != 1 {
		t.Fatalf("the malformed decision call issued %d requests, want exactly 1", n)
	}
}
