package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Runner drives each question through the real chat path (/v1 OpenResponses
// turns in fresh sessions) and captures the answer plus any memory.search
// tool evidence the run exposed in its transcript output.
type Runner struct {
	client *APIClient
}

// RunOptions parameterizes one question pass.
type RunOptions struct {
	WorkspaceSlug string
	AgentSlug     string
	RunID         string
}

// TurnResult is one question's raw outcome before scoring.
type TurnResult struct {
	Question Question
	Answer   Answer
	Evidence []Evidence
	Err      error
}

// Run executes every fixture question. Each turn runs in its own
// eval-<runid>-q-<id> session so questions cannot contaminate each other's
// context; the chat key used is the asking actor's, which is what structurally
// scopes memory.search and ingestion to their identity.
func (r *Runner) Run(ctx context.Context, opts RunOptions) ([]TurnResult, error) {
	keys, err := r.actorKeys(ctx, opts.WorkspaceSlug)
	if err != nil {
		return nil, err
	}

	results := make([]TurnResult, 0, len(Fixture.Questions))
	for _, q := range Fixture.Questions {
		tr := TurnResult{Question: q}
		ak := keys[q.AskAs]
		if ak == nil {
			tr.Err = fmt.Errorf("no chat key for actor %s", q.AskAs)
			results = append(results, tr)
			continue
		}

		sessionID := fmt.Sprintf("%s-q-%s", opts.RunID, q.ID)
		resp, err := r.client.ChatTurn(ctx, ak.Key, opts.AgentSlug, q.Text, sessionID)
		if err != nil {
			tr.Err = fmt.Errorf("question %s: %w", q.ID, err)
			results = append(results, tr)
			continue
		}

		tr.Answer = extractAnswer(resp)
		tr.Evidence = extractSearchEvidence(resp)
		if st, _ := resp["status"].(string); st != "completed" {
			tr.Err = fmt.Errorf("question %s: run status %q (need completed)", q.ID, st)
		}

		// Fallback evidence correlation: when the transcript exposed no
		// memory.search cards, correlate through the notes REST API with the
		// question text as the same identity.
		if len(tr.Evidence) == 0 && tr.Err == nil {
			tr.Evidence = r.notesFallback(ctx, ak.Token, opts.WorkspaceSlug, q.Text)
		}
		results = append(results, tr)
	}
	return results, nil
}

// actorKeys logs every fixture actor in and exchanges their chat keys.
func (r *Runner) actorKeys(ctx context.Context, wsSlug string) (map[string]*actorKey, error) {
	keys := map[string]*actorKey{}
	for _, a := range Fixture.Actors {
		token, _, err := r.client.Login(ctx, a.Email, a.Password)
		if err != nil {
			return nil, fmt.Errorf("runner login %s (seed the fixture first): %w", a.Email, err)
		}
		key, err := r.client.ExchangeChatKey(ctx, token, wsSlug)
		if err != nil {
			return nil, fmt.Errorf("runner key exchange for %s: %w", a.Email, err)
		}
		keys[a.Email] = &actorKey{Email: a.Email, Token: token, Key: key}
	}
	return keys, nil
}

// notesFallback lists notes matching the question text as the acting user.
// Presence of a leak-shaped row is evidence in the negative sense — the score
// arms treat it like an opened search result, with the fallback flag set.
func (r *Runner) notesFallback(ctx context.Context, token, wsSlug, q string) []Evidence {
	notes, live, err := r.client.ListNotes(ctx, token, wsSlug, q)
	if err != nil || !live {
		return nil
	}
	ev := make([]Evidence, 0, len(notes))
	for _, n := range notes {
		ev = append(ev, Evidence{ToolName: "notes-api", Result: n.Content, Fallback: true})
	}
	return ev
}

// extractAnswer folds the Response output's message items into one text.
func extractAnswer(resp map[string]any) Answer {
	ans := Answer{}
	if st, ok := resp["status"].(string); ok {
		ans.Status = st
	}
	output, _ := resp["output"].([]any)
	var parts []string
	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok || m["type"] != "message" {
			continue
		}
		content, _ := m["content"].([]any)
		for _, part := range content {
			p, ok := part.(map[string]any)
			if !ok || p["type"] != "output_text" {
				continue
			}
			if t, ok := p["text"].(string); ok && strings.TrimSpace(t) != "" {
				parts = append(parts, t)
			}
		}
	}
	ans.Text = strings.Join(parts, "\n")
	return ans
}

// extractSearchEvidence pairs memory.search function_call items with their
// onclaw.function_call_output counterparts from the same transcript.
func extractSearchEvidence(resp map[string]any) []Evidence {
	output, _ := resp["output"].([]any)

	// Collect the memory.search call ids in order.
	type call struct {
		args string
	}
	searches := map[string]*call{}
	var order []string
	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok || m["type"] != "function_call" {
			continue
		}
		name, _ := m["name"].(string)
		if name != "memory.search" {
			continue
		}
		callID, _ := m["call_id"].(string)
		args, _ := m["arguments"].(string)
		searches[callID] = &call{args: args}
		order = append(order, callID)
	}
	if len(order) == 0 {
		return nil
	}

	results := map[string]string{}
	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok || m["type"] != "onclaw.function_call_output" {
			continue
		}
		name, _ := m["name"].(string)
		if name != "memory.search" {
			continue
		}
		callID, _ := m["call_id"].(string)
		if _, tracked := searches[callID]; !tracked {
			continue
		}
		results[callID] = stringify(m["result"])
	}

	ev := make([]Evidence, 0, len(order))
	for _, callID := range order {
		ev = append(ev, Evidence{
			ToolName:  "memory.search",
			Arguments: searches[callID].args,
			Result:    results[callID],
			CallID:    callID,
		})
	}
	return ev
}

// stringify renders an arbitrary tool-result JSON value as readable text.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
