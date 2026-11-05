package agents

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The MCP seam (spec agent-mcp): an agent sees an enabled MCP server's tools
// when its server list is empty — every enabled server, the default — or
// names that server; a non-empty list restricts the agent to the named
// servers, the same allowlist reading as agent.Tools and agent.Skills
// (design D3). The server list — not the built-in tool allowlist and not the
// Tools-pane global toggles — is the whole gate for MCP tools (design D3/D7).

// noopMCPServer is the default MCP tool source: no connections, so
// resolution contributes zero MCP tools. It exists so the manager holds no
// nil dependencies at the point of use (composition-root wiring replaces it
// through WithMCPManager).
type noopMCPServer struct{}

func (noopMCPServer) Tools(context.Context, mcp.Ref) ([]tool.BaseTool, error) {
	return nil, nil
}

// MCPServerRef adapts a stored server definition into the mcp package's Ref.
// Shared by the turn resolution and the settings service's probe so both
// dial from exactly the same view of a row. An OAuth-mode row (design D4)
// hydrates the connection's OAuth settings from the stored registration,
// with tokens flowing through the NewMCPTokenStore adapter; a none-mode row
// keeps the zero value — the static-header dial.
func MCPServerRef(server store.MCPServer, tokens store.MCPOAuthTokenStore) mcp.Ref {
	ref := mcp.Ref{
		ServerID: server.ID,
		Name:     server.Name,
		Conn: mcp.Connection{
			Transport: server.Transport,
			Command:   server.Command,
			Args:      slices.Clone(server.Args),
			Env:       maps.Clone(server.Env),
			URL:       server.URL,
			Headers:   maps.Clone(server.Headers),

			// The integration credential rides the auth JSON (design D4) and
			// is injected into the request only at dial time (design D3) —
			// never into Headers, so it never reaches the frontend.
			IntegrationCredential: server.Auth.IntegrationCredential,
			CredentialHeaderStyle: server.Auth.CredentialHeaderStyle,
		},
	}
	if server.Auth.IsOAuth() {
		ref.Conn.OAuth = mcp.OAuth{
			Enabled:               true,
			ClientID:              server.Auth.ClientID,
			ClientSecret:          server.Auth.ClientSecret,
			RedirectURI:           server.Auth.RedirectURI,
			Scopes:                slices.Clone(server.Auth.Scopes),
			AuthServerMetadataURL: server.Auth.AuthServerMetadataURL,
			Tokens:                NewMCPTokenStore(server.ID, tokens),
		}
	}
	return ref
}

// reservedToolPrefix marks the parent↔child internal channel's tools
// (add-browser-login-handover design D7): __deliver_secret, __begin_takeover,
// __resolve_attention, and the preparation tools behind the intercepted
// submit_secret/request_takeover surface. The model never sees them.
const reservedToolPrefix = "__"

// filterReservedTools drops tools whose raw name carries the reserved prefix.
// It always allocates a fresh slice: the returned slice is owned by the
// connection cache, and filtering in place would corrupt it.
func filterReservedTools(ctx context.Context, tools []tool.BaseTool) []tool.BaseTool {
	kept := make([]tool.BaseTool, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			kept = append(kept, t)
			continue
		}
		if strings.HasPrefix(info.Name, reservedToolPrefix) {
			continue
		}
		kept = append(kept, t)
	}
	return kept
}

// resolveMCPTools appends the agent's MCP servers' tools after the built-ins
// (design D7): every enabled server when the agent's list is empty, otherwise
// the enabled servers it names, in store list order — the pass-shared
// mcp.Namer makes collision suffixes deterministic given that stable order. A
// per-server connection failure contributes zero tools and never fails the
// turn (design D7: skip and let the probe surface the failure); a store-level
// read failure is infrastructure failure and fails the resolution like any
// other store error.
//
// Every server's tool list passes the reserved-prefix filter BEFORE the
// naming pass (add-browser-login-handover design D7): the `__` tools are the
// parent↔child channel and never reach the model.
//
// The app-synthesized system server (browser-use) rides the same gate after
// the store rows (design D6): always enabled, it is offered to every agent by
// default and only to agents that name it once a list exists — the system
// row has no Enabled toggle to consult by construction. Its
// submit_secret/request_takeover are additionally wrapped with the
// session-aware attention interceptor (at, design D3/D7), so those calls park
// the turn on an attention card instead of reaching the child directly.
func (m *manager) resolveMCPTools(ctx context.Context, agent store.Agent, at attentionScope) ([]tool.BaseTool, error) {
	servers, err := m.store.MCPServers().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve mcp servers: %w", err)
	}

	unrestricted := len(agent.MCPServers) == 0
	var tools []tool.BaseTool
	namer := mcp.NewNamer()
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		if !unrestricted && !slices.Contains(agent.MCPServers, server.ID) {
			continue
		}
		serverTools, err := m.mcpManager.Tools(ctx, MCPServerRef(server, m.store.MCPOAuthTokens()))
		if err != nil {
			slog.WarnContext(ctx, "mcp server unavailable; skipping its tools",
				"server", server.Name, "error", err)
			continue
		}
		tools = append(tools, mcp.ApplyNames(server.Name, filterReservedTools(ctx, serverTools), namer)...)
	}
	if src, ok := m.mcpManager.(mcp.SystemServerSource); ok {
		if ref, ok := src.SystemServer(); ok {
			if unrestricted || slices.Contains(agent.MCPServers, ref.ServerID) {
				serverTools, err := m.mcpManager.Tools(ctx, ref)
				if err != nil {
					slog.WarnContext(ctx, "system mcp server unavailable; skipping its tools",
						"server", ref.ServerID, "error", err)
				} else {
					metaed := m.wrapSessionDirTools(ref, serverTools, agent, at)
					named := mcp.ApplyNames(ref.Name, filterReservedTools(ctx, metaed), namer)
					tools = append(tools, m.wrapAttentionTools(ref, named, at)...)
				}
			}
		}
	}
	return tools, nil
}

// MetaScreenshotDir mirrors internal/browseruse.MetaScreenshotDir (the mcp
// seam stays import-decoupled from the engine, like the child's reason-code
// vocabulary does): the _meta key under which every parent-forwarded
// browser-use call carries the calling session's workspace screenshots
// directory.
const MetaScreenshotDir = "buddies.dev/screenshot-dir"

// metaInvoker is the slice of *mcp.MCPManager the session-dir wrap needs:
// the usual tool source plus the meta-carrying direct invoke. The noop and
// plain test doubles need not implement it, so the wrap degrades to the
// unwrapped tools when the manager can't carry meta.
type metaInvoker interface {
	mcp.ToolSource
	Invoke(ctx context.Context, ref mcp.Ref, name, argsJSON string, meta map[string]any) (string, error)
}

// wrapSessionDirTools wraps the system server's tools so every model-invoked
// call carries the calling session's workspace screenshots directory through
// the call's _meta (never the arguments — arguments land in the transcript,
// the metadata does not, and the model can neither see nor influence it).
// The child's screenshot tool writes captures there — inside the workspace
// jail — so the model can read its own screenshot back with read_file
// (add-browser-login-handover follow-up). A manager without the meta
// invoker, an agent without a workspace, or a scope without a session
// degrades to the unwrapped tools: the child then falls back to its own
// browser screenshots directory.
func (m *manager) wrapSessionDirTools(ref mcp.Ref, serverTools []tool.BaseTool, agent store.Agent, at attentionScope) []tool.BaseTool {
	inv, ok := m.mcpManager.(metaInvoker)
	if !ok || m.workspaces == nil || agent.Slug == "" || at.sessionID == "" {
		return serverTools
	}
	dir, err := m.workspaces.SessionDir(agent.Slug, at.sessionID)
	if err != nil {
		return serverTools
	}
	browser := filepath.Join(dir, workspaces.DirBrowser)
	out := make([]tool.BaseTool, 0, len(serverTools))
	for _, t := range serverTools {
		if invk, isInv := t.(tool.InvokableTool); isInv {
			out = append(out, &sessionDirTool{
				InvokableTool: invk,
				ref:           ref,
				invoker:       inv,
				dir:           browser,
			})
			continue
		}
		out = append(out, t)
	}
	return out
}

// sessionDirTool routes one system server tool's invocations through the
// manager's meta-carrying Invoke, stamping the session's workspace
// screenshots directory. Info passes through so the naming pass and the
// reserved-tool filter see the original tool.
type sessionDirTool struct {
	tool.InvokableTool
	ref     mcp.Ref
	invoker metaInvoker
	dir     string
}

func (t *sessionDirTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	info, err := t.InvokableTool.Info(ctx)
	if err != nil || info == nil {
		return "", fmt.Errorf("browser session wrap: resolve tool name: %w", err)
	}
	return t.invoker.Invoke(ctx, t.ref, info.Name, argumentsInJSON, map[string]any{
		MetaScreenshotDir: t.dir,
	})
}
