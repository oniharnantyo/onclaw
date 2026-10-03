package eval

// The decision-backend A/B (add-configurable-decision-backend 6.1): the
// fixture's question turns are classified through two arms on the same
// fixture —
//
//  (a) the LLM intent gate over a scripted stub model (the harness's
//      stub-model pattern: fixed responses, no live model), and
//  (b) the decision gate over a stub /v1/systemone endpoint, wired exactly as
//      production does (memory settings record → workspace provider catalog →
//      decrypted key → base_url override → one systemone call).
//
// Both arms are deterministic and offline (httptest stubs only). Each arm's
// verdicts are folded into the scoreboard's own BuildScoreboard math and the
// decision arm must stay within the bounds the LLM/stub arm achieves —
// the pre-registered fixture gates (Fixture.ValidateFixture, the fixture
// shape gates in fixture_test.go, and the stub e2e's Overall > 0 gate) are
// honored by construction: this test validates the fixture first and asserts
// both arms score above zero. This is a GATE-level A/B, not a full-run
// simulation: the harness drives real agent turns (which need a live model),
// so the substituted quantity is the answering model — recall/abstention here
// measure whether the gate routes the question's retrieval at all, which is
// the decision backend's only job.

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
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// abWorkspaceID scopes the A/B's fake tenant; the fakes are test-local so no
// shared fake store is needed.
const abWorkspaceID = "ws-ab"

// abLLMModel is the scripted stub model of arm (a): it looks the turn text up
// in a fixed script (the same map the decision arm's probabilities are derived
// from) and returns the verdict JSON, recording every prompt it classified.
type abLLMModel struct {
	mu      sync.Mutex
	scripts map[string]string
	calls   []string
}

func (m *abLLMModel) Generate(_ context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	var turn strings.Builder
	for _, msg := range input {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block == nil || block.UserInputText == nil {
				continue
			}
			turn.WriteString(block.UserInputText.Text)
		}
	}
	text := turn.String()
	// The gate wraps the raw turn in intentUserPrompt's "## Turn\n" header;
	// strip it back off for the script lookup.
	if idx := strings.Index(text, "## Turn\n"); idx >= 0 {
		text = text[idx+len("## Turn\n"):]
	}
	m.mu.Lock()
	m.calls = append(m.calls, text)
	response, ok := m.scripts[text]
	m.mu.Unlock()
	if !ok {
		// Unknown turns classify self-contained — the safe default.
		response = `{"needs_memory":false,"buckets":[]}`
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: response}}},
	}, nil
}

func (m *abLLMModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("abLLMModel: stream not supported")
}

// abLLMResponse scripts the LLM arm's verdict for one fixture question: the
// withhold types stay self-contained, memory-needing types route notes and
// events, and the multihop (entity-shaped) questions additionally draw the
// associative traversal — the one capability the decision backend
// deliberately lacks (spec: associative unavailable in decision mode). The
// A/B asserts recall parity anyway: associative seeding narrows the
// prefetch, it does not decide whether retrieval runs.
func abLLMResponse(q Question) string {
	switch q.Type {
	case TypeAbstention, TypeScope:
		return `{"needs_memory":false,"buckets":[]}`
	case TypeMultihop:
		return `{"needs_memory":true,"buckets":["notes","events","associative"],"entity":"fixture-entity"}`
	default:
		return `{"needs_memory":true,"buckets":["notes","events"]}`
	}
}

// abPlan is the decision arm's scripted noul probabilities for one fixture
// question turn (keyed by the raw turn text, which arrives as the request's
// state).
type abPlan struct {
	needs, notes, events float64
}

// abDecisionPlan derives the decision arm's script from the same question
// data the LLM arm scripts from: withhold turns read below threshold on
// everything (self-contained), memory-needing turns read needs_memory above
// threshold, and every third memory-needing question (catalog order —
// deterministic) gets ALL buckets below threshold to exercise the
// route-both-notes-and-events fallback mapping.
func abDecisionPlan(index int, q Question) abPlan {
	switch q.Type {
	case TypeAbstention, TypeScope:
		return abPlan{needs: 0.05, notes: 0.05, events: 0.05}
	default:
		if index%3 == 0 {
			return abPlan{needs: 0.9, notes: 0.2, events: 0.3}
		}
		return abPlan{needs: 0.9, notes: 0.9, events: 0.9}
	}
}

// abSystemoneStub is the stub /v1/systemone endpoint of arm (b): it validates
// the pinned request contract, looks the state up in the scripted plans, and
// serves object-form noul answers. It counts requests so the one-attempt
// contract is visible at the harness level too.
type abSystemoneStub struct {
	server *httptest.Server
	hits   atomic.Int64

	mu    sync.Mutex
	plans map[string]abPlan
}

func newABSystemoneStub(t *testing.T, plans map[string]abPlan) *abSystemoneStub {
	t.Helper()
	stub := &abSystemoneStub{plans: plans}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("systemone stub: Authorization = %q, want a bearer key", auth)
		}
		var body struct {
			State     string `json:"state"`
			Model     string `json:"model"`
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("systemone stub: decode request: %v", err)
			writeJSON(w, 400, map[string]any{"error": "bad request"})
			return
		}
		if body.Model == "" {
			t.Error("systemone stub: request carries no model")
		}
		if len(body.Questions) != 3 {
			t.Errorf("systemone stub: request carries %d questions, want exactly the three noul questions", len(body.Questions))
		} else {
			for id, q := range body.Questions {
				if q.Type != "noul" {
					t.Errorf("systemone stub: question %q type = %q, want noul", id, q.Type)
				}
			}
		}
		plan, ok := stub.plans[body.State]
		if !ok {
			t.Errorf("systemone stub: unscripted state %q", body.State)
			plan = abPlan{needs: 0.05, notes: 0.05, events: 0.05}
		}
		writeJSON(w, 200, map[string]any{
			"answers": map[string]any{
				"needs_memory":  map[string]any{"noul": plan.needs},
				"bucket_notes":  map[string]any{"noul": plan.notes},
				"bucket_events": map[string]any{"noul": plan.events},
			},
			"usage": map[string]any{"total": 1},
		})
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// abProviderStore is the A/B's one-provider catalog: only ByID is on the
// decision path.
type abProviderStore struct {
	provider domain.ProviderConfig
}

func (s abProviderStore) ByID(_ context.Context, workspaceID, id string) (*domain.ProviderConfig, error) {
	if id != s.provider.ID || workspaceID != s.provider.WorkspaceID {
		return nil, domain.ErrNotFound
	}
	p := s.provider
	return &p, nil
}

func (abProviderStore) Create(context.Context, *domain.ProviderConfig) error {
	return errors.New("abProviderStore: create not expected")
}

func (abProviderStore) ListForWorkspace(context.Context, string) ([]domain.ProviderConfig, error) {
	return nil, errors.New("abProviderStore: list not expected")
}

func (abProviderStore) Update(context.Context, *domain.ProviderConfig) error {
	return errors.New("abProviderStore: update not expected")
}

func (abProviderStore) Delete(context.Context, string, string) error {
	return errors.New("abProviderStore: delete not expected")
}

// abSettingsStore serves the A/B's memory settings record carrying the
// decision pair; only Get is on the decision path.
type abSettingsStore struct {
	row *domain.WorkspaceToolSetting
}

func (s abSettingsStore) Get(context.Context, string, string) (*domain.WorkspaceToolSetting, error) {
	if s.row == nil {
		return nil, domain.ErrNotFound
	}
	return s.row, nil
}

func (abSettingsStore) Upsert(context.Context, *domain.WorkspaceToolSetting) error {
	return errors.New("abSettingsStore: upsert not expected")
}

func (abSettingsStore) List(context.Context, string) ([]domain.WorkspaceToolSetting, error) {
	return nil, errors.New("abSettingsStore: list not expected")
}

// abScore converts one gate verdict into the scoreboard row the A/B asserts
// on. Gate-level arm semantics (the substituted quantity is the answering
// model, which a live run would supply):
//
//   - a memory-needing question (recall/update/temporal/multihop) "recalls"
//     when the gate routes retrieval (the prefetch would run) and "cites"
//     only when it routed (unrouted → a memory-flavored answer with no
//     evidence opened fails the citation lock);
//   - a withhold question (abstention/scope) "abstains correctly" when the
//     gate leaves it self-contained — nothing is retrieved, so nothing can
//     be fabricated from memory;
//   - classification cannot leak private content, so scope safety holds by
//     construction for both arms.
func abScore(q Question, v memory.IntentVerdict) QuestionScore {
	routed := v.NeedsDeepMemory
	row := QuestionScore{ID: q.ID, Type: q.Type, ScopeSafe: boolPtr(true), Notes: []string{}}
	switch q.Type {
	case TypeAbstention, TypeScope:
		row.AbstainedCorrect = boolPtr(!routed)
		row.CitationValid = boolPtr(true)
	default:
		row.Recall = boolPtr(routed)
		row.CitationValid = boolPtr(routed)
	}
	return row
}

// TestDecisionBackendABAgainstLLMStub runs the A/B over the full fixture
// question catalog and asserts the decision arm stays within the bounds the
// LLM/stub arm achieves.
func TestDecisionBackendABAgainstLLMStub(t *testing.T) {
	// The pre-registered fixture gates hold before anything is classified.
	if err := Fixture.ValidateFixture(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Arm (a): the LLM gate over the scripted stub model.
	scripts := make(map[string]string, len(Fixture.Questions))
	for _, q := range Fixture.Questions {
		scripts[q.Text] = abLLMResponse(q)
	}
	llm := &abLLMModel{scripts: scripts}
	llmGate := memory.NewIntentGate(nil, nil, nil, log,
		memory.WithModelResolver(func(context.Context, string, string) (memory.Model, error) {
			return llm, nil
		}))

	// Arm (b): the decision gate over the stub systemone, wired exactly as
	// the composition roots wire it (WithWorkspaceModelSource carrying the
	// decision pair; the provider catalog + encryption key on the gate).
	plans := make(map[string]abPlan, len(Fixture.Questions))
	for i, q := range Fixture.Questions {
		plans[q.Text] = abDecisionPlan(i, q)
	}
	stub := newABSystemoneStub(t, plans)

	encryptionKey := bytes.Repeat([]byte{0x2c}, secrets.KeySize)
	ciphertext, err := secrets.Encrypt(encryptionKey, []byte(abWorkspaceID), []byte("ts-key"))
	if err != nil {
		t.Fatalf("encrypt provider key: %v", err)
	}
	provider := domain.ProviderConfig{
		ID:            "prov-ab-typesafe",
		WorkspaceID:   abWorkspaceID,
		Type:          providers.TypeTypeSafe,
		Name:          "TypeSafe AB",
		BaseURL:       stub.server.URL, // the base_url IS the full endpoint for the type
		KeyCiphertext: ciphertext,
		Enabled:       true,
	}
	decisionGate := memory.NewIntentGate(abProviderStore{provider: provider}, encryptionKey, nil, log,
		memory.WithWorkspaceModelSource(abSettingsStore{row: &domain.WorkspaceToolSetting{
			WorkspaceID: abWorkspaceID,
			ToolKey:     "memory",
			Enabled:     true,
			Config: map[string]any{
				"decision_provider_id": provider.ID,
				"decision_model":       "jev-latest",
			},
		}}))

	// Classify every fixture question turn through both arms.
	llmRows := make([]QuestionScore, 0, len(Fixture.Questions))
	decisionRows := make([]QuestionScore, 0, len(Fixture.Questions))
	llmVerdicts := make([]memory.IntentVerdict, 0, len(Fixture.Questions))
	decisionVerdicts := make([]memory.IntentVerdict, 0, len(Fixture.Questions))
	for _, q := range Fixture.Questions {
		lv, err := llmGate.Classify(context.Background(), abWorkspaceID, "", q.Text, 4*time.Second)
		if err != nil {
			t.Fatalf("LLM arm %s: %v", q.ID, err)
		}
		dv, err := decisionGate.Classify(context.Background(), abWorkspaceID, "", q.Text, 4*time.Second)
		if err != nil {
			t.Fatalf("decision arm %s: %v", q.ID, err)
		}
		llmVerdicts = append(llmVerdicts, lv)
		decisionVerdicts = append(decisionVerdicts, dv)
		llmRows = append(llmRows, abScore(q, lv))
		decisionRows = append(decisionRows, abScore(q, dv))
	}

	_, llmSummary := BuildScoreboard(llmRows)
	_, decisionSummary := BuildScoreboard(decisionRows)

	// Sanity gates (mirroring the stub e2e's Overall > 0 gate): a silently
	// degenerate stub arm must fail loudly, not pass the bounds trivially.
	if llmSummary.Overall <= 0 || decisionSummary.Overall <= 0 {
		t.Fatalf("degenerate A/B: llm overall %.1f, decision overall %.1f — both stub arms must classify the fixture", llmSummary.Overall, decisionSummary.Overall)
	}

	// The A/B bound: the decision arm stays within the LLM/stub arm's
	// achieved recall and overall on the same fixture.
	if decisionSummary.Recall < llmSummary.Recall {
		t.Errorf("decision recall %.1f < LLM arm recall %.1f on the same fixture", decisionSummary.Recall, llmSummary.Recall)
	}
	if decisionSummary.Overall < llmSummary.Overall {
		t.Errorf("decision overall %.1f < LLM arm overall %.1f on the same fixture", decisionSummary.Overall, llmSummary.Overall)
	}

	// 3.3 at fixture scale: no decision verdict ever routes associative or
	// names an entity, and the withhold types (abstention/scope) stay
	// self-contained in BOTH arms — the decider must not spend retrieval on
	// turns whose correct answer withholds.
	for i, q := range Fixture.Questions {
		dv := decisionVerdicts[i]
		if dv.Associative || dv.Entity != "" {
			t.Errorf("%s: decision verdict carries the associative route or an entity: %+v", q.ID, dv)
		}
		switch q.Type {
		case TypeAbstention, TypeScope:
			if dv.NeedsDeepMemory {
				t.Errorf("%s: decision arm routed a withhold question", q.ID)
			}
			if lv := llmVerdicts[i]; lv.NeedsDeepMemory {
				t.Errorf("%s: LLM arm routed a withhold question", q.ID)
			}
		}
	}

	// The all-buckets-below turns (every third memory-needing question) must
	// still route both buckets in the decision arm — the narrowing fallback.
	for i, q := range Fixture.Questions {
		if q.Type == TypeAbstention || q.Type == TypeScope || i%3 != 0 {
			continue
		}
		dv := decisionVerdicts[i]
		if !dv.NeedsDeepMemory || !dv.Notes || !dv.Events {
			t.Errorf("%s: all-buckets-below turn must route notes+events, got %+v", q.ID, dv)
		}
	}

	// One decision request per classification, and every question classified
	// exactly once through the LLM stub.
	if n := stub.hits.Load(); n != int64(len(Fixture.Questions)) {
		t.Errorf("decision stub served %d requests for %d questions, want exactly one attempt each", n, len(Fixture.Questions))
	}
	llm.mu.Lock()
	calls := len(llm.calls)
	llm.mu.Unlock()
	if calls != len(Fixture.Questions) {
		t.Errorf("LLM stub classified %d turns for %d questions", calls, len(Fixture.Questions))
	}
}
