package agents

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
)

// capturedModelCall records one agentic-factory invocation: the effective
// provider type, model id, and API key the run composes on.
type capturedModelCall struct {
	providerType string
	model        string
	apiKey       string
}

// recordingFactory is a thread-safe AgenticModelFactory capturing every call.
type recordingFactory struct {
	mu    sync.Mutex
	calls []capturedModelCall
	mdl   *hooksModel
}

func (f *recordingFactory) factory(_ context.Context, providerType string, cred providers.Credential, modelName string) (Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, capturedModelCall{providerType: providerType, model: modelName, apiKey: cred.APIKey})
	return f.mdl, nil
}

func (f *recordingFactory) snapshot() []capturedModelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedModelCall(nil), f.calls...)
}

// TestRunner_PinnedAgentUnaffected (agent-runtime: Pinned agent unaffected):
// a pinned agent composes on its own pair — the workspace default is never
// consulted.
func TestRunner_PinnedAgentUnaffected(t *testing.T) {
	rec := &recordingFactory{mdl: &hooksModel{final: "done"}}
	st, runner, ws, ag, req := setupHooksRunnerWithOpts(t, nil, rec.mdl, []RunnerOption{
		WithAgenticModelFactory(rec.factory),
	})

	// A workspace default exists but must be ignored for pinned agents.
	ctx := context.Background()
	ws.DefaultModel = &domain.DefaultModelPair{ProviderID: ag.ProviderID, Model: "ignored-default"}
	if err := st.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("set default: %v", err)
	}

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one factory call, got %+v", calls)
	}
	if calls[0].providerType != providers.TypeOpenAI || calls[0].model != "gpt-4o" || calls[0].apiKey != "" {
		t.Fatalf("pinned pair not used: %+v", calls[0])
	}
}

// TestRunner_InheritingAgentRunsWorkspaceDefault (agent-runtime: Inheriting
// agent runs the workspace default): the empty pair composes on the default's
// provider credential and model id — including when the effective provider
// type requires max_tokens and the agent pins none (D4: the run proceeds).
func TestRunner_InheritingAgentRunsWorkspaceDefault(t *testing.T) {
	rec := &recordingFactory{mdl: &hooksModel{final: "done"}}
	st, runner, ws, ag, req := setupHooksRunnerWithOpts(t, nil, rec.mdl, []RunnerOption{
		WithAgenticModelFactory(rec.factory),
	})

	ctx := context.Background()

	// The default provider is an anthropic-type (RequiresMaxTokens) with a
	// distinct API key: resolution must land on its credential and model.
	defaultProv := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: providers.TypeAnthropic, Name: "Claude", Enabled: true}
	if err := st.Providers().Create(ctx, defaultProv); err != nil {
		t.Fatalf("create default provider: %v", err)
	}
	ws.DefaultModel = &domain.DefaultModelPair{ProviderID: defaultProv.ID, Model: "claude-default"}
	if err := st.Workspaces().Update(ctx, ws); err != nil {
		t.Fatalf("set default: %v", err)
	}

	// Switch the seeded pinned agent to inherit.
	ag.ProviderID = ""
	ag.Model = ""
	if err := st.Agents().Update(ctx, ag); err != nil {
		t.Fatalf("switch agent to inherit: %v", err)
	}

	stream, err := runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := collectStream(t, stream)
	if !hasKind(events, TranscriptEventTurnCompleted) {
		t.Fatalf("expected turn_completed, got %+v", events)
	}

	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one factory call, got %+v", calls)
	}
	if calls[0].providerType != providers.TypeAnthropic || calls[0].model != "claude-default" || calls[0].apiKey != "" {
		t.Fatalf("workspace default not used: %+v", calls[0])
	}
}

// TestRunner_InheritingAgentWithoutDefaultFailsFast (agent-runtime:
// Inheriting agent without a default fails fast): the run errors before any
// model construction, with the missing workspace setting named.
func TestRunner_InheritingAgentWithoutDefaultFailsFast(t *testing.T) {
	rec := &recordingFactory{mdl: &hooksModel{final: "done"}}
	st, runner, _, ag, req := setupHooksRunnerWithOpts(t, nil, rec.mdl, []RunnerOption{
		WithAgenticModelFactory(rec.factory),
	})

	ctx := context.Background()
	ag.ProviderID = ""
	ag.Model = ""
	if err := st.Agents().Update(ctx, ag); err != nil {
		t.Fatalf("switch agent to inherit: %v", err)
	}

	stream, err := runner.Run(ctx, req)
	if err == nil {
		if stream != nil {
			collectStream(t, stream)
		}
		t.Fatal("expected the run to fail fast")
	}
	if !strings.Contains(err.Error(), "workspace default model") || !strings.Contains(err.Error(), "workspace settings") {
		t.Fatalf("expected the missing setting named, got %v", err)
	}
	if !strings.Contains(err.Error(), "no provider pinned") {
		t.Fatalf("expected the inherit condition named, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Fatalf("the model must never be constructed, got %+v", rec.snapshot())
	}
}

// TestDefaultMaxTokens_Constant (agent-runtime: Run-time max-tokens default):
// the claude-family connector config carries the documented DefaultMaxTokens
// so an inherit agent on a requiring type proceeds without pinning one.
func TestDefaultMaxTokens_Constant(t *testing.T) {
	if DefaultMaxTokens != 4096 {
		t.Fatalf("expected the documented 4096 default, got %d", DefaultMaxTokens)
	}
	cfg := newAgenticClaudeConfig(providers.Credential{APIKey: "sk", BaseURL: ""}, "claude-default")
	if cfg.MaxTokens != DefaultMaxTokens {
		t.Fatalf("expected the connector to carry DefaultMaxTokens, got %d", cfg.MaxTokens)
	}
	if cfg.Model != "claude-default" {
		t.Fatalf("model id not propagated: %q", cfg.Model)
	}
}
