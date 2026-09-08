// Command mockmcpserver is a minimal in-process MCP server used by the
// client-factory tests: the tests compile it as a child binary and drive it
// over stdio (the mcp-go client test-suite pattern). It exposes:
//
//	test_tool  — returns a fixed text result
//	echo_env   — returns the value of ONCLAW_MCP_TEST_MARKER (env passing)
//	echo_args  — returns its own command-line args (args passing)
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("onclaw-mock", "1.0.0")

	s.AddTool(mcp.NewTool("test_tool"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("tool result"), nil
	})
	s.AddTool(mcp.NewTool("echo_env"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(os.Getenv("ONCLAW_MCP_TEST_MARKER")), nil
	})
	s.AddTool(mcp.NewTool("echo_args"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(strings.Join(os.Args[1:], " ")), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "mockmcpserver: %v\n", err)
		os.Exit(1)
	}
}
