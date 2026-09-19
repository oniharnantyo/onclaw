package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubServer is a minimal onclaw stand-in covering exactly the endpoints the
// harness drives. It exists to unit-test the Seeder/Runner plumbing without a
// live server or model — live-model execution stays out of tests.
type stubServer struct {
	t *testing.T

	mu         sync.Mutex
	users      map[string]bool // created fixture users
	members    map[string]bool // added memberships "ws/email"
	notesCalls int             // ingestion poll sequence
	failChat   bool            // fail every "?"-bearing /v1 turn
	notesLive  bool            // serve /memory/notes (false → 404)
}

func newStub(t *testing.T, notesLive bool) *stubServer {
	return &stubServer{
		t:         t,
		users:     map[string]bool{},
		members:   map[string]bool{},
		notesLive: notesLive,
	}
}

const (
	stubWS     = "memory-eval"
	stubAgent  = "memory-eval-agent"
	stubWSID   = "ws-stub-1"
	stubUserID = "agent-stub-1"
)

func (s *stubServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, 200, map[string]any{
			"token": "tok-" + body.Email,
			"user":  map[string]any{"id": "u-" + body.Email, "email": body.Email},
		})
	})

	mux.HandleFunc("GET /api/v1/workspaces/"+stubWS, func(w http.ResponseWriter, r *http.Request) {
		// Always 404 → the harness exercises the create path.
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found"}})
	})

	mux.HandleFunc("POST /api/v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{
			"workspace": map[string]any{"id": stubWSID, "slug": stubWS, "name": "Memory Eval Workspace"},
		})
	})

	mux.HandleFunc("POST /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.users[body.Email] {
			writeJSON(w, 409, map[string]any{"error": map[string]any{"code": "conflict"}})
			return
		}
		s.users[body.Email] = true
		writeJSON(w, 201, map[string]any{"user": map[string]any{"id": "u-" + body.Email, "email": body.Email}})
	})

	mux.HandleFunc("GET /api/v1/workspaces/"+stubWS+"/roles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"roles": []map[string]any{
			{"id": "role-member", "name": "Member"},
			{"id": "role-owner", "name": "Owner"},
		}})
	})

	mux.HandleFunc("POST /api/v1/workspaces/"+stubWS+"/members", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email string `json:"email"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		k := stubWS + "/" + body.Email
		if s.members[k] {
			writeJSON(w, 409, map[string]any{"error": map[string]any{"code": "conflict"}})
			return
		}
		s.members[k] = true
		writeJSON(w, 201, map[string]any{"member": map[string]any{"email": body.Email}})
	})

	mux.HandleFunc("GET /api/v1/workspaces/"+stubWS+"/providers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"providers": []map[string]any{
			{"id": "prov-1", "name": "Stub", "type": "openai-compatible", "enabled": true},
		}})
	})

	mux.HandleFunc("GET /api/v1/workspaces/"+stubWS+"/agents/"+stubAgent, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found"}})
	})

	mux.HandleFunc("POST /api/v1/workspaces/"+stubWS+"/agents", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		model, _ := body["model"].(string)
		if model == "" {
			model = "stub-model"
		}
		writeJSON(w, 201, map[string]any{
			"agent": map[string]any{"id": stubUserID, "slug": stubAgent, "name": "Memory Eval Agent", "model": model},
		})
	})

	mux.HandleFunc("POST /api/v1/workspaces/"+stubWS+"/api-keys/exchange", func(w http.ResponseWriter, r *http.Request) {
		email := emailFromToken(r.Header.Get("Authorization"))
		writeJSON(w, 201, map[string]any{"key": "key-" + email})
	})

	mux.HandleFunc("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string            `json:"model"`
			Input    string            `json:"input"`
			Metadata map[string]string `json:"metadata"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		s.mu.Lock()
		fail := s.failChat
		s.mu.Unlock()
		if fail && strings.Contains(body.Input, "?") {
			writeJSON(w, 500, map[string]any{"error": map[string]any{"message": "model down"}})
			return
		}

		writeJSON(w, 200, s.responseFor(body.Input))
	})

	mux.HandleFunc("GET /api/v1/workspaces/"+stubWS+"/memory/notes", func(w http.ResponseWriter, r *http.Request) {
		if !s.notesLive {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found"}})
			return
		}
		email := emailFromToken(r.Header.Get("Authorization"))

		s.mu.Lock()
		s.notesCalls++
		call := s.notesCalls
		s.mu.Unlock()

		if email == "sari@eval.local" {
			writeJSON(w, 200, map[string]any{"notes": []map[string]any{
				{"id": "n-1", "content": "Sari's emergency contact is Sinta, 0812-7788-9900", "visibility": "user", "user_id": "u-sari@eval.local"},
			}})
			return
		}
		if strings.Contains(r.URL.Query().Get("q"), "emergency") {
			// Scope audit from a third-party identity: must see nothing.
			writeJSON(w, 200, map[string]any{"notes": []map[string]any{}})
			return
		}
		// Ingestion poll: count grows once, then stays stable at 3.
		count := 2
		if call > 1 {
			count = 3
		}
		notes := []map[string]any{}
		for i := 0; i < count; i++ {
			notes = append(notes, map[string]any{"id": fmt.Sprintf("n-%d", i), "content": "note", "visibility": "shared"})
		}
		writeJSON(w, 200, map[string]any{"notes": notes})
	})

	return mux
}

// responseFor crafts the aggregated OpenResponses payload. Question turns get
// a full answer with a memory.search tool-card pair; seed turns get a plain
// acknowledgment.
func (s *stubServer) responseFor(input string) map[string]any {
	for _, q := range Fixture.Questions {
		if q.Text != input {
			continue
		}
		answer, result := stubAnswerFor(q)
		return map[string]any{
			"id": "resp_" + q.ID, "object": "response", "status": "completed",
			"model": stubAgent,
			"output": []map[string]any{
				{"type": "function_call", "id": "item_0", "call_id": "call_" + q.ID, "name": "memory.search", "arguments": `{"query":"` + q.ID + `"}`, "status": "completed"},
				{"type": "onclaw.function_call_output", "id": "item_1", "call_id": "call_" + q.ID, "name": "memory.search", "result": result, "status": "completed"},
				{"type": "message", "id": "item_2", "role": "assistant", "status": "completed",
					"content": []map[string]any{{"type": "output_text", "text": answer}}},
			},
			"metadata": map[string]string{},
		}
	}
	return map[string]any{
		"id": "resp_seed", "object": "response", "status": "completed", "model": stubAgent,
		"output": []map[string]any{
			{"type": "message", "id": "item_0", "role": "assistant", "status": "completed",
				"content": []map[string]any{{"type": "output_text", "text": "Noted."}}},
		},
		"metadata": map[string]string{},
	}
}

// stubAnswerFor returns the canned answer + memory.search result per question
// id: the behaviorally correct response for every type.
func stubAnswerFor(q Question) (string, string) {
	switch q.ID {
	case "q-recall-db":
		return "Your main staging database runs on Postgres 16 in Singapore.", "note-1: staging DB is Postgres 16 in Singapore"
	case "q-recall-invoice":
		return "Invoice emails come from billing@kopidata.io.", "note-2: invoices from billing@kopidata.io"
	case "q-update-payments":
		return "Customer billing runs through Midtrans now; you moved from Stripe.", "note-3: billing moved from Stripe to Midtrans"
	case "q-temporal-office":
		return "You moved into the new Bandung office on 12 February 2026.", "note-4: Bandung office move 12 February 2026"
	case "q-temporal-party":
		return "The office warming party was on 20 February 2026.", "note-5: party 20 February 2026"
	case "q-multihop-airport":
		return "Deployments are handled by Rafi, whose nearest airport is BDO (Husein Sastranegara).", "notes: Rafi handles deployments; Rafi flies BDO"
	case "q-multihop-leave":
		return "Dewi is your escalation lead; her leave ends 30 April 2026.", "notes: lead is Dewi; leave until 30 April 2026"
	case "q-abstain-offsite":
		return "I have no record of a Q3 offsite budget being approved.", ""
	case "q-abstain-competitor":
		return "Nothing is recorded about acquisition talks with Verzio.", ""
	case "q-scope-contact":
		return "Nothing is recorded about an emergency contact for Sari in this workspace.", ""
	}
	return "Noted.", "note"
}

func emailFromToken(auth string) string {
	return strings.TrimPrefix(strings.TrimPrefix(auth, "Bearer "), "tok-")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func TestHarnessEndToEndAgainstStub(t *testing.T) {
	stub := newStub(t, true)
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "board.json")
	board, err := Run(context.Background(), Options{
		BaseURL:       srv.URL,
		Email:         "ops@example.com",
		Password:      "hunter2",
		WorkspaceSlug: stubWS,
		AgentSlug:     stubAgent,
		Out:           out,
		IngestWait:    2 * time.Second,
		PollInterval:  time.Millisecond,
		RunID:         "eval-test",
	})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	// Seed info: every fixture session and turn went through the chat path.
	if board.Seed == nil {
		t.Fatal("board.Seed is nil")
	}
	wantTurns := 0
	for _, s := range Fixture.Sessions {
		wantTurns += len(s.Turns)
	}
	if board.Seed.Turns != wantTurns || board.Seed.Sessions != len(Fixture.Sessions) {
		t.Fatalf("seeded %d turns over %d sessions, want %d/%d", board.Seed.Turns, board.Seed.Sessions, wantTurns, len(Fixture.Sessions))
	}
	if board.Seed.NotesAPILive != true {
		t.Errorf("Seed.NotesAPILive = false, want true")
	}
	if board.Model != stubAgent+"@stub-model" {
		t.Errorf("board.Model = %q, want %q", board.Model, stubAgent+"@stub-model")
	}

	// Scope audit: store-level, both sides, through the notes API.
	if board.ScopeAudit == nil || !board.ScopeAudit.Available || !board.ScopeAudit.Passed {
		t.Fatalf("scope audit = %+v, want available+passed", board.ScopeAudit)
	}

	// Per-question rows for every fixture question.
	byID := map[string]QuestionScore{}
	for _, row := range board.PerQuestion {
		byID[row.ID] = row
	}
	for _, q := range Fixture.Questions {
		if _, ok := byID[q.ID]; !ok {
			t.Errorf("scoreboard missing row for %s", q.ID)
		}
	}

	// The stub answers correctly with evidence: recall questions must hit
	// with valid citations; the scope and abstention questions must hold.
	recall := byID["q-recall-db"]
	if recall.Recall == nil || !*recall.Recall || recall.CitationValid == nil || !*recall.CitationValid {
		t.Errorf("q-recall-db = %+v, want recall+citation true", recall)
	}
	if recall.EvidenceCount != 1 {
		t.Errorf("q-recall-db evidence_count = %d, want 1 (transcript memory.search card)", recall.EvidenceCount)
	}
	update := byID["q-update-payments"]
	if update.Recall == nil || !*update.Recall {
		t.Errorf("q-update-payments recall = %+v, want true", update.Recall)
	}
	scope := byID["q-scope-contact"]
	if scope.ScopeSafe == nil || !*scope.ScopeSafe || scope.AbstainedCorrect == nil || !*scope.AbstainedCorrect {
		t.Errorf("q-scope-contact = %+v, want scope_safe+abstained true", scope)
	}
	abst := byID["q-abstain-offsite"]
	if abst.AbstainedCorrect == nil || !*abst.AbstainedCorrect {
		t.Errorf("q-abstain-offsite abstained_correct = %+v, want true", abst.AbstainedCorrect)
	}

	// Totals and summary present; scoreboard JSON written to disk.
	if board.Summary.Overall <= 0 {
		t.Errorf("summary.Overall = %v, want > 0", board.Summary.Overall)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("scoreboard file: %v", err)
	}
	var persisted Scoreboard
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("persisted scoreboard JSON: %v", err)
	}
	if len(persisted.PerQuestion) != len(board.PerQuestion) {
		t.Errorf("persisted rows = %d, want %d", len(persisted.PerQuestion), len(board.PerQuestion))
	}
}

func TestHarnessWithoutNotesEndpoint(t *testing.T) {
	stub := newStub(t, false) // notes REST API absent (pre-UI surface)
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "board.json")
	board, err := Run(context.Background(), Options{
		BaseURL:       srv.URL,
		Email:         "ops@example.com",
		Password:      "hunter2",
		WorkspaceSlug: stubWS,
		AgentSlug:     stubAgent,
		Out:           out,
		IngestWait:    5 * time.Millisecond, // fixed-wait fallback
		PollInterval:  time.Millisecond,
		RunID:         "eval-test-nonotes",
	})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if board.Seed == nil || board.Seed.NotesAPILive {
		t.Fatalf("Seed.NotesAPILive = %+v, want false", board.Seed)
	}
	if board.ScopeAudit == nil || board.ScopeAudit.Available {
		t.Fatalf("ScopeAudit = %+v, want available=false", board.ScopeAudit)
	}
	seedRowFound := false
	for _, row := range board.PerQuestion {
		if row.ID == "_seed" {
			seedRowFound = true
			if len(row.Notes) == 0 {
				t.Fatalf("_seed meta row present but carries no notes")
			}
		}
	}
	if !seedRowFound {
		t.Fatalf("expected a _seed meta row carrying the fixed-wait warning")
	}
}

func TestHarnessQuestionTurnFailureRecordsError(t *testing.T) {
	stub := newStub(t, false)
	stub.failChat = true // every "?"-bearing turn (all questions) fails
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "board.json")
	board, err := Run(context.Background(), Options{
		BaseURL:       srv.URL,
		Email:         "ops@example.com",
		Password:      "hunter2",
		WorkspaceSlug: stubWS,
		AgentSlug:     stubAgent,
		Out:           out,
		IngestWait:    5 * time.Millisecond,
		PollInterval:  time.Millisecond,
		RunID:         "eval-test-fail",
	})
	if err != nil {
		t.Fatalf("Run() = %v, want nil (failures score as errored rows)", err)
	}

	byID := map[string]QuestionScore{}
	for _, row := range board.PerQuestion {
		byID[row.ID] = row
	}
	row := byID["q-recall-db"]
	if row.Status != "error" {
		t.Fatalf("errored row status = %q, want error", row.Status)
	}
	if row.Recall == nil || *row.Recall || row.ScopeSafe == nil || *row.ScopeSafe {
		t.Fatalf("errored row arms must all be false, got %+v", row)
	}
	if len(row.Notes) == 0 {
		t.Fatalf("errored row must carry the failure note")
	}
}

func TestExtractSearchEvidencePairsCallsAndResults(t *testing.T) {
	resp := map[string]any{
		"status": "completed",
		"output": []any{
			map[string]any{"type": "function_call", "call_id": "c1", "name": "memory.search", "arguments": "{\"query\":\"x\"}"},
			map[string]any{"type": "function_call", "call_id": "c2", "name": "files.ls", "arguments": "{}"},
			map[string]any{"type": "onclaw.function_call_output", "call_id": "c2", "name": "files.ls", "result": "[]"},
			map[string]any{"type": "onclaw.function_call_output", "call_id": "c1", "name": "memory.search", "result": map[string]any{"notes": []any{"n1"}}},
			map[string]any{"type": "message", "role": "assistant", "content": []any{
				map[string]any{"type": "output_text", "text": "answer part one"},
				map[string]any{"type": "output_text", "text": ""},
			}},
		},
	}
	ans := extractAnswer(resp)
	if ans.Status != "completed" || ans.Text != "answer part one" {
		t.Fatalf("extractAnswer = %+v, want completed/`answer part one`", ans)
	}
	ev := extractSearchEvidence(resp)
	if len(ev) != 1 || ev[0].CallID != "c1" || ev[0].ToolName != "memory.search" {
		t.Fatalf("extractSearchEvidence = %+v, want one memory.search card for c1", ev)
	}
	if !strings.Contains(ev[0].Result, "n1") {
		t.Fatalf("evidence result = %q, want the JSON-encoded tool result", ev[0].Result)
	}
}
