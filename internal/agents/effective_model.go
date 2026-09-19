package agents

import (
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// EffectiveModel resolves the provider/model pair a run composes on
// (refactor-workspace-settings D3): the agent's pinned pair when both fields
// are set, otherwise the workspace default model pair. It is the single
// resolution point for every agent provider/model consumer — the runner's
// compose path (conversation models, credential lookup, input modality),
// prompt generation, and the read-side previews — so inherit agents resolve
// in exactly one way everywhere.
//
// An inheriting agent whose workspace has no default model returns an error
// wrapping domain.ErrNoWorkspaceDefault naming the missing workspace setting:
// callers fail fast with it (the run never reaches a mid-run model error) and
// save-time validation reuses the same message.
func EffectiveModel(agent *domain.Agent, wsDefault *domain.DefaultModelPair) (providerID, model string, err error) {
	if !agent.InheritsModel() {
		return agent.ProviderID, agent.Model, nil
	}
	if wsDefault == nil || wsDefault.ProviderID == "" || wsDefault.Model == "" {
		return "", "", fmt.Errorf("%w: agent has no provider pinned and no workspace default model is set — set one in workspace settings first", domain.ErrNoWorkspaceDefault)
	}
	return wsDefault.ProviderID, wsDefault.Model, nil
}
