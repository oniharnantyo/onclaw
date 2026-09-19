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

// intentTimeout is the HARD classification budget (design parameter pin:
// gate timeout 1.5s). It is enforced with a context deadline inside Classify
// so a slow side-call model is abandoned at the pin and can never delay
// instruction composition beyond it.
const intentTimeout = 1500 * time.Millisecond

// IntentVerdict is the gate's routing decision: whether the turn needs deep
// memory at all and, when it does, which buckets are relevant (curated notes,
// episodic events, or both). The zero value is the self-contained verdict —
// no retrieval.
type IntentVerdict struct {
	NeedsDeepMemory bool
	Notes           bool
	Events          bool
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
// a parameter (the only deviation from a text-only signature).
//
// Model-side failures (resolution, generation, timeout, undecodable output)
// return the self-contained verdict with the error; the caller fail-opens by
// proceeding without retrieval. An empty or whitespace turn is quietly
// self-contained with a nil error — there is nothing to classify.
func (g *IntentGate) Classify(ctx context.Context, workspaceID, agentID, turnText string) (IntentVerdict, error) {
	text := strings.TrimSpace(turnText)
	if text == "" {
		return IntentVerdict{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, intentTimeout)
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

// intentPayload is the strict JSON the side-call emits.
type intentPayload struct {
	NeedsMemory bool     `json:"needs_memory"`
	Buckets     []string `json:"buckets"`
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
		}
	}
	if !routed {
		verdict.Notes = true
		verdict.Events = true
	}
	return verdict, nil
}

const intentSystemPrompt = `You are the intent gate for an AI agent workspace's memory. Given one user turn, decide whether answering it needs the workspace's extracted long-term memory (stored facts and episodic summaries beyond the documents already in your context) or whether the turn is self-contained.

Emit ONLY a JSON object — no prose:
{"needs_memory":true,"buckets":["notes","events"]}

Rules:
- needs_memory is false for greetings, small talk, pure reasoning, coding, and anything answerable without workspace history.
- needs_memory is true when the turn references past discussions, decisions, people, projects, timelines, preferences, or anything previously said or done in this workspace.
- buckets lists which stores are relevant: "notes" for durable facts, "events" for what happened and when. Omit nothing relevant; both when unsure.`

// intentUserPrompt renders the classification call's user turn: the raw turn
// text and nothing else — the gate must stay cheap.
func intentUserPrompt(text string) string {
	var sb strings.Builder
	sb.WriteString("## Turn\n")
	sb.WriteString(text)
	return sb.String()
}
