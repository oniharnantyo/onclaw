package memory

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// intentModel is a scripted intent side-call model: fixed response, records
// the received prompt texts, and honors the context deadline (a real
// provider's HTTP call does too) so the per-call budget is testable.
type intentModel struct {
	response string
	err      error
	delay    time.Duration

	prompts []string
}

func (m *intentModel) Generate(ctx context.Context, input []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	var sb strings.Builder
	for _, msg := range input {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			if block.AssistantGenText != nil {
				sb.WriteString(block.AssistantGenText.Text)
				sb.WriteByte('\n')
			}
			if block.UserInputText != nil {
				sb.WriteString(block.UserInputText.Text)
				sb.WriteByte('\n')
			}
		}
	}
	m.prompts = append(m.prompts, sb.String())
	if m.err != nil {
		return nil, m.err
	}
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{{AssistantGenText: &schema.AssistantGenText{Text: m.response}}},
	}, nil
}

func (m *intentModel) Stream(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, errors.New("intentModel: stream not supported")
}

func newTestIntentGate(m Model) *IntentGate {
	return NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), WithModelResolver(staticResolver(m)))
}

func TestIntentGate_HitRoutesBuckets(t *testing.T) {
	m := &intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide about the vendor renewal?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected notes-routed hit, got %+v", verdict)
	}
	if len(m.prompts) != 1 || !strings.Contains(m.prompts[0], "vendor renewal") {
		t.Fatalf("the turn text must reach the side-call prompt: %q", m.prompts)
	}
}

func TestIntentGate_BothBucketsAndFences(t *testing.T) {
	m := &intentModel{response: "Sure!\n```json\n{\"needs_memory\": true, \"buckets\": [\"notes\", \"events\"]}\n```"}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "Recap last week's incident and what we changed since.", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || !verdict.Events {
		t.Fatalf("expected both buckets, got %+v", verdict)
	}
}

func TestIntentGate_SelfContainedQuiet(t *testing.T) {
	m := &intentModel{response: `{"needs_memory":false,"buckets":[]}`}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "Write me a haiku about deploy pipelines.", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("expected the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_EmptyTextSkipsModel: nothing to classify is quietly
// self-contained and never spends the side-call.
func TestIntentGate_EmptyTextSkipsModel(t *testing.T) {
	m := &intentModel{response: `{}`}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "   ", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if verdict.NeedsDeepMemory || len(m.prompts) != 0 {
		t.Fatalf("empty turn must skip the model, got verdict %+v and %d calls", verdict, len(m.prompts))
	}
}

// TestIntentGate_ModelErrorFailsOpen (spec: gate failure fails open): a dead
// side-call model classifies as self-contained with the error surfaced.
func TestIntentGate_ModelErrorFailsOpen(t *testing.T) {
	m := &intentModel{err: errors.New("provider down")}
	gate := newTestIntentGate(m)

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide about the vendor renewal?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the model error to surface")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_UndecodableFailsOpen: prose without a verdict JSON object
// fails open.
func TestIntentGate_UndecodableFailsOpen(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: "I think it needs memory, probably."})

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the undecodable-output error")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_HardTimeout pins the hard-deadline contract: the caller's
// budget is enforced with a context deadline, so a model that honors its
// context is abandoned at it and the gate fails open. If Classify skipped the
// deadline the slow model would answer and the test would see a verdict
// instead of the deadline error.
func TestIntentGate_HardTimeout(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`, delay: 3 * time.Second})

	start := time.Now()
	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 100*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the context deadline error, got %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if elapsed > time.Second {
		t.Fatalf("the gate must abort at the budget, took %v", elapsed)
	}
}

// TestIntentGate_MissingBucketsRoutesBoth: a needs_memory verdict whose
// bucket list is unreadable routes both buckets — a bucket typo must never
// starve the retrieval the model asked for.
func TestIntentGate_MissingBucketsRoutesBoth(t *testing.T) {
	gate := newTestIntentGate(&intentModel{response: `{"needs_memory":true,"buckets":["everything"]}`})

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || !verdict.Events {
		t.Fatalf("expected the both-buckets fallback, got %+v", verdict)
	}
}

// TestIntentGate_ResolverFailureFailsOpen covers the unwired side-call tier:
// a resolution failure is a fail-open classification, not a panic.
func TestIntentGate_ResolverFailureFailsOpen(t *testing.T) {
	gate := NewIntentGate(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), WithModelResolver(failingResolver("no cheap tier")))

	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err == nil {
		t.Fatal("expected the resolver error")
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
}

// TestIntentGate_HonorsConfiguredBudget (fix-memory-retrieval-lane D1/D2,
// tasks 1.4): the classification budget is the caller's per-turn value, not a
// package pin. The same ~2s side-call is abandoned under a 100ms budget
// (fail-open with the deadline error) and answers under the 4s default-sized
// budget.
func TestIntentGate_HonorsConfiguredBudget(t *testing.T) {
	m := &intentModel{response: `{"needs_memory":true,"buckets":["notes"]}`, delay: 2 * time.Second}
	gate := newTestIntentGate(m)

	start := time.Now()
	verdict, err := gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 100*time.Millisecond)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the deadline error under the 100ms budget, got %v", err)
	}
	if verdict.NeedsDeepMemory {
		t.Fatalf("fail-open must return the self-contained verdict, got %+v", verdict)
	}
	if elapsed >= time.Second {
		t.Fatalf("the 100ms budget must abort early, took %v", elapsed)
	}

	verdict, err = gate.Classify(context.Background(), "ws-1", "", "What did we decide?", 4*time.Second)
	if err != nil {
		t.Fatalf("Classify under the 4s budget: %v", err)
	}
	if !verdict.NeedsDeepMemory || !verdict.Notes || verdict.Events {
		t.Fatalf("expected the notes-routed verdict under the 4s budget, got %+v", verdict)
	}
}
