package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// The connection-seam envelope unwrap (mcp-result-unwrapping): the eino
// adapter marshals the whole MCP result envelope as a tool's output, so the
// model and the transcript would see the JSON-RPC wrapper around every
// result. The unwrap happens once in connect, before any other wrapper, and
// MCPManager.Invoke unwraps its own marshal so both paths agree.

func TestUnwrapResultEnvelope(t *testing.T) {
	t.Run("single text part yields the inner text verbatim", func(t *testing.T) {
		inner := `{"url":"https://example.com","title":"LinkAja | LinkedIn"}`
		out := unwrapResultEnvelope(`{"content":[{"type":"text","text":` + quoteJSON(t, inner) + `}]}`)
		require.Equal(t, inner, out)
	})
	t.Run("plain-text inner text passes through verbatim", func(t *testing.T) {
		out := unwrapResultEnvelope(`{"content":[{"type":"text","text":"just words"}]}`)
		require.Equal(t, "just words", out)
	})
	t.Run("inner text is never re-parsed", func(t *testing.T) {
		// A payload that itself looks like an envelope stays byte-identical:
		// the unwrap is exactly one level, so the parent's own second unwrap
		// (attention's invokeChildTool) cannot over-strip.
		inner := `{"content":[{"type":"text","text":"nested"}]}`
		out := unwrapResultEnvelope(`{"content":[{"type":"text","text":` + quoteJSON(t, inner) + `}]}`)
		require.Equal(t, inner, out)
	})
	t.Run("multiple parts pass through unchanged", func(t *testing.T) {
		in := `{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`
		require.Equal(t, in, unwrapResultEnvelope(in))
	})
	t.Run("image part passes through unchanged", func(t *testing.T) {
		in := `{"content":[{"type":"image","data":"aGVsbG8=","mimeType":"image/png"}]}`
		require.Equal(t, in, unwrapResultEnvelope(in))
	})
	t.Run("plain JSON non-envelope passes through unchanged", func(t *testing.T) {
		in := `{"url":"https://example.com","title":"not an envelope"}`
		require.Equal(t, in, unwrapResultEnvelope(in))
	})
	t.Run("not JSON passes through unchanged", func(t *testing.T) {
		in := "plain text, not JSON at all"
		require.Equal(t, in, unwrapResultEnvelope(in))
	})
	t.Run("empty string passes through unchanged", func(t *testing.T) {
		require.Empty(t, unwrapResultEnvelope(""))
	})
	t.Run("empty content array passes through unchanged", func(t *testing.T) {
		in := `{"content":[]}`
		require.Equal(t, in, unwrapResultEnvelope(in))
	})
	t.Run("envelope-shaped body without a text field passes through unchanged", func(t *testing.T) {
		in := `{"content":[{"type":"text"}]}`
		require.Equal(t, in, unwrapResultEnvelope(in))
	})
}

// quoteJSON marshals s as a JSON string literal (the envelope's "text" value).
func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

// fakeInvokable is a minimal InvokableTool standing in for the adapter's
// toolHelper in the wrap tests.
type fakeInvokable struct {
	out string
	err error
}

func (f *fakeInvokable) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "fake"}, nil
}

func (f *fakeInvokable) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return f.out, f.err
}

// fakeBase is a bare BaseTool: schema only, never executed.
type fakeBase struct{}

func (fakeBase) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "bare"}, nil
}

func TestWrapResultUnwrapping(t *testing.T) {
	t.Run("invokable tool results are unwrapped", func(t *testing.T) {
		wrapped := wrapResultUnwrapping([]tool.BaseTool{&fakeInvokable{
			out: `{"content":[{"type":"text","text":"payload"}]}`,
		}})
		require.Len(t, wrapped, 1)
		inv, ok := wrapped[0].(tool.InvokableTool)
		require.True(t, ok, "the wrapper must keep the InvokableTool capability")
		out, err := inv.InvokableRun(context.Background(), "{}")
		require.NoError(t, err)
		require.Equal(t, "payload", out)
	})
	t.Run("errors pass through unwrapped and unswallowed", func(t *testing.T) {
		boom := errors.New("call failed")
		wrapped := wrapResultUnwrapping([]tool.BaseTool{&fakeInvokable{err: boom}})
		inv := wrapped[0].(tool.InvokableTool)
		out, err := inv.InvokableRun(context.Background(), "{}")
		require.ErrorIs(t, err, boom)
		require.Empty(t, out)
	})
	t.Run("non-envelope output passes through unchanged", func(t *testing.T) {
		wrapped := wrapResultUnwrapping([]tool.BaseTool{&fakeInvokable{out: `{"status":"ok"}`}})
		inv := wrapped[0].(tool.InvokableTool)
		out, err := inv.InvokableRun(context.Background(), "{}")
		require.NoError(t, err)
		require.Equal(t, `{"status":"ok"}`, out)
	})
	t.Run("bare BaseTool passes through unchanged", func(t *testing.T) {
		bare := fakeBase{}
		wrapped := wrapResultUnwrapping([]tool.BaseTool{bare})
		require.Len(t, wrapped, 1)
		require.Equal(t, "bare", mustInfoName(t, wrapped[0]))
		_, ok := wrapped[0].(tool.InvokableTool)
		require.False(t, ok, "a schema-only tool gains no invokable surface")
	})
}

// TestConnectToolsReturnUnwrappedPayload pins the seam end to end: tools
// built through connect over the in-process HTTP stub (the real eino-ext
// adapter) answer InvokableRun with the inner payload text, not the marshaled
// envelope.
func TestConnectToolsReturnUnwrappedPayload(t *testing.T) {
	srv := httptest.NewServer(mcpserver.NewStreamableHTTPServer(newStubServer()))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn := Connection{Transport: TransportStreamableHTTP, URL: srv.URL + "/mcp"}
	ready, err := connect(ctx, conn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = ready.client.Close() }()

	for _, base := range ready.tools {
		info, err := base.Info(ctx)
		if err != nil || info == nil || info.Name != "test_tool" {
			continue
		}
		inv, ok := base.(tool.InvokableTool)
		require.True(t, ok, "the adapter's tools are invokable and must stay so")
		out, err := inv.InvokableRun(ctx, "{}")
		require.NoError(t, err)
		require.Equal(t, "tool result", out,
			"the connection's tools return the inner payload, not the MCP envelope")
		return
	}
	t.Fatalf("tools/list missing test_tool; got %v", listedToolNames(t, ctx, ready))
}

// TestManagerInvokeReturnsUnwrappedPayload pins the direct-invoke path: an
// envelope result from the wire comes back as the inner payload text, so both
// invocation paths agree.
func TestManagerInvokeReturnsUnwrappedPayload(t *testing.T) {
	cli := &recordingClient{result: mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(`{"status":"captured"}`)},
	}}
	m, ref := newInvokeManager(t, cli)

	out, err := m.Invoke(context.Background(), ref, "screenshot", `{}`, nil)
	require.NoError(t, err)
	require.Equal(t, `{"status":"captured"}`, out)
}

func mustInfoName(t *testing.T, base tool.BaseTool) string {
	t.Helper()
	info, err := base.Info(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	return info.Name
}
