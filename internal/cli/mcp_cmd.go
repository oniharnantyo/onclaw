package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

func mcpCommand(st *appState) *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "Manage Model Context Protocol (MCP) tool servers",
		Commands: []*cli.Command{
			{
				Name:      "add",
				Usage:     "Add or update an MCP tool server configuration",
				ArgsUsage: "<name> [--url <url> | --sse-url <sse-url> | -- command args...]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "url",
						Usage: "Streamable HTTP URL of the remote MCP server",
					},
					&cli.StringFlag{
						Name:  "sse-url",
						Usage: "Legacy SSE HTTP URL of the remote MCP server",
					},
					&cli.StringSliceFlag{
						Name:  "env",
						Usage: "Environment variables to pass to the stdio subprocess (repeatable, format KEY=VAL)",
					},
					&cli.BoolFlag{
						Name:  "disable",
						Usage: "Add the server in disabled state",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("server name is required")
					}
					name := c.Args().First()

					var transport string
					var command string
					var args []string
					var url string

					if c.String("url") != "" {
						transport = "http"
						url = c.String("url")
					} else if c.String("sse-url") != "" {
						transport = "sse"
						url = c.String("sse-url")
					} else {
						transport = "stdio"
						if c.Args().Len() < 2 {
							return fmt.Errorf("stdio command/arguments are required after server name or specify --url/--sse-url")
						}
						allArgs := c.Args().Slice()
						command = allArgs[1]
						args = allArgs[2:]
					}

					// Process environment variables
					envMap := make(map[string]string)
					for _, envRaw := range c.StringSlice("env") {
						parts := strings.SplitN(envRaw, "=", 2)
						if len(parts) != 2 {
							return fmt.Errorf("invalid env format %q, must be KEY=VAL", envRaw)
						}
						envMap[parts[0]] = parts[1]
					}

					argsJSON, err := json.Marshal(args)
					if err != nil {
						return fmt.Errorf("marshal args: %w", err)
					}

					envJSON, err := json.Marshal(envMap)
					if err != nil {
						return fmt.Errorf("marshal env: %w", err)
					}

					enabled := 1
					if c.Bool("disable") {
						enabled = 0
					}

					_, mcpStore, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					srv := &store.MCPServer{
						Name:      name,
						Transport: transport,
						Command:   command,
						Args:      string(argsJSON),
						Env:       string(envJSON),
						URL:       url,
						Enabled:   enabled,
					}

					// Check if server exists to do Add or Update
					existing, err := mcpStore.GetServer(ctx, name)
					if err == nil && existing != nil {
						srv.CreatedAt = existing.CreatedAt
						err = mcpStore.UpdateServer(ctx, srv)
						if err != nil {
							return fmt.Errorf("update server: %w", err)
						}
						fmt.Printf("MCP server %q updated successfully.\n", name)
					} else {
						err = mcpStore.AddServer(ctx, srv)
						if err != nil {
							return fmt.Errorf("add server: %w", err)
						}
						fmt.Printf("MCP server %q added successfully.\n", name)
					}

					_ = signalRunningProcess(st.cfg.DbPath)
					return nil
				},
			},
			{
				Name:  "list",
				Usage: "List all configured MCP tool servers",
				Action: func(ctx context.Context, c *cli.Command) error {
					_, mcpStore, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					servers, err := mcpStore.ListServers(ctx)
					if err != nil {
						return fmt.Errorf("list servers: %w", err)
					}

					if len(servers) == 0 {
						fmt.Println("No MCP servers configured.")
						return nil
					}

					for _, srv := range servers {
						status := "enabled"
						if srv.Enabled != 1 {
							status = "disabled"
						}

						var detail string
						if srv.Transport == "stdio" {
							var args []string
							_ = json.Unmarshal([]byte(srv.Args), &args)
							
							var envMap map[string]string
							_ = json.Unmarshal([]byte(srv.Env), &envMap)

							var redactedEnv []string
							for k := range envMap {
								redactedEnv = append(redactedEnv, fmt.Sprintf("%s=***", k))
							}
							
							detail = fmt.Sprintf("command: %s %s, env: {%s}", srv.Command, strings.Join(args, " "), strings.Join(redactedEnv, ", "))
						} else {
							detail = fmt.Sprintf("url: %s", srv.URL)
						}

						fmt.Printf("name: %s, transport: %s, %s, status: %s\n", srv.Name, srv.Transport, detail, status)
					}
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Remove a configured MCP tool server",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("server name is required")
					}
					name := c.Args().First()

					_, mcpStore, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					err = mcpStore.RemoveServer(ctx, name)
					if err != nil {
						return fmt.Errorf("remove server %q: %w", name, err)
					}

					_ = signalRunningProcess(st.cfg.DbPath)
					fmt.Printf("MCP server %q removed successfully.\n", name)
					return nil
				},
			},
			{
				Name:      "test",
				Usage:     "Test connection to a configured MCP tool server and list its tools",
				ArgsUsage: "<name>",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() < 1 {
						return fmt.Errorf("server name is required")
					}
					name := c.Args().First()

					_, mcpStore, db, err := st.getProviderManager(c)
					if err != nil {
						return err
					}
					defer db.Close()

					srv, err := mcpStore.GetServer(ctx, name)
					if err != nil {
						return fmt.Errorf("get server %q: %w", name, err)
					}

					fmt.Printf("Connecting to MCP server %q (%s)...\n", srv.Name, srv.Transport)

					// Connect to the client using NewClient
					cliCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
					cli, err := mcp.NewClient(cliCtx, srv)
					cancel()
					if err != nil {
						return fmt.Errorf("failed to connect/initialize server: %w", err)
					}
					defer cli.Close()

					// List tools
					toolsCtx, toolsCancel := context.WithTimeout(ctx, 5*time.Second)
					listResult, err := cli.ListTools(toolsCtx, mcpgo.ListToolsRequest{})
					toolsCancel()
					if err != nil {
						return fmt.Errorf("failed to list tools: %w", err)
					}

					fmt.Printf("Connection successful. Discovered %d tool(s):\n", len(listResult.Tools))
					for _, t := range listResult.Tools {
						fmt.Printf("- %s: %s\n", t.Name, t.Description)
					}

					return nil
				},
			},
		},
	}
}
