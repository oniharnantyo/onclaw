package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// captureServer serves a fixed status/body for provider endpoint tests and
// records the auth header and request path the service actually sent.
type captureServer struct {
	authHeader string
	authName   string
	path       string
	statusCode int
	body       string
	server     *httptest.Server
}

func newCaptureServer(statusCode int, body string) *captureServer {
	c := &captureServer{statusCode: statusCode, body: body}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path = r.URL.Path
		c.authName = "Authorization"
		c.authHeader = r.Header.Get("Authorization")
		if xKey := r.Header.Get("x-api-key"); xKey != "" {
			c.authName = "x-api-key"
			c.authHeader = xKey
		}
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	return c
}

func TestProviderView_NeverCarriesRawKey(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	withKey, err := svc.CreateProvider(ctx, ProviderInput{Name: "keyed", Type: "anthropic", APIKey: "sk-ant-api99-abc12345", Enabled: true})
	require.NoError(t, err)
	keyless, err := svc.CreateProvider(ctx, ProviderInput{Name: "keyless", Type: "openai-compatible", BaseURL: "https://relay.internal/v1"})
	require.NoError(t, err)

	got, err := svc.Provider(ctx, withKey.ID)
	require.NoError(t, err)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "sk-ant-api99-abc12345")
	require.True(t, got.KeySet)
	require.Equal(t, "2345", got.KeyHint)

	// The hint slices runes, not bytes, so multi-byte keys stay legible.
	multi, err := svc.CreateProvider(ctx, ProviderInput{Name: "multi", Type: "anthropic", APIKey: "κλειδί-αβγδ"})
	require.NoError(t, err)
	gotMulti, err := svc.Provider(ctx, multi.ID)
	require.NoError(t, err)
	require.Equal(t, "αβγδ", gotMulti.KeyHint)

	list, err := svc.Providers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3)
	for _, view := range list {
		payload, err := json.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, string(payload), "sk-ant-api99-abc12345")
	}

	gotKeyless, err := svc.Provider(ctx, keyless.ID)
	require.NoError(t, err)
	require.False(t, gotKeyless.KeySet)
	require.Empty(t, gotKeyless.KeyHint)
}

func TestUpdateProvider_KeySemantics(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{Name: "p", Type: "anthropic", APIKey: "aaaa-1111", Enabled: true})
	require.NoError(t, err)

	t.Run("blank key preserves the stored key", func(t *testing.T) {
		updated, err := svc.UpdateProvider(ctx, created.ID, ProviderInput{Type: "anthropic", Name: "p-renamed", Enabled: true})
		require.NoError(t, err)
		require.Equal(t, "p-renamed", updated.Name)
		require.Equal(t, "1111", updated.KeyHint)

		stored, err := st.Providers().ByID(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, "aaaa-1111", stored.APIKey)
	})

	t.Run("name-only edit keeps the key", func(t *testing.T) {
		before, err := st.Providers().ByID(ctx, created.ID)
		require.NoError(t, err)

		updated, err := svc.UpdateProvider(ctx, created.ID, ProviderInput{Type: "anthropic", Name: "p-renamed-again", Enabled: before.Enabled, APIKey: ""})
		require.NoError(t, err)
		require.Equal(t, "p-renamed-again", updated.Name)

		stored, err := st.Providers().ByID(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, "aaaa-1111", stored.APIKey)
	})

	t.Run("non-blank key replaces the stored key", func(t *testing.T) {
		updated, err := svc.UpdateProvider(ctx, created.ID, ProviderInput{Type: "anthropic", Name: "p", APIKey: "bbbb-2222", Enabled: true})
		require.NoError(t, err)
		require.Equal(t, "2222", updated.KeyHint)

		stored, err := st.Providers().ByID(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, "bbbb-2222", stored.APIKey)
	})
}

func TestCreateProvider_RejectsUnknownType(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	_, err := svc.CreateProvider(ctx, ProviderInput{Name: "x", Type: "mystery", APIKey: "k"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown provider type "mystery"`)
	require.Contains(t, err.Error(), agents.ProviderOpenRouter)

	list, err := st.Providers().List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestUpdateProvider_RejectsUnknownType(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{Name: "p", Type: "anthropic", APIKey: "k"})
	require.NoError(t, err)

	_, err = svc.UpdateProvider(ctx, created.ID, ProviderInput{Type: "mystery", Name: "p", APIKey: "new-key"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown provider type "mystery"`)

	stored, err := st.Providers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "anthropic", stored.Type)
	require.Equal(t, "k", stored.APIKey)
}

func TestVerifyProviderDraft_UsesTypedKey(t *testing.T) {
	capture := newCaptureServer(http.StatusOK, `{"data":[]}`)
	defer capture.server.Close()
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	result, err := svc.VerifyProviderDraft(ctx, ProviderInput{
		Name:    "relay",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: capture.server.URL,
		APIKey:  "typed-key",
	}, "")
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Empty(t, result.Detail)
	require.Equal(t, "/models", capture.path)
	require.Equal(t, "Bearer typed-key", capture.authHeader)
}

func TestVerifyProviderDraft_BlankKeyFallsBackToStoredKey(t *testing.T) {
	capture := newCaptureServer(http.StatusOK, `{"data":[]}`)
	defer capture.server.Close()
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "relay",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: capture.server.URL,
		APIKey:  "stored-key-4321",
	})
	require.NoError(t, err)

	result, err := svc.VerifyProviderDraft(ctx, ProviderInput{
		Name:    "relay",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: capture.server.URL,
		APIKey:  "",
	}, created.ID)
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "Bearer stored-key-4321", capture.authHeader)
}

func TestVerifyProviderDraft_FailureLandsInTheResult(t *testing.T) {
	capture := newCaptureServer(http.StatusUnauthorized, `{"error":"bad key"}`)
	defer capture.server.Close()
	svc := NewSettingsService(fake.New())
	ctx := context.Background()

	result, err := svc.VerifyProviderDraft(ctx, ProviderInput{
		Name:    "relay",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: capture.server.URL,
		APIKey:  "wrong",
	}, "")
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Contains(t, result.Detail, "401")
	require.Contains(t, result.Detail, "bad key")
}

func TestVerifyProviderDraft_UnknownTypeIsError(t *testing.T) {
	svc := NewSettingsService(fake.New())

	_, err := svc.VerifyProviderDraft(context.Background(), ProviderInput{Name: "x", Type: "mystery"}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mystery")
}

func TestVerifyProvider_VerifiesStoredValues(t *testing.T) {
	capture := newCaptureServer(http.StatusOK, `{}`)
	defer capture.server.Close()
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "relay",
		Type:    agents.ProviderAnthropicCompatible,
		BaseURL: capture.server.URL,
		APIKey:  "stored-abc",
	})
	require.NoError(t, err)

	result, err := svc.VerifyProvider(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "/v1/models", capture.path)
	require.Equal(t, "stored-abc", capture.authHeader)

	_, err = svc.VerifyProvider(ctx, "missing")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestVerifyProvider_NothingPersists(t *testing.T) {
	capture := newCaptureServer(http.StatusOK, `{}`)
	defer capture.server.Close()
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	created, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "relay",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: capture.server.URL,
		APIKey:  "stored-key",
	})
	require.NoError(t, err)
	before, err := st.Providers().ByID(ctx, created.ID)
	require.NoError(t, err)

	_, err = svc.VerifyProviderDraft(ctx, ProviderInput{Name: "relay", Type: agents.ProviderOpenAICompatible, BaseURL: capture.server.URL}, created.ID)
	require.NoError(t, err)
	_, err = svc.VerifyProvider(ctx, created.ID)
	require.NoError(t, err)

	after, err := st.Providers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, *before, *after)

	list, err := svc.Providers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestProviderModels_StoreReadOnly(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	// Canonical provider: CreateProvider seeds catalog models enabled. Seeded
	// rows declare no efforts, so the stored-first rule yields exactly high
	// regardless of the catalog set (design D3).
	created, err := svc.CreateProvider(ctx, ProviderInput{Name: "anthropic", Type: agents.ProviderAnthropic, APIKey: "k"})
	require.NoError(t, err)

	models, err := svc.ProviderModels(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, []ModelCapability{
		{ID: "claude-haiku-4-5", ContextWindow: 200000, Efforts: []string{"low", "medium", "high"}, ImageInput: true, PDFInput: true, PDFParts: true, VideoParts: false},
		{ID: "claude-opus-4-1", ContextWindow: 200000, Efforts: []string{"low", "medium", "high"}, ImageInput: true, PDFInput: true, PDFParts: true, VideoParts: false},
		{ID: "claude-sonnet-4-5", ContextWindow: 200000, Efforts: []string{"low", "medium", "high"}, ImageInput: true, PDFInput: true, PDFParts: true, VideoParts: false},
	}, models)

	// Disabling a model excludes it from ProviderModels.
	storedModels, err := st.ProviderModels().ByProvider(ctx, created.ID)
	require.NoError(t, err)
	storedModels[0].Enabled = false
	require.NoError(t, st.ProviderModels().Update(ctx, storedModels[0]))

	modelsAfter, err := svc.ProviderModels(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, modelsAfter, 2)
	for _, m := range modelsAfter {
		require.NotEqual(t, storedModels[0].ModelID, m.ID)
	}

	// Live-listed provider reads stored rows without making network calls.
	liveProv, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "openrouter",
		Type:    agents.ProviderOpenRouter,
		BaseURL: "http://invalid.unreachable.domain.test",
		APIKey:  "k",
	})
	require.NoError(t, err)

	// Starts empty
	liveModels, err := svc.ProviderModels(ctx, liveProv.ID)
	require.NoError(t, err)
	require.Empty(t, liveModels)

	// Add stored models directly: one enabled undeclared, one enabled with a
	// declared effort set, one disabled.
	require.NoError(t, st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID:    liveProv.ID,
		ModelID:       "openai/gpt-5-preview",
		ContextWindow: 400000,
		Enabled:       true,
	}))
	require.NoError(t, st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID:    liveProv.ID,
		ModelID:       "vendor/declared-model",
		ContextWindow: 256000,
		Efforts:       []string{"none", "high"},
		VideoInput:    true,
		Enabled:       true,
	}))
	require.NoError(t, st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID:    liveProv.ID,
		ModelID:       "meta/llama-disabled",
		ContextWindow: 128000,
		Enabled:       false,
	}))

	// ProviderModels returns only the enabled rows: the undeclared row
	// resolves to high with no heuristic fallback, the declared row rides
	// through in exactly its declared order (design D3).
	liveModelsAfter, err := svc.ProviderModels(ctx, liveProv.ID)
	require.NoError(t, err)
	require.Equal(t, []ModelCapability{
		{ID: "openai/gpt-5-preview", ContextWindow: 400000, Efforts: []string{"high"}},
		{ID: "vendor/declared-model", ContextWindow: 256000, Efforts: []string{"none", "high"}, VideoInput: true},
	}, liveModelsAfter)
}

func TestProviderModels_PDFPartsFollowsProviderFamily(t *testing.T) {
	tests := []struct {
		name           string
		providerType   string
		wantPDFParts   bool
		wantVideoParts bool
	}{
		{name: "gemini carries PDF and video parts", providerType: agents.ProviderGemini, wantPDFParts: true, wantVideoParts: true},
		{name: "anthropic carries PDF parts only", providerType: agents.ProviderAnthropic, wantPDFParts: true, wantVideoParts: false},
		{name: "anthropic-compatible carries PDF parts only", providerType: agents.ProviderAnthropicCompatible, wantPDFParts: true, wantVideoParts: false},
		{name: "openai carries neither", providerType: agents.ProviderOpenAI, wantPDFParts: false, wantVideoParts: false},
		{name: "openrouter carries neither", providerType: agents.ProviderOpenRouter, wantPDFParts: false, wantVideoParts: false},
		{name: "openai-compatible carries neither", providerType: agents.ProviderOpenAICompatible, wantPDFParts: false, wantVideoParts: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := fake.New()
			svc := NewSettingsService(st)
			ctx := context.Background()

			// Seed directly so non-canonical types (openrouter, the
			// compatible endpoints) take the same path as canonical ones.
			provider := store.Provider{Name: "p", Type: tt.providerType, Enabled: true}
			require.NoError(t, st.Providers().Create(ctx, &provider))
			require.NoError(t, st.ProviderModels().Create(ctx, &store.ProviderModel{
				ProviderID: provider.ID,
				ModelID:    "some-model",
				Enabled:    true,
			}))

			models, err := svc.ProviderModels(ctx, provider.ID)
			require.NoError(t, err)
			require.Len(t, models, 1)
			require.Equal(t, tt.wantPDFParts, models[0].PDFParts)
			// Video carriage is gemini-only (the matrix): video parts ride
			// nowhere else, so every non-gemini family reads false.
			require.Equal(t, tt.wantVideoParts, models[0].VideoParts)

			// Derived, never stored: the row itself stays untouched.
			stored, err := st.ProviderModels().ByProvider(ctx, provider.ID)
			require.NoError(t, err)
			require.Len(t, stored, 1)
		})
	}
}

func TestProviderModels_UnknownTypeYieldsEmptyListing(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	// Seed directly: unknown types cannot come through the service anymore.
	seeded := store.Provider{Name: "x", Type: "mystery", APIKey: "k"}
	require.NoError(t, st.Providers().Create(ctx, &seeded))

	models, err := svc.ProviderModels(ctx, seeded.ID)
	require.NoError(t, err)
	require.Empty(t, models)

	// An undeclared stored row still resolves to high — the stored-first
	// rule is provider-type independent (design D3).
	require.NoError(t, st.ProviderModels().Create(ctx, &store.ProviderModel{
		ProviderID: seeded.ID,
		ModelID:    "mystery-model",
		Enabled:    true,
	}))
	models, err = svc.ProviderModels(ctx, seeded.ID)
	require.NoError(t, err)
	require.Equal(t, []ModelCapability{
		{ID: "mystery-model", ContextWindow: agents.DefaultContextWindow, Efforts: []string{"high"}, PDFParts: false, VideoParts: false},
	}, models)
}

func TestModelCRUD_LifecycleAndValidation(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	prov, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "custom-provider",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: "https://compat.example.com/v1",
		APIKey:  "secret-key-12345",
	})
	require.NoError(t, err)

	// 1. CreateModel
	created, err := svc.CreateModel(ctx, prov.ID, ProviderModelInput{
		ModelID:       "meta/llama-3-70b",
		ContextWindow: 131072,
		ImageInput:    true,
		PDFInput:      false,
		Enabled:       true,
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	require.Equal(t, prov.ID, created.ProviderID)
	require.Equal(t, "meta/llama-3-70b", created.ModelID)
	require.Equal(t, 131072, created.ContextWindow)
	require.True(t, created.ImageInput)
	require.False(t, created.PDFInput)
	require.True(t, created.Enabled)
	require.False(t, created.CreatedAt.IsZero())
	require.False(t, created.UpdatedAt.IsZero())

	// Secrecy check: JSON payload must not contain APIKey or secret
	payload, err := json.Marshal(created)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "secret-key")

	// CreateModel with ContextWindow <= 0 defaults to DefaultContextWindow
	defaultWin, err := svc.CreateModel(ctx, prov.ID, ProviderModelInput{
		ModelID: "custom-small",
		Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, agents.DefaultContextWindow, defaultWin.ContextWindow)

	// CreateModel duplicate rejection
	_, err = svc.CreateModel(ctx, prov.ID, ProviderModelInput{
		ModelID: "meta/llama-3-70b",
		Enabled: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrDuplicate)

	// CreateModel for missing provider
	_, err = svc.CreateModel(ctx, "non-existent-provider", ProviderModelInput{
		ModelID: "foo",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)

	// 2. ListProviderModels
	list, err := svc.ListProviderModels(ctx, prov.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, created.ID, list[0].ID)
	require.Equal(t, defaultWin.ID, list[1].ID)

	// ListProviderModels on non-existent provider
	_, err = svc.ListProviderModels(ctx, "non-existent")
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)

	// 3. UpdateModel
	updated, err := svc.UpdateModel(ctx, created.ID, ProviderModelInput{
		ModelID:       "meta/llama-3-70b-instruct",
		ContextWindow: 65536,
		ImageInput:    false,
		PDFInput:      true,
		Enabled:       false,
	})
	require.NoError(t, err)
	require.Equal(t, "meta/llama-3-70b-instruct", updated.ModelID)
	require.Equal(t, 65536, updated.ContextWindow)
	require.False(t, updated.ImageInput)
	require.True(t, updated.PDFInput)
	require.False(t, updated.Enabled)

	// UpdateModel duplicate rejection
	_, err = svc.UpdateModel(ctx, created.ID, ProviderModelInput{
		ModelID: "custom-small", // already taken by defaultWin
	})
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrDuplicate)

	// UpdateModel not found
	_, err = svc.UpdateModel(ctx, 999999, ProviderModelInput{
		ModelID: "anything",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)

	// 4. ToggleModel
	toggled, err := svc.ToggleModel(ctx, created.ID, true)
	require.NoError(t, err)
	require.True(t, toggled.Enabled)

	toggledOff, err := svc.ToggleModel(ctx, created.ID, false)
	require.NoError(t, err)
	require.False(t, toggledOff.Enabled)

	_, err = svc.ToggleModel(ctx, 999999, true)
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)

	// 5. DeleteModel
	require.NoError(t, svc.DeleteModel(ctx, created.ID))
	remaining, err := svc.ListProviderModels(ctx, prov.ID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, defaultWin.ID, remaining[0].ID)

	// Deleting again returns not found
	err = svc.DeleteModel(ctx, created.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestNormalizeEfforts(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "trims, drops empties, dedupes keeping first occurrence and order",
			in:   []string{" low", "low", "", "high", "high ", "  "},
			want: []string{"low", "high"},
		},
		{
			name: "nil input yields nil",
			in:   nil,
			want: nil,
		},
		{
			name: "nothing but blanks yields nil",
			in:   []string{"", " "},
			want: nil,
		},
		{
			name: "values kept verbatim, no case-folding",
			in:   []string{"none", "minimal", "none"},
			want: []string{"none", "minimal"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeEfforts(tt.in))
		})
	}
}

func TestModelCRUD_EffortsAndVideoInputRoundTrip(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	prov, err := svc.CreateProvider(ctx, ProviderInput{
		Name: "compat",
		Type: agents.ProviderOpenAICompatible,
	})
	require.NoError(t, err)

	// Create carries the declared efforts in declared order plus the video
	// flag, onto both the view and the stored row.
	created, err := svc.CreateModel(ctx, prov.ID, ProviderModelInput{
		ModelID:    "vendor/video-model",
		Efforts:    []string{"none", "high"},
		VideoInput: true,
		Enabled:    true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"none", "high"}, created.Efforts)
	require.True(t, created.VideoInput)

	stored, err := st.ProviderModels().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"none", "high"}, stored.Efforts)
	require.True(t, stored.VideoInput)

	// Updating with nil efforts clears the declaration; the view is empty
	// and the row reads back empty.
	updated, err := svc.UpdateModel(ctx, created.ID, ProviderModelInput{
		ModelID: "vendor/video-model",
		Enabled: true,
	})
	require.NoError(t, err)
	require.Empty(t, updated.Efforts)
	require.False(t, updated.VideoInput)

	stored, err = st.ProviderModels().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, stored.Efforts)
	require.False(t, stored.VideoInput)

	// Updating with whitespace and duplicates normalizes on write.
	updated, err = svc.UpdateModel(ctx, created.ID, ProviderModelInput{
		ModelID:    "vendor/video-model",
		Efforts:    []string{" low", "low"},
		VideoInput: true,
		Enabled:    true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"low"}, updated.Efforts)
	require.True(t, updated.VideoInput)

	stored, err = st.ProviderModels().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"low"}, stored.Efforts)
}

func TestCreateProvider_SeedsCanonicalModels(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	// 1. Creating a canonical Anthropic provider seeds its catalog models.
	created, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "my-anthropic",
		Type:    agents.ProviderAnthropic,
		APIKey:  "sk-ant-test",
		Enabled: true,
	})
	require.NoError(t, err)

	models, err := st.ProviderModels().ByProvider(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, models, 3)

	byID := make(map[string]*store.ProviderModel)
	for _, m := range models {
		byID[m.ModelID] = m
	}

	for _, expectedID := range []string{"claude-opus-4-1", "claude-sonnet-4-5", "claude-haiku-4-5"} {
		m, ok := byID[expectedID]
		require.True(t, ok, "expected model %s in seeded models", expectedID)
		require.Equal(t, created.ID, m.ProviderID)
		require.Equal(t, 200000, m.ContextWindow)
		require.True(t, m.ImageInput)
		require.True(t, m.PDFInput)
		require.True(t, m.Enabled)
	}

	// 2. Creating a live-listed provider (e.g. openai-compatible) starts empty.
	liveCreated, err := svc.CreateProvider(ctx, ProviderInput{
		Name:    "my-compat",
		Type:    agents.ProviderOpenAICompatible,
		BaseURL: "https://compat.example.com/v1",
		Enabled: true,
	})
	require.NoError(t, err)

	liveModels, err := st.ProviderModels().ByProvider(ctx, liveCreated.ID)
	require.NoError(t, err)
	require.Empty(t, liveModels, "live-listed provider must start with an empty model list")
}

func TestSeedCanonicalModels_IdempotentAndPreservesUserEdits(t *testing.T) {
	st := fake.New()
	svc := NewSettingsService(st)
	ctx := context.Background()

	// Seed providers directly in store with no models.
	anthropic := &store.Provider{Name: "anthropic", Type: agents.ProviderAnthropic, Enabled: true}
	openai := &store.Provider{Name: "openai", Type: agents.ProviderOpenAI, Enabled: true}
	gemini := &store.Provider{Name: "gemini", Type: agents.ProviderGemini, Enabled: true}
	openrouter := &store.Provider{Name: "openrouter", Type: agents.ProviderOpenRouter, Enabled: true}

	require.NoError(t, st.Providers().Create(ctx, anthropic))
	require.NoError(t, st.Providers().Create(ctx, openai))
	require.NoError(t, st.Providers().Create(ctx, gemini))
	require.NoError(t, st.Providers().Create(ctx, openrouter))

	// Initial sweep
	require.NoError(t, svc.SeedCanonicalModels(ctx))

	antModels, err := st.ProviderModels().ByProvider(ctx, anthropic.ID)
	require.NoError(t, err)
	require.Len(t, antModels, 3)

	oaiModels, err := st.ProviderModels().ByProvider(ctx, openai.ID)
	require.NoError(t, err)
	require.Len(t, oaiModels, 3)

	gemModels, err := st.ProviderModels().ByProvider(ctx, gemini.ID)
	require.NoError(t, err)
	require.Len(t, gemModels, 2)

	orModels, err := st.ProviderModels().ByProvider(ctx, openrouter.ID)
	require.NoError(t, err)
	require.Empty(t, orModels, "live-listed provider remains untouched by canonical seed sweep")

	// User edits a model: toggle disabled and edit context window
	var sonnet *store.ProviderModel
	for _, m := range antModels {
		if m.ModelID == "claude-sonnet-4-5" {
			sonnet = m
			break
		}
	}
	require.NotNil(t, sonnet)
	sonnet.Enabled = false
	sonnet.ContextWindow = 123456
	require.NoError(t, st.ProviderModels().Update(ctx, sonnet))

	// Re-run seed sweep (simulating startup restart)
	require.NoError(t, svc.SeedCanonicalModels(ctx))

	// Verify user edit is preserved
	reloaded, err := st.ProviderModels().ByID(ctx, sonnet.ID)
	require.NoError(t, err)
	require.False(t, reloaded.Enabled, "user toggle must be preserved across seed sweeps")
	require.Equal(t, 123456, reloaded.ContextWindow, "user edited context window must be preserved")

	// Total count must remain unchanged (no duplicates inserted)
	antModelsAfter, err := st.ProviderModels().ByProvider(ctx, anthropic.ID)
	require.NoError(t, err)
	require.Len(t, antModelsAfter, 3)

	orModelsAfter, err := st.ProviderModels().ByProvider(ctx, openrouter.ID)
	require.NoError(t, err)
	require.Empty(t, orModelsAfter)
}
