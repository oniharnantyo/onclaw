package adapter

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// StubChatModel implements model.ToolCallingChatModel as a test stub.
type StubChatModel struct{}

func (s *StubChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return &schema.Message{
		Role:    schema.Assistant,
		Content: "Stub response",
	}, nil
}

func (s *StubChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(&schema.Message{
		Role:    schema.Assistant,
		Content: "Stub streaming response",
	}, nil)
	sw.Close()
	return sr, nil
}

func (s *StubChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

type stubAdapter struct{}

func (s *stubAdapter) Build(ctx context.Context, p *store.Profile, apiKey string) (model.ToolCallingChatModel, error) {
	return &StubChatModel{}, nil
}

// NewStubAdapter returns a stub Adapter factory.
func NewStubAdapter() Adapter {
	return &stubAdapter{}
}
