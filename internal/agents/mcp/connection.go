package mcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Transport vocabulary of an MCP server definition. The values match the
// store's mcp_servers.transport column and the frontend's transport picker
// ("http" names the streamable HTTP transport).
const (
	TransportStdio          = "stdio"
	TransportStreamableHTTP = "http"
	TransportSSE            = "sse"
)

// Connection-status vocabulary persisted by the probe (the store's
// mcp_servers.status column; 'unknown' is the un-probed default the
// migration ships). StatusAuthRequired marks a server waiting on user
// authorization (design D5's four-value vocabulary).
const (
	StatusConnected    = "connected"
	StatusError        = "error"
	StatusAuthRequired = "auth_required"
)

// ErrUnsupportedTransport is returned for a connection whose Transport is not
// one of the supported vocabulary values.
var ErrUnsupportedTransport = errors.New("unsupported mcp transport")

// clientInfo identifies buddies during the MCP initialize handshake.
var clientInfo = mcp.Implementation{Name: "buddies", Version: "1.0.0"}

// Connection is one MCP server's dial settings, the transport-agnostic form
// of a stored server definition (store.MCPServer minus identity, status, and
// bookkeeping). The mcp package defines its own type — it accepts narrow
// data, not the store (design D1).
type Connection struct {
	// Transport is one of the Transport* constants.
	Transport string
	// Command and Args launch a stdio server.
	Command string
	Args    []string
	// Env is the stdio child's configured environment rows.
	Env map[string]string
	// URL and Headers configure the HTTP transports.
	URL     string
	Headers map[string]string
	// OAuth holds the HTTP transports' OAuth 2.1 client settings; the zero
	// value keeps today's static-header behavior (design D1).
	OAuth OAuth
}

// connection is a ready-to-use MCP connection: the initialized client plus
// the tool surface it listed at handshake time.
type connection struct {
	client client.MCPClient
	tools  []tool.BaseTool
}

// connect dials the server described by conn and performs the initialize
// handshake, returning the ready client with its listed tools. The caller's
// context bounds dialing, handshake, and tool listing (the manager wraps it
// with its connect timeout). A failed step is classified (design D5): a
// server waiting on user authorization comes back as *AuthRequiredError —
// StatusAuthRequired to the probe — everything else as a plain error.
func connect(ctx context.Context, conn Connection) (*connection, error) {
	cli, err := dial(ctx, conn)
	if err != nil {
		return nil, classifyConnectError(ctx, conn, err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = clientInfo
	if _, err := cli.Initialize(ctx, initReq); err != nil {
		abandonClient(cli)
		return nil, classifyConnectError(ctx, conn, fmt.Errorf("mcp initialize handshake: %w", err))
	}

	tools, err := mcpp.GetTools(ctx, &mcpp.Config{Cli: cli})
	if err != nil {
		abandonClient(cli)
		return nil, classifyConnectError(ctx, conn, fmt.Errorf("mcp list tools: %w", err))
	}
	return &connection{client: cli, tools: tools}, nil
}

// classifyConnectError maps a failed dial/handshake onto the probe's status
// vocabulary (design D5): an OAuth transport that reports missing, expired,
// or rejected tokens is auth_required outright; a failed HTTP/SSE attempt in
// none mode counts as auth_required only when a fresh unauthenticated probe
// discovers a well-formed OAuth advertisement. Everything else — a server
// that is merely down, a non-OAuth 401 — stays the original error.
func classifyConnectError(ctx context.Context, conn Connection, err error) error {
	if client.IsOAuthAuthorizationRequiredError(err) {
		return &AuthRequiredError{Cause: err}
	}
	if (conn.Transport == TransportStreamableHTTP || conn.Transport == TransportSSE) && !conn.OAuth.Enabled {
		if discovery, derr := Discover(ctx, conn.Transport, conn.URL, conn.Headers); derr == nil {
			return &AuthRequiredError{Discovery: discovery, Cause: err}
		}
	}
	return err
}

// abandonClient tears down a half-open client asynchronously under the close
// watchdog (the manager's closeClient). mcp-go's stdio Close waits for the
// child to exit, so a synchronous Close would hold the caller — run or probe
// alike — until a wedged child dies, past any dial bound (design D7: a dead
// server never holds a run hostage).
func abandonClient(cli client.MCPClient) {
	go closeClient(key{}, &cacheEntry{conn: &connection{client: cli}})
}

// dial opens the transport for conn. stdio launches the configured command
// (mcp-go starts the transport in the constructor); streamable HTTP and SSE
// dial lazily through Start. The handshake follows in connect.
func dial(ctx context.Context, conn Connection) (client.MCPClient, error) {
	switch conn.Transport {
	case TransportStdio:
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
	case TransportStreamableHTTP:
		opts := []transport.StreamableHTTPCOption{transport.WithHTTPHeaders(headerMap(conn.Headers))}
		var (
			cli *client.Client
			err error
		)
		if conn.OAuth.Enabled {
			if err := conn.OAuth.ready(); err != nil {
				return nil, fmt.Errorf("mcp streamable http dial %s: %w", conn.URL, err)
			}
			cli, err = client.NewOAuthStreamableHttpClient(conn.URL, conn.OAuth.config(), opts...)
		} else {
			cli, err = client.NewStreamableHttpClient(conn.URL, opts...)
		}
		if err != nil {
			return nil, fmt.Errorf("mcp streamable http dial %s: %w", conn.URL, err)
		}
		if err := cli.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp streamable http connect %s: %w", conn.URL, err)
		}
		return cli, nil
	case TransportSSE:
		opts := []transport.ClientOption{transport.WithHeaders(headerMap(conn.Headers))}
		var (
			cli *client.Client
			err error
		)
		if conn.OAuth.Enabled {
			if err := conn.OAuth.ready(); err != nil {
				return nil, fmt.Errorf("mcp sse dial %s: %w", conn.URL, err)
			}
			cli, err = client.NewOAuthSSEClient(conn.URL, conn.OAuth.config(), opts...)
		} else {
			cli, err = client.NewSSEMCPClient(conn.URL, opts...)
		}
		if err != nil {
			return nil, fmt.Errorf("mcp sse dial %s: %w", conn.URL, err)
		}
		if err := cli.Start(ctx); err != nil {
			return nil, fmt.Errorf("mcp sse connect %s: %w", conn.URL, err)
		}
		return cli, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedTransport, conn.Transport)
	}
}

// stdioEnvBaseline is the fixed allowlist of parent environment variables a
// stdio MCP child receives beyond the connection's configured rows (design
// D7; onclaw's fix-stdio-env-leak contract): names children need to shell out
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
// every returned entry is NAME=VALUE with a non-empty NAME. Configured rows
// are emitted in sorted-name order so the slice is deterministic.
//
// dial installs the result wholesale via transport.WithCommandFunc because
// mcp-go v0.43.0's default stdio launch path merges a supplied env over
// os.Environ() (client/transport/stdio.go spawnCommand: cmd.Env =
// append(os.Environ(), c.env...)); passing the slice alone would still leak
// every parent variable into the child.
func constructChildEnv(parent []string, rows map[string]string) []string {
	values := make(map[string]string, len(stdioEnvBaseline)+len(rows))
	var order []string
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
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		add(name, rows[name])
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		out = append(out, name+"="+values[name])
	}
	return out
}

// headerMap flattens the configured headers (trimmed names) for the HTTP
// transports' header options.
func headerMap(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		out[strings.TrimSpace(name)] = value
	}
	return out
}
