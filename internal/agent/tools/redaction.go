package tools

import (
	"context"
	"regexp"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[a-zA-Z0-9_-]{20,}`),
	regexp.MustCompile(`(?i)nvapi-[a-zA-Z0-9_-]{20,}`),
}

// Redact replaces any secret patterns in the input string with [REDACTED].
func Redact(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// RedactedTool wraps an eino InvokableTool and redacts input arguments and output results.
type RedactedTool struct {
	tool.InvokableTool
}

// InvokableRun executes the underlying tool with redacted inputs and outputs.
func (r *RedactedTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	maskedInput := Redact(input)
	output, err := r.InvokableTool.InvokableRun(ctx, maskedInput, opts...)
	if err != nil {
		return "", err
	}
	return Redact(output), nil
}

// WrapRedacted wraps a tool.InvokableTool into a RedactedTool.
func WrapRedacted(t tool.InvokableTool) tool.InvokableTool {
	return &RedactedTool{t}
}

// RedactAgenticMessage walks the content blocks of the message and redacts any secrets in text fields.
func RedactAgenticMessage(msg *schema.AgenticMessage) *schema.AgenticMessage {
	if msg == nil {
		return nil
	}
	res := &schema.AgenticMessage{
		Role: msg.Role,
	}
	if msg.Extra != nil {
		res.Extra = make(map[string]any)
		for k, v := range msg.Extra {
			res.Extra[k] = v
		}
	}
	if msg.ResponseMeta != nil {
		res.ResponseMeta = msg.ResponseMeta
	}
	if len(msg.ContentBlocks) > 0 {
		res.ContentBlocks = make([]*schema.ContentBlock, len(msg.ContentBlocks))
		for i, block := range msg.ContentBlocks {
			res.ContentBlocks[i] = redactContentBlock(block)
		}
	}
	return res
}

func redactContentBlock(b *schema.ContentBlock) *schema.ContentBlock {
	if b == nil {
		return nil
	}
	res := &schema.ContentBlock{
		Type: b.Type,
	}
	if b.Extra != nil {
		res.Extra = make(map[string]any)
		for k, v := range b.Extra {
			res.Extra[k] = v
		}
	}
	if b.UserInputText != nil {
		res.UserInputText = &schema.UserInputText{
			Text: Redact(b.UserInputText.Text),
		}
	}
	if b.AssistantGenText != nil {
		res.AssistantGenText = &schema.AssistantGenText{
			Text:            Redact(b.AssistantGenText.Text),
			OpenAIExtension: b.AssistantGenText.OpenAIExtension,
			ClaudeExtension: b.AssistantGenText.ClaudeExtension,
			Extension:       b.AssistantGenText.Extension,
		}
	}
	if b.Reasoning != nil {
		res.Reasoning = &schema.Reasoning{
			Text:            Redact(b.Reasoning.Text),
			Signature:       b.Reasoning.Signature,
			OpenAIExtension: b.Reasoning.OpenAIExtension,
		}
	}
	if b.FunctionToolCall != nil {
		res.FunctionToolCall = &schema.FunctionToolCall{
			CallID:    b.FunctionToolCall.CallID,
			Name:      b.FunctionToolCall.Name,
			Arguments: Redact(b.FunctionToolCall.Arguments),
		}
	}
	if b.FunctionToolResult != nil {
		res.FunctionToolResult = &schema.FunctionToolResult{
			CallID: b.FunctionToolResult.CallID,
			Name:   b.FunctionToolResult.Name,
		}
		if len(b.FunctionToolResult.Content) > 0 {
			res.FunctionToolResult.Content = make([]*schema.FunctionToolResultContentBlock, len(b.FunctionToolResult.Content))
			for i, cb := range b.FunctionToolResult.Content {
				res.FunctionToolResult.Content[i] = redactToolResultContentBlock(cb)
			}
		}
	}
	if b.UserInputImage != nil {
		res.UserInputImage = b.UserInputImage
	}
	if b.UserInputAudio != nil {
		res.UserInputAudio = b.UserInputAudio
	}
	if b.UserInputVideo != nil {
		res.UserInputVideo = b.UserInputVideo
	}
	if b.UserInputFile != nil {
		res.UserInputFile = b.UserInputFile
	}
	if b.AssistantGenImage != nil {
		res.AssistantGenImage = b.AssistantGenImage
	}
	if b.AssistantGenAudio != nil {
		res.AssistantGenAudio = b.AssistantGenAudio
	}
	if b.AssistantGenVideo != nil {
		res.AssistantGenVideo = b.AssistantGenVideo
	}
	if b.ServerToolCall != nil {
		res.ServerToolCall = b.ServerToolCall
	}
	if b.ServerToolResult != nil {
		res.ServerToolResult = b.ServerToolResult
	}
	if b.MCPToolCall != nil {
		res.MCPToolCall = b.MCPToolCall
	}
	if b.MCPToolResult != nil {
		res.MCPToolResult = b.MCPToolResult
	}
	if b.MCPListToolsResult != nil {
		res.MCPListToolsResult = b.MCPListToolsResult
	}
	if b.MCPToolApprovalRequest != nil {
		res.MCPToolApprovalRequest = b.MCPToolApprovalRequest
	}
	if b.MCPToolApprovalResponse != nil {
		res.MCPToolApprovalResponse = b.MCPToolApprovalResponse
	}
	if b.StreamingMeta != nil {
		res.StreamingMeta = b.StreamingMeta
	}
	return res
}

func redactToolResultContentBlock(cb *schema.FunctionToolResultContentBlock) *schema.FunctionToolResultContentBlock {
	if cb == nil {
		return nil
	}
	res := &schema.FunctionToolResultContentBlock{
		Type:  cb.Type,
		Image: cb.Image,
		Audio: cb.Audio,
		Video: cb.Video,
		File:  cb.File,
	}
	if cb.Extra != nil {
		res.Extra = make(map[string]any)
		for k, v := range cb.Extra {
			res.Extra[k] = v
		}
	}
	if cb.Text != nil {
		res.Text = &schema.UserInputText{
			Text: Redact(cb.Text.Text),
		}
	}
	return res
}
