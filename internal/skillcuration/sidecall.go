package skillcuration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The curation side-call seam (add-skill-curation-from-traces 4.2, design
// D7 "Curation model resolution"). The two model side-calls — wiki
// maintenance (this task) and proposal (task 5) — resolve their model per
// agent through the same factory and credential discipline the memory
// pipeline's seam uses (internal/memory/gate.go); the resolver lives here so
// skillcuration stays decoupled from memory's in-flight files.

// Model is the Eino agentic chat-model interface the curation side-calls
// generate through — the same type memory's side-call seam and the runner's
// conversation model share (internal/memory gate.go Model).
type Model = model.BaseModel[*schema.AgenticMessage]

// ModelFactory builds a side-call model from a workspace provider credential
// — the agents.DefaultAgenticModelFactory signature (the composition root
// passes that factory; importing internal/agents here would cycle once the
// runner wires the curation consumer, task 7).
type ModelFactory func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (Model, error)

// ModelResolver resolves one side-call model for a workspace per call —
// credentials stay tenant-scoped and re-resolve, never cached (the memory
// ModelResolver precedent). Tests stub the seam wholesale.
type ModelResolver func(ctx context.Context, workspaceID, agentID string) (Model, error)

// DefaultSideCallBudget bounds one curation side-call (the memory budget.go
// precedent: a classification budget with a survivable default, since a
// bounded side-call must never wedge the cycle). The curation config carries
// no ms-budget knob (2.2's contract), so the default is pinned here; the
// parent context's earlier deadline, if any, always wins.
const DefaultSideCallBudget = 30 * time.Second

// CurationModelResolver returns the provider-backed resolver implementing
// the spec's four-step fallback (design D7), most-specific-first:
//
//  1. the agent's explicit curation pair (SkillCurationProviderID/Model),
//  2. the agent's memory side-call pair (MemorySidecallProviderID/Model),
//  3. the workspace default — the curation settings' sidecall pair, falling
//     back to the memory settings' sidecall pair,
//  4. the model the agent itself runs.
//
// A half-set pair degrades to the next tier (mirroring memory's posture for
// a hand-edited settings row). There is no silent default provider: an
// unresolvable workspace errors, and the side-call stage fails soft.
func CurationModelResolver(providerStore store.ProviderStore, agents store.AgentStore, settings store.ToolSettingsStore, encryptionKey []byte, factory ModelFactory, cfg Config) ModelResolver {
	return func(ctx context.Context, workspaceID, agentID string) (Model, error) {
		// Curation side-calls are always agent-anchored: the sampled runs
		// and the wiki both belong to one agent's procedure.
		if agentID == "" {
			return nil, errors.New("skillcuration side-call: no agent to resolve the model for")
		}
		agent, err := agents.ByID(ctx, workspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("skillcuration side-call: agent: %w", err)
		}

		// 1. The agent pinned the curation pair.
		if agent.SkillCurationProviderID != "" && agent.SkillCurationModel != "" {
			provider, err := providerStore.ByID(ctx, workspaceID, agent.SkillCurationProviderID)
			if err != nil {
				return nil, fmt.Errorf("skillcuration side-call: curation override provider: %w", err)
			}
			return buildCurationModel(ctx, encryptionKey, factory, provider, agent.SkillCurationModel, workspaceID)
		}
		// 2. The agent pinned the memory side-call pair.
		if agent.MemorySidecallProviderID != "" && agent.MemorySidecallModel != "" {
			provider, err := providerStore.ByID(ctx, workspaceID, agent.MemorySidecallProviderID)
			if err != nil {
				return nil, fmt.Errorf("skillcuration side-call: memory override provider: %w", err)
			}
			return buildCurationModel(ctx, encryptionKey, factory, provider, agent.MemorySidecallModel, workspaceID)
		}
		// 3. The workspace default: the curation settings' pair, else the
		// memory settings' pair.
		providerID, modelName := "", ""
		if cfg.SidecallProviderID != "" && cfg.SidecallModel != "" {
			providerID, modelName = cfg.SidecallProviderID, cfg.SidecallModel
		} else {
			providerID, modelName, _ = workspaceCurationFallbackModel(ctx, settings, workspaceID)
		}
		if providerID != "" && modelName != "" {
			provider, err := providerStore.ByID(ctx, workspaceID, providerID)
			if err != nil {
				return nil, fmt.Errorf("skillcuration side-call: workspace default provider: %w", err)
			}
			return buildCurationModel(ctx, encryptionKey, factory, provider, modelName, workspaceID)
		}
		// 4. The model the agent itself runs — the one model the workspace
		// has already proven it can run.
		provider, err := providerStore.ByID(ctx, workspaceID, agent.ProviderID)
		if err != nil {
			return nil, fmt.Errorf("skillcuration side-call: agent provider: %w", err)
		}
		return buildCurationModel(ctx, encryptionKey, factory, provider, agent.Model, workspaceID)
	}
}

// workspaceCurationFallbackModel reads the memory settings record's side-call
// choice as the curation workspace default's fallback tier. Replicated from
// the memory gate's workspaceSideCallModel (internal/memory/gate.go:175 —
// unexported there, and memory's files are in-flight): flat keys
// "sidecall_provider_id"/"sidecall_model" on the "memory" ToolSettings row;
// ok=false (empty pair) when unset, half-set, or unreadable — the next tier
// resolves instead of failing.
func workspaceCurationFallbackModel(ctx context.Context, settings store.ToolSettingsStore, workspaceID string) (providerID, modelName string, ok bool) {
	if settings == nil {
		return "", "", false
	}
	row, err := settings.Get(ctx, workspaceID, "memory")
	if err != nil || row == nil || row.Config == nil {
		return "", "", false
	}
	providerID, _ = row.Config["sidecall_provider_id"].(string)
	modelName, _ = row.Config["sidecall_model"].(string)
	if providerID == "" || modelName == "" {
		return "", "", false
	}
	return providerID, modelName, true
}

// buildCurationModel decrypts the provider credential with the workspace id
// as AAD and builds the model through the shared agentic factory. Replicated
// from the memory gate's buildSideCallModel (internal/memory/gate.go:210).
func buildCurationModel(ctx context.Context, encryptionKey []byte, factory ModelFactory, provider *domain.ProviderConfig, modelName, workspaceID string) (Model, error) {
	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(encryptionKey, []byte(workspaceID), provider.KeyCiphertext)
		if err != nil {
			return nil, errors.New("skillcuration side-call: decrypt provider credentials")
		}
		apiKey = string(plaintext)
	}
	m, err := factory(ctx, provider.Type, providers.Credential{
		Type:    provider.Type,
		BaseURL: provider.BaseURL,
		APIKey:  apiKey,
	}, modelName)
	if err != nil {
		return nil, fmt.Errorf("skillcuration side-call: build model: %w", err)
	}
	return m, nil
}

// generateSideCallText performs one curation side-call: system+user in, the
// response's generated text out. Side-calls run with no tools — free text is
// the only channel; reasoning and tool plumbing are never read. Replicated
// from the memory gate's generateText (internal/memory/gate.go:248).
func generateSideCallText(ctx context.Context, m Model, system, user string) (string, error) {
	out, err := m.Generate(ctx, []*schema.AgenticMessage{
		schema.SystemAgenticMessage(system),
		schema.UserAgenticMessage(user),
	})
	if err != nil {
		return "", err
	}
	if out == nil {
		return "", errors.New("skillcuration side-call: empty model response")
	}
	var sb []byte
	for _, block := range out.ContentBlocks {
		if block != nil && block.AssistantGenText != nil {
			sb = append(sb, block.AssistantGenText.Text...)
		}
	}
	return string(sb), nil
}
