package compose

import (
	"context"
	"testing"

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

func TestComposeTurnAgent_RequiresConfigAndModel(t *testing.T) {
	ctx := t.Context()

	_, _, err := ComposeTurnAgent(ctx, nil)
	require.Error(t, err)

	_, _, err = ComposeTurnAgent(ctx, &ComposeConfig{Name: "test"})
	require.Error(t, err)

	_, _, err = ComposeTurnAgent(ctx, &ComposeConfig{
		Name:      "test",
		ChatModel: &fakeChatModel{reply: "OK"},
	})
	require.NoError(t, err)
}

func TestContextHelpers(t *testing.T) {
	assert.Equal(t, 0, UsedTokens(nil))
}

func TestResolvedTools_Names(t *testing.T) {
	r := &ResolvedTools{
		AgentDir: "/workspace",
	}
	names, err := r.Names(t.Context(), []string{ToolReadFile, ToolWriteFile, ToolTaskCreate})
	require.NoError(t, err)
	assert.Contains(t, names, ToolReadFile)
	assert.Contains(t, names, ToolWriteFile)
	assert.Contains(t, names, ToolTaskCreate)
}
