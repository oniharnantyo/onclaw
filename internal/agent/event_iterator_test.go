package agent_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent"
)

func summarizeActionEvent(ca *summarization.TypedCustomizedAction[*schema.AgenticMessage]) *adk.TypedAgentEvent[*schema.AgenticMessage] {
	return &adk.TypedAgentEvent[*schema.AgenticMessage]{
		Action: &adk.AgentAction{CustomizedAction: ca},
	}
}

// TestEventIterator_CompactionProgress asserts the iterator parses the
// summarization middleware's CustomizedAction events into CompactionSignals
// (before -> started+10%, generate -> 60%, after -> completed+100%) in order,
// and still surfaces real messages afterward.
func TestEventIterator_CompactionProgress(t *testing.T) {
	ctx := context.Background()
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	it := agent.NewEventIterator(ctx, iter, nil, nil, nil)

	finalMsg := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}

	gen.Send(summarizeActionEvent(&summarization.TypedCustomizedAction[*schema.AgenticMessage]{
		Type: summarization.ActionTypeBeforeSummarize,
	}))
	gen.Send(summarizeActionEvent(&summarization.TypedCustomizedAction[*schema.AgenticMessage]{
		Type: summarization.ActionTypeGenerateSummary,
	}))
	gen.Send(summarizeActionEvent(&summarization.TypedCustomizedAction[*schema.AgenticMessage]{
		Type: summarization.ActionTypeAfterSummarize,
	}))
	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
			MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
				Message: finalMsg,
			},
		},
	})
	gen.Close()

	var got []agent.Event
	for {
		ev, ok := it.Next()
		if !ok {
			break
		}
		got = append(got, ev)
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator errored: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(got), got)
	}

	if c := got[0].Compaction; c == nil || c.Status != agent.CompactionStarted || c.Progress != 10 {
		t.Errorf("event[0] = %+v, want started/10", got[0].Compaction)
	}
	if c := got[1].Compaction; c == nil || c.Status != "" || c.Progress != 60 {
		t.Errorf("event[1] = %+v, want progress-only 60", got[1].Compaction)
	}
	if c := got[2].Compaction; c == nil || c.Status != agent.CompactionCompleted || c.Progress != 100 {
		t.Errorf("event[2] = %+v, want completed/100", got[2].Compaction)
	}
	if got[3].Message != finalMsg {
		t.Errorf("event[3] = %+v, want the trailing assistant message", got[3])
	}
}

// TestEventIterator_IgnoresUnknownCustomizedAction ensures a non-summarization
// customized action is skipped — no compaction signal, no message, no panic.
func TestEventIterator_IgnoresUnknownCustomizedAction(t *testing.T) {
	ctx := context.Background()
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	it := agent.NewEventIterator(ctx, iter, nil, nil, nil)

	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		Action: &adk.AgentAction{CustomizedAction: "some-other-action"},
	})
	gen.Close()

	var n int
	for {
		ev, ok := it.Next()
		if !ok {
			break
		}
		if ev.Compaction != nil || ev.Message != nil {
			t.Errorf("unexpected non-empty event: %+v", ev)
		}
		n++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator errored: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 emitted events for unknown action, got %d", n)
	}
}

// TestEventIterator_AccumulatesStreamingReasoning verifies that reasoning blocks
// streamed across multiple chunks are concatenated into a single reasoning block,
// preserved through CollectedTurn(), and not lost during stream assembly.
func TestEventIterator_AccumulatesStreamingReasoning(t *testing.T) {
	ctx := context.Background()
	iter, gen := adk.NewAsyncIteratorPair[*adk.TypedAgentEvent[*schema.AgenticMessage]]()
	it := agent.NewEventIterator(ctx, iter, nil, nil, nil)

	// Construct a stream of three chunks: two reasoning deltas, then final answer
	chunk1 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeReasoning,
				Reasoning: &schema.Reasoning{
					Text: "Think ",
				},
			},
		},
	}
	chunk2 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeReasoning,
				Reasoning: &schema.Reasoning{
					Text: "step by step",
				},
			},
		},
	}
	chunk3 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type:             schema.ContentBlockTypeAssistantGenText,
				AssistantGenText: &schema.AssistantGenText{Text: "Answer"},
			},
		},
	}

	// Create a streaming reader from the chunks
	stream := schema.StreamReaderFromArray([]*schema.AgenticMessage{chunk1, chunk2, chunk3})

	// Send the streaming event
	gen.Send(&adk.TypedAgentEvent[*schema.AgenticMessage]{
		Output: &adk.TypedAgentOutput[*schema.AgenticMessage]{
			MessageOutput: &adk.TypedMessageVariant[*schema.AgenticMessage]{
				IsStreaming:  true,
				MessageStream: stream,
			},
		},
	})
	gen.Close()

	// Drive the iterator to completion
	for {
		_, ok := it.Next()
		if !ok {
			break
		}
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator errored: %v", err)
	}

	// Verify CollectedTurn returns the accumulated message
	collected := it.CollectedTurn()
	if len(collected) != 1 {
		t.Fatalf("expected 1 collected message, got %d", len(collected))
	}
	msg := collected[0]

	// Should have 2 blocks: reasoning (concatenated) + assistantGenText
	if len(msg.ContentBlocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.ContentBlocks))
	}

	// First block: reasoning with concatenated text
	reasoningBlock := msg.ContentBlocks[0]
	if reasoningBlock.Type != schema.ContentBlockTypeReasoning {
		t.Errorf("expected first block type reasoning, got %v", reasoningBlock.Type)
	}
	if reasoningBlock.Reasoning == nil {
		t.Fatal("expected non-nil Reasoning field")
	}
	expectedText := "Think step by step"
	if reasoningBlock.Reasoning.Text != expectedText {
		t.Errorf("expected reasoning text %q, got %q", expectedText, reasoningBlock.Reasoning.Text)
	}

	// Second block: assistantGenText
	answerBlock := msg.ContentBlocks[1]
	if answerBlock.Type != schema.ContentBlockTypeAssistantGenText {
		t.Errorf("expected second block type assistant_gen_text, got %v", answerBlock.Type)
	}
	if answerBlock.AssistantGenText == nil {
		t.Fatal("expected non-nil AssistantGenText field")
	}
	if answerBlock.AssistantGenText.Text != "Answer" {
		t.Errorf("expected answer text %q, got %q", "Answer", answerBlock.AssistantGenText.Text)
	}
}
