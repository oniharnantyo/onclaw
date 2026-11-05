package browseruse

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// wantToolNames is the registered surface: the 11 discrete page tools, the 2
// guarded handover tools, and the 5 internal __-prefixed channel tools the
// parent filters from the model (design D7).
var wantToolNames = []string{
	"navigate", "snapshot", "click", "type", "press_key", "scroll",
	"back", "screenshot", "cookies", "read", "wait",
	"submit_secret", "request_takeover",
	"__request_secret", "__deliver_secret", "__request_takeover",
	"__begin_takeover", "__resolve_attention",
}

// listTools drives the server's HandleMessage with a tools/list request and
// returns the decoded result.
func listTools(t *testing.T, srv interface {
	HandleMessage(context.Context, json.RawMessage) mcp.JSONRPCMessage
}) mcp.ListToolsResult {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	require.NoError(t, err)
	resp := srv.HandleMessage(context.Background(), raw)
	toolResult, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "tools/list must answer with a response, got %T", resp)
	result, ok := toolResult.Result.(mcp.ListToolsResult)
	require.True(t, ok, "tools/list result must decode, got %T", toolResult.Result)
	return result
}

// callTool drives a tools/call request through the server and returns the
// result.
func callTool(t *testing.T, srv any, name, args string) mcp.CallToolResult {
	t.Helper()
	type messenger interface {
		HandleMessage(context.Context, json.RawMessage) mcp.JSONRPCMessage
	}
	msg, ok := srv.(messenger)
	require.True(t, ok)
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": json.RawMessage(args),
		},
	})
	require.NoError(t, err)
	resp := msg.HandleMessage(context.Background(), raw)
	response, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "tools/call must answer with a response, got %T", resp)
	result, ok := response.Result.(mcp.CallToolResult)
	require.True(t, ok, "tools/call result must decode, got %T", response.Result)
	return result
}

// resultText flattens a tool result's text content.
func resultText(t *testing.T, res mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content, "every tool result carries its message")
	text, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok, "content must be text, got %T", res.Content[0])
	return text.Text
}

func TestNewMCPServer_ListTools(t *testing.T) {
	srv := NewMCPServer(DefaultConfig(), "test")
	res := listTools(t, srv)

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		require.NotEmpty(t, tool.Description, "tool %s must carry its description", tool.Name)
	}
	require.ElementsMatch(t, wantToolNames, names, "exactly the registered surface is present")

	// Schema round trip: every tool marshals over the wire and decodes back
	// with its name, description, and declared properties intact.
	for _, tool := range res.Tools {
		b, err := json.Marshal(tool)
		require.NoError(t, err)
		var decoded mcp.Tool
		require.NoError(t, json.Unmarshal(b, &decoded))
		require.Equal(t, tool.Name, decoded.Name)
		require.Equal(t, tool.Description, decoded.Description)
		require.Equal(t, "object", decoded.InputSchema.Type)
		require.NotEmpty(t, decoded.InputSchema.Properties, "tool %s must declare properties", tool.Name)
	}

	// Per-tool argument contract (the coordination surface the spawning
	// parent builds against).
	byName := map[string]mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, arg := range []string{"url", "profile"} {
		require.Contains(t, byName["navigate"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"ref", "name", "role", "profile"} {
		require.Contains(t, byName["click"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"ref", "name", "role", "text", "profile"} {
		require.Contains(t, byName["type"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"key", "profile"} {
		require.Contains(t, byName["press_key"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"direction", "profile"} {
		require.Contains(t, byName["scroll"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"op", "name", "value", "domain", "path", "profile"} {
		require.Contains(t, byName["cookies"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"maxLength", "profile"} {
		require.Contains(t, byName["read"].InputSchema.Properties, arg)
	}
	for _, arg := range []string{"ms", "profile"} {
		require.Contains(t, byName["wait"].InputSchema.Properties, arg)
	}

	// Handover contracts (design D2/D7/D8): the secret schema carries field
	// targets and submission controls only — no value field anywhere — and
	// the internal channel tools carry exactly the parent's wire shape.
	for _, tool := range []string{"submit_secret", "__request_secret"} {
		for _, arg := range []string{"targets", "method", "digits", "submission", "submitTarget", "loginId", "username", "profile"} {
			require.Contains(t, byName[tool].InputSchema.Properties, arg, "%s must declare %s", tool, arg)
		}
		for _, forbidden := range []string{"value", "password", "secret", "code", "otp", "text"} {
			require.NotContains(t, byName[tool].InputSchema.Properties, forbidden,
				"%s must never declare a value-bearing field %s", tool, forbidden)
		}
		require.Contains(t, byName[tool].InputSchema.Required, "targets")
		require.Contains(t, byName[tool].InputSchema.Required, "method")
		require.Contains(t, byName[tool].InputSchema.Required, "submission")
	}
	targetsProp := byName["submit_secret"].InputSchema.Properties["targets"].(map[string]any)
	require.Equal(t, "array", targetsProp["type"], "targets must be an array")
	require.Equal(t, 1, targetsProp["minItems"], "targets must carry at least one entry")
	require.Equal(t, 12, targetsProp["maxItems"], "targets must carry at most 12 entries")
	methodProp := byName["submit_secret"].InputSchema.Properties["method"].(map[string]any)
	require.Equal(t, []string{"password", "otp", "authenticator"}, methodProp["enum"])
	submissionProp := byName["submit_secret"].InputSchema.Properties["submission"].(map[string]any)
	require.Equal(t, []string{"click", "enter", "on_input"}, submissionProp["enum"])
	digitsProp := byName["submit_secret"].InputSchema.Properties["digits"].(map[string]any)
	require.Equal(t, float64(6), digitsProp["default"], "digits defaults to 6")
	require.Contains(t, byName["submit_secret"].InputSchema.Required, "submission")

	for _, arg := range []string{"reason", "profile"} {
		require.Contains(t, byName["request_takeover"].InputSchema.Properties, arg)
	}
	require.Contains(t, byName["request_takeover"].InputSchema.Required, "reason")
	reasonProp := byName["request_takeover"].InputSchema.Properties["reason"].(map[string]any)
	require.Equal(t, []string{"sign-in-blocked"}, reasonProp["enum"])

	for _, arg := range []string{"requestID", "value"} {
		require.Contains(t, byName["__deliver_secret"].InputSchema.Properties, arg)
	}
	require.Contains(t, byName["__deliver_secret"].InputSchema.Required, "requestID")
	require.Contains(t, byName["__deliver_secret"].InputSchema.Required, "value")
	for _, arg := range []string{"reason", "profile"} {
		require.Contains(t, byName["__request_takeover"].InputSchema.Properties, arg)
	}
	require.Contains(t, byName["__begin_takeover"].InputSchema.Properties, "requestID")
	for _, arg := range []string{"requestID", "outcome"} {
		require.Contains(t, byName["__resolve_attention"].InputSchema.Properties, arg)
	}
	outcomeProp := byName["__resolve_attention"].InputSchema.Properties["outcome"].(map[string]any)
	require.Equal(t, []string{"completed", "cancelled", "expired"}, outcomeProp["enum"])

	// Only the genuinely required arguments are marked required.
	require.Contains(t, byName["navigate"].InputSchema.Required, "url")
	require.Contains(t, byName["type"].InputSchema.Required, "text")
	require.Contains(t, byName["cookies"].InputSchema.Required, "op")
	require.NotContains(t, byName["click"].InputSchema.Required, "name",
		"click addresses by ref OR name; neither is required at the schema level")
}

func TestNewMCPServer_ToolCallValidation(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.BrowserDir = dir
	srv := NewMCPServer(cfg, "test")

	// Validation failures must be readable tool-error results, and must not
	// touch the filesystem: no profile directories may appear.
	cases := []struct {
		name    string
		tool    string
		args    string
		wantErr string
	}{
		{"navigate without url", "navigate", `{}`, "url is required"},
		{"click without ref or name", "click", `{}`, "either ref or name is required"},
		{"type without text", "type", `{"ref":"e1"}`, "text is required"},
		{"type without target", "type", `{"text":"hi"}`, "either ref or name is required"},
		{"press_key without key", "press_key", `{}`, "key is required"},
		{"press_key unsupported key", "press_key", `{"key":"ChannelUp"}`, "unsupported key"},
		{"cookies without op", "cookies", `{}`, "op must be one of"},
		{"cookies bad op", "cookies", `{"op":"wipe"}`, "op must be one of"},
		{"cookies set without value", "cookies", `{"op":"set","name":"a"}`, "name and value are required"},
		{"cookies delete without name", "cookies", `{"op":"delete"}`, "name is required"},
		{"invalid profile traversal", "snapshot", `{"profile":"../etc"}`, "invalid profile name"},
		{"invalid profile spaces", "snapshot", `{"profile":"My Profile"}`, "invalid profile name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, srv, tc.tool, tc.args)
			require.True(t, res.IsError, "validation failures are tool errors, not JSON-RPC errors")
			require.Contains(t, resultText(t, res), tc.wantErr)
		})
	}

	_, err := os.Stat(filepath.Join(dir, "profiles"))
	require.True(t, os.IsNotExist(err), "validation must fail before any filesystem access")
}

func TestNewMCPServer_UnknownTool(t *testing.T) {
	srv := NewMCPServer(DefaultConfig(), "test")
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "explode",
			"arguments": json.RawMessage(`{}`),
		},
	})
	require.NoError(t, err)
	resp := srv.HandleMessage(context.Background(), raw)
	_, isErr := resp.(mcp.JSONRPCError)
	require.True(t, isErr, "an unknown tool is rejected at the protocol level, got %T", resp)
}

func TestNewMCPServer_Name(t *testing.T) {
	require.Equal(t, "browser-use", ServerName,
		"the parent's namer surfaces tools as mcp__browser-use__<tool>")
}

// TestNewMCPServer_InitializeRoundTrip pins the initialize handshake the
// spawning parent performs.
func TestNewMCPServer_InitializeRoundTrip(t *testing.T) {
	srv := NewMCPServer(DefaultConfig(), "test")
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"clientInfo":      map[string]any{"name": "test-client", "version": "1"},
		},
	})
	require.NoError(t, err)
	resp, ok := srv.HandleMessage(context.Background(), raw).(mcp.JSONRPCResponse)
	require.True(t, ok)
	init, ok := resp.Result.(mcp.InitializeResult)
	require.True(t, ok)
	require.Equal(t, ServerName, init.ServerInfo.Name)
	require.Equal(t, "test", init.ServerInfo.Version)
	require.True(t, fmt.Sprint(init.ProtocolVersion) != "")
}
