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
		resolver: newSideCallResolver(providerStore, encryptionKey, factory, cfg),
		trace:    cfg.trace,
		log:      log,
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
// self-contained with a nil error — there is nothing to classify.
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

	m, err := g.resolver(ctx, workspaceID, agentID)
	if err != nil {
		g.debugFail(ctx, "resolve model", err)
		return IntentVerdict{}, err
	}
	ctx = sideCallContext(ctx, g.trace, "memory.intent_gate")

	raw, err := generateText(ctx, m, intentSystemPrompt, intentUserPrompt(text))
	if err != nil {
		g.debugFail(ctx, "model call", err)
		return IntentVerdict{}, err
	}
	verdict, err := parseIntent(raw)
	if err != nil {
		g.debugFail(ctx, "decode verdict", err)
		return IntentVerdict{}, err
	}
	return verdict, nil
}

// debugFail logs one fail-open classification failure. The gate is an
// optimization: the log is the only observable trace of the skip (debug, so
// production logs stay quiet about turns that simply needed no memory).
func (g *IntentGate) debugFail(ctx context.Context, stage string, err error) {
	g.log.DebugContext(ctx, "memory: intent gate failed open",
		"stage", stage, "error", err)
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
