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

	mu              sync.Mutex
	users           map[string]bool // created fixture users
	members         map[string]bool // added memberships "ws/email"
	notesCalls      int             // ingestion poll sequence
	ownerNotePolls  int             // unfiltered notes polls as the fixture owner (the ingestion wait)
	adminNotePolls  int             // unfiltered notes polls as the admin identity (must stay 0)
	ownerCorpusSize int             // unfiltered owner-view note count; <= 0 → floor+1 (floor exit)
	failChat        bool            // fail every "?"-bearing /v1 turn
	notesLive       bool            // serve /memory/notes (false → 404)

	// Fixture-agent provisioning capture (fix-memory-prefetch-matching D6,
	// denylist form): the create body's disabled_tools denylist, an optional
	// pre-existing agent's denylist (nil → GET agent 404s, the create path),
	// and every PATCHed denylist in order.
	createdAgentDisabledTools  []string
	existingAgentDisabledTools []string
	disabledToolsPatches       [][]string
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
		s.mu.Lock()
		existing := s.existingAgentDisabledTools
		s.mu.Unlock()
		if existing == nil {
			writeJSON(w, 404, map[string]any{"error": map[string]any{"code": "not_found"}})
			return
		}
		writeJSON(w, 200, map[string]any{
			"agent": map[string]any{"id": stubUserID, "slug": stubAgent, "name": "Memory Eval Agent", "model": "stub-model", "disabled_tools": existing},
		})
	})

	mux.HandleFunc("PATCH /api/v1/workspaces/"+stubWS+"/agents/"+stubUserID, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DisabledTools []string `json:"disabled_tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.disabledToolsPatches = append(s.disabledToolsPatches, body.DisabledTools)
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{
			"agent": map[string]any{"id": stubUserID, "slug": stubAgent, "name": "Memory Eval Agent", "model": "stub-model", "disabled_tools": body.DisabledTools},
		})
	})

	mux.HandleFunc("POST /api/v1/workspaces/"+stubWS+"/agents", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		model, _ := body["model"].(string)
		if model == "" {
			model = "stub-model"
		}
		var disabled []string
		if raw, ok := body["disabled_tools"].([]any); ok {
			for _, item := range raw {
				if s, ok := item.(string); ok {
					disabled = append(disabled, s)
				}
			}
		}
		s.mu.Lock()
		s.createdAgentDisabledTools = disabled
		s.mu.Unlock()
		writeJSON(w, 201, map[string]any{
			"agent": map[string]any{"id": stubUserID, "slug": stubAgent, "name": "Memory Eval Agent", "model": model, "disabled_tools": disabled},
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
		q := r.URL.Query().Get("q")

		s.mu.Lock()
		s.notesCalls++
		call := s.notesCalls
		ownerSize := s.ownerCorpusSize
		if ownerSize <= 0 {
			ownerSize = ingestFloorNotes() + 1 // owner view reaches the floor → wait exits on it
		}
		isOwner := email == fixtureOwnerEmail
		isAdmin := email == "ops@example.com"
		if isOwner && q == "" {
			s.ownerNotePolls++
		}
		if isAdmin && q == "" {
			s.adminNotePolls++
		}
		s.mu.Unlock()

		// The notes API filters to the caller's visible set (shared +
		// own-user rows): the fixture owner sees the corpus she ingested,
		// while the admin — no membership rows in the fixture workspace —
		// structurally sees none. That asymmetry is why the original
		// admin-token ingestion wait was blind (design D4).
		switch {
		case isOwner && q == "":
			// Unfiltered owner view: the seeded corpus the wait must
			// observe.
			notes := make([]map[string]any, 0, ownerSize)
			for i := 0; i < ownerSize; i++ {
				notes = append(notes, map[string]any{"id": fmt.Sprintf("n-%d", i), "content": "note", "visibility": "user", "user_id": "u-" + fixtureOwnerEmail})
			}
			writeJSON(w, 200, map[string]any{"notes": notes})
		case isOwner:
			// Owner-side scope audit (q-filtered): exactly the private row.
			writeJSON(w, 200, map[string]any{"notes": []map[string]any{
				{"id": "n-private", "content": "Sari's emergency contact is Sinta, 0812-7788-9900", "visibility": "user", "user_id": "u-" + fixtureOwnerEmail},
			}})
		case strings.Contains(q, "emergency"):
			// Scope audit from a third-party identity: must see nothing.
			writeJSON(w, 200, map[string]any{"notes": []map[string]any{}})
		case isAdmin:
			// Admin-visible set: empty — the blindness the old wait
			// suffered.
			writeJSON(w, 200, map[string]any{"notes": []map[string]any{}})
		default:
			// Other members (budi): own-user rows; grow once, then stable.
			count := 2
			if call > 1 {
				count = 3
			}
			notes := []map[string]any{}
			for i := 0; i < count; i++ {
				notes = append(notes, map[string]any{"id": fmt.Sprintf("n-%d", i), "content": "note", "visibility": "user", "user_id": "u-budi@eval.local"})
			}
			writeJSON(w, 200, map[string]any{"notes": notes})
		}
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
	case "q-recall-office-city":
		return "The new office is in Bandung.", "note: new office in Bandung"
	case "q-recall-vendor-region":
		return "Your Nusantara Cloud staging environment runs in the Jakarta region.", "note: staging in the Jakarta region"
	case "q-recall-vendor-contract":
		return "The Nusantara Cloud contract was signed on 17 February 2026.", "note: contract signed 17 February 2026"
	case "q-recall-person-start":
		return "He starts as a data analyst contractor on 23 February 2026.", "note: contractor start 23 February 2026"
	case "q-recall-kickoff":
		return "Project Nilam kicked off on 18 February 2026.", "note: kickoff 18 February 2026"
	case "q-recall-expansion-effective":
		return "The expanded seat plan took effect on 2 March 2026.", "note: expansion effective 2 March 2026"
	case "q-recall-tier":
		return "You are on the Priority support tier with Nusantara Cloud.", "note: tier upgraded to Priority"
	case "q-recall-sla":
		return "Priority support comes with a 1 hour response SLA.", "note: 1 hour response SLA"
	case "q-recall-floor":
		return "His desk is on floor 3 now.", "note: desk moved to floor 3"
	case "q-recall-war-room":
		return "The launch war room runs in #nilam-launch.", "note: war room channel #nilam-launch"
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
	case "q-multihop-seats":
		return "Yusuf requested the expansion; the plan now carries 40 seats.", "notes: expansion requested by Yusuf; plan at 40 seats"
	case "q-multihop-travel":
		return "Yusuf books his trips through the corporate travel desk.", "notes: Yusuf books via the corporate travel desk"
	case "q-multihop-nilam-beta":
		return "Project Nilam's internal beta is on 24 March 2026.", "note: Nilam beta 24 March 2026"
	case "q-multihop-dry-run":
		return "Yusuf runs the Project Nilam dry run on 20 April 2026.", "notes: dry run 20 April 2026, run by Yusuf"
	case "q-multihop-renewal":
		return "The Nusantara Cloud contract renews on 30 September 2026.", "note: renewal 30 September 2026"
	case "q-multihop-conversion":
		return "Yusuf converts to full-time effective 6 April 2026.", "note: conversion effective 6 April 2026"
	case "q-multihop-invoice":
		return "Yusuf requested the change; the monthly invoice is Rp 24 juta today.", "notes: change requested by Yusuf; invoice Rp 24 juta"
	case "q-multihop-launch":
		return "After the slip the Project Nilam launch targets 21 April 2026, with the dry run on 20 April 2026.", "notes: launch moved to 21 April 2026; dry run 20 April 2026"
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

	// Ingestion wait (design D4): polls as the fixture owner, observes the
	// seeded corpus on the first poll (owner view reaches the ingest floor →
	// early exit) and never polls as the blind admin identity.
	if stub.ownerNotePolls != 1 {
		t.Errorf("ingestion wait polled the notes API %d times as the fixture owner, want 1 (floor reached on the first poll)", stub.ownerNotePolls)
	}
	if stub.adminNotePolls != 0 {
		t.Errorf("ingestion wait polled the notes API as admin %d times, want 0 (the admin cannot see the corpus)", stub.adminNotePolls)
	}
	for _, row := range board.PerQuestion {
		if row.ID != "_seed" {
			continue
		}
		for _, n := range row.Notes {
			if strings.Contains(n, "ingestion wait elapsed") {
				t.Errorf("_seed row claims the ingestion wait elapsed: %q", n)
			}
		}
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
			foundAbsentNote := false
			for _, n := range row.Notes {
				if strings.Contains(n, "memory notes REST endpoint absent") {
					foundAbsentNote = true
				}
			}
			if !foundAbsentNote {
				t.Fatalf("_seed meta row notes %v, want one naming the absent notes endpoint", row.Notes)
			}
		}
	}
	if !seedRowFound {
		t.Fatalf("expected a _seed meta row carrying the endpoint-absent fixed-wait note")
	}
}

// TestIngestionWaitObservesFixtureOwnerNotAdmin pins the identity asymmetry
// that blinded the original ingestion wait (fix-memory-retrieval-lane D4):
// the notes API filters to the caller's visible set, so on the same fake the
// admin identity sees zero notes while the fixture owner sees the seeded
// corpus. The wait must therefore poll as the owner — observing the notes
// (stable non-zero count → early return, no elapsed warning) — and never
// burn its budget polling as the blind identity.
func TestIngestionWaitObservesFixtureOwnerNotAdmin(t *testing.T) {
	stub := newStub(t, true)
	// Owner view stuck at 1 note, below the ingest floor: the wait must still
	// observe it and return via the stable-count rule rather than the
	// deadline.
	stub.ownerCorpusSize = 1
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	ctx := context.Background()
	client := NewAPIClient(srv.URL, time.Minute)
	sari := fixtureActorByEmail(fixtureOwnerEmail)
	if sari == nil {
		t.Fatalf("fixture actor %s missing", fixtureOwnerEmail)
	}

	seeder := &Seeder{client: client, AdminEmail: "ops@example.com", AdminPassword: "hunter2", PollInterval: time.Millisecond}
	start := time.Now()
	res, err := seeder.Seed(ctx, SeedOptions{
		WorkspaceSlug: stubWS,
		AgentSlug:     stubAgent,
		IngestWait:    5 * time.Second,
		RunID:         "eval-test-ownerwait",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Seed() = %v, want nil", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Seed() took %s with a 5s ingest budget, want a fast stable-count exit (a full-budget burn means the wait went blind again)", elapsed)
	}
	if !res.NotesAPILive {
		t.Fatalf("Seed.NotesAPILive = false, want true")
	}
	for _, n := range res.Notes {
		if strings.Contains(n, "ingestion wait elapsed") {
			t.Errorf("wait reported %q; it should have observed the owner's notes and returned early", n)
		}
	}

	// The wait polled AS THE OWNER, repeatedly enough to see the count
	// stabilize (initial fetch + 2 confirming polls), and never as admin.
	stub.mu.Lock()
	ownerPolls, adminPolls := stub.ownerNotePolls, stub.adminNotePolls
	stub.mu.Unlock()
	if ownerPolls < 3 {
		t.Errorf("ingestion wait polled %d times as the fixture owner, want >= 3 (initial fetch + stable-count confirmation)", ownerPolls)
	}
	if adminPolls != 0 {
		t.Errorf("ingestion wait polled the notes API as admin %d times, want 0 — the admin identity is structurally blind", adminPolls)
	}

	// The asymmetry itself, asserted on the same fake through the same
	// client: admin sees nothing, the owner sees the seeded notes.
	adminToken, _, err := client.Login(ctx, "ops@example.com", "hunter2")
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	adminNotes, live, err := client.ListNotes(ctx, adminToken, stubWS, "")
	if err != nil || !live {
		t.Fatalf("admin notes listing: live=%v err=%v", live, err)
	}
	if len(adminNotes) != 0 {
		t.Errorf("admin sees %d notes, want 0 — this blindness is why the old admin-token wait never observed ingestion", len(adminNotes))
	}
	ownerToken, _, err := client.Login(ctx, sari.Email, sari.Password)
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	ownerNotes, live, err := client.ListNotes(ctx, ownerToken, stubWS, "")
	if err != nil || !live {
		t.Fatalf("owner notes listing: live=%v err=%v", live, err)
	}
	if len(ownerNotes) < 1 {
		t.Errorf("fixture owner sees %d notes, want >= 1 (the seeded corpus must be visible to the identity that ingested it)", len(ownerNotes))
	}

	// The scope audit is untouched by the wait's identity: still admin-driven
	// on purpose (admin must NOT see the owner's private row).
	if !res.ScopeAudit.Available || !res.ScopeAudit.Passed {
		t.Errorf("scope audit = %+v, want available+passed", res.ScopeAudit)
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

// TestFixtureAgentExposesMemorySearch pins the self-search leg's provisioning
// contract (fix-memory-prefetch-matching D6, denylist form). The recorded live
// failure predates the denylist: the fixture agent carried an empty tools
// allowlist, the runner exposed zero registry tools for an empty allowlist,
// and the model had no memory.search schema to call. Under the denylist an
// empty DisabledTools exposes every catalog tool — memory.search included —
// so the harness must (a) create the fixture agent with the default (empty)
// denylist and no legacy `tools` key, (b) repair reused agents whose stored
// denylist denies a required tool, and (c) stay idempotent when the stored
// denylist denies none of them.
func TestFixtureAgentExposesMemorySearch(t *testing.T) {
	seedOpts := func(runID string) SeedOptions {
		return SeedOptions{
			WorkspaceSlug: stubWS,
			AgentSlug:     stubAgent,
			IngestWait:    5 * time.Millisecond,
			RunID:         runID,
		}
	}
	newSeeder := func(srv *httptest.Server) *Seeder {
		return &Seeder{
			client:        NewAPIClient(srv.URL, time.Minute),
			AdminEmail:    "ops@example.com",
			AdminPassword: "hunter2",
			PollInterval:  time.Millisecond,
		}
	}

	// (a) Create path: the create body carries no tools key at all (the
	// legacy allowlist is gone) and no disabled_tools key — the server's
	// default empty denylist exposes memory.search.
	stub := newStub(t, false)
	srv := httptest.NewServer(stub.handler())
	res, err := newSeeder(srv).Seed(context.Background(), seedOpts("eval-test-tools-create"))
	if err != nil {
		t.Fatalf("Seed() create path = %v, want nil", err)
	}
	stub.mu.Lock()
	created := stub.createdAgentDisabledTools
	stub.mu.Unlock()
	if len(created) != 0 {
		t.Errorf("agent create body disabled_tools = %v, want empty (the default denylist exposes memory.search)", created)
	}
	if len(res.Agent.DisabledTools) != 0 {
		t.Errorf("SeedResult.Agent.DisabledTools = %v, want empty", res.Agent.DisabledTools)
	}

	// (b) Reuse path, denylist denying memory.search: exactly one repair
	// PATCH replacing the denylist with memory.search removed.
	stub = newStub(t, false)
	stub.existingAgentDisabledTools = []string{"memory.search"} // the denying state
	srv2 := httptest.NewServer(stub.handler())
	res, err = newSeeder(srv2).Seed(context.Background(), seedOpts("eval-test-tools-reuse"))
	if err != nil {
		t.Fatalf("Seed() reuse path = %v, want nil", err)
	}
	stub.mu.Lock()
	patches := append([][]string(nil), stub.disabledToolsPatches...)
	stub.mu.Unlock()
	if len(patches) != 1 {
		t.Fatalf("reuse path issued %d disabled_tools PATCHes, want 1", len(patches))
	}
	if len(patches[0]) != 0 {
		t.Errorf("repair PATCH disabled_tools = %v, want empty (memory.search removed)", patches[0])
	}
	if len(res.Agent.DisabledTools) != 0 {
		t.Errorf("repaired SeedResult.Agent.DisabledTools = %v, want empty", res.Agent.DisabledTools)
	}

	// (c) Reuse path, denylist denying none of the required tools
	// (unrelated denial kept): idempotent, zero PATCHes.
	stub = newStub(t, false)
	stub.existingAgentDisabledTools = []string{"browser"}
	srv3 := httptest.NewServer(stub.handler())
	if _, err := newSeeder(srv3).Seed(context.Background(), seedOpts("eval-test-tools-idem")); err != nil {
		t.Fatalf("Seed() idempotent reuse = %v, want nil", err)
	}
	stub.mu.Lock()
	patches = append([][]string(nil), stub.disabledToolsPatches...)
	stub.mu.Unlock()
	if len(patches) != 0 {
		t.Errorf("idempotent reuse issued %d disabled_tools PATCHes, want 0", len(patches))
	}
}

// storedEvalRecordPath is the archived scoreboard of live run
// eval-20260920-042455 (openspec change fix-memory-eval-grader), kept as the
// determinism guard's regression input.
const storedEvalRecordPath = "../../../openspec/changes/archive/2026-09-20-fix-memory-eval-grader/eval-scoreboard.json"

// TestStoredEvalRecordRegradesIdentically is the determinism guard
// (harden-memory-eval-multihop tasks 3.1): the stored eval-20260920-042455
// record is re-graded with the NEW (hardened) fixture loaded, and every
// verdict must come out identical to the recorded one. The grader is a pure
// function of (question, answer, evidence) — this proves it is
// fixture-independent: loading the 30-session corpus changes no verdict, so a
// hardened-run scoreboard stays comparable to the recorded baseline.
func TestStoredEvalRecordRegradesIdentically(t *testing.T) {
	if err := Fixture.ValidateFixture(); err != nil {
		t.Fatalf("hardened fixture invalid: %v", err)
	}

	raw, err := os.ReadFile(storedEvalRecordPath)
	if err != nil {
		t.Fatalf("stored eval record %s unreadable: %v", storedEvalRecordPath, err)
	}
	var recorded struct {
		RunID       string `json:"run_id"`
		PerQuestion []struct {
			ID               string `json:"id"`
			Type             string `json:"type"`
			Recall           *bool  `json:"recall"`
			CitationValid    *bool  `json:"citation_valid"`
			ScopeSafe        *bool  `json:"scope_safe"`
			AbstainedCorrect *bool  `json:"abstained_correct"`
			Answer           string `json:"answer"`
			Status           string `json:"status"`
			EvidenceCount    int    `json:"evidence_count"`
		} `json:"per_question"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatalf("stored eval record JSON: %v", err)
	}
	if recorded.RunID != "eval-20260920-042455" {
		t.Fatalf("stored record run_id = %q, want eval-20260920-042455", recorded.RunID)
	}
	if len(recorded.PerQuestion) == 0 {
		t.Fatal("stored record carries no per-question rows")
	}

	assertArm := func(t *testing.T, id, arm string, got, want *bool) {
		t.Helper()
		gotStr, wantStr := "<nil>", "<nil>"
		if got != nil {
			gotStr = fmt.Sprintf("%v", *got)
		}
		if want != nil {
			wantStr = fmt.Sprintf("%v", *want)
		}
		if (got == nil) != (want == nil) || (got != nil && want != nil && *got != *want) {
			t.Errorf("%s %s: grader = %s, recorded = %s (grader must be fixture-independent)", id, arm, gotStr, wantStr)
		}
	}

	for _, row := range recorded.PerQuestion {
		t.Run(row.ID, func(t *testing.T) {
			ans := Answer{Text: row.Answer, Status: row.Status}
			var ev []Evidence
			if row.EvidenceCount > 0 {
				ev = make([]Evidence, row.EvidenceCount) // only len(evidence) reaches the arms
			}

			q, found := questionByIDOK(row.ID)
			if !found {
				// The new catalog dropped this id: grade the recorded row by
				// its own stored data (stored-answer contract: type + answer
				// + evidence count). Only the fixture-field-independent arms
				// (scope safety, and the exempt-type arms) are comparable.
				q = Question{ID: row.ID, Type: QuestionType(row.Type)}
				got := ScoreQuestion(q, ans, ev)
				assertArm(t, row.ID, "scope_safe", got.ScopeSafe, row.ScopeSafe)
				if q.Type == TypeAbstention || q.Type == TypeScope {
					assertArm(t, row.ID, "citation_valid", got.CitationValid, row.CitationValid)
					assertArm(t, row.ID, "abstained_correct", got.AbstainedCorrect, row.AbstainedCorrect)
				}
				return
			}

			got := ScoreQuestion(q, ans, ev)
			assertArm(t, row.ID, "recall", got.Recall, row.Recall)
			assertArm(t, row.ID, "citation_valid", got.CitationValid, row.CitationValid)
			assertArm(t, row.ID, "scope_safe", got.ScopeSafe, row.ScopeSafe)
			assertArm(t, row.ID, "abstained_correct", got.AbstainedCorrect, row.AbstainedCorrect)
		})
	}
}

// questionByIDOK returns the fixture question with the given id and whether
// it exists (the non-fatal lookup the determinism guard needs).
func questionByIDOK(id string) (Question, bool) {
	for _, q := range Fixture.Questions {
		if q.ID == id {
			return q, true
		}
	}
	return Question{}, false
}
