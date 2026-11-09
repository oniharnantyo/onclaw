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
// The app-synthesized system servers ride the same gate after the store rows
// (design D6; add-computer-use design D2 generalizes the single seam to a
// registry): always enabled — no Enabled toggle to consult by construction —
// each offered under the standard allowlist reading (every server when the
// list is empty, else the named ones) with the registry's opt-in rule on top:
// an opt-in spec is offered ONLY to agents that name it, even when the list
// is empty, so registering one never silently widens agents whose allowlist
// is empty. Each server's tools pass the same wrap pipeline, in order:
// session-dir meta wrap, reserved-prefix filter, the naming pass, then the
// session-aware attention interceptor (at, design D3/D7) — so
// submit_secret/request_takeover park the turn on an attention card instead
// of reaching the child directly — and, for the computer-use server only,
// the approval gate last (add-computer-use design D7): the gated subset
// parks the turn on an approval card before reaching the adapter.
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
		for _, entry := range src.SystemServers() {
			// Offered iff the agent names the server, or the list is empty
			// and the spec is not opt-in (add-computer-use design D2).
			if !slices.Contains(agent.MCPServers, entry.Ref.ServerID) && (!unrestricted || entry.OptIn) {
				continue
			}
			serverTools, err := m.mcpManager.Tools(ctx, entry.Ref)
			if err != nil {
				slog.WarnContext(ctx, "system mcp server unavailable; skipping its tools",
					"server", entry.Ref.ServerID, "error", err)
				continue
			}
			metaed := m.wrapSessionDirTools(entry.Ref, serverTools, agent, at)
			named := mcp.ApplyNames(entry.Ref.Name, filterReservedTools(ctx, metaed), namer)
			attended := m.wrapAttentionTools(entry.Ref, named, at)
			// The approval gate rides the computer-use server only (design
			// D7): the browser-use surface carries no gated subset, and the
			// gate is ordered AFTER the attention wrap.
			if entry.Ref.ServerID == computerUseServerID {
				attended = m.wrapApprovalTools(attended, at)
			}
			tools = append(tools, attended...)
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

// MetaScreenshotRel is the _meta key carrying the workspace-relative facing
// path of the stamped directory (add-computer-use coordination contract C1):
// `sessions/<session id>/computer` for computer-use calls. The adapter's
// marker text and the observation lane read the relative form, so a
// relocated workspace root cannot strand a stale absolute path in the
// persisted transcript. Browser-use calls keep stamping the absolute
// directory only.
const MetaScreenshotRel = "buddies.dev/screenshot-rel"

// MetaProfileDir and MetaProfileName mirror internal/browseruse's binding
// keys (add-per-agent-browser-profiles design D1; the mcp seam stays
// import-decoupled from the engine, like MetaScreenshotDir): the _meta keys
// under which the parent binds a browser-use call to an agent's own profile —
// the absolute workspace profile directory and the display name (the agent
// slug). Host-stamped like every meta key: the model can neither see nor
// influence them.
const (
	MetaProfileDir  = "buddies.dev/profile-dir"
	MetaProfileName = "buddies.dev/profile-name"
)

// metaInvoker is the slice of *mcp.MCPManager the session-dir wrap needs:
// the usual tool source plus the meta-carrying direct invoke. The noop and
// plain test doubles need not implement it, so the wrap degrades to the
// unwrapped tools when the manager can't carry meta.
type metaInvoker interface {
	mcp.ToolSource
	Invoke(ctx context.Context, ref mcp.Ref, name, argsJSON string, meta map[string]any) (string, error)
}

// agentProfileBinding resolves the agent's browser profile binding
// (add-per-agent-browser-profiles design D2): the agent-level profile
// directory inside its workspace and the display name — the slug. A manager
// without workspaces, a slug-less agent, or a resolution failure degrades to
// the empty binding: the call then runs unbound (the child's named-profile
// behavior), never half-bound.
func (m *manager) agentProfileBinding(agent store.Agent) (profileDir, profileName string) {
	if m.workspaces == nil || agent.Slug == "" {
		return "", ""
	}
	dir, err := m.workspaces.BrowserProfileDir(agent.Slug)
	if err != nil {
		return "", ""
	}
	return dir, agent.Slug
}

// wrapSessionDirTools wraps the system server's tools so every model-invoked
// call carries the calling session's workspace screenshots directory through
// the call's _meta (never the arguments — arguments land in the transcript,
// the metadata does not, and the model can neither see nor influence it).
// The child's screenshot tool writes captures there — inside the workspace
// jail — so the model can read its own screenshot back with read_file
// (add-browser-login-handover follow-up). The stamped directory is
// per-server (add-computer-use C1): browser-use keeps the session's browser
// screenshots directory (unchanged behavior), computer-use gets the
// session's computer screenshots directory plus the workspace-relative
// facing path under MetaScreenshotRel. The agent's browser profile binding
// rides the browser-use server only (add-per-agent-browser-profiles design
// D5): computer-use and any other registered system server carry neither
// binding key. A manager without the meta invoker, an agent without a
// workspace, or a scope without a session degrades to the unwrapped tools:
// the child then falls back to its own screenshots directory.
func (m *manager) wrapSessionDirTools(ref mcp.Ref, serverTools []tool.BaseTool, agent store.Agent, at attentionScope) []tool.BaseTool {
	inv, ok := m.mcpManager.(metaInvoker)
	if !ok || m.workspaces == nil || agent.Slug == "" || at.sessionID == "" {
		return serverTools
	}
	var dir, rel, profileDir, profileName string
	switch ref.ServerID {
	case computerUseServerID:
		computer, err := m.workspaces.SessionComputerDir(agent.Slug, at.sessionID)
		if err != nil {
			return serverTools
		}
		dir = computer
		rel = workspaces.SessionRel(at.sessionID, workspaces.DirComputer)
	default:
		// Browser-use (and any other registered system server) keeps the
		// session's browser screenshots directory — the pre-registry
		// behavior, bit-identical (add-computer-use design D2).
		session, err := m.workspaces.SessionDir(agent.Slug, at.sessionID)
		if err != nil {
			return serverTools
		}
		dir = filepath.Join(session, workspaces.DirBrowser)
		// The profile binding is gated explicitly to the browser-use system
		// server (add-per-agent-browser-profiles design D5), not to the
		// default branch: only its handlers read the binding keys. A binding
		// resolution failure degrades the two fields to empty — the
		// session-dir stamping above stays intact.
		if ref.ServerID == mcp.SystemServerID {
			profileDir, profileName = m.agentProfileBinding(agent)
		}
	}
	out := make([]tool.BaseTool, 0, len(serverTools))
	for _, t := range serverTools {
		if invk, isInv := t.(tool.InvokableTool); isInv {
			out = append(out, &sessionDirTool{
				InvokableTool: invk,
				ref:           ref,
				invoker:       inv,
				dir:           dir,
				rel:           rel,
				profileDir:    profileDir,
				profileName:   profileName,
			})
			continue
		}
		out = append(out, t)
	}
	return out
}

// sessionDirTool routes one system server tool's invocations through the
// manager's meta-carrying Invoke, stamping the session's workspace
// screenshots directory — plus the workspace-relative facing path when the
// server carries one (computer-use, C1). Info passes through so the naming
// pass and the reserved-tool filter see the original tool.
type sessionDirTool struct {
	tool.InvokableTool
	ref     mcp.Ref
	invoker metaInvoker
	dir     string
	// rel is the workspace-relative facing path stamped under
	// MetaScreenshotRel; empty keeps the meta to the absolute directory only
	// (browser-use, unchanged).
	rel string
	// profileDir/profileName are the agent's browser profile binding
	// (add-per-agent-browser-profiles design D1/D5, browser-use tools only):
	// the workspace profile directory and the display name stamped under
	// MetaProfileDir/MetaProfileName. Empty fields are left out of the meta —
	// an unbound call keeps the named-profile meta contract exactly.
	profileDir  string
	profileName string
}

func (t *sessionDirTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	info, err := t.InvokableTool.Info(ctx)
	if err != nil || info == nil {
		return "", fmt.Errorf("session dir wrap: resolve tool name: %w", err)
	}
	meta := map[string]any{MetaScreenshotDir: t.dir}
	if t.rel != "" {
		meta[MetaScreenshotRel] = t.rel
	}
	if t.profileDir != "" {
		meta[MetaProfileDir] = t.profileDir
	}
	if t.profileName != "" {
		meta[MetaProfileName] = t.profileName
	}
	return t.invoker.Invoke(ctx, t.ref, info.Name, argumentsInJSON, meta)
}
