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
	model.BaseChatModel
	reply string
}

func (m *fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.reply, nil), nil
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
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:  "test-agent",
		Model: &fakeChatModel{reply: "test response"},
	})
	require.NoError(t, err)

	runner := NewRunner(RunnerDependencies{})
	turn := NewTurn("turn-1")
	rt := NewRunningTurn(turn, "session-1", ctx, func() {})

	err = runner.ExecuteTurn(ctx, rt, agent, []*schema.Message{schema.UserMessage("hi")})
	require.NoError(t, err)
}
