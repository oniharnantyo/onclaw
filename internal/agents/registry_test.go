package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// newRegistryFixture seeds the store with one enabled anthropic provider and
// an agent on it, and returns the real (store-backed) registry.
func newRegistryFixture(t *testing.T) (*store.Provider, store.Agent, modelRegistry) {
	t.Helper()
	fs := fakeStoreForRegistry(t)
	provider, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")
	return provider, agent, newModelRegistry(fs)
}

func fakeStoreForRegistry(t *testing.T) *fake.Store {
	t.Helper()
	return fake.New()
}

func seedAgentOn(fs *fake.Store, providerType, model string) (*store.Provider, store.Agent) {
	ctx := context.Background()
	provider := &store.Provider{Name: "prov-" + providerType, Type: providerType, APIKey: "key-123", Enabled: true}
	if err := fs.Providers().Create(ctx, provider); err != nil {
		panic(err)
	}
	if IsCanonicalProviderType(providerType) {
		if _, err := fs.ProviderModels().UpsertMissing(ctx, CatalogProviderModels(provider.ID, providerType)); err != nil {
			panic(err)
		}
	}
	if model != "" {
		if _, err := fs.ProviderModels().UpsertMissing(ctx, []*store.ProviderModel{{
			ProviderID:    provider.ID,
			ModelID:       model,
			ContextWindow: 200000,
			Enabled:       true,
		}}); err != nil {
			panic(err)
		}
	}
	agent := &store.Agent{Name: "agent", ProviderID: provider.ID, Model: model}
	if err := fs.Agents().Create(ctx, agent); err != nil {
		panic(err)
	}
	return provider, *agent
}

// TestRegistry_BuildAnthropicWiresClaude covers 4.2's verified path: an
// agent on an enabled anthropic provider builds the eino-ext claude
// connector end-to-end (construction performs no network I/O).
func TestRegistry_BuildAnthropicWiresClaude(t *testing.T) {
	_, agent, reg := newRegistryFixture(t)

	mdl, err := reg.Build(t.Context(), agent, ModelOverride{})
	require.NoError(t, err)
	require.IsType(t, &claude.ChatModel{}, mdl, "the anthropic path must construct the eino-ext claude connector")
}

// TestRegistry_BuildOpenAIAndGeminiRegistered covers the other registered
// adapters: they construct their connectors and are accepted, though not
// required to pass runtime verification (design D5).
func TestRegistry_BuildOpenAIAndGeminiRegistered(t *testing.T) {
	for _, tc := range []struct {
		providerType string
		model        string
		wantType     any
	}{
		{ProviderOpenAI, "gpt-4o", &openai.ChatModel{}},
		{ProviderGemini, "gemini-2.5-flash", &gemini.ChatModel{}},
	} {
		t.Run(tc.providerType, func(t *testing.T) {
			fs := fakeStoreForRegistry(t)
			_, agent := seedAgentOn(fs, tc.providerType, tc.model)

			mdl, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{})
			require.NoError(t, err)
			require.IsType(t, tc.wantType, mdl)
		})
	}
}

// TestRegistry_RejectsUnconfiguredProviders covers the typed rejections:
// missing provider reference, absent provider, disabled provider, missing
// credential, and unsupported provider type.
func TestRegistry_RejectsUnconfiguredProviders(t *testing.T) {
	t.Run("no provider selected", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, err := newModelRegistry(fs).Build(t.Context(), store.Agent{Name: "lonely"}, ModelOverride{})
		require.ErrorIs(t, err, ErrProviderNotConfigured)
	})

	t.Run("provider does not exist", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, err := newModelRegistry(fs).Build(t.Context(), store.Agent{Name: "a", ProviderID: "ghost"}, ModelOverride{})
		require.ErrorIs(t, err, ErrProviderNotConfigured)
	})

	t.Run("provider disabled", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")
		provider.Enabled = false
		require.NoError(t, fs.Providers().Update(t.Context(), provider))

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{})
		require.ErrorIs(t, err, ErrProviderNotConfigured)
	})

	t.Run("provider without API key", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")
		provider.APIKey = ""
		require.NoError(t, fs.Providers().Update(t.Context(), provider))

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{})
		require.ErrorIs(t, err, ErrProviderNotConfigured)
	})

	t.Run("compatible provider without API key builds", func(t *testing.T) {
		// Local runtimes (Ollama, LM Studio) sit behind the compatible types
		// and legitimately need no credential; an empty key must not fail
		// the build.
		fs := fakeStoreForRegistry(t)
		provider := &store.Provider{
			Name:    "ollama",
			Type:    ProviderOpenAICompatible,
			BaseURL: "http://127.0.0.1:11434/v1",
			Enabled: true,
		}
		require.NoError(t, fs.Providers().Create(t.Context(), provider))
		require.NoError(t, fs.ProviderModels().Create(t.Context(), &store.ProviderModel{
			ProviderID:    provider.ID,
			ModelID:       "llama3.2",
			ContextWindow: 128000,
			Enabled:       true,
		}))
		agent := &store.Agent{Name: "local", ProviderID: provider.ID, Model: "llama3.2"}
		require.NoError(t, fs.Agents().Create(t.Context(), agent))

		mdl, err := newModelRegistry(fs).Build(t.Context(), *agent, ModelOverride{})
		require.NoError(t, err)
		require.IsType(t, &openai.ChatModel{}, mdl)
	})

	t.Run("unsupported provider type", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider := &store.Provider{Name: "mystery", Type: "quantum", APIKey: "k", Enabled: true}
		require.NoError(t, fs.Providers().Create(t.Context(), provider))
		agent := &store.Agent{Name: "a", ProviderID: provider.ID, Model: "m"}
		require.NoError(t, fs.Agents().Create(t.Context(), agent))

		_, err := newModelRegistry(fs).Build(t.Context(), *agent, ModelOverride{})
		require.ErrorIs(t, err, ErrProviderUnsupported)
	})
}

// TestRegistry_UnreachableProviderFailsTurnWithErrorEvent covers the spec's
// unreachable-provider scenario end to end: the connector builds, the call
// against an unreachable endpoint fails the turn, and the failure lands as a
// persisted turn_error event after the user message.
func TestRegistry_UnreachableProviderFailsTurnWithErrorEvent(t *testing.T) {
	fs := fakeStoreForRegistry(t)
	provider, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")
	// Port 1 on loopback: connection refused, no external network.
	provider.BaseURL = "http://127.0.0.1:1"
	require.NoError(t, fs.Providers().Update(t.Context(), provider))

	svc := sessions.NewService(fs)
	// The browser is in the catalog: an open-allowlist turn constructs it.
	m := newManager(fs, svc, newModelRegistry(fs), WithBrowserDir(t.TempDir()))
	sess, err := m.CreateSession(t.Context(), agent.ID)
	require.NoError(t, err)

	turn, err := m.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.Error(t, turn.Wait(), "the turn must fail against an unreachable provider")

	events, err := svc.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindTurnError),
	}, kindsOf(events))
	require.NotEmpty(t, events[1].Error)
}

// TestCatalog_ModelsAndWindows covers the built-in catalog: per-type model
// listings are sorted, and context windows resolve per model id with the
// default fallback for unknown ids and types.
func TestCatalog_ModelsAndWindows(t *testing.T) {
	anthropic := CatalogModels(ProviderAnthropic)
	require.NotEmpty(t, anthropic)
	require.IsIncreasing(t, anthropic, "catalog ids must be sorted")

	openai := CatalogModels(ProviderOpenAI)
	require.NotEmpty(t, openai)
	require.IsIncreasing(t, openai)

	require.Nil(t, CatalogModels("quantum"), "unknown provider types list nothing")

	require.Equal(t, 200000, ContextWindow(ProviderAnthropic, "claude-sonnet-4-5"))
	require.Equal(t, 128000, ContextWindow(ProviderOpenAI, "gpt-4o"))
	require.Equal(t, DefaultContextWindow, ContextWindow(ProviderAnthropic, "claude-unknown"))
	require.Equal(t, DefaultContextWindow, ContextWindow("quantum", "anything"))
}

// TestRegistry_ModelsListing exposes the catalog through the registry.
func TestRegistry_ModelsListing(t *testing.T) {
	_, _, reg := newRegistryFixture(t)
	require.NotEmpty(t, reg.Models(ProviderAnthropic))
	require.Nil(t, reg.Models("quantum"))
}

// TestRegistry_OverrideModelReplacesAgentModel covers the model validation of
// the override parameter and agent configuration (design D6): a set override
// replaces the agent's configured model, an empty one inherits it, and an
// unlisted or disabled model fails the turn with a clear error naming the
// provider (replacing the free-form allowance).
func TestRegistry_OverrideModelReplacesAgentModel(t *testing.T) {
	t.Run("override replaces the agent's model", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")

		mdl, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Model: "claude-opus-4-1"})
		require.NoError(t, err)
		require.IsType(t, &claude.ChatModel{}, mdl)
	})

	t.Run("empty override inherits the agent's model", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-haiku-4-5")

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{})
		require.NoError(t, err)
	})

	t.Run("unlisted canonical model rejected naming provider", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Model: "claude-ghost"})
		require.ErrorIs(t, err, ErrModelNotListed)
		require.Contains(t, err.Error(), provider.Name)
	})

	t.Run("openrouter rejects unlisted model override naming provider", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider, agent := seedAgentOn(fs, ProviderOpenRouter, "some/vendor-model")

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Model: "other/free-form-id"})
		require.ErrorIs(t, err, ErrModelNotListed)
		require.Contains(t, err.Error(), provider.Name)
		require.Contains(t, err.Error(), "other/free-form-id")
	})

	t.Run("openrouter accepts enabled stored model", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider, agent := seedAgentOn(fs, ProviderOpenRouter, "some/vendor-model")

		require.NoError(t, fs.ProviderModels().Create(t.Context(), &store.ProviderModel{
			ProviderID:    provider.ID,
			ModelID:       "other/stored-model",
			ContextWindow: 200000,
			Enabled:       true,
		}))

		mdl, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Model: "other/stored-model"})
		require.NoError(t, err)
		require.NotNil(t, mdl)
	})

	t.Run("disabled stored model rejected naming provider", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		provider, agent := seedAgentOn(fs, ProviderOpenRouter, "some/vendor-model")

		require.NoError(t, fs.ProviderModels().Create(t.Context(), &store.ProviderModel{
			ProviderID:    provider.ID,
			ModelID:       "other/disabled-model",
			ContextWindow: 200000,
			Enabled:       false,
		}))

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Model: "other/disabled-model"})
		require.ErrorIs(t, err, ErrModelNotListed)
		require.Contains(t, err.Error(), provider.Name)
		require.Contains(t, err.Error(), "disabled")
	})

	t.Run("agent unlisted model rejected at build time naming provider", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		ctx := t.Context()
		provider := &store.Provider{Name: "my-prov", Type: ProviderOpenRouter, APIKey: "key-123", Enabled: true}
		require.NoError(t, fs.Providers().Create(ctx, provider))
		agent := &store.Agent{Name: "unconfigured-agent", ProviderID: provider.ID, Model: "unlisted-model"}
		require.NoError(t, fs.Agents().Create(ctx, agent))

		_, err := newModelRegistry(fs).Build(ctx, *agent, ModelOverride{})
		require.ErrorIs(t, err, ErrModelNotListed)
		require.Contains(t, err.Error(), provider.Name)
	})
}

// TestRegistry_OverrideEffortValidation covers the effort half of the
// override parameter (design D2): a supported effort builds, an effort the
// effective model does not support is rejected with the typed error, and the
// empty effort inherits (no parameter at all).
func TestRegistry_OverrideEffortValidation(t *testing.T) {
	t.Run("supported effort builds", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Effort: EffortHigh})
		require.NoError(t, err)
	})

	t.Run("unsupported effort rejected", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		// Claude models have no minimal tier; gpt-4o has no effort at all.
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")
		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Effort: EffortMinimal})
		require.ErrorIs(t, err, ErrEffortUnsupported)

		_, gptAgent := seedAgentOn(fs, ProviderOpenAI, "gpt-4o")
		_, err = newModelRegistry(fs).Build(t.Context(), gptAgent, ModelOverride{Effort: EffortLow})
		require.ErrorIs(t, err, ErrEffortUnsupported, "a non-reasoning model supports no effort")
	})

	t.Run("effort validated against the override model, not the agent's", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		// The agent runs gpt-4o (undeclared efforts, so only high); the
		// override model gpt-5 declares the gpt-5 set with minimal — the
		// pair must validate against gpt-5's declared set.
		_, agent := seedAgentOn(fs, ProviderOpenAI, "gpt-4o")
		rows, err := fs.ProviderModels().ByProvider(t.Context(), agent.ProviderID)
		require.NoError(t, err)
		for _, row := range rows {
			if row.ModelID == "gpt-5" {
				row.Efforts = gpt5Efforts
				require.NoError(t, fs.ProviderModels().Update(t.Context(), row))
			}
		}

		_, err = newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{Model: "gpt-5", Effort: EffortMinimal})
		require.NoError(t, err)
	})

	t.Run("empty effort inherits", func(t *testing.T) {
		fs := fakeStoreForRegistry(t)
		_, agent := seedAgentOn(fs, ProviderAnthropic, "claude-sonnet-4-5")

		_, err := newModelRegistry(fs).Build(t.Context(), agent, ModelOverride{})
		require.NoError(t, err)
	})
}

// TestManager_ToolCallsSurfaceAsEvents covers the tool-activity event path:
// a model tool call is executed and surfaced as a persisted tool_activity
// event carrying its identity and the streamed argument text.
func TestManager_ToolCallsSurfaceAsEvents(t *testing.T) {
	index := 0
	calls := 0
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		calls++
		if calls == 1 {
			return fakeReply{
				toolCalls: []schema.ToolCall{{
					Index:    &index,
					ID:       "call-1",
					Function: schema.FunctionCall{Name: ToolCurrentTime, Arguments: `{}`},
				}},
			}
		}
		return fakeReply{text: "done", promptTokens: 5, completionTokens: 5}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "what time is it?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait(), "the tool call executes inside the turn")

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	toolEvents := eventsOfKind(events, sessions.KindToolActivity)
	require.Len(t, toolEvents, 1)
	require.NotNil(t, toolEvents[0].Tool)
	require.Equal(t, ToolCurrentTime, toolEvents[0].Tool.Name)
	require.Equal(t, "call-1", toolEvents[0].Tool.CallID)
	require.Equal(t, `{}`, toolEvents[0].Tool.Arguments)
	// The call really ran: the record is terminal with the real result.
	require.Equal(t, sessions.ToolStatusCompleted, toolEvents[0].Tool.Status)
	require.NotEmpty(t, toolEvents[0].Tool.Result)
}

// TestManager_Send_ReasoningStreamsAndPersists covers the reasoning path:
// reasoning deltas stream live and the completed reasoning persists as its
// own event, distinct from the assistant text.
func TestManager_Send_ReasoningStreamsAndPersists(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{
			reasoning:    "pondering the question",
			text:         "the answer",
			promptTokens: 10, completionTokens: 10,
		}
	})
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "why?")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	live := collector.Wait()
	require.Equal(t, "pondering the question", textOf(live, sessions.KindReasoningDelta),
		"reasoning must stream live, distinct from text deltas")
	require.Equal(t, "the answer", textOf(live, sessions.KindTextDelta))

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindReasoning),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.True(t, strings.HasPrefix(events[1].Text, "pondering"))
}
