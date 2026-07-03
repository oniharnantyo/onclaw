package tools_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "my secret is sk-12345678901234567890",
			expected: "my secret is [REDACTED]",
		},
		{
			input:    "openai key: sk-proj-abcdefghijklmnopqrstuvwxyz12",
			expected: "openai key: [REDACTED]",
		},
		{
			input:    "nvidia key: nvapi-abcdefghijklmnopqrstuvwxyz12",
			expected: "nvidia key: [REDACTED]",
		},
		{
			input:    "normal text without secrets",
			expected: "normal text without secrets",
		},
	}

	for _, tc := range tests {
		actual := tools.Redact(tc.input)
		if actual != tc.expected {
			t.Errorf("Redact(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestRedactedToolDecorator(t *testing.T) {
	innerTool, err := utils.InferTool("test_tool", "a test tool that returns input",
		func(ctx context.Context, input *struct{ Text string }) (string, error) {
			return input.Text, nil
		})
	if err != nil {
		t.Fatalf("failed to infer tool: %v", err)
	}

	redacted := tools.WrapRedacted(innerTool)

	ctx := context.Background()
	output, err := redacted.InvokableRun(ctx, `{"Text": "my secret is sk-12345678901234567890"}`)
	if err != nil {
		t.Fatalf("failed to run redacted tool: %v", err)
	}

	expected := "my secret is [REDACTED]"
	if output != expected {
		t.Errorf("expected tool output %q, got %q", expected, output)
	}
}

func TestRedactAgenticMessage(t *testing.T) {
	// Nil message
	if tools.RedactAgenticMessage(nil) != nil {
		t.Error("expected nil for nil input")
	}

	// Complex message
	meta := &schema.AgenticResponseMeta{}
	msg := &schema.AgenticMessage{
		Role:         schema.AgenticRoleType("user"),
		ResponseMeta: meta,
		Extra:        map[string]any{"extra-key": "extra-val"},
		ContentBlocks: []*schema.ContentBlock{
			nil, // nil block handling
			{
				Type:          schema.ContentBlockType("text_input"),
				UserInputText: &schema.UserInputText{Text: "my secret is sk-12345678901234567890"},
				Extra:         map[string]any{"block-extra": 123},
			},
			{
				Type:             schema.ContentBlockType("text_output"),
				AssistantGenText: &schema.AssistantGenText{Text: "key: nvapi-abcdefghijklmnopqrstuvwxyz12"},
			},
			{
				Type:      schema.ContentBlockType("reasoning"),
				Reasoning: &schema.Reasoning{Text: "thinking with sk-12345678901234567890"},
			},
			{
				Type:             schema.ContentBlockType("tool_call"),
				FunctionToolCall: &schema.FunctionToolCall{Arguments: `{"key": "sk-12345678901234567890"}`},
			},
			{
				Type: schema.ContentBlockType("tool_result"),
				FunctionToolResult: &schema.FunctionToolResult{
					Content: []*schema.FunctionToolResultContentBlock{
						nil, // nil inner block
						{
							Type:  schema.FunctionToolResultContentBlockTypeText,
							Text:  &schema.UserInputText{Text: "output with sk-12345678901234567890"},
							Extra: map[string]any{"inner-extra": "yes"},
						},
						{
							Type: schema.FunctionToolResultContentBlockTypeImage,
						},
					},
				},
			},
			{
				Type:           schema.ContentBlockType("text_input"),
				UserInputImage: &schema.UserInputImage{},
				UserInputAudio: &schema.UserInputAudio{},
				UserInputVideo: &schema.UserInputVideo{},
				UserInputFile:  &schema.UserInputFile{},
			},
			{
				Type:              schema.ContentBlockType("text_output"),
				AssistantGenImage: &schema.AssistantGenImage{},
				AssistantGenAudio: &schema.AssistantGenAudio{},
				AssistantGenVideo: &schema.AssistantGenVideo{},
			},
			{
				Type:           schema.ContentBlockType("tool_call"),
				ServerToolCall: &schema.ServerToolCall{},
			},
			{
				Type:             schema.ContentBlockType("tool_result"),
				ServerToolResult: &schema.ServerToolResult{},
			},
			{
				Type:        schema.ContentBlockType("tool_call"),
				MCPToolCall: &schema.MCPToolCall{},
			},
			{
				Type:          schema.ContentBlockType("tool_result"),
				MCPToolResult: &schema.MCPToolResult{},
			},
			{
				Type:               schema.ContentBlockType("tool_result"),
				MCPListToolsResult: &schema.MCPListToolsResult{},
			},
			{
				Type:                   schema.ContentBlockType("tool_call"),
				MCPToolApprovalRequest: &schema.MCPToolApprovalRequest{},
			},
			{
				Type:                    schema.ContentBlockType("tool_call"),
				MCPToolApprovalResponse: &schema.MCPToolApprovalResponse{},
			},
			{
				Type:          schema.ContentBlockType("text_output"),
				StreamingMeta: &schema.StreamingMeta{},
			},
		},
	}

	redacted := tools.RedactAgenticMessage(msg)
	if redacted == nil {
		t.Fatal("expected non-nil redacted message")
	}

	if redacted.Role != msg.Role {
		t.Errorf("expected role %v, got %v", msg.Role, redacted.Role)
	}

	if redacted.Extra["extra-key"] != "extra-val" {
		t.Errorf("expected Extra key to be preserved")
	}

	if redacted.ResponseMeta != msg.ResponseMeta {
		t.Errorf("expected ResponseMeta to be preserved")
	}

	blocks := redacted.ContentBlocks
	if len(blocks) != len(msg.ContentBlocks) {
		t.Fatalf("expected %d blocks, got %d", len(msg.ContentBlocks), len(blocks))
	}

	if blocks[0] != nil {
		t.Error("expected nil block to be handled and remain nil")
	}

	// Check redacted fields
	if blocks[1].UserInputText.Text != "my secret is [REDACTED]" {
		t.Errorf("UserInputText not redacted: %q", blocks[1].UserInputText.Text)
	}
	if blocks[1].Extra["block-extra"] != 123 {
		t.Error("block Extra not preserved")
	}

	if blocks[2].AssistantGenText.Text != "key: [REDACTED]" {
		t.Errorf("AssistantGenText not redacted: %q", blocks[2].AssistantGenText.Text)
	}

	if blocks[3].Reasoning.Text != "thinking with [REDACTED]" {
		t.Errorf("Reasoning not redacted: %q", blocks[3].Reasoning.Text)
	}

	if blocks[4].FunctionToolCall.Arguments != `{"key": "[REDACTED]"}` {
		t.Errorf("FunctionToolCall not redacted: %q", blocks[4].FunctionToolCall.Arguments)
	}

	resBlock := blocks[5].FunctionToolResult
	if len(resBlock.Content) != 3 {
		t.Fatalf("expected 3 content parts, got %d", len(resBlock.Content))
	}
	if resBlock.Content[0] != nil {
		t.Error("expected nil inner block to remain nil")
	}
	if resBlock.Content[1].Text.Text != "output with [REDACTED]" {
		t.Errorf("inner text block not redacted: %q", resBlock.Content[1].Text.Text)
	}
	if resBlock.Content[1].Extra["inner-extra"] != "yes" {
		t.Error("inner block Extra not preserved")
	}

	// Verify passthroughs did not cause panic or lose values
	if blocks[6].UserInputImage == nil || blocks[6].UserInputAudio == nil || blocks[6].UserInputVideo == nil || blocks[6].UserInputFile == nil {
		t.Error("userInput passthroughs failed")
	}
	if blocks[7].AssistantGenImage == nil || blocks[7].AssistantGenAudio == nil || blocks[7].AssistantGenVideo == nil {
		t.Error("assistantGen passthroughs failed")
	}
	if blocks[8].ServerToolCall == nil || blocks[9].ServerToolResult == nil {
		t.Error("serverTool passthroughs failed")
	}
	if blocks[10].MCPToolCall == nil || blocks[11].MCPToolResult == nil || blocks[12].MCPListToolsResult == nil {
		t.Error("mcpTool passthroughs failed")
	}
	if blocks[13].MCPToolApprovalRequest == nil || blocks[14].MCPToolApprovalResponse == nil {
		t.Error("mcpApproval passthroughs failed")
	}
	if blocks[15].StreamingMeta == nil {
		t.Error("streamingMeta passthrough failed")
	}
}
