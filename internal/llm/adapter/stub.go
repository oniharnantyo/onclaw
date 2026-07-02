package adapter

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// StubChatModel implements model.AgenticModel as a test stub.
type StubChatModel struct{}

func (s *StubChatModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{
				Text: "Stub response",
			}),
		},
	}, nil
}

func (s *StubChatModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	sr, sw := schema.Pipe[*schema.AgenticMessage](1)
	sw.Send(&schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{
				Text: "Stub streaming response",
			}),
		},
	}, nil)
	sw.Close()
	return sr, nil
}

type stubAdapter struct{}

func (s *stubAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	return &StubChatModel{}, nil
}

// NewStubAdapter returns a stub Adapter factory.
func NewStubAdapter() Adapter {
	return &stubAdapter{}
}
