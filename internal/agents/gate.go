package agents

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"

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
	// tool (filesystem, shell, delete_file, todos) is offered; empty
	// otherwise — no workspace, no filesystem capability.
	agentDir string
	// shell offers the filesystem middleware's jailed execute tool.
	shell bool
	// fsDisabled lists the filesystem middleware tool names the gate withholds.
	fsDisabled []string
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
	withheld := make(map[string]bool, len(r.fsDisabled))
	for _, name := range r.fsDisabled {
		withheld[name] = true
	}
	// The fs middleware's tools exist only when the jail was rooted.
	if r.agentDir != "" {
		for _, name := range filesystemToolNames {
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
func (m *manager) resolveTurnTools(ctx context.Context, agent store.Agent) (*resolvedTools, error) {
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
		switch entry.kind {
		case toolMiddleware:
			// offered: stays out of fsDisabled (computed below).
		case toolShell:
			res.shell = true
		case toolDirect:
			t, err := m.buildDirectTool(entry.meta.Name, toolScope{AgentID: agent.ID, Workspace: res.agentDir})
			if err != nil {
				return nil, fmt.Errorf("resolve tool %s: %w", entry.meta.Name, err)
			}
			res.tools = append(res.tools, t)
		}
	}

	// The fs middleware attaches whenever the jail exists; every one of its
	// six tools the gate withheld is disabled via per-tool config.
	if res.agentDir != "" {
		for _, name := range filesystemToolNames {
			if !offered[name] {
				res.fsDisabled = append(res.fsDisabled, name)
			}
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
