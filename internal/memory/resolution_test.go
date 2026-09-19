package memory

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
	"github.com/oniharnantyo/onclaw/internal/providers"
)

// resolutionFactory records the provider base URL + model the resolver
// picked; the fake providers are distinguished by BaseURL.
type resolutionFactory struct {
	calls []string
}

func (f *resolutionFactory) build(_ context.Context, _ string, cred providers.Credential, modelName string) (Model, error) {
	f.calls = append(f.calls, cred.BaseURL+"|"+modelName)
	return &scriptedModel{}, nil
}

func seedResolutionWorld(t *testing.T) (store.Store, *domain.ProviderConfig) {
	t.Helper()
	s := fake.New()
	ctx := context.Background()
	ws := &domain.Workspace{Slug: "acme", Name: "Acme"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	testWorkspaceID = ws.ID
	p1 := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "One", BaseURL: "https://p1.example.com"}
	p2 := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "Two", BaseURL: "https://p2.example.com"}
	if err := s.Providers().Create(ctx, p1); err != nil {
		t.Fatalf("seed provider 1: %v", err)
	}
	if err := s.Providers().Create(ctx, p2); err != nil {
		t.Fatalf("seed provider 2: %v", err)
	}
	inheriting := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p1.ID, Model: "m-agent-default"}
	if err := s.Agents().Create(ctx, inheriting); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	override := &domain.Agent{
		WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: p1.ID, Model: "m-agent-default",
		MemorySidecallProviderID: p2.ID, MemorySidecallModel: "m-agent-custom",
	}
	if err := s.Agents().Create(ctx, override); err != nil {
		t.Fatalf("seed override agent: %v", err)
	}
	return s, p1
}

func inheritingAgentID(t *testing.T, s store.Store) string {
	t.Helper()
	list, err := s.Agents().ListForWorkspace(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	for i := range list {
		if list[i].MemorySidecallModel == "" {
			return list[i].ID
		}
	}
	t.Fatal("inheriting agent missing")
	return ""
}

// pinWorkspaceSideCall writes (or clears) the memory settings record's
// side-call choice; an empty model clears the pin.
func pinWorkspaceSideCall(t *testing.T, s store.Store, providerID, model string) {
	t.Helper()
	config := map[string]any{}
	if model != "" {
		config["sidecall_provider_id"] = providerID
		config["sidecall_model"] = model
	}
	if err := s.ToolSettings().Upsert(context.Background(), &domain.WorkspaceToolSetting{
		WorkspaceID: testWorkspaceID,
		ToolKey:     "memory",
		Enabled:     true,
		Config:      config,
	}); err != nil {
		t.Fatalf("pin workspace side-call model: %v", err)
	}
}

// TestResolveSideCallModelOverrideChain walks the most-specific-first chain:
// the agent's own memory model beats the workspace memory setting, which
// beats the model the agent runs.
func TestResolveSideCallModelOverrideChain(t *testing.T) {
	s, p1 := seedResolutionWorld(t)
	ctx := context.Background()
	agents, settings, providersStore := s.Agents(), s.ToolSettings(), s.Providers()
	factory := &resolutionFactory{}
	var mk ModelFactory = func(ctx context.Context, pt string, cred providers.Credential, m string) (Model, error) {
		return factory.build(ctx, pt, cred, m)
	}

	// 1. The agent's own override wins even when the workspace pins a model.
	pinWorkspaceSideCall(t, s, p1.ID, "m-workspace")
	overrideID := ""
	list, err := agents.ListForWorkspace(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	for i := range list {
		if list[i].MemorySidecallModel != "" {
			overrideID = list[i].ID
		}
	}
	if _, err := resolveSideCallModel(ctx, providersStore, agents, settings, nil, mk, testWorkspaceID, overrideID, "", ""); err != nil {
		t.Fatalf("agent override resolution: %v", err)
	}
	if len(factory.calls) != 1 || factory.calls[len(factory.calls)-1] != "https://p2.example.com|m-agent-custom" {
		t.Fatalf("agent override should resolve p2/m-agent-custom, got %v", factory.calls)
	}

	// 2. An inheriting agent falls to the workspace memory setting.
	factory.calls = nil
	pinWorkspaceSideCall(t, s, p1.ID, "m-workspace")
	inheriting := inheritingAgentID(t, s)
	if _, err := resolveSideCallModel(ctx, providersStore, agents, settings, nil, mk, testWorkspaceID, inheriting, "", ""); err != nil {
		t.Fatalf("workspace override resolution: %v", err)
	}
	if len(factory.calls) != 1 || factory.calls[len(factory.calls)-1] != "https://p1.example.com|m-workspace" {
		t.Fatalf("workspace setting should resolve p1/m-workspace, got %v", factory.calls)
	}

	// 3. No workspace pin: the inheriting agent's own run model.
	factory.calls = nil
	pinWorkspaceSideCall(t, s, "", "")
	if _, err := resolveSideCallModel(ctx, providersStore, agents, settings, nil, mk, testWorkspaceID, inheriting, "", ""); err != nil {
		t.Fatalf("agent default resolution: %v", err)
	}
	if len(factory.calls) != 1 || factory.calls[len(factory.calls)-1] != "https://p1.example.com|m-agent-default" {
		t.Fatalf("agent default should resolve p1/m-agent-default, got %v", factory.calls)
	}
}
