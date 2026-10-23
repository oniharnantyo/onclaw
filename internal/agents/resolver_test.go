package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// TestCapabilityResolver covers the three-tier resolution hierarchy and
// disabled-row exclusion (design D4).
func TestCapabilityResolver(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupStore func(t *testing.T, s store.Store) (providerID string)
		modelID    string
		wantCaps   ModelCapabilities
		wantErr    error
	}{
		// --- Tier 1: Store-backed lookup ---
		{
			name: "tier 1: stored enabled model on canonical provider",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "anthropic", Type: ProviderAnthropic, APIKey: "key-1", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "claude-custom-1",
					ContextWindow: 300000,
					ImageInput:    true,
					PDFInput:      false,
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "claude-custom-1",
			wantCaps: ModelCapabilities{
				ContextWindow: 300000,
				Efforts:       []string{EffortHigh}, // the row is authoritative: undeclared efforts resolve to high (design D3)
				ImageInput:    true,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1: stored model overrides catalog values",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "openai", Type: ProviderOpenAI, APIKey: "key-2", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "gpt-4o",
					ContextWindow: 500000, // catalog is 128000
					ImageInput:    false,  // catalog is true
					PDFInput:      true,   // catalog is false
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "gpt-4o",
			wantCaps: ModelCapabilities{
				ContextWindow: 500000,
				Efforts:       []string{EffortHigh}, // the row is authoritative: undeclared efforts resolve to high, no catalog lookup (design D3)
				ImageInput:    false,
				PDFInput:      true,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1: stored disabled row excluded",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "gemini", Type: ProviderGemini, APIKey: "key-3", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "gemini-2.5-flash",
					ContextWindow: 1048576,
					ImageInput:    true,
					PDFInput:      true,
					Enabled:       false,
				}))
				return p.ID
			},
			modelID: "gemini-2.5-flash",
			wantCaps: ModelCapabilities{
				ContextWindow: 1048576,
				Efforts:       []string{EffortHigh},
				ImageInput:    true,
				PDFInput:      true,
				Enabled:       false,
			},
			wantErr: ErrModelDisabled,
		},
		{
			name: "tier 1: live-listed provider with custom model",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "openrouter", Type: ProviderOpenRouter, APIKey: "key-4", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "openai/gpt-5-preview",
					ContextWindow: 400000,
					ImageInput:    true,
					PDFInput:      false,
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "openai/gpt-5-preview",
			wantCaps: ModelCapabilities{
				ContextWindow: 400000,
				Efforts:       []string{EffortHigh}, // the row is authoritative: undeclared efforts resolve to high, no heuristic (design D3)
				ImageInput:    true,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1: stored row declares its own effort set in order",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "openai", Type: ProviderOpenAI, APIKey: "key-10", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "o4-custom",
					ContextWindow: 256000,
					Efforts:       []string{"none", "high"}, // declared verbatim, order preserved
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "o4-custom",
			wantCaps: ModelCapabilities{
				ContextWindow: 256000,
				Efforts:       []string{"none", "high"},
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1: stored row with no declared efforts resolves to high",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "anthropic", Type: ProviderAnthropic, APIKey: "key-11", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "claude-undeclared",
					ContextWindow: 200000,
					Efforts:       nil, // undeclared: degrades to high, never the catalog or heuristic
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "claude-undeclared",
			wantCaps: ModelCapabilities{
				ContextWindow: 200000,
				Efforts:       []string{EffortHigh},
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1: video input flows from the stored row",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "gemini", Type: ProviderGemini, APIKey: "key-12", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "gemini-video-custom",
					ContextWindow: 1048576,
					ImageInput:    true,
					PDFInput:      true,
					VideoInput:    true,
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "gemini-video-custom",
			wantCaps: ModelCapabilities{
				ContextWindow: 1048576,
				Efforts:       []string{EffortHigh},
				ImageInput:    true,
				PDFInput:      true,
				VideoInput:    true,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1 miss: stored rows for other models fall to catalog (unchanged)",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "anthropic", Type: ProviderAnthropic, APIKey: "key-13", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "custom-only-model",
					ContextWindow: 90000,
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "claude-sonnet-4-5", // no stored row: the catalog effort set still applies
			wantCaps: ModelCapabilities{
				ContextWindow: 200000,
				Efforts:       baseEfforts,
				ImageInput:    true,
				PDFInput:      true,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 1 miss: stored rows for other models fall to heuristic (unchanged)",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "openrouter", Type: ProviderOpenRouter, APIKey: "key-14", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				require.NoError(t, s.ProviderModels().Create(ctx, &store.ProviderModel{
					ProviderID:    p.ID,
					ModelID:       "vendor/custom-only",
					ContextWindow: 90000,
					Enabled:       true,
				}))
				return p.ID
			},
			modelID: "openai/gpt-5-preview", // no stored row: the id heuristic still applies
			wantCaps: ModelCapabilities{
				ContextWindow: DefaultContextWindow,
				Efforts:       gpt5Efforts,
				Enabled:       true,
			},
			wantErr: nil,
		},

		// --- Tier 2: Static catalog fallback ---
		{
			name: "tier 2: static catalog fallback for Anthropic",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "anthropic", Type: ProviderAnthropic, APIKey: "key-5", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				return p.ID
			},
			modelID: "claude-sonnet-4-5",
			wantCaps: ModelCapabilities{
				ContextWindow: 200000,
				Efforts:       baseEfforts,
				ImageInput:    true,
				PDFInput:      true,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 2: static catalog fallback for OpenAI",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "openai", Type: ProviderOpenAI, APIKey: "key-6", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				return p.ID
			},
			modelID: "gpt-4o",
			wantCaps: ModelCapabilities{
				ContextWindow: 128000,
				Efforts:       nil,
				ImageInput:    true,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 2: static catalog fallback for Gemini",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "gemini", Type: ProviderGemini, APIKey: "key-7", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				return p.ID
			},
			modelID: "gemini-2.5-pro",
			wantCaps: ModelCapabilities{
				ContextWindow: 1048576,
				Efforts:       baseEfforts,
				ImageInput:    true,
				PDFInput:      true,
				VideoInput:    true,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 2: providerID is provider type directly",
			setupStore: func(t *testing.T, s store.Store) string {
				return ProviderAnthropic
			},
			modelID: "claude-opus-4-1",
			wantCaps: ModelCapabilities{
				ContextWindow: 200000,
				Efforts:       baseEfforts,
				ImageInput:    true,
				PDFInput:      true,
				Enabled:       true,
			},
			wantErr: nil,
		},

		// --- Tier 3: Conservative defaults fallback ---
		{
			name: "tier 3: unlisted model on canonical provider with heuristic efforts",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "anthropic", Type: ProviderAnthropic, APIKey: "key-8", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				return p.ID
			},
			modelID: "claude-next-gen",
			wantCaps: ModelCapabilities{
				ContextWindow: DefaultContextWindow,
				Efforts:       baseEfforts, // heuristic: claude- prefix
				ImageInput:    false,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 3: unlisted model on live-listed provider with no heuristic match",
			setupStore: func(t *testing.T, s store.Store) string {
				p := &store.Provider{Name: "openrouter", Type: ProviderOpenRouter, APIKey: "key-9", Enabled: true}
				require.NoError(t, s.Providers().Create(ctx, p))
				return p.ID
			},
			modelID: "meta-llama/llama-3-70b",
			wantCaps: ModelCapabilities{
				ContextWindow: DefaultContextWindow,
				Efforts:       nil,
				ImageInput:    false,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 3: unknown provider and unknown model",
			setupStore: func(t *testing.T, s store.Store) string {
				return "unknown-provider-id"
			},
			modelID: "mysterious-model",
			wantCaps: ModelCapabilities{
				ContextWindow: DefaultContextWindow,
				Efforts:       nil,
				ImageInput:    false,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
		{
			name: "tier 3: empty providerID",
			setupStore: func(t *testing.T, s store.Store) string {
				return ""
			},
			modelID: "some-model",
			wantCaps: ModelCapabilities{
				ContextWindow: DefaultContextWindow,
				Efforts:       nil,
				ImageInput:    false,
				PDFInput:      false,
				Enabled:       true,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fake.New()
			providerID := tt.setupStore(t, s)
			resolver := NewCapabilityResolver(s)

			got, err := resolver.Resolve(ctx, providerID, tt.modelID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantCaps, got)
		})
	}

	t.Run("nil store skips tier 1 to tier 2 and 3", func(t *testing.T) {
		resolver := NewCapabilityResolver(nil)
		// Tier 2 with known provider type
		got, err := resolver.Resolve(ctx, ProviderAnthropic, "claude-sonnet-4-5")
		require.NoError(t, err)
		require.Equal(t, 200000, got.ContextWindow)
		require.True(t, got.ImageInput)

		// Tier 3 with unknown model
		got3, err := resolver.Resolve(ctx, "anything", "unknown")
		require.NoError(t, err)
		require.Equal(t, DefaultContextWindow, got3.ContextWindow)
		require.False(t, got3.ImageInput)
	})
}

// TestCapabilityResolver_ContextAccounting verifies that contextLimit in the
// manager accounts against stored row limits through the resolver.
func TestCapabilityResolver_ContextAccounting(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok"}
	})
	ctx := t.Context()

	p := &store.Provider{Name: "custom-p", Type: ProviderOpenAI, APIKey: "key-test", Enabled: true}
	require.NoError(t, f.store.Providers().Create(ctx, p))

	// Stored row with custom context window.
	require.NoError(t, f.store.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "custom-window-model",
		ContextWindow: 350000,
		Enabled:       true,
	}))

	agent := &store.Agent{
		Name:       "custom-agent",
		Slug:       "custom-agent",
		ProviderID: p.ID,
		Model:      "custom-window-model",
	}
	require.NoError(t, f.store.Agents().Create(ctx, agent))

	sess, err := f.mgr.CreateSession(ctx, agent.ID)
	require.NoError(t, err)

	usage, err := f.mgr.ContextUsage(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, 350000, usage.Limit, "context limit must resolve from the stored row")
}

// TestCapabilityResolver_EffortValidation verifies that effort checks route
// through the capability resolver.
func TestCapabilityResolver_EffortValidation(t *testing.T) {
	st := fake.New()
	reg := newModelRegistry(st)
	ctx := context.Background()

	p := &store.Provider{Name: "openai-p", Type: ProviderOpenAI, APIKey: "key-test", Enabled: true}
	require.NoError(t, st.Providers().Create(ctx, p))

	// Stored row declaring its effort set (design D3: the row is
	// authoritative) routes through resolver-driven validation.
	require.NoError(t, st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID:    p.ID,
		ModelID:       "gpt-5-custom",
		ContextWindow: 400000,
		Efforts:       gpt5Efforts,
		Enabled:       true,
	}))

	agent := store.Agent{
		Name:       "a",
		ProviderID: p.ID,
		Model:      "gpt-5-custom",
	}

	// EffortMinimal is in the row's declared set
	_, err := reg.Build(ctx, agent, ModelOverride{Effort: EffortMinimal})
	require.NoError(t, err)

	// An effort not in any reasoning set fails
	_, err = reg.Build(ctx, agent, ModelOverride{Effort: "extreme"})
	require.ErrorIs(t, err, ErrEffortUnsupported)
}

// TestCapabilityResolver_AttachmentDegradation verifies that attachment image & PDF
// input checks route through the capability resolver and respect stored rows.
func TestCapabilityResolver_AttachmentDegradation(t *testing.T) {
	t.Run("stored row disables image input for catalog model", func(t *testing.T) {
		f, atts := newAttachmentFixture(t, func(input []*schema.Message) fakeReply {
			last := input[len(input)-1]
			sawNote := false
			for _, part := range last.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeImageURL {
					return fakeReply{err: errors.New("image bytes reached model with imageInput=false")}
				}
				if part.Type == schema.ChatMessagePartTypeText && strings.Contains(part.Text, "cannot read image input") {
					sawNote = true
				}
			}
			if !sawNote {
				return fakeReply{err: errors.New("pointer note was not generated")}
			}
			return fakeReply{text: "degraded ok"}
		})

		ctx := t.Context()
		// Store row for claude-sonnet-4-5 with ImageInput: false
		require.NoError(t, f.store.ProviderModels().Create(ctx, &store.ProviderModel{
			ProviderID:    f.agent.ProviderID,
			ModelID:       "claude-sonnet-4-5",
			ContextWindow: 200000,
			ImageInput:    false, // user disabled image input!
			PDFInput:      true,
			Enabled:       true,
		}))

		sess := f.newSession(t)
		att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")

		turn, err := f.mgr.Send(ctx, sess.ID, "check this", att.Key)
		require.NoError(t, err)
		require.NoError(t, turn.Wait())
	})

	t.Run("stored row enables image input for custom model", func(t *testing.T) {
		sawImage := false
		f, atts := newAttachmentFixture(t, func(input []*schema.Message) fakeReply {
			last := input[len(input)-1]
			for _, part := range last.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeImageURL {
					sawImage = true
				}
			}
			return fakeReply{text: "vision ok"}
		})

		ctx := t.Context()
		// Update agent model to custom model
		f.agent.Model = "custom-vision-model"
		require.NoError(t, f.store.Agents().Update(ctx, &f.agent))

		// Store row with ImageInput: true
		require.NoError(t, f.store.ProviderModels().Create(ctx, &store.ProviderModel{
			ProviderID:    f.agent.ProviderID,
			ModelID:       "custom-vision-model",
			ContextWindow: 200000,
			ImageInput:    true,
			PDFInput:      false,
			Enabled:       true,
		}))

		sess := f.newSession(t)
		att := saveUpload(t, atts, "shot.png", "image/png", "png-bytes")

		turn, err := f.mgr.Send(ctx, sess.ID, "look here", att.Key)
		require.NoError(t, err)
		require.NoError(t, turn.Wait())
		require.True(t, sawImage, "model should have received image bytes due to stored row")
	})
}
