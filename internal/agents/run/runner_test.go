package run

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestTurnLifecycle(t *testing.T) {
	turn := NewTurn("turn-1")
	assert.Equal(t, "turn-1", turn.TurnID())

	go func() {
		turn.Finish(nil)
	}()

	err := turn.Wait()
	require.NoError(t, err)
}

func TestStreamSubscription(t *testing.T) {
	sub := NewStreamSub()
	require.NotNil(t, sub)
	sub.Close()
}

func TestRunner_ExecuteTurn(t *testing.T) {
	ctx := t.Context()
	agent, err := adk.NewTypedChatModelAgent[*schema.AgenticMessage](ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:  "test-agent",
		Model: &fakeChatModel{reply: "test response"},
	})
	require.NoError(t, err)

	runner := NewRunner(RunnerDependencies{})
	turn := NewTurn("turn-1")
	rt := NewRunningTurn(turn, "session-1", ctx, func() {})

	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: "hi"}),
		},
	}
	err = runner.ExecuteTurn(ctx, rt, agent, []*schema.AgenticMessage{msg})
	require.NoError(t, err)
}
