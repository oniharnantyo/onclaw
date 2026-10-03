package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// countingStubServer wraps handler with a request counter and serves it; the
// counter is how the one-attempt contract is asserted server-side.
func countingStubServer(t *testing.T, hits *atomic.Int64, handler func(w http.ResponseWriter, r *http.Request, n int64)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		handler(w, r, n)
	}))
}

// systemoneAnswers renders a systemone response body from probabilities in
// object form ({"noul": p}); bare-number rows are written inline where a test
// needs them.
func systemoneAnswers(needs, notes, events float64) string {
	body, err := json.Marshal(map[string]any{
		"answers": map[string]any{
			decisionNeedsMemoryQuestion: map[string]any{"noul": needs},
			decisionNotesQuestion:       map[string]any{"noul": notes},
			decisionEventsQuestion:      map[string]any{"noul": events},
		},
		"usage": map[string]any{"input": 1, "output": 1},
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func TestDecisionVerdictMapping(t *testing.T) {
	tests := []struct {
		name               string
		needs, notes, evts float64
		want               IntentVerdict
	}{
		{"needs below threshold is self-contained", 0.49, 0.9, 0.9, IntentVerdict{}},
		{"needs exactly at threshold counts as yes", 0.5, 0, 0, IntentVerdict{NeedsDeepMemory: true, Notes: true, Events: true}},
		{"needs above with all buckets below routes both", 0.9, 0.2, 0.3, IntentVerdict{NeedsDeepMemory: true, Notes: true, Events: true}},
		{"needs above with notes only", 0.9, 0.6, 0.1, IntentVerdict{NeedsDeepMemory: true, Notes: true}},
		{"needs above with events only", 0.9, 0.1, 0.6, IntentVerdict{NeedsDeepMemory: true, Events: true}},
		{"needs above with both buckets", 0.9, 0.6, 0.6, IntentVerdict{NeedsDeepMemory: true, Notes: true, Events: true}},
		{"needs above with buckets exactly at threshold", 0.9, 0.5, 0.5, IntentVerdict{NeedsDeepMemory: true, Notes: true, Events: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decisionVerdict(map[string]float64{
				decisionNeedsMemoryQuestion: tt.needs,
				decisionNotesQuestion:       tt.notes,
				decisionEventsQuestion:      tt.evts,
			})
			if got != tt.want {
				t.Fatalf("decisionVerdict = %+v, want %+v", got, tt.want)
			}
			// 3.3: a decision verdict never routes associative or names an
			// entity — structurally, but asserted on every row against drift.
			if got.Associative || got.Entity != "" {
				t.Fatalf("decision verdict must never carry the associative route or an entity, got %+v", got)
			}
		})
	}
}

// TestDecisionClientContract pins the wire contract (docs.typesafe.ai): the
// pinned path, auth header, state/model fields, and exactly three noul
// questions keyed by id with instructions and {true,false} criteria.
func TestDecisionClientContract(t *testing.T) {
	var hits atomic.Int64
	var gotBody decisionRequest
	var gotAuth, gotPath, gotMethod string
	srv := countingStubServer(t, &hits, func(w http.ResponseWriter, r *http.Request, _ int64) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(systemoneAnswers(0.9, 0.9, 0.1)))
	})
	defer srv.Close()

	client := newDecisionClient(srv.URL, "ts-key", "jev-latest")
	verdict, err := client.classify(context.Background(), "What did we decide about the vendor renewal?")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected notes-routed verdict, got %+v", verdict)
	}

	if gotMethod != http.MethodPost || gotPath != "/" {
		t.Fatalf("request = %s %s, want POST /", gotMethod, gotPath)
	}
	if gotAuth != "Bearer ts-key" {
		t.Fatalf("Authorization = %q, want bearer auth with the provider key", gotAuth)
	}
	if gotBody.State != "What did we decide about the vendor renewal?" {
		t.Fatalf("state = %q, want the raw turn text", gotBody.State)
	}
	if gotBody.Model != "jev-latest" {
		t.Fatalf("model = %q, want the configured decision model", gotBody.Model)
	}
	if len(gotBody.Questions) != 3 {
		t.Fatalf("questions = %d, want exactly three noul questions", len(gotBody.Questions))
	}
	// questions is a map keyed by question id (docs: map<string, Question>).
	wantInstructions := map[string]string{
		decisionNeedsMemoryQuestion: "does answering this turn require the workspace's long-term memory",
		decisionNotesQuestion:       "are durable stored facts relevant",
		decisionEventsQuestion:      "is what-happened-and-when relevant",
	}
	for _, id := range []string{decisionNeedsMemoryQuestion, decisionNotesQuestion, decisionEventsQuestion} {
		q, ok := gotBody.Questions[id]
		if !ok {
			t.Fatalf("question %q missing from the questions map", id)
		}
		if q.Type != "noul" {
			t.Fatalf("question %q type = %q, want noul", id, q.Type)
		}
		if q.Instructions != wantInstructions[id] {
			t.Fatalf("question %q instructions = %q, want the pinned text", id, q.Instructions)
		}
		if q.Criteria == nil || q.Criteria.True == "" || q.Criteria.False == "" {
			t.Fatalf("question %q carries no true/false criteria", id)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("classification issued %d requests, want exactly 1", hits.Load())
	}
}

// TestDecisionClientOverHTTP walks the answer-form and failure table through a
// stub systemone: every failure row must return the self-contained zero
// verdict with an error and NEVER issue a second HTTP attempt.
func TestDecisionClientOverHTTP(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		delay      time.Duration
		ctxTimeout time.Duration
		want       IntentVerdict
		wantErr    bool
	}{
		{
			name:   "object answer values map to the verdict",
			status: 200,
			body:   systemoneAnswers(0.9, 0.9, 0.1),
			want:   IntentVerdict{NeedsDeepMemory: true, Notes: true},
		},
		{
			name:   "bare number answer values map to the verdict",
			status: 200,
			body:   `{"answers": {"needs_memory": 0.9, "bucket_notes": 0.1, "bucket_events": 0.8}, "usage": {"total": 3}}`,
			want:   IntentVerdict{NeedsDeepMemory: true, Events: true},
		},
		{
			name:   "needs below threshold is self-contained",
			status: 200,
			body:   systemoneAnswers(0.1, 0.9, 0.9),
			want:   IntentVerdict{},
		},
		{
			name:   "all buckets below routes notes and events",
			status: 200,
			body:   systemoneAnswers(0.9, 0.2, 0.3),
			want:   IntentVerdict{NeedsDeepMemory: true, Notes: true, Events: true},
		},
		{
			name:    "non-string answer value fails open",
			status:  200,
			body:    `{"answers": {"needs_memory": "0.9", "bucket_notes": 0.9, "bucket_events": 0.9}}`,
			wantErr: true,
		},
		{
			name:    "object answer without noul fails open",
			status:  200,
			body:    `{"answers": {"needs_memory": {"confidence": 0.9}, "bucket_notes": 0.9, "bucket_events": 0.9}}`,
			wantErr: true,
		},
		{
			name:    "null answer fails open",
			status:  200,
			body:    `{"answers": {"needs_memory": null, "bucket_notes": 0.9, "bucket_events": 0.9}}`,
			wantErr: true,
		},
		{
			name:    "missing bucket answer fails open",
			status:  200,
			body:    `{"answers": {"needs_memory": {"noul": 0.9}}}`,
			wantErr: true,
		},
		{
			name:    "missing answers map fails open",
			status:  200,
			body:    `{"usage": {"total": 3}}`,
			wantErr: true,
		},
		{
			name:    "malformed JSON body fails open",
			status:  200,
			body:    `<html>gateway error</html>`,
			wantErr: true,
		},
		{
			name:    "401 fails open",
			status:  401,
			body:    `{"error": {"message": "bad key"}}`,
			wantErr: true,
		},
		{
			name:    "500 fails open",
			status:  500,
			body:    `{"error": {"message": "backend down"}}`,
			wantErr: true,
		},
		{
			name:       "budget timeout fails open",
			status:     200,
			body:       systemoneAnswers(0.9, 0.9, 0.9),
			delay:      300 * time.Millisecond,
			ctxTimeout: 50 * time.Millisecond,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int64
			srv := countingStubServer(t, &hits, func(w http.ResponseWriter, _ *http.Request, _ int64) {
				if tt.delay > 0 {
					time.Sleep(tt.delay)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			defer srv.Close()

			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}
			verdict, err := newDecisionClient(srv.URL, "ts-key", "jev-latest").classify(ctx, "What did we decide?")
			if (err != nil) != tt.wantErr {
				t.Fatalf("classify error = %v, wantErr %v", err, tt.wantErr)
			}
			if verdict != tt.want {
				t.Fatalf("verdict = %+v, want %+v", verdict, tt.want)
			}
			if hits.Load() != 1 {
				t.Fatalf("classification issued %d requests, want exactly 1 (no retry on any status)", hits.Load())
			}
		})
	}
}

// TestDecisionClient_EntityShapedTurnRoutesNotesEventsOnly (3.3): an
// entity-shaped turn classified through the decision client routes at most
// notes and events — no associative question is ever carried and no entity
// name ever lands on the verdict, even for a turn the LLM gate would route
// associatively.
func TestDecisionClient_EntityShapedTurnRoutesNotesEventsOnly(t *testing.T) {
	var gotQuestions map[string]decisionQuestion
	var hits atomic.Int64
	srv := countingStubServer(t, &hits, func(w http.ResponseWriter, r *http.Request, _ int64) {
		var body decisionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotQuestions = body.Questions
		// Every question screams yes — the decider is maximally confident.
		_, _ = w.Write([]byte(systemoneAnswers(0.95, 0.9, 0.85)))
	})
	defer srv.Close()

	verdict, err := newDecisionClient(srv.URL, "ts-key", "jev-latest").
		classify(context.Background(), "What is the status of ProjectX and who owns it?")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || !verdict.Events {
		t.Fatalf("expected the notes+events routing, got %+v", verdict)
	}
	if verdict.Associative || verdict.Entity != "" {
		t.Fatalf("entity-shaped turn must never route associative or carry an entity, got %+v", verdict)
	}
	for id := range gotQuestions {
		if id == "associative" || id == "entity" {
			t.Fatalf("the decision request must not carry an associative question, got %q", id)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("classification issued %d requests, want exactly 1", hits.Load())
	}
}

// TestDecisionClientTimeoutAbandonsUnderCallerBudget (D3): the client adds no
// second timeout layer — a slow endpoint is abandoned exactly at the caller's
// already-budgeted context deadline.
func TestDecisionClientTimeoutAbandonsUnderCallerBudget(t *testing.T) {
	var hits atomic.Int64
	srv := countingStubServer(t, &hits, func(w http.ResponseWriter, _ *http.Request, _ int64) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(systemoneAnswers(0.9, 0.9, 0.9)))
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	verdict, err := newDecisionClient(srv.URL, "ts-key", "jev-latest").classify(ctx, "What did we decide?")
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the caller's deadline error, got %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if elapsed >= time.Second {
		t.Fatalf("the call must abort at the caller's deadline, took %v", elapsed)
	}
	if hits.Load() != 1 {
		t.Fatalf("classification issued %d requests, want exactly 1", hits.Load())
	}
}
