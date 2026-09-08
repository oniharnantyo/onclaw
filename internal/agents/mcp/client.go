// Package mcp provides the MCP (Model Context Protocol) runtime: a transport
// factory for mcp-go clients (design.md D3), the idle-reaped connection
// manager (D5), provider-safe tool naming (D7), and the policy/status seams
// the agent runner consumes (D6/D8).
package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// clientInfo identifies OnClaw during the MCP initialize handshake.
var clientInfo = mcp.Implementation{Name: "onclaw", Version: "1.0.0"}

// connection is a ready-to-use MCP connection: the initialized client plus
// the tool surface it listed at handshake time.
type connection struct {
	client client.MCPClient
	tools  []tool.BaseTool
}

// connect dials the server described by conn and performs the initialize
// handshake, returning the ready client with its listed tools. The caller's
// context bounds dialing, handshake, and tool listing (the manager wraps it
// with its connect timeout).
func connect(ctx context.Context, conn domain.MCPConnection) (*connection, error) {
	cli, err := dial(ctx, conn)
	if err != nil {
		return nil, err
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = clientInfo
	if _, err := cli.Initialize(ctx, initReq); err != nil {
		abandonClient(cli)
		return nil, fmt.Errorf("mcp initialize handshake: %w", err)
	}

	tools, err := mcpp.GetTools(ctx, &mcpp.Config{Cli: cli})
	if err != nil {
		abandonClient(cli)
		return nil, fmt.Errorf("mcp list tools: %w", err)
	}
	return &connection{client: cli, tools: tools}, nil
}

// abandonClient tears down a half-open client asynchronously under the close
// watchdog (the manager's closeClient). mcp-go's stdio Close waits for the
// child to exit, so a synchronous Close would hold the caller — run or probe
// alike — until a wedged child dies, past any dial bound (design.md D8: a
// dead server never holds a run hostage).
func abandonClient(cli client.MCPClient) {
	go closeClient(key{}, &cacheEntry{conn: &connection{client: cli}})
}

// dial opens the transport for conn. stdio launches the configured command
// (mcp-go starts the transport in the constructor); streamable HTTP and SSE
// dial lazily through Start. The handshake follows in connect.
func dial(ctx context.Context, conn domain.MCPConnection) (client.MCPClient, error) {
	switch conn.Transport {
	case domain.MCPTransportStdio:
		cli, err := client.NewStdioMCPClient(conn.Command, envSlice(conn.Env), conn.Args...)
		if err != nil {
			return nil, fmt.Errorf("mcp stdio start %q: %w", conn.Command, err)
		}
		return cli, nil
	case domain.MCPTransportStreamableHTTP:
		cli, err := client.NewStreamableHttpClient(conn.URL, transport.WithHTTPHeaders(headerMap(conn.Headers)))
		if err != nil {
			return nil, fmt.Errorf("mcp streamable http dial %s: %w", conn.URL, err)
		}
		if err := cli.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp streamable http connect %s: %w", conn.URL, err)
		}
		return cli, nil
	case domain.MCPTransportSSE:
		cli, err := client.NewSSEMCPClient(conn.URL, transport.WithHeaders(headerMap(conn.Headers)))
		if err != nil {
			return nil, fmt.Errorf("mcp sse dial %s: %w", conn.URL, err)
		}
		if err := cli.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp sse connect %s: %w", conn.URL, err)
		}
		return cli, nil
	default:
		return nil, fmt.Errorf("%w: transport %q", domain.ErrInvalid, conn.Transport)
	}
}

// envSlice flattens env rows into KEY=VALUE entries. mcp-go merges them over
// the parent environment, so configured vars win without dropping PATH.
func envSlice(rows []domain.EnvRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Name+"="+row.Value)
	}
	return out
}

// headerMap flattens header rows for the HTTP transports' WithHeaders options.
func headerMap(rows []domain.EnvRow) map[string]string {
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.Name)] = row.Value
	}
	return out
}
