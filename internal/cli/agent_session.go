package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	einoembedgemini "github.com/cloudwego/eino-ext/components/embedding/gemini"
	einoembedollama "github.com/cloudwego/eino-ext/components/embedding/ollama"
	einoembedopenai "github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"google.golang.org/genai"

	"github.com/oniharnantyo/onclaw/internal/agent"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/mcp"
	"github.com/oniharnantyo/onclaw/internal/memory"
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

	contextWindow := resolveContextWindow(agentConf.MaxContextTokens, st.cfg.MaxContextTokens, agentConf.ModelMetadata)

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
		mcpTools, err = mcpMgr.ToolsForAgent(ctx, req.AgentName)
		if err != nil {
			return nil, "", fmt.Errorf("retrieve mcp tools: %w", err)
		}
	}

	hookStore := sqlite.NewHookStore(db)
	execStore := sqlite.NewHookExecutionStore(db)
	toolRegistryStore := sqlite.NewToolRegistryStore(db)
	toolGroupConfigStore := sqlite.NewToolGroupConfigStore(db)
	kvStore := sqlite.NewKVStore(db)

	var memOver memory.AgentMemoryConfig
	if agentConf.MemoryConfig != "" {
		_ = json.Unmarshal([]byte(agentConf.MemoryConfig), &memOver)
	}

	resolvedMem := memOver.Resolve(
		st.cfg.Memory.Enabled,
		true, // default CuratedEnabled to true
		true, // default EpisodicEnabled to true
		st.cfg.Memory.KGEnabled,
		st.cfg.Memory.EmbeddingProvider,
		st.cfg.Memory.EmbeddingModel,
		true, // default SecurityScanEnabled to true
		true, // default ExtractionEnabled to true
		true, // default RetrievalEnabled to true
		true, // default DreamingEnabled to true
		st.cfg.Memory.WriteApproval,
	)

	var memoryStore memory.MemoryStore
	var coreStore memory.CoreStore
	var embedder *memory.Embedder
	var stagedWriteStore memory.StagedWriteStore
	var episodicStore memory.EpisodicStore
	var kgStore memory.KGStore
	if resolvedMem.Enabled {
		memoryStore = sqlite.NewMemoryStore(db)
		coreStore = memory.NewFileCoreStore(st.cfg.Memory.CharLimit)
		stagedWriteStore = sqlite.NewStagedWriteStore(db)
		episodicStore = sqlite.NewEpisodicStore(db)
		if st.cfg.Memory.KGEnabled {
			kgStore = sqlite.NewKGStore(db)
		}

		embedProvider := resolvedMem.EmbeddingProvider
		if embedProvider == "" {
			embedProvider = providerName
		}
		embedModel := resolvedMem.EmbeddingModel
		if embedModel == "" {
			if embedProvider == "openai" {
				embedModel = "text-embedding-3-small"
			} else if embedProvider == "gemini" {
				embedModel = "text-embedding-004"
			} else if embedProvider == "ollama" {
				embedModel = "nomic-embed-text"
			}
		}

		var embedAPIKey string
		var embedAPIBase string
		if ep, err := mgr.GetProfile(ctx, embedProvider); err == nil && ep != nil {
			embedAPIBase = ep.APIBase
			embedAPIKey, _ = mgr.GetSecret(ctx, embedProvider)
		}

		var einoProvider memory.EinoEmbedder
		switch embedProvider {
		case "gemini", "google":
			if embedAPIKey != "" {
				genaiClient, genaiErr := genai.NewClient(ctx, &genai.ClientConfig{
					APIKey:  embedAPIKey,
					Backend: genai.BackendGeminiAPI,
				})
				if genaiErr == nil {
					einoProvider, _ = einoembedgemini.NewEmbedder(ctx, &einoembedgemini.EmbeddingConfig{
						Client: genaiClient,
						Model:  embedModel,
					})
				}
			}
		case "ollama":
			einoProvider, _ = einoembedollama.NewEmbedder(ctx, &einoembedollama.EmbeddingConfig{
				BaseURL: embedAPIBase,
				Model:   embedModel,
			})
		default: // openai, agenticopenai, or any OpenAI-compatible provider
			if embedAPIKey != "" {
				einoProvider, _ = einoembedopenai.NewEmbedder(ctx, &einoembedopenai.EmbeddingConfig{
					APIKey:  embedAPIKey,
					BaseURL: embedAPIBase,
					Model:   embedModel,
				})
			}
		}

		// einoProvider may be nil (no API key, provider build failed) — FTS-only mode.
		embedder = memory.NewEmbedder(memoryStore, einoProvider, embedModel)
	}

	var reviewModel model.AgenticModel
	if st.cfg.Memory.ReviewModel != "" {
		reviewProfile, revErr := mgr.GetProfile(ctx, providerName)
		if revErr == nil {
			reviewModel, _ = mgr.BuildWithProfile(ctx, reviewProfile, st.cfg.Memory.ReviewModel)
		}
	}

	var dreamer *memory.Dreamer
	if episodicStore != nil {
		dreamer = memory.NewDreamer(
			episodicStore,
			coreStore,
			stagedWriteStore,
			reviewModel,
			req.AgentName,
			resolvedWorkspace,
			st.cfg.Memory.DreamThreshold,
			10*time.Minute,
			resolvedMem.StagedWriteApproval,
			st.cfg.Memory.ReviewModel,
		)
	}

	assembledAgent, err := agent.AssembleAgent(
		ctx,
		agentConf,
		chatModel,
		reviewModel,
		resolvedWorkspace,
		userConfigDir,
		st.cfg.Tools.Shell.Policy,
		st.cfg.Tools.Shell.Allowlist,
		st.cfg.Tools.Shell.Denylist,
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
		mgr,
		memoryStore,
		coreStore,
		embedder,
		stagedWriteStore,
		episodicStore,
		dreamer,
		kgStore,
		st.cfg.Memory.CharLimit,
		st.cfg.Memory.EpisodicTTLDays,
		db,
		st.cfg.Memory.KGTraversalDepth,
	)
	if err != nil {
		return nil, "", fmt.Errorf("assemble agent: %w", err)
	}

	return assembledAgent, resolvedWorkspace, nil
}

func resolveContextWindow(maxContextTokens int, globalMaxContextTokens int, modelMetadata string) int {
	return store.ResolveContextWindow(maxContextTokens, globalMaxContextTokens, modelMetadata)
}
