package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/components/tool"
	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"github.com/oniharnantyo/onclaw/internal/workspace"
)

type agentSessionRequest struct {
	AgentName    string
	ProviderName string
	ModelName    string
	Reasoning    string
	Workspace    string
	Channel      string
}

func resolveAndAssemble(ctx context.Context, st *appState, db *sql.DB, mgr *llm.Service, req agentSessionRequest, convStore store.ConversationStore, convID int64, mcpMgr mcp.Manager) (*agent.Agent, string, error) {
	// 1. Resolve agent configuration
	agentConf, err := mgr.GetAgent(ctx, req.AgentName)
	if err != nil {
		if req.AgentName == "master" {
			agentConf, err = st.getOrSeedMasterAgent(ctx, db, mgr)
			if err != nil {
				return nil, "", fmt.Errorf("failed to auto-seed master agent: %w", err)
			}
		} else {
			return nil, "", fmt.Errorf("agent %q not found: %w", req.AgentName, err)
		}
	}

	// 2. Resolve workspace
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("get current directory: %w", err)
	}

	resolvedWorkspace, err := workspace.ResolveWorkspace(
		req.Workspace,
		agentConf.Workspace,
		st.cfg.Workspace,
		cwd,
	)
	if err != nil {
		return nil, "", fmt.Errorf("resolve workspace: %w", err)
	}

	// 3. Build effective profile
	var defaultProvider string
	err = db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = 'default_provider'").Scan(&defaultProvider)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}

	providerName := req.ProviderName
	if providerName == "" {
		profiles, err := mgr.ListProfiles(ctx)
		if err != nil {
			return nil, "", err
		}

		var enabledCount int
		for _, pr := range profiles {
			if pr.Enabled != 0 {
				enabledCount++
			}
		}

		if enabledCount > 1 && defaultProvider == "" {
			return nil, "", fmt.Errorf("multiple providers available but no default provider is set; use 'onclaw provider use <name>' to set one")
		}

		if agentConf.Provider != "" {
			providerName = agentConf.Provider
		} else {
			providerName = defaultProvider
		}
	}

	if providerName == "" {
		return nil, "", fmt.Errorf("no provider specified for agent %q; configure a provider or use the --provider flag", req.AgentName)
	}

	p, err := mgr.GetProfile(ctx, providerName)
	if err != nil {
		return nil, "", fmt.Errorf("provider %q not found: %w", providerName, err)
	}
	if p.Enabled == 0 {
		return nil, "", fmt.Errorf("provider %q is disabled", providerName)
	}

	effModel := req.ModelName
	if effModel == "" {
		effModel = agentConf.Model
	}
	if effModel == "" {
		effModel = st.cfg.Model
	}
	if effModel == "" {
		return nil, "", fmt.Errorf("no model specified for agent %q and no default model is configured", req.AgentName)
	}

	effReasoning := req.Reasoning
	if effReasoning == "" {
		effReasoning = agentConf.ReasoningEffort
	}

	var contextWindow int
	if agentConf.ModelMetadata != "" {
		meta, err := store.UnmarshalModelMetadata(agentConf.ModelMetadata)
		if err == nil && meta != nil {
			contextWindow = meta.ContextWindow
		}
	}
	if contextWindow <= 0 {
		if st.cfg.MaxContextTokens > 0 {
			contextWindow = st.cfg.MaxContextTokens
		} else {
			contextWindow = 64000
		}
	}

	effProfile := *p

	var settings map[string]interface{}
	if effProfile.Settings != "" {
		_ = json.Unmarshal([]byte(effProfile.Settings), &settings)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	if effReasoning != "" {
		settings["reasoning_effort"] = effReasoning
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal settings: %w", err)
	}
	effProfile.Settings = string(settingsJSON)

	// 4. Build ChatModel and assemble agent
	chatModel, err := mgr.BuildWithProfile(ctx, &effProfile, effModel)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build model: %w", err)
	}

	resolvedDbPath, err := sqlite.ResolveDbPath(st.cfg.DbPath)
	if err != nil {
		return nil, "", err
	}
	userConfigDir := filepath.Dir(resolvedDbPath)

	var mcpTools []tool.BaseTool
	if mcpMgr != nil {
		mcpTools, err = mcpMgr.Tools(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("retrieve mcp tools: %w", err)
		}
	}

	hookStore := sqlite.NewHookStore(db)
	execStore := sqlite.NewHookExecutionStore(db)
	toolRegistryStore := sqlite.NewToolRegistryStore(db)
	toolGroupConfigStore := sqlite.NewToolGroupConfigStore(db)
	kvStore := sqlite.NewKVStore(db)

	assembledAgent, err := agent.AssembleAgent(
		ctx,
		agentConf,
		chatModel,
		resolvedWorkspace,
		userConfigDir,
		st.cfg.Tools.Shell.Policy,
		st.cfg.Tools.Shell.Allowlist,
		contextWindow,
		convStore,
		convID,
		mcpTools,
		hookStore,
		execStore,
		req.Channel,
		toolRegistryStore,
		&agent.ToolGroupCfgWrapper{Store: toolGroupConfigStore},
		kvStore,
	)
	if err != nil {
		return nil, "", fmt.Errorf("assemble agent: %w", err)
	}

	return assembledAgent, resolvedWorkspace, nil
}
