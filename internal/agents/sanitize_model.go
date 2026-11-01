package agents

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// sanitizeModel is a buddies-owned decorator around the turn's chat model
// (design D2 of fix-truncated-toolcall-request). A provider-side output
// truncation mid-tool-call leaves a partial arguments fragment in the
// conversation history; vendored connectors splice that fragment into the
// next request as raw JSON, making the whole request body malformed and the
// turn permanently dead on a provider 400. This decorator sanitizes the
// outbound view before delegating: any history tool call whose arguments are
// non-empty but not valid JSON is rewritten to "{}" on a copied block. ADK
// state keeps the original fragment — only what the model sees is repaired,
// and the tool-level error result already in history tells the model what
// happened.
type sanitizeModel struct {
	base model.BaseModel[*schema.AgenticMessage]
}

func newSanitizeModel(base model.BaseModel[*schema.AgenticMessage]) *sanitizeModel {
	return &sanitizeModel{base: base}
}

func (m *sanitizeModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return m.base.Generate(ctx, sanitizeToolCallArguments(input), opts...)
}

func (m *sanitizeModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return m.base.Stream(ctx, sanitizeToolCallArguments(input), opts...)
}

// sanitizeToolCallArguments returns the input messages with every
// FunctionToolCall block carrying non-empty, unparseable arguments rewritten
// to "{}" — copy-on-write: messages and blocks that are fine stay
// pointer-identical, the input slices are never mutated in place. Empty
// arguments stay untouched (the connector already emits {} for them).
func sanitizeToolCallArguments(input []*schema.AgenticMessage) []*schema.AgenticMessage {
	out := input
	copied := false
	for i, msg := range input {
		var sanitized bool
		blocks := make([]*schema.ContentBlock, len(msg.ContentBlocks))
		for j, block := range msg.ContentBlocks {
			if block != nil && block.Type == schema.ContentBlockTypeFunctionToolCall &&
				block.FunctionToolCall != nil && block.FunctionToolCall.Arguments != "" &&
				!json.Valid([]byte(block.FunctionToolCall.Arguments)) {
				call := *block.FunctionToolCall
				call.Arguments = "{}"
				newBlock := *block
				newBlock.FunctionToolCall = &call
				blocks[j] = &newBlock
				sanitized = true
				continue
			}
			blocks[j] = block
		}
		if !sanitized {
			continue
		}
		if !copied {
			out = make([]*schema.AgenticMessage, len(input))
			copy(out, input)
			copied = true
		}
		newMsg := *msg
		newMsg.ContentBlocks = blocks
		out[i] = &newMsg
	}
	return out
}
