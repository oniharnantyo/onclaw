package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// The TypeSafe /v1/systemone wire contract (docs.typesafe.ai API reference;
// the verify probe in providers.TypeSafeProvider rides the same shapes):
//
//	POST {endpoint}
//	Authorization: Bearer <provider key>
//	{"state": "<raw turn text>", "model": "<decision model>", "questions": {
//	  "needs_memory": {"type": "noul", "instructions": "...", "criteria": {"true": "...", "false": "..."}},
//	  "bucket_notes": {...}, "bucket_events": {...}}}
//	→ {"model": "...", "answers": {"needs_memory": {"type": "noul", "noul": 0.83}, ...},
//	   "usage": {"input_tokens": 296, "output_tokens": 20}}
//
// questions is a MAP keyed by the question id (the key is the answer key, not
// a "name" field), and a noul criteria is a {true, false} object, not a list.
// The endpoint is the provider's stored base_url (which IS the full endpoint
// including the resource path, unlike the language providers) or the canonical
// TypesafeDefaultEndpoint. Answer values decode tolerantly: an object carrying
// a "noul" probability or a bare number; anything else is malformed and fails
// open. One request per classification — never retried on any status (D3).

const (
	// decisionThreshold reads a noul probability as yes at or above it (D2).
	// Not workspace-configurable in v1; the fail direction is safe
	// (needs_memory is recall-oriented: buckets default wide).
	decisionThreshold = 0.5
	// decisionMaxBodyBytes bounds the response read; three answers plus the
	// usage blob are tiny, so this only caps a runaway endpoint.
	decisionMaxBodyBytes = 64 << 10
)

// Decision configuration keys on the workspace "memory" tool-settings record,
// written together by the settings validation (D6) and read per turn (D1).
const (
	decisionProviderIDKey = "decision_provider_id"
	decisionModelKey      = "decision_model"
)

// The three fixed noul question names (spec: TypeSafe decision call).
const (
	decisionNeedsMemoryQuestion = "needs_memory"
	decisionNotesQuestion       = "bucket_notes"
	decisionEventsQuestion      = "bucket_events"
)

// decisionCriteria is a noul question's criteria: what a yes and a no mean
// (docs: criteria.true / criteria.false, both optional).
type decisionCriteria struct {
	True  string `json:"true"`
	False string `json:"false"`
}

// decisionQuestion is one noul question of the systemone request. The map key
// it rides under IS the question id — answers come back under the same key;
// there is no separate name field.
type decisionQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     *decisionCriteria `json:"criteria,omitempty"`
}

// decisionRequest is the one request body the client sends.
type decisionRequest struct {
	State     string                      `json:"state"`
	Model     string                      `json:"model"`
	Questions map[string]decisionQuestion `json:"questions"`
}

// decisionResponse is the tolerated response shape: answers keyed by question
// name (values decoded tolerantly) plus an opaque usage blob.
type decisionResponse struct {
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   map[string]json.RawMessage `json:"usage"`
}

// decisionQuestions renders the three fixed noul questions under their
// pinned ids. The instructions/criteria text is the deterministic Go mapping
// of today's intentSystemPrompt rules (D2): the decision backend must
// classify by the same policy the LLM gate prompts for, not a drifting
// paraphrase.
func decisionQuestions() map[string]decisionQuestion {
	return map[string]decisionQuestion{
		decisionNeedsMemoryQuestion: {
			Type:         "noul",
			Instructions: "does answering this turn require the workspace's long-term memory",
			Criteria: &decisionCriteria{
				True:  "the turn references past discussions, decisions, people, projects, timelines, preferences, or anything previously said or done in this workspace",
				False: "the turn is greetings, small talk, pure reasoning, coding, or anything answerable without workspace history",
			},
		},
		decisionNotesQuestion: {
			Type:         "noul",
			Instructions: "are durable stored facts relevant",
			Criteria: &decisionCriteria{
				True:  "answering needs the workspace's durable stored facts (curated notes about people, projects, systems, and preferences)",
				False: "no stored fact would contribute to the answer",
			},
		},
		decisionEventsQuestion: {
			Type:         "noul",
			Instructions: "is what-happened-and-when relevant",
			Criteria: &decisionCriteria{
				True:  "answering needs what happened and when (episodic events and timelines)",
				False: "no dated or episodic context would contribute to the answer",
			},
		},
	}
}

// decisionClient is one turn's wired decision backend (D1): the systemone
// endpoint, the provider's decrypted key, and the configured decision model.
// Built per classification; holds no state worth pooling.
type decisionClient struct {
	endpoint string
	apiKey   string
	model    string
}

func newDecisionClient(endpoint, apiKey, model string) *decisionClient {
	return &decisionClient{endpoint: endpoint, apiKey: apiKey, model: model}
}

// classify runs the one systemone request and maps its answers to a verdict.
// One attempt, no retry (D3): the caller's already-budgeted context bounds
// the whole call — no second timeout layer is added here, and any failure
// errors for the caller to fail open on.
func (c *decisionClient) classify(ctx context.Context, turnText string) (IntentVerdict, error) {
	probs, err := c.probabilities(ctx, turnText)
	if err != nil {
		return IntentVerdict{}, err
	}
	return decisionVerdict(probs), nil
}

// probabilities performs the single HTTP attempt and decodes the three
// answers. Non-2xx is an error like any other; the body of a failed response
// is drained and discarded.
func (c *decisionClient) probabilities(ctx context.Context, state string) (map[string]float64, error) {
	payload, err := json.Marshal(decisionRequest{
		State:     state,
		Model:     c.model,
		Questions: decisionQuestions(),
	})
	if err != nil {
		return nil, fmt.Errorf("memory decision call: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("memory decision call: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "onclaw")

	// One attempt — no retry on any status (D3): the budget deadline owns
	// latency, and a lost recall is recoverable next turn.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("memory decision call: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, decisionMaxBodyBytes))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("memory decision call: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("memory decision call: HTTP %d", resp.StatusCode)
	}

	var parsed decisionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("memory decision call: decode response: %w", err)
	}
	if parsed.Answers == nil {
		return nil, errors.New("memory decision call: response carries no answers map")
	}
	probs := make(map[string]float64, 3)
	for _, name := range []string{decisionNeedsMemoryQuestion, decisionNotesQuestion, decisionEventsQuestion} {
		raw, ok := parsed.Answers[name]
		if !ok {
			return nil, fmt.Errorf("memory decision call: answer %q missing", name)
		}
		p, err := decodeDecisionProbability(raw)
		if err != nil {
			return nil, err
		}
		probs[name] = p
	}
	return probs, nil
}

// decodeDecisionProbability tolerantly decodes one answer value: an object
// carrying a "noul" probability or a bare number. Anything else (strings,
// arrays, objects without noul, null) is malformed — the caller fails open.
func decodeDecisionProbability(raw json.RawMessage) (float64, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return 0, errors.New("memory decision call: answer is null")
	}
	var obj struct {
		Noul *float64 `json:"noul"`
	}
	if err := json.Unmarshal(trimmed, &obj); err == nil && obj.Noul != nil {
		return *obj.Noul, nil
	}
	var num float64
	if err := json.Unmarshal(trimmed, &num); err == nil {
		return num, nil
	}
	return 0, fmt.Errorf("memory decision call: answer %s is neither a noul object nor a number", trimmed)
}

// decisionVerdict maps the decoded probabilities to the routing verdict (D2):
// needs_memory below the threshold is self-contained; needs_memory above it
// routes the buckets that are themselves above the threshold, and when every
// bucket reads no it routes both — the same narrowing-is-not-our-job rule as
// parseIntent's unrouted-buckets fallback. A decision verdict never carries
// the associative route or an entity name (spec: associative unavailable in
// decision mode) — the mapping only ever sets NeedsDeepMemory, Notes, Events.
func decisionVerdict(probs map[string]float64) IntentVerdict {
	if probs[decisionNeedsMemoryQuestion] < decisionThreshold {
		return IntentVerdict{}
	}
	notes := probs[decisionNotesQuestion] >= decisionThreshold
	events := probs[decisionEventsQuestion] >= decisionThreshold
	if !notes && !events {
		return IntentVerdict{NeedsDeepMemory: true, Notes: true, Events: true}
	}
	return IntentVerdict{NeedsDeepMemory: true, Notes: notes, Events: events}
}

// decisionBackend is the workspace's stored decision configuration: the
// decision-class provider config id and the model the endpoint classifies
// with.
type decisionBackend struct {
	providerID string
	model      string
}

// storedDecisionBackend reads the decision configuration off the workspace
// "memory" settings record, mirroring workspaceSideCallModel's read pattern:
// ok=false — no settings source wired, absent or half-set pair, or a failed
// read — keeps the LLM path (absence-is-defaults). A present pair commits the
// turn to the decision path; every later failure there fails open
// self-contained instead of falling back to the LLM path.
func (g *IntentGate) storedDecisionBackend(ctx context.Context, workspaceID string) (decisionBackend, bool) {
	if g.settings == nil {
		return decisionBackend{}, false
	}
	row, err := g.settings.Get(ctx, workspaceID, "memory")
	if err != nil || row == nil || row.Config == nil {
		return decisionBackend{}, false
	}
	providerID, _ := row.Config[decisionProviderIDKey].(string)
	model, _ := row.Config[decisionModelKey].(string)
	if providerID == "" || model == "" {
		return decisionBackend{}, false
	}
	return decisionBackend{providerID: providerID, model: model}, true
}

// classifyDecision runs the decision path for a turn whose workspace stored a
// decision configuration: tenant-scoped provider resolution through the gate's
// provider catalog, the key decrypted with the workspace AAD exactly as
// buildSideCallModel does, and one systemone call under the caller's already
// budgeted context. The raw noul probabilities return alongside the verdict
// for the per-turn logDecision record. Any failure — provider resolution,
// keyless config, decrypt, transport, timeout, non-2xx, malformed body —
// errors; Classify fails open self-contained and never routes the turn back
// to the LLM path (the workspace chose decision mode).
func (g *IntentGate) classifyDecision(ctx context.Context, workspaceID string, backend decisionBackend, turnText string) (IntentVerdict, map[string]float64, error) {
	provider, err := g.providers.ByID(ctx, workspaceID, backend.providerID)
	if err != nil {
		return IntentVerdict{}, nil, fmt.Errorf("memory decision call: provider: %w", err)
	}
	if provider.KeyCiphertext == "" {
		return IntentVerdict{}, nil, errors.New("memory decision call: provider config carries no stored key")
	}
	plaintext, err := secrets.Decrypt(g.encryptionKey, []byte(workspaceID), provider.KeyCiphertext)
	if err != nil {
		return IntentVerdict{}, nil, errors.New("memory decision call: decrypt provider credentials")
	}
	endpoint := strings.TrimSpace(provider.BaseURL)
	if endpoint == "" {
		endpoint = providers.TypesafeDefaultEndpoint
	}
	// A dangling trailing slash makes the endpoint 307-redirect to an http://
	// downgrade whose redirect chain fails the call — the base_url IS the
	// full endpoint, so strip it.
	endpoint = strings.TrimRight(endpoint, "/")
	probs, err := newDecisionClient(endpoint, string(plaintext), backend.model).probabilities(ctx, turnText)
	if err != nil {
		return IntentVerdict{}, nil, err
	}
	return decisionVerdict(probs), probs, nil
}
