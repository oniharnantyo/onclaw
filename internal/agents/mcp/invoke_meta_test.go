package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The parent's meta-carrying invoke (add-browser-login-handover follow-up):
// the system server's tools are invoked directly with the calling session's
// workspace screenshots directory riding the call's _meta — never the
// arguments, which the transcript records. The semantics mirror the listed
// tools' eino adapter, and the call counts toward the in-flight gauge.

// recordingClient answers CallTool with a canned result and records every
// request, so a test can pin exactly what rode the wire.
type recordingClient struct {
	client.MCPClient
	closed atomic.Bool

	mu      sync.Mutex
	request mcp.CallToolRequest
	calls   int
	result  mcp.CallToolResult
	err     error
}

func (c *recordingClient) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *recordingClient) CallTool(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.request = req
	if c.err != nil {
		return nil, c.err
	}
	out := c.result
	return &out, nil
}

func (c *recordingClient) lastCall() (mcp.CallToolRequest, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.request, c.calls
}

func newInvokeManager(t *testing.T, cli *recordingClient) (*MCPManager, Ref) {
	t.Helper()
	m := NewMCPManager(
		withConnector(func(context.Context, Ref) (*connection, error) {
			return nil, errors.New("the test owns the system entry directly")
		}),
		withManualSupervision(),
		WithSystemBrowserUse("/data/browser", func(context.Context) (SystemServerConfig, error) {
			return SystemServerConfig{DefaultProfile: "default"}, nil
		}),
	)
	t.Cleanup(m.Close)
	m.mu.Lock()
	m.entries[key{SystemServerID}] = &cacheEntry{
		conn:   &connection{client: cli},
		status: StatusConnected,
	}
	m.mu.Unlock()
	entries := m.SystemServers()
	require.Len(t, entries, 1)
	return m, entries[0].Ref
}

func TestManagerInvokeCarriesMetaOnTheWire(t *testing.T) {
	cli := &recordingClient{}
	m, ref := newInvokeManager(t, cli)

	out, err := m.Invoke(context.Background(), ref, "screenshot", `{"profile":"work"}`,
		map[string]any{"buddies.dev/screenshot-dir": "/ws/scout/sessions/abc/browser"})
	require.NoError(t, err)
	require.NotEmpty(t, out, "the marshaled MCP envelope is the result")

	req, calls := cli.lastCall()
	require.Equal(t, 1, calls)
	require.Equal(t, "screenshot", req.Params.Name)
	argsRaw, jerr := json.Marshal(req.Params.Arguments)
	require.NoError(t, jerr)
	require.JSONEq(t, `{"profile":"work"}`, string(argsRaw),
		"arguments pass through verbatim")
	require.NotNil(t, req.Params.Meta)
	require.Equal(t, "/ws/scout/sessions/abc/browser", req.Params.Meta.AdditionalFields["buddies.dev/screenshot-dir"])

	// The in-flight gauge is back to zero after the call.
	require.Zero(t, m.systems[0].inflight.Load(), "the direct invoke counts toward the gauge like every listed tool")
}

func TestManagerInvokeRefusesNonSystemRefs(t *testing.T) {
	cli := &recordingClient{}
	m, _ := newInvokeManager(t, cli)

	_, err := m.Invoke(context.Background(), Ref{ServerID: "github", Name: "github"}, "list", `{}`, nil)
	require.Error(t, err, "third-party servers carry no meta contract")

	_, calls := cli.lastCall()
	require.Zero(t, calls, "the refusal never reaches the wire")
}

func TestManagerInvokeSurfacesErrors(t *testing.T) {
	t.Run("transport failure", func(t *testing.T) {
		cli := &recordingClient{err: errors.New("pipe broken")}
		m, ref := newInvokeManager(t, cli)
		_, err := m.Invoke(context.Background(), ref, "screenshot", `{}`, nil)
		require.ErrorContains(t, err, "pipe broken")
	})
	t.Run("server-side error result", func(t *testing.T) {
		cli := &recordingClient{result: mcp.CallToolResult{IsError: true}}
		m, ref := newInvokeManager(t, cli)
		_, err := m.Invoke(context.Background(), ref, "screenshot", `{}`, nil)
		require.ErrorContains(t, err, "mcp server return error")
	})
	t.Run("no ready connection", func(t *testing.T) {
		m, ref := newInvokeManager(t, &recordingClient{})
		m.mu.Lock()
		m.entries[key{SystemServerID}] = &cacheEntry{err: errors.New("starting")}
		m.mu.Unlock()
		_, err := m.Invoke(context.Background(), ref, "screenshot", `{}`, nil)
		require.ErrorContains(t, err, "not ready")
	})
}
