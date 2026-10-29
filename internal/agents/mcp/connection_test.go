package mcp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

func TestRequestHeadersCredentialStyles(t *testing.T) {
	cases := []struct {
		name  string
		style string
		cred  string
		want  string // the Authorization value; empty means none set
	}{
		{name: "empty style defaults to bearer", style: "", cred: "s3cret", want: "Bearer s3cret"},
		{name: "bearer", style: CredentialStyleBearer, cred: "s3cret", want: "Bearer s3cret"},
		{name: "token=", style: CredentialStyleTokenEq, cred: "s3cret", want: "Token token=s3cret"},
		{name: "basic", style: CredentialStyleBasic, cred: "me@example.com:apitoken", want: "Basic " + base64.StdEncoding.EncodeToString([]byte("me@example.com:apitoken"))},
		{name: "custom scheme", style: "custom:Sentry-Bearer", cred: "s3cret", want: "Sentry-Bearer s3cret"},
		{name: "unknown style falls back to bearer", style: "mystery", cred: "s3cret", want: "Bearer s3cret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := Connection{
				Headers:               map[string]string{"Accept": "application/json"},
				IntegrationCredential: tc.cred,
				CredentialHeaderStyle: tc.style,
			}
			headers := requestHeaders(conn)
			require.Equal(t, tc.want, headers["Authorization"])
			require.Equal(t, "application/json", headers["Accept"])
		})
	}
}

func TestRequestHeadersNamedHeaderStyle(t *testing.T) {
	conn := Connection{
		Headers:               map[string]string{"X-Goog-Api-Key": "configured-anyway"},
		IntegrationCredential: "maps-key",
		CredentialHeaderStyle: "header:X-Goog-Api-Key",
	}
	headers := requestHeaders(conn)
	require.Equal(t, "maps-key", headers["X-Goog-Api-Key"])
	require.NotContains(t, headers, "Authorization")
}

func TestRequestHeadersWithoutCredentialLeavesHeadersAlone(t *testing.T) {
	conn := Connection{Headers: map[string]string{"Authorization": "Bearer configured"}}
	headers := requestHeaders(conn)
	require.Equal(t, "Bearer configured", headers["Authorization"])
}

func TestRequestHeadersNeverMutatesConfiguredHeaders(t *testing.T) {
	// The credential is injected into a fresh map: the configured headers —
	// the stored row's view (design D4) — must come out clean.
	configured := map[string]string{"Accept": "application/json"}
	conn := Connection{
		Headers:               configured,
		IntegrationCredential: "s3cret",
		CredentialHeaderStyle: CredentialStyleBearer,
	}
	_ = requestHeaders(conn)
	require.NotContains(t, configured, "Authorization")
	require.Equal(t, map[string]string{"Accept": "application/json"}, configured)
}

// TestDialInjectsCredentialHeader exercises the dial-time injection end to
// end: the HTTP MCP server sees the styled credential on its requests, while
// the connection's Headers map stays clean.
func TestDialInjectsCredentialHeader(t *testing.T) {
	target := mcpserver.NewMCPServer("dial-target", "1.0.0")
	target.AddTool(mcp.NewTool("ping"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("pong"), nil
	})
	mcpHandler := mcpserver.NewStreamableHTTPServer(target)
	var mu sync.Mutex
	var authorizations []string
	var apiKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		apiKeys = append(apiKeys, r.Header.Get("X-Api-Key"))
		mu.Unlock()
		mcpHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	configured := map[string]string{"Accept": "application/json"}
	conn := Connection{
		Transport:             TransportStreamableHTTP,
		URL:                   server.URL,
		Headers:               configured,
		IntegrationCredential: "dial-key",
		CredentialHeaderStyle: "header:X-Api-Key",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// connect performs the initialize handshake — dial alone is lazy and
	// would send nothing.
	connx, err := connect(ctx, conn)
	require.NoError(t, err)
	require.NotEmpty(t, connx.tools)
	defer func() { _ = connx.client.Close() }()

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, apiKeys)
	for _, key := range apiKeys {
		require.Equal(t, "dial-key", key)
	}
	for _, value := range authorizations {
		require.Empty(t, value)
	}
	// The configured headers were never mutated with the credential.
	require.NotContains(t, configured, "X-Api-Key")
}
