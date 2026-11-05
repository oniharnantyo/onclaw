package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
)

// The system server allowlist tests (rebrand-browser-use-system-mcp task 3.3,
// spec agent-mcp "System-provided server", scenario "Agent allowlist gates
// the system server"): the synthesized browser-use server rides the standard
// per-agent server allowlist — offered by default, offered when named, and
// fully absent (never consulted at the connection layer) when an agent's
// list excludes it.
//
// systemMCPSource decorates the turn tests' stub with the optional
// SystemServerSource capability the resolution type-asserts.
type systemMCPSource struct {
	*stubMCPSource
	ref mcp.Ref
	has bool
}

func (s *systemMCPSource) SystemServer() (mcp.Ref, bool) { return s.ref, s.has }

func newSystemMCPSource() *systemMCPSource {
	return &systemMCPSource{
		stubMCPSource: &stubMCPSource{
			tools: map[string][]tool.BaseTool{},
			errs:  map[string]error{},
		},
		ref: mcp.Ref{ServerID: mcp.SystemServerID, Name: mcp.SystemServerID},
		has: true,
	}
}

// mcpToolNames reads a resolved surface's tool names.
func mcpToolNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		require.NoError(t, err)
		names = append(names, info.Name)
	}
	return names
}

// TestResolveMCPToolsSystemServerOfferedByDefault pins the default-on
// posture: an agent naming no servers is offered the system server's tools
// without any store row (spec agent-mcp "available to every agent by
// default").
func TestResolveMCPToolsSystemServerOfferedByDefault(t *testing.T) {
	src := newSystemMCPSource()
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	src.tools[mcp.SystemServerID] = []tool.BaseTool{fakeMCPTool{raw: "navigate"}}

	require.Empty(t, f.agent.MCPServers, "fixture premise: the agent names no servers")
	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{})
	require.NoError(t, err)
	require.Contains(t, mcpToolNames(t, tools), "mcp__browser-use__navigate")
	require.Contains(t, src.seenServerIDs(), mcp.SystemServerID)
}

// TestResolveMCPToolsSystemServerOfferedWhenNamed pins the explicit-list
// inclusion: an agent whose list names browser-use keeps the system tools
// alongside its named user servers.
func TestResolveMCPToolsSystemServerOfferedWhenNamed(t *testing.T) {
	src := newSystemMCPSource()
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	src.tools[mcp.SystemServerID] = []tool.BaseTool{fakeMCPTool{raw: "navigate"}}

	// A stored user server, listed alongside the system server.
	stored := f.seedMCPServer(t, "github", true, false)
	src.tools[stored.ID] = []tool.BaseTool{fakeMCPTool{raw: "create_issue"}}
	f.agent.MCPServers = []string{stored.ID, mcp.SystemServerID}

	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{})
	require.NoError(t, err)
	names := mcpToolNames(t, tools)
	require.Contains(t, names, "mcp__browser-use__navigate")
	require.Contains(t, names, "mcp__github__create_issue")
}

// TestResolveMCPToolsSystemServerExcludedByList pins the exclusion gate (spec
// scenario "Agent allowlist gates the system server"): an agent whose list
// names other servers sees no mcp__browser-use__* tools, and the system
// server never reaches the connection layer.
func TestResolveMCPToolsSystemServerExcludedByList(t *testing.T) {
	src := newSystemMCPSource()
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	src.tools[mcp.SystemServerID] = []tool.BaseTool{fakeMCPTool{raw: "navigate"}}

	stored := f.seedMCPServer(t, "github", true, false)
	src.tools[stored.ID] = []tool.BaseTool{fakeMCPTool{raw: "create_issue"}}
	f.agent.MCPServers = []string{stored.ID} // browser-use not named

	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{})
	require.NoError(t, err)
	names := mcpToolNames(t, tools)
	require.Contains(t, names, "mcp__github__create_issue")
	require.False(t, hasMCPPrefixMatching(names, "mcp__browser-use__"),
		"no browser-use tool may be offered")
	require.NotContains(t, src.seenServerIDs(), mcp.SystemServerID,
		"the excluded system server must never reach the connection layer")
}

// TestResolveMCPToolsSystemServerUnavailableSkipped pins the degradation: a
// system server whose connection layer reports unavailable contributes zero
// tools and never fails the resolution (the pane's supervision state shows
// the failure).
func TestResolveMCPToolsSystemServerUnavailableSkipped(t *testing.T) {
	src := newSystemMCPSource()
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src))
	src.errs[mcp.SystemServerID] = errors.New("browser-use system server is starting")

	tools, err := f.mgr.resolveMCPTools(t.Context(), f.agent, attentionScope{})
	require.NoError(t, err)
	require.Empty(t, tools)
}

// hasMCPPrefixMatching reports whether any offered name carries the prefix.
func hasMCPPrefixMatching(names []string, prefix string) bool {
	for _, name := range names {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// Compile-time check: the decorated stub still satisfies ToolSource.
var _ mcp.ToolSource = (*systemMCPSource)(nil)
