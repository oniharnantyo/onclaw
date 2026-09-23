// Package mcp provides the MCP (Model Context Protocol) runtime: a transport
// factory for mcp-go clients (design.md D3), the idle-reaped connection
// manager (D5), provider-safe tool naming (D7), and the policy/status seams
// the agent runner consumes (D6/D8).
package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
		// mcp-go v0.43.0's default launch path merges the supplied env over
		// os.Environ() (client/transport/stdio.go spawnCommand: cmd.Env =
		// append(os.Environ(), c.env...)), which would leak every parent
		// variable into the child. WithCommandFunc is its replace-semantics
		// escape hatch: the CommandFunc builds the whole exec.Cmd, so cmd.Env
		// is exactly the constructed slice, never merged over the parent.
		env := constructChildEnv(os.Environ(), conn.Env)
		cli, err := client.NewStdioMCPClientWithOptions(conn.Command, env, conn.Args,
			transport.WithCommandFunc(func(ctx context.Context, command string, cmdEnv []string, args []string) (*exec.Cmd, error) {
				cmd := exec.CommandContext(ctx, command, args...)
				cmd.Env = cmdEnv
				return cmd, nil
			}))
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

// stdioEnvBaseline is the fixed allowlist of parent environment variables a
// stdio MCP child receives beyond the connection's configured rows (design.md
// D1 of openspec/changes/fix-stdio-env-leak): names children need to shell out
// (PATH, HOME, TMPDIR), localize (LANG, LC_ALL, TZ), and traverse enterprise
// proxies (upper- and lowercase forms). The baseline is deliberately not
// configurable — a configurable allowlist reintroduces the leak via
// misconfiguration.
var stdioEnvBaseline = map[string]struct{}{
	"PATH":        {},
	"HOME":        {},
	"TMPDIR":      {},
	"LANG":        {},
	"LC_ALL":      {},
	"TZ":          {},
	"HTTP_PROXY":  {},
	"HTTPS_PROXY": {},
	"ALL_PROXY":   {},
	"NO_PROXY":    {},
	"http_proxy":  {},
	"https_proxy": {},
	"all_proxy":   {},
	"no_proxy":    {},
}

// constructChildEnv builds the stdio child environment: baseline entries
// present in parent, overridden by the connection's configured rows (rows win
// on name conflicts), and nothing else from the parent. Baseline entries
// absent from parent are omitted — never synthesized as empty strings — while
// rows keep empty values (an explicit empty row unsets nothing, it sets ""):
// every returned entry is NAME=VALUE with a non-empty NAME.
//
// dial installs the result wholesale via transport.WithCommandFunc because
// mcp-go v0.43.0's default stdio launch path merges a supplied env over
// os.Environ() (client/transport/stdio.go spawnCommand: cmd.Env =
// append(os.Environ(), c.env...)); passing the slice alone would still leak
// every parent variable into the child.
func constructChildEnv(parent []string, rows []domain.EnvRow) []string {
	values := make(map[string]string, len(stdioEnvBaseline)+len(rows))
	order := make([]string, 0, len(stdioEnvBaseline)+len(rows))
	add := func(name, value string) {
		if _, dup := values[name]; !dup {
			order = append(order, name)
		}
		values[name] = value
	}
	for _, entry := range parent {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		if _, base := stdioEnvBaseline[name]; !base {
			continue
		}
		add(name, value)
	}
	for _, row := range rows {
		add(row.Name, row.Value)
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		out = append(out, name+"="+values[name])
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
