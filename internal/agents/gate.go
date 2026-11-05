package agents

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The tool gate (spec agent-tools): a built-in tool is offered to a turn only
// when it is in the catalog, enabled globally in the Tools pane, and permitted
// by the agent's tool allowlist (an empty allowlist permits all catalog
// tools). Resolution is the first stage of the turn pipeline
// (resolve → compose → execute, design D1): it hands compose the offered
// direct tools plus the filesystem capability's offer/withhold configuration.

// resolvedTools is one turn's gated tool surface — the product of resolve and
// the input to compose.
type resolvedTools struct {
	// tools are the direct tools offered to the model through the agent's
	// ToolsConfig, in catalog order.
	tools []tool.BaseTool
	// agentDir is the agent's workspace directory when any workspace-rooted
	// tool (filesystem, shell, delete_file, plantask) is offered; empty
	// otherwise — no workspace, no filesystem capability.
	agentDir string
	// shell offers the filesystem middleware's jailed execute tool.
	shell bool
	// fsDisabled lists the filesystem middleware tool names the gate withholds.
	fsDisabled []string
	// plantaskDisabled lists the plantask middleware tool names the gate withholds.
	plantaskDisabled []string
	// knowledge is the read-only corpus mount exposed through the filesystem
	// capability (add-knowledge-corpus design D7): non-nil only when a
	// knowledge store is wired, the agent is workspace-rootable, and at
	// least one filesystem tool is offered to read the corpus with.
	knowledge *backend.KnowledgeRoots
	// createDocument reports whether the create_document tool is offered to
	// the turn (add-create-document design D6): the composed context's
	// document-preset appendix rides it — no tool offered, no appendix.
	createDocument bool
}

// names returns every offered tool name in catalog order. It exists for
// tests and diagnostics; resolution itself never needs it.
func (r *resolvedTools) names(ctx context.Context) ([]string, error) {
	offered := make(map[string]bool)
	for _, t := range r.tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolve tool names: %w", err)
		}
		offered[info.Name] = true
	}
	withheld := make(map[string]bool, len(r.fsDisabled)+len(r.plantaskDisabled))
	for _, name := range r.fsDisabled {
		withheld[name] = true
	}
	for _, name := range r.plantaskDisabled {
		withheld[name] = true
	}
	// The fs and plantask middlewares' tools exist only when the jail was rooted.
	if r.agentDir != "" {
		for _, name := range filesystemToolNames {
			if !withheld[name] {
				offered[name] = true
			}
		}
		for _, name := range plantaskToolNames {
			if !withheld[name] {
				offered[name] = true
			}
		}
		if r.shell {
			offered[ToolExecute] = true
		}
	}

	names := make([]string, 0, len(offered))
	for _, entry := range catalogEntries {
		if offered[entry.meta.Name] {
			names = append(names, entry.meta.Name)
		}
	}
	return names, nil
}

// resolveTurnTools applies the three-way gate over the catalog and builds the
// turn's tool surface. Global state (store.ToolStates) is read per tool; the
// agent's allowlist is an empty-means-all set. Workspace-rooted tools
// additionally require a rootable workspace, and the scheduler tool a
// schedule service: an agent without a slug, a runtime without a workspaces
// source, or a runtime built without a scheduler has those tools withheld
// (null-object degradation, like absent persona documents) — only a real IO
// failure while materializing the workspace directory fails the turn.
// sessionID is the calling session's id, scoping the session-directory tools
// (create_document, edit_document) to that conversation's artifacts.
func (m *manager) resolveTurnTools(ctx context.Context, agent store.Agent, sessionID string) (*resolvedTools, error) {
	allowed := make(map[string]struct{}, len(agent.Tools))
	for _, name := range agent.Tools {
		allowed[name] = struct{}{}
	}
	unrestricted := len(agent.Tools) == 0

	// Pass 1: the offered name set (catalog × allowlist × global enable).
	offered := make(map[string]bool, len(catalogEntries))
	for _, entry := range catalogEntries {
		if !unrestricted {
			if _, ok := allowed[entry.meta.Name]; !ok {
				continue
			}
		}
		enabled, err := m.store.ToolStates().Enabled(ctx, entry.meta.Name)
		if err != nil {
			return nil, fmt.Errorf("resolve tool %s: %w", entry.meta.Name, err)
		}
		if enabled {
			offered[entry.meta.Name] = true
		}
	}

	// Workspace rootability: a slugless agent owns no workspace at all (the
	// prompts.go convention), and a runtime without a workspaces source has
	// nowhere to root one.
	rootable := agent.Slug != "" && m.workspaces != nil
	res := &resolvedTools{}
	// Pre-resolve the workspace directory once: the delegation capability
	// needs it whether or not any filesystem tool is offered (child
	// transcripts live under .tasks/ beside the prompt documents).
	if rootable {
		dir, err := m.workspaceDir(agent)
		if err != nil {
			return nil, err
		}
		res.agentDir = dir
	}

	// Pass 2: materialize the surface in catalog order.
	for _, entry := range catalogEntries {
		if !offered[entry.meta.Name] {
			continue
		}
		if entry.workspaceScoped && res.agentDir == "" {
			continue // not rootable: withheld, not a failure
		}
		if entry.meta.Name == ToolScheduler && m.scheduler == nil {
			continue // no schedule service: withheld, not a failure (spec add-scheduler)
		}
		if entry.meta.Name == ToolSaveDocument && m.knowledge == nil {
			continue // no knowledge store: withheld, not a failure (spec add-knowledge-corpus)
		}
		switch entry.kind {
		case toolMiddleware:
			// offered: stays out of fsDisabled and plantaskDisabled (computed below).
		case toolShell:
			res.shell = true
		case toolDirect:
			if entry.meta.Name == ToolCreateDocument {
				// The gate admitted the workspace-scoped tool: the turn's
				// composed context may carry the preset appendix (design D6).
				res.createDocument = true
			}
			t, err := m.buildDirectTool(entry.meta.Name, toolScope{AgentID: agent.ID, Slug: agent.Slug, Workspace: res.agentDir, SessionID: sessionID})
			if err != nil {
				return nil, fmt.Errorf("resolve tool %s: %w", entry.meta.Name, err)
			}
			res.tools = append(res.tools, t)
		}
	}

	// The fs and plantask middlewares attach whenever the jail exists; every tool
	// the gate withheld is disabled via per-tool config.
	if res.agentDir != "" {
		for _, name := range filesystemToolNames {
			if !offered[name] {
				res.fsDisabled = append(res.fsDisabled, name)
			}
		}
		for _, name := range plantaskToolNames {
			if !offered[name] {
				res.plantaskDisabled = append(res.plantaskDisabled, name)
			}
		}
		// Knowledge corpus exposure (add-knowledge-corpus design D7): the
		// /knowledge mount rides the jail only when there is a corpus to
		// expose and at least one filesystem tool offered to read it with.
		if m.knowledge != nil && len(res.fsDisabled) < len(filesystemToolNames) {
			roots := m.knowledge.Roots()
			res.knowledge = &backend.KnowledgeRoots{GlobalDir: roots.Global, AgentDir: m.knowledge.AgentDir(agent.Slug)}
		}
	}
	return res, nil
}

// workspaceDir materializes the agent's workspace directory on demand
// (idempotent mkdir -p): the jailed fs tools and shell require an existing
// root, and legacy agents may predate their workspace.
func (m *manager) workspaceDir(agent store.Agent) (string, error) {
	dir, err := m.workspaces.Create(agent.Slug)
	if err != nil {
		return "", fmt.Errorf("resolve workspace for agent %q: %w", agent.Slug, err)
	}
	return dir, nil
}

// mergeGroupTools appends the source-provided in-channel group tools to the
// turn's resolved ordinary surface (fix-agent-group-member-tools design D1,
// spec agent-groups "Group tools ride beside the work tools"): the member's
// gated catalog surface and the group tools are offered together, the group
// tools never displacing the ordinary ones. The merge never shadows: a group
// tool whose name is already taken by the composed surface — a catalog tool,
// a workspace middleware tool, or a delegation tool — fails the turn with a
// clear error instead of silently replacing the ordinary tool.
func mergeGroupTools(surface *resolvedTools, groupTools []tool.BaseTool) ([]tool.BaseTool, error) {
	taken := make(map[string]bool, len(surface.tools)+len(filesystemToolNames)+len(plantaskToolNames)+3)
	for _, t := range surface.tools {
		if t == nil {
			continue
		}
		info, err := t.Info(context.Background())
		if err != nil {
			continue
		}
		taken[info.Name] = true
	}
	// The fs and plantask middlewares' tools exist beside the direct ones
	// whenever the jail was rooted; the shell answers to execute, and the
	// delegation tools are always reserved names.
	if surface.agentDir != "" {
		for _, name := range filesystemToolNames {
			taken[name] = true
		}
		for _, name := range plantaskToolNames {
			taken[name] = true
		}
		if surface.shell {
			taken[ToolExecute] = true
		}
	}
	taken[ToolAgent] = true
	taken[ToolTaskOutput] = true
	taken[ToolTaskStop] = true

	merged := make([]tool.BaseTool, 0, len(surface.tools)+len(groupTools))
	merged = append(merged, surface.tools...)
	for _, t := range groupTools {
		if t == nil {
			continue
		}
		info, err := t.Info(context.Background())
		if err != nil {
			return nil, fmt.Errorf("resolve group tool: %w", err)
		}
		if taken[info.Name] {
			return nil, fmt.Errorf("group tool %q collides with the turn's ordinary tool surface", info.Name)
		}
		taken[info.Name] = true
		merged = append(merged, t)
	}
	return merged, nil
}
