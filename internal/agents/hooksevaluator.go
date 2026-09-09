package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// HookEvaluatorFactory adapts the workspace tenant-provider catalog plus the
// agentic model factory to the hook dispatcher's sandboxed-evaluator model
// port (design.md D12). It is the composition-root wiring for the prompt
// handler: provider and model come from the hook config explicitly, the
// credential resolves through the workspace's own providers (encrypted at
// rest, workspace ID as AAD — the same path runner.resolve uses), and no
// silent default model exists. Satisfies agenthooks.EvaluatorModelFactory.
type HookEvaluatorFactory struct {
	providers store.ProviderStore
	encKey    []byte
	factory   AgenticModelFactory
}

// NewHookEvaluatorFactory builds the evaluator factory from its three
// positional dependencies: the workspace provider catalog, the instance
// encryption key, and the agentic model factory the runner resolves models
// through.
func NewHookEvaluatorFactory(providers store.ProviderStore, encryptionKey []byte, factory AgenticModelFactory) *HookEvaluatorFactory {
	return &HookEvaluatorFactory{providers: providers, encKey: encryptionKey, factory: factory}
}

// EvaluatorModel resolves the hook config's explicit provider type to a
// workspace provider credential and builds a one-shot evaluator model bound
// to no tools until GenerateOnce binds the forced decide tool.
func (f *HookEvaluatorFactory) EvaluatorModel(ctx context.Context, workspaceID, providerType, modelName string) (agenthooks.EvaluatorModel, error) {
	list, err := f.providers.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("hook evaluator: list providers: %w", err)
	}
	var provider *domain.ProviderConfig
	for i := range list {
		if list[i].Type == providerType {
			provider = &list[i]
			break
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("hook evaluator: workspace has no %q provider", providerType)
	}

	var apiKey string
	if provider.KeyCiphertext != "" {
		plaintext, err := secrets.Decrypt(f.encKey, []byte(workspaceID), provider.KeyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("hook evaluator: decrypt provider credentials")
		}
		apiKey = string(plaintext)
	}
	cred := providers.Credential{
		Type:    provider.Type,
		BaseURL: provider.BaseURL,
		APIKey:  apiKey,
	}

	agenticModel, err := f.factory(ctx, provider.Type, cred, modelName)
	if err != nil {
		return nil, fmt.Errorf("hook evaluator: build evaluator model: %w", err)
	}
	return &hookEvaluatorModel{model: agenticModel}, nil
}

// hookEvaluatorModel is the one-shot evaluator model: a single Generate call
// with the forced decision tool, whose tool call — never free text — is the
// verdict (D12).
type hookEvaluatorModel struct {
	model Model
}

// GenerateOnce performs the evaluator call with toolDef as the response's
// only tool. A response without a tool call yields an empty ToolCall — the
// prompt handler treats that as evaluator failure (free text is the injection
// channel and is never read).
func (m *hookEvaluatorModel) GenerateOnce(ctx context.Context, system, user string, toolDef agenthooks.ToolDef) (agenthooks.ToolCall, agenthooks.TokenUsage, error) {
	var params jsonschema.Schema
	if len(toolDef.Parameters) > 0 {
		if err := json.Unmarshal(toolDef.Parameters, &params); err != nil {
			return agenthooks.ToolCall{}, agenthooks.TokenUsage{}, fmt.Errorf("hook evaluator: decode %s tool schema: %w", toolDef.Name, err)
		}
	}

	out, err := m.model.Generate(ctx,
		[]*schema.AgenticMessage{
			schema.SystemAgenticMessage(system),
			schema.UserAgenticMessage(user),
		},
		model.WithTools([]*schema.ToolInfo{{
			Name:        toolDef.Name,
			Desc:        toolDef.Description,
			ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&params),
		}}),
	)
	if err != nil {
		return agenthooks.ToolCall{}, agenthooks.TokenUsage{}, err
	}

	calls := agenticToolCalls(out)
	var call agenthooks.ToolCall
	if len(calls) > 0 {
		call = agenthooks.ToolCall{Name: calls[0].Name, Arguments: calls[0].Arguments}
	}

	var usage agenthooks.TokenUsage
	if out != nil && out.ResponseMeta != nil && out.ResponseMeta.TokenUsage != nil {
		u := out.ResponseMeta.TokenUsage
		usage = agenthooks.TokenUsage{
			InputTokens:  int64(u.PromptTokens),
			OutputTokens: int64(u.CompletionTokens),
			TotalTokens:  int64(u.TotalTokens),
		}
	}
	return call, usage, nil
}
