package mcp

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
)

// The MCP result envelope (mcp-result-unwrapping): the eino adapter
// (eino-ext components/tool/mcp) marshals the whole mcp.CallToolResult as a
// tool's output string, so the model and the transcript would pay for and
// render the JSON-RPC wrapper around every result. The unwrap happens once,
// at the connection seam (connect), so every downstream wrapper — the
// supervisor's in-flight gauge, the renaming pass, the attention and
// session-dir interceptors — and MCPManager.Invoke all see the payload text.

// unwrapResultEnvelope strips the MCP result envelope the adapter wraps every
// tool result in ({"content":[{"type":"text","text":...}]}): when the body is
// exactly that — one content part, of type "text" — the inner text is the
// tool payload, returned verbatim; it may itself be JSON, plain text, or
// anything else and is never parsed further. Any other shape — multiple
// parts, non-text parts (images), non-envelope JSON, not JSON at all — passes
// through unchanged, and so does an empty inner text (an envelope-shaped body
// without a text field is not a payload). Error results never reach here:
// both call sites surface result.IsError before unwrapping.
func unwrapResultEnvelope(out string) string {
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Content) != 1 {
		return out
	}
	part := env.Content[0]
	if part.Type != "text" || part.Text == "" {
		return out
	}
	return part.Text
}

// wrapResultUnwrapping wraps each tool so its invocation result passes
// through [unwrapResultEnvelope] once. The wrapper keeps the tool's narrowest
// interface — eino's ToolsNode and every buddy wrapper type-switch on
// InvokableTool/StreamableTool — so the adapter's InvokableTools stay
// InvokableTools; tools without an invokable surface (schema-only) pass
// through unchanged.
func wrapResultUnwrapping(tools []tool.BaseTool) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		if inv, ok := t.(tool.InvokableTool); ok {
			out = append(out, &unwrappedInvokableTool{InvokableTool: inv})
			continue
		}
		out = append(out, t)
	}
	return out
}

// unwrappedInvokableTool passes an invokable tool's output through the
// envelope unwrap. Info stays promoted: the model sees exactly the surface
// the server advertised. Errors pass through untouched — the wrap never
// swallows or rewrites a failed call.
type unwrappedInvokableTool struct {
	tool.InvokableTool
}

func (t *unwrappedInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	out, err := t.InvokableTool.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "", err
	}
	return unwrapResultEnvelope(out), nil
}
