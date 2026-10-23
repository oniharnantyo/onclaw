package compaction

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

type fakeChatModel struct {
	model.BaseModel[*schema.AgenticMessage]
	reply string
}

func (m *fakeChatModel) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: m.reply}),
		},
	}, nil
}

func TestCompactionNeeded(t *testing.T) {
	assert.False(t, CompactionNeeded(0, 200000))
	assert.False(t, CompactionNeeded(159999, 200000))
	assert.True(t, CompactionNeeded(160000, 200000))
	assert.True(t, CompactionNeeded(180000, 200000))
	assert.False(t, CompactionNeeded(100, 0))
}

func TestCompactableUpToAndHasCompactableView(t *testing.T) {
	events := []sessions.Event{
		{Seq: 1, Kind: sessions.KindUserMessage, Text: "hello"},
		{Seq: 2, Kind: sessions.KindAssistantMessage, Text: "hi"},
		{Seq: 3, Kind: sessions.KindTurnCompleted},
	}

	assert.Equal(t, int64(3), CompactableUpTo(events))
	assert.True(t, HasCompactableView(events, 3))
	assert.False(t, HasCompactableView(events, 0))
}

func TestRenderTranscript(t *testing.T) {
	events := []sessions.Event{
		{Seq: 1, Kind: sessions.KindUserMessage, Text: "first question"},
		{Seq: 2, Kind: sessions.KindAssistantMessage, Text: "first answer"},
	}

	rendered := RenderTranscript(events, 2)
	assert.Contains(t, rendered, "User: first question")
	assert.Contains(t, rendered, "Assistant: first answer")
}

func TestRunCompaction(t *testing.T) {
	ctx := t.Context()
	fs := fake.New()
	svc := sessions.NewService(fs)
	sess, err := svc.CreateSession(ctx, "agent-1", "Test")
	require.NoError(t, err)

	events := []sessions.Event{
		{Seq: 1, Kind: sessions.KindUserMessage, Text: "hello"},
		{Seq: 2, Kind: sessions.KindAssistantMessage, Text: "hi"},
		{Seq: 3, Kind: sessions.KindTurnCompleted, Usage: &sessions.Usage{FinalInputTokens: 100, OutputTokens: 20}},
	}
	require.NoError(t, svc.Append(ctx, sess.ID, events))

	mdl := &fakeChatModel{reply: "A condensed summary of the conversation."}
	marker, err := RunCompaction(ctx, sess.ID, "turn-1", mdl, events, 3, svc)
	require.NoError(t, err)
	assert.Equal(t, sessions.KindCompaction, marker.Kind)
	assert.Equal(t, "A condensed summary of the conversation.", marker.Compaction.Summary)

	cp, ok, err := svc.Checkpoint(ctx, sess.ID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "A condensed summary of the conversation.", cp.Summary)
	assert.Equal(t, int64(3), cp.UpToSeq)
}
