package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Pagination-parameter pass-through for add-server-authoritative-transcript
// D2: ListSessionEvents parses `before` and `reverse` alongside the existing
// `after`/`limit` pair and hands them to History verbatim; an unknown cursor
// surfaces the invalid-request error contract instead of an empty page.

// TestAgents_ListSessionEventsBackwardParams verifies the backward-window
// query params reach History exactly as requested.
func TestAgents_ListSessionEventsBackwardParams(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
	runner.historyReqs = make(chan agents.HistoryRequest, 1)
	r := newAgentStreamTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?before=evt-5&reverse=true&limit=60", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case got := <-runner.historyReqs:
		want := agents.HistoryRequest{
			WorkspaceID: "ws-run-test",
			AgentID:     "agent-atlas-1",
			SessionID:   "sess-1",
			Before:      "evt-5",
			Limit:       60,
			Reverse:     true,
		}
		if got != want {
			t.Errorf("history request = %+v, want %+v", got, want)
		}
	default:
		t.Fatal("expected the handler to query history")
	}
}

// TestAgents_ListSessionEventsForwardParamsUnchanged pins that forward
// requests — with or without the pre-change params — produce the same
// HistoryRequest they always have: Before and Reverse stay zero, the limit
// clamp is untouched.
func TestAgents_ListSessionEventsForwardParamsUnchanged(t *testing.T) {
	t.Run("after+limit as before the change", func(t *testing.T) {
		runner := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
		runner.historyReqs = make(chan agents.HistoryRequest, 1)
		r := newAgentStreamTestEnv(t, runner)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?after=evt-8&limit=10", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		want := agents.HistoryRequest{
			WorkspaceID: "ws-run-test",
			AgentID:     "agent-atlas-1",
			SessionID:   "sess-1",
			After:       "evt-8",
			Limit:       10,
		}
		select {
		case got := <-runner.historyReqs:
			if got != want {
				t.Errorf("history request = %+v, want %+v", got, want)
			}
		default:
			t.Fatal("expected the handler to query history")
		}
	})

	t.Run("no params", func(t *testing.T) {
		runner := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
		runner.historyReqs = make(chan agents.HistoryRequest, 1)
		r := newAgentStreamTestEnv(t, runner)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		want := agents.HistoryRequest{
			WorkspaceID: "ws-run-test",
			AgentID:     "agent-atlas-1",
			SessionID:   "sess-1",
		}
		select {
		case got := <-runner.historyReqs:
			if got != want {
				t.Errorf("history request = %+v, want %+v", got, want)
			}
		default:
			t.Fatal("expected the handler to query history")
		}
	})

	t.Run("limit clamps at 500", func(t *testing.T) {
		runner := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
		runner.historyReqs = make(chan agents.HistoryRequest, 1)
		r := newAgentStreamTestEnv(t, runner)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?limit=9999", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		select {
		case got := <-runner.historyReqs:
			if got.Limit != 500 {
				t.Errorf("clamped limit = %d, want 500", got.Limit)
			}
		default:
			t.Fatal("expected the handler to query history")
		}
	})
}

// TestAgents_ListSessionEventsUnknownCursorIsInvalidRequest verifies the
// unknown-cursor contract end to end at the handler: the runner's
// cursor-not-found error (agents.History wraps the store's ErrNotFound in
// domain.ErrInvalid) answers 400 invalid_request, never an empty page.
func TestAgents_ListSessionEventsUnknownCursorIsInvalidRequest(t *testing.T) {
	runner := newFakeStreamingRunner(&agents.HistoryResult{Events: []agents.TranscriptEvent{}})
	runner.historyErr = fmt.Errorf("%w: cursor event %q not found in session", domain.ErrInvalid, "evt-nope")
	r := newAgentStreamTestEnv(t, runner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspaces/run-ws/agents/atlas/sessions/sess-1/events?before=evt-nope&limit=60", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body is not an envelope: %v (%s)", err, w.Body.String())
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("expected invalid_request, got %q", env.Error.Code)
	}
	if env.Error.Message == "" {
		t.Errorf("expected a client-safe message carrying the cursor detail, got %q", env.Error.Message)
	}
}
