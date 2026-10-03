package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// defaultGateBudget is the classification budget used when the caller passes
// no usable one (budget <= 0). The budget itself is a configurable workspace
// memory-setting, gate_budget_ms (fix-memory-retrieval-lane D1/D2): the
// runner resolves it per turn from the settings record, bounded
// [MinGateBudgetMS, MaxGateBudgetMS] with DefaultGateBudgetMS as the
// absence-is-defaults value. Whatever budget applies is enforced with a
// context deadline inside Classify so a slow side-call model is abandoned at
// it and can never delay instruction composition beyond it.
const defaultGateBudget = time.Duration(DefaultGateBudgetMS) * time.Millisecond

// IntentVerdict is the gate's routing decision: whether the turn needs deep
// memory at all and, when it does, which buckets are relevant (curated notes,
// episodic events, or — wave3 D8's associative route — the entity graph, in
// which case Entity names the entity the turn is about, advisory and
// fail-open: the searcher re-resolves it through the store's exact →
// prefix/trigram seed resolution). The zero value is the self-contained
// verdict — no retrieval.
type IntentVerdict struct {
	NeedsDeepMemory bool
	Notes           bool
	Events          bool
	Associative     bool
	Entity          string
}

// IntentGate is the pre-compose intent classification (task 4.1, design D8):
// one cheap-model call before instruction composition that decides whether
// the turn needs deep memory and which buckets to route. Fail-open is the
// contract: a timeout, a model error, or undecodable output is equivalent to
// "self-contained" (debug-logged, never surfaced), because the agent can
// always search explicitly through memory.search — the gate is an
// optimization, never a correctness dependency.
type IntentGate struct {
	resolver ModelResolver
	trace    callbacks.Handler
	log      *slog.Logger
	// Decision-path dependencies (add-configurable-decision-backend D1): the
	// provider catalog and instance encryption key re-used from side-call
	// resolution, and the optional workspace settings source whose "memory"
	// record may carry a decision configuration. settings nil (unwired, the
	// trace-handler precedent) keeps the gate on the LLM path for every
	// workspace.
	providers     store.ProviderStore
	encryptionKey []byte
	settings      store.ToolSettingsStore
}

// NewIntentGate constructs the gate over the shared cheap-model seam: the
// workspace provider catalog, the instance encryption key, and the agentic
// model factory. The Langfuse trace callback rides SideCallOption as for the
// gate and gister; with no side-call model named, resolution fails per call
// and Classify fail-opens to self-contained.
func NewIntentGate(providerStore store.ProviderStore, encryptionKey []byte, factory ModelFactory, log *slog.Logger, opts ...SideCallOption) *IntentGate {
	var cfg sideCallConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &IntentGate{
		resolver:      newSideCallResolver(providerStore, encryptionKey, factory, cfg),
		trace:         cfg.trace,
		log:           log,
		providers:     providerStore,
		encryptionKey: encryptionKey,
		settings:      cfg.settings,
	}
}

// Classify runs the one bounded classification call for the turn's text. The
// workspace id scopes the side-call model's credential resolution — the gate
// is a single instance shared by every workspace, so the per-call identity is
// a parameter (the only deviation from a text-only signature). The budget is
// the classification deadline (fix-memory-retrieval-lane D1/D3): the caller
// resolves it from the workspace memory settings per turn; a budget <= 0
// falls back to defaultGateBudget. The deadline stays hard — a slow
// side-call model is abandoned at the budget, never allowed to stretch it.
//
// Model-side failures (resolution, generation, timeout, undecodable output)
// return the self-contained verdict with the error; the caller fail-opens by
// proceeding without retrieval. An empty or whitespace turn is quietly
// self-contained with a nil error — there is nothing to classify. When the
// workspace's memory settings carry a decision configuration, the turn
// classifies through the decision backend instead (same budget, same
// fail-open contract); see the decision branch below.
func (g *IntentGate) Classify(ctx context.Context, workspaceID, agentID, turnText string, budget time.Duration) (IntentVerdict, error) {
	text := strings.TrimSpace(turnText)
	if text == "" {
		return IntentVerdict{}, nil
	}

	if budget <= 0 {
		budget = defaultGateBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	start := time.Now()

	// Decision branch (add-configurable-decision-backend D1/D2): when the
	// workspace's memory settings record stores a decision configuration, the
	// turn classifies through the decision backend — one systemone request
	// under this same budget — and every decision-path failure fails open
	// self-contained without ever falling back to the LLM path: the workspace
	// chose decision mode. Absent configuration (or no settings source wired,
	// or an unreadable record) keeps the LLM path byte-identical below.
	if backend, ok := g.storedDecisionBackend(ctx, workspaceID); ok {
		verdict, probs, err := g.classifyDecision(ctx, workspaceID, backend, text)
		if err != nil {
			g.failOpen(ctx, workspaceID, agentID, "decision call", err)
			return IntentVerdict{}, err
		}
		g.logDecision(ctx, workspaceID, agentID, "decision", backend.model, verdict, probs, time.Since(start))
		return verdict, nil
	}

	m, err := g.resolver(ctx, workspaceID, agentID)
	if err != nil {
		g.failOpen(ctx, workspaceID, agentID, "resolve model", err)
		return IntentVerdict{}, err
	}
	ctx = sideCallContext(ctx, g.trace, "memory.intent_gate")

	raw, err := generateText(ctx, m, intentSystemPrompt, intentUserPrompt(text))
	if err != nil {
		g.failOpen(ctx, workspaceID, agentID, "model call", err)
		return IntentVerdict{}, err
	}
	verdict, err := parseIntent(raw)
	if err != nil {
		g.failOpen(ctx, workspaceID, agentID, "decode verdict", err)
		return IntentVerdict{}, err
	}
	g.logDecision(ctx, workspaceID, agentID, "llm", "", verdict, nil, time.Since(start))
	return verdict, nil
}

// logDecision logs one completed classification at info: the per-turn record
// that makes the gate's routing visible in server logs the way failOpen makes
// its failures visible — which backend classified the turn (decision or llm),
// the configured model, the routing verdict, and for the decision backend the
// raw noul probabilities behind it.
func (g *IntentGate) logDecision(ctx context.Context, workspaceID, agentID, source, model string, verdict IntentVerdict, probs map[string]float64, elapsed time.Duration) {
	attrs := []any{
		"workspace_id", workspaceID, "agent_id", agentID,
		"source", source, "model", model,
		"needs_memory", verdict.NeedsDeepMemory,
		"notes", verdict.Notes, "events", verdict.Events,
		"associative", verdict.Associative, "entity", verdict.Entity,
		"elapsed_ms", elapsed.Milliseconds(),
	}
	for _, q := range []string{decisionNeedsMemoryQuestion, decisionNotesQuestion, decisionEventsQuestion} {
		if p, ok := probs[q]; ok {
			attrs = append(attrs, "probability_"+q, p)
		}
	}
	g.log.InfoContext(ctx, "memory: intent gate classified turn", attrs...)
}

// failOpen logs one failed-open classification at warn: the turn proceeds
// without retrieval, so the log line is the only production-visible trace
// that memory was skipped — the level that makes a misconfigured side-call
// model (resolution failure, budget timeout, undecodable verdict) findable
// from server logs instead of DB forensics.
func (g *IntentGate) failOpen(ctx context.Context, workspaceID, agentID, stage string, err error) {
	g.log.WarnContext(ctx, "memory: intent gate failed open; turn proceeds without retrieval",
		"workspace_id", workspaceID, "agent_id", agentID, "stage", stage, "error", err)
}

// intentPayload is the strict JSON the side-call emits. Entity is the
// associative route's advisory seed (wave3 D8) — the entity the turn is
// about, spelled as the turn spells it.
type intentPayload struct {
	NeedsMemory bool     `json:"needs_memory"`
	Buckets     []string `json:"buckets"`
	Entity      string   `json:"entity"`
}

// parseIntent extracts the verdict from the model response: code fences and
// surrounding prose are tolerated, anything unparseable is an error the
// caller fail-opens on. A needs_memory verdict with no recognizable bucket
// routes both — the model saw the turn and asked for depth; narrowing is its
// job, and dropping the request over a bucket typo would silently starve
// retrieval.
func parseIntent(raw string) (IntentVerdict, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return IntentVerdict{}, errors.New("memory intent gate: no JSON object in response")
	}
	var payload intentPayload
	if err := json.Unmarshal([]byte(raw[start:end+1]), &payload); err != nil {
		return IntentVerdict{}, fmt.Errorf("memory intent gate: decode verdict: %w", err)
	}
	if !payload.NeedsMemory {
		return IntentVerdict{}, nil
	}
	verdict := IntentVerdict{NeedsDeepMemory: true}
	routed := false
	for _, b := range payload.Buckets {
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "notes":
			verdict.Notes = true
			routed = true
		case "events":
			verdict.Events = true
			routed = true
		case "associative", "entity":
			// The wave3 D8 associative route: entity-shaped queries draw
			// graph-traversal candidates into the same shared prefetch
			// budget. The model may spell the bucket either way; both route.
			verdict.Associative = true
			routed = true
		}
	}
	if !routed {
		verdict.Notes = true
		verdict.Events = true
	}
	verdict.Entity = strings.TrimSpace(payload.Entity)
	return verdict, nil
}

const intentSystemPrompt = `You are the intent gate for an AI agent workspace's memory. Given one user turn, decide whether answering it needs the workspace's extracted long-term memory (stored facts and episodic summaries beyond the documents already in your context) or whether the turn is self-contained.

Emit ONLY a JSON object — no prose:
{"needs_memory":true,"buckets":["notes","events"],"entity":null}

Rules:
- needs_memory is false for greetings, small talk, pure reasoning, coding, and anything answerable without workspace history.
- needs_memory is true when the turn references past discussions, decisions, people, projects, timelines, preferences, or anything previously said or done in this workspace.
- buckets lists which stores are relevant: "notes" for durable facts, "events" for what happened and when, "associative" when the turn is about one specific named entity (a project, person, system, or vendor) whose linked facts and events answer it. Omit nothing relevant; both notes and events when unsure.
- entity names that entity as the turn spells it, so its linked rows can be traversed; omit it (null) whenever buckets has no "associative".`

// intentUserPrompt renders the classification call's user turn: the raw turn
// text and nothing else — the gate must stay cheap.
func intentUserPrompt(text string) string {
	var sb strings.Builder
	sb.WriteString("## Turn\n")
	sb.WriteString(text)
	return sb.String()
}
