package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// NewClient creates, starts, and initializes an MCP client for the given server configuration.
func NewClient(ctx context.Context, srv *store.MCPServer) (*client.Client, error) {
	var cli *client.Client
	var err error

	switch srv.Transport {
	case "stdio":
		var args []string
		if srv.Args != "" {
			if err = json.Unmarshal([]byte(srv.Args), &args); err != nil {
				return nil, fmt.Errorf("unmarshal args: %w", err)
			}
		}
		var env []string
		if srv.Env != "" {
			var envMap map[string]string
			if err = json.Unmarshal([]byte(srv.Env), &envMap); err != nil {
				return nil, fmt.Errorf("unmarshal env: %w", err)
			}
			for k, v := range envMap {
				env = append(env, fmt.Sprintf("%s=%s", k, v))
			}
		}

		cli, err = client.NewStdioMCPClient(srv.Command, env, args...)
		if err != nil {
			return nil, fmt.Errorf("create stdio client: %w", err)
		}

	case "http":
		cli, err = client.NewStreamableHttpClient(srv.URL)
		if err != nil {
			return nil, fmt.Errorf("create streamable http client: %w", err)
		}

	case "sse":
		cli, err = client.NewSSEMCPClient(srv.URL)
		if err != nil {
			return nil, fmt.Errorf("create sse client: %w", err)
		}

	default:
		return nil, fmt.Errorf("unknown transport type: %q", srv.Transport)
	}

	// Start the client transport
	if err = cli.Start(ctx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("start transport: %w", err)
	}

	// Perform the Initialize handshake
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "onclaw",
		Version: "1.0.0",
	}

	_, err = cli.Initialize(ctx, initRequest)
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("initialize client: %w", err)
	}

	return cli, nil
}
