package render_test

import (
	"bytes"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/render"
)

func TestTextRenderer(t *testing.T) {
	var buf bytes.Buffer
	tr := render.Text(&buf)

	// 1. Render assistant text block
	msg1 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{
				Text: "Hello, ",
			}),
		},
	}
	if err := tr.Render(msg1); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	msg2 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{
				Text: "world!",
			}),
		},
	}
	if err := tr.Render(msg2); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if buf.String() != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", buf.String())
	}

	// 2. Render tool calls streaming
	buf.Reset()
	tcMsg1 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeFunctionToolCall,
				FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-1",
					Name:      "search_web",
					Arguments: `{"qu`,
				},
				StreamingMeta: &schema.StreamingMeta{
					Index: 0,
				},
			},
		},
	}
	if err := tr.Render(tcMsg1); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	tcMsg2 := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeFunctionToolCall,
				FunctionToolCall: &schema.FunctionToolCall{
					CallID:    "call-1",
					Name:      "search_web",
					Arguments: `ery": "go"}`,
				},
				StreamingMeta: &schema.StreamingMeta{
					Index: 0,
				},
			},
		},
	}
	if err := tr.Render(tcMsg2); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Flush to ensure the tool call output is printed
	if err := tr.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	expectedToolCall := "\n[Tool Call] Calling tool \"search_web\" with arguments: {\"query\": \"go\"}\n"
	if buf.String() != expectedToolCall {
		t.Errorf("expected tool call output:\n%q\ngot:\n%q", expectedToolCall, buf.String())
	}

	// 3. Render tool result
	buf.Reset()
	trMsg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			{
				Type: schema.ContentBlockTypeFunctionToolResult,
				FunctionToolResult: &schema.FunctionToolResult{
					CallID: "call-1",
					Name:   "search_web",
					Content: []*schema.FunctionToolResultContentBlock{
						{
							Type: schema.FunctionToolResultContentBlockTypeText,
							Text: &schema.UserInputText{
								Text: "Found Go programming language",
							},
						},
					},
				},
			},
		},
	}
	if err := tr.Render(trMsg); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	expectedResult := "\n[Tool Result] Found Go programming language\n"
	if buf.String() != expectedResult {
		t.Errorf("expected tool result output:\n%q\ngot:\n%q", expectedResult, buf.String())
	}
}
