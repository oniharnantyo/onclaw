package services_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/services"
)

const sampleCatalogJSON = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-4o": {
        "id": "gpt-4o",
        "name": "GPT-4o",
        "temperature": true,
        "attachment": true,
        "reasoning": false,
        "tool_call": true,
        "limit": {
          "context": 128000,
          "output": 16384
        },
        "modalities": {
          "input": ["text", "image"],
          "output": ["text"]
        },
        "reasoning_options": [
          {
            "type": "effort",
            "values": ["minimal", "low", "medium", "high"]
          }
        ]
      },
      "o1": {
        "id": "o1",
        "name": "o1",
        "temperature": false,
        "reasoning_options": [
          {
            "type": "effort",
            "values": ["low", "medium", "high"]
          }
        ]
      }
    }
  },
  "anthropic": {
    "id": "anthropic",
    "name": "Anthropic",
    "models": {
      "claude-3-5-sonnet-20241022": {
        "id": "claude-3-5-sonnet-20241022",
        "name": "Claude 3.5 Sonnet",
        "temperature": true,
        "limit": {
          "context": 200000
        }
      }
    }
  },
  "google": {
    "id": "google",
    "name": "Google",
    "models": {
      "gemini-1.5-pro": {
        "id": "gemini-1.5-pro",
        "name": "Gemini 1.5 Pro",
        "temperature": true,
        "limit": {
          "context": 2097152
        }
      }
    }
  },
  "openrouter": {
    "id": "openrouter",
    "name": "OpenRouter",
    "models": {
      "openai/gpt-4o": {
        "id": "openai/gpt-4o",
        "name": "OpenAI: GPT-4o",
        "temperature": true
      }
    }
  }
}`

func TestMapProviderType(t *testing.T) {
	tests := []struct {
		providerType string
		wantID       string
		wantMapped   bool
	}{
		{providerType: providers.TypeOpenAI, wantID: "openai", wantMapped: true},
		{providerType: providers.TypeAnthropic, wantID: "anthropic", wantMapped: true},
		{providerType: providers.TypeGemini, wantID: "google", wantMapped: true},
		{providerType: providers.TypeOpenRouter, wantID: "openrouter", wantMapped: true},
		{providerType: providers.TypeOpenAICompatible, wantID: "", wantMapped: false},
		{providerType: providers.TypeAnthropicCompatible, wantID: "", wantMapped: false},
		{providerType: "unknown", wantID: "", wantMapped: false},
	}

	for _, tt := range tests {
		t.Run(tt.providerType, func(t *testing.T) {
			id, mapped := services.MapProviderType(tt.providerType)
			if id != tt.wantID || mapped != tt.wantMapped {
				t.Errorf("MapProviderType(%q) = (%q, %v), want (%q, %v)", tt.providerType, id, mapped, tt.wantID, tt.wantMapped)
			}
		})
	}
}

func TestFetchCatalog_TTLAndCache(t *testing.T) {
	ctx := context.Background()
	var fetchCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fetchCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer server.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})

	// First fetch: should make 1 HTTP call and write atomic cache
	data1, err := svc.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("first FetchCatalog failed: %v", err)
	}
	if len(data1.Providers) != 4 {
		t.Fatalf("data1 providers len = %d, want 4", len(data1.Providers))
	}
	if atomic.LoadInt64(&fetchCount) != 1 {
		t.Fatalf("fetchCount = %d, want 1", atomic.LoadInt64(&fetchCount))
	}

	// Verify disk file exists
	cacheFilePath := filepath.Join(cacheDir, services.CacheFileName)
	if _, err := os.Stat(cacheFilePath); err != nil {
		t.Fatalf("cache file not found on disk: %v", err)
	}

	// Second fetch within TTL: should hit memory cache (no HTTP call)
	data2, err := svc.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("second FetchCatalog failed: %v", err)
	}
	if len(data2.Providers) != 4 {
		t.Fatalf("data2 providers len = %d, want 4", len(data2.Providers))
	}
	if atomic.LoadInt64(&fetchCount) != 1 {
		t.Fatalf("fetchCount after second fetch = %d, want 1", atomic.LoadInt64(&fetchCount))
	}

	// New Service instance reading from disk cache within TTL
	svc2 := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})
	data3, err := svc2.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("third FetchCatalog failed: %v", err)
	}
	if len(data3.Providers) != 4 {
		t.Fatalf("data3 providers len = %d, want 4", len(data3.Providers))
	}
	if atomic.LoadInt64(&fetchCount) != 1 {
		t.Fatalf("fetchCount after third fetch = %d, want 1", atomic.LoadInt64(&fetchCount))
	}
}

func TestFetchCatalog_StaleServeOnError(t *testing.T) {
	ctx := context.Background()
	var serverFail atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverFail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer server.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-stale-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        10 * time.Millisecond, // very short TTL
		Client:     server.Client(),
	})

	// Initial successful fetch
	_, err = svc.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	// Now make server fail
	serverFail.Store(true)

	// Fetch should serve stale catalog rather than failing
	data, err := svc.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("FetchCatalog expected stale serve, got err: %v", err)
	}
	if len(data.Providers) != 4 {
		t.Errorf("stale data providers len = %d, want 4", len(data.Providers))
	}
}

func TestFetchCatalog_Singleflight(t *testing.T) {
	ctx := context.Background()
	var fetchCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&fetchCount, 1)
		time.Sleep(50 * time.Millisecond) // simulate delay
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer server.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-sf-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})

	var wg sync.WaitGroup
	concurrent := 10
	errorsChan := make(chan error, concurrent)

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.FetchCatalog(ctx)
			if err != nil {
				errorsChan <- err
			}
		}()
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		t.Errorf("concurrent fetch returned error: %v", err)
	}

	if atomic.LoadInt64(&fetchCount) != 1 {
		t.Errorf("singleflight fetchCount = %d, want 1", atomic.LoadInt64(&fetchCount))
	}
}

func TestDefensiveParsing(t *testing.T) {
	ctx := context.Background()

	// Test array-based root and model structures
	arrayJSON := `[
		{
			"id": "openai",
			"name": "OpenAI",
			"models": [
				{
					"id": "gpt-custom",
					"name": "Custom Model",
					"temperature": false,
					"reasoning_options": {
						"type": "effort",
						"values": ["low", "high"]
					}
				}
			]
		}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(arrayJSON))
	}))
	defer server.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-defensive-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})

	data, err := svc.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("FetchCatalog with array schema failed: %v", err)
	}

	prov, ok := data.Providers["openai"]
	if !ok {
		t.Fatalf("provider openai not found in parsed data")
	}

	model, ok := prov.Models["gpt-custom"]
	if !ok {
		t.Fatalf("model gpt-custom not found")
	}
	if model.Name != "Custom Model" {
		t.Errorf("model.Name = %q, want 'Custom Model'", model.Name)
	}
	if model.Temperature == nil || *model.Temperature != false {
		t.Errorf("model.Temperature = %v, want false", model.Temperature)
	}
	efforts := model.EffortValues()
	if len(efforts) != 2 || efforts[0] != "low" || efforts[1] != "high" {
		t.Errorf("model.EffortValues() = %v, want [low high]", efforts)
	}
}

func TestResolveModels_Tier1_LiveWithEnrichment(t *testing.T) {
	ctx := context.Background()

	// 1. Catalog server
	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer catServer.Close()

	// 2. OpenAI provider live server
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "gpt-4o", "name": "GPT-4o Live"},
				{"id": "o1", "name": "o1 Live"},
				{"id": "unknown-live-model"}
			]
		}`))
	}))
	defer liveServer.Close()

	reg := providers.NewRegistryWithClient(liveServer.Client())

	cacheDir, err := os.MkdirTemp("", "modelcatalog-tier1-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
		Registry:   reg,
	})

	res, err := svc.ResolveModels(ctx, providers.Credential{
		Type:    providers.TypeOpenAI,
		BaseURL: liveServer.URL,
		APIKey:  "sk-test",
	})
	if err != nil {
		t.Fatalf("ResolveModels failed: %v", err)
	}

	if res.Source != domain.ModelSourceLive {
		t.Fatalf("res.Source = %q, want 'live'", res.Source)
	}
	if len(res.Models) != 3 {
		t.Fatalf("res.Models len = %d, want 3", len(res.Models))
	}

	// Check model 1 (gpt-4o): enriched with catalog effort values and limit
	m1 := res.Models[0]
	if m1.ID != "gpt-4o" || m1.Name != "GPT-4o Live" {
		t.Errorf("m1 = %+v", m1)
	}
	if len(m1.Efforts) != 4 || m1.Efforts[0] != "minimal" {
		t.Errorf("m1.Efforts = %v, want [minimal low medium high]", m1.Efforts)
	}
	if !m1.SupportsTemperature {
		t.Errorf("m1.SupportsTemperature = false, want true")
	}
	if m1.ContextLimit == nil || *m1.ContextLimit != 128000 {
		t.Errorf("m1.ContextLimit = %v, want 128000", m1.ContextLimit)
	}

	// Check model 2 (o1): enriched with catalog temperature = false, no limit
	m2 := res.Models[1]
	if m2.ID != "o1" {
		t.Errorf("m2.ID = %q, want 'o1'", m2.ID)
	}
	if m2.SupportsTemperature {
		t.Errorf("m2.SupportsTemperature = true, want false (overridden by catalog)")
	}
	if m2.ContextLimit != nil {
		t.Errorf("m2.ContextLimit = %v, want nil", m2.ContextLimit)
	}

	// Check model 3 (unknown-live-model): not in catalog -> uses provider static floor
	m3 := res.Models[2]
	if m3.ID != "unknown-live-model" || m3.Name != "unknown-live-model" {
		t.Errorf("m3 = %+v", m3)
	}
	if len(m3.Efforts) != 3 || m3.Efforts[0] != "low" {
		t.Errorf("m3.Efforts = %v, want provider static floor [low medium high]", m3.Efforts)
	}
	if !m3.SupportsTemperature {
		t.Errorf("m3.SupportsTemperature = false, want true (from static floor)")
	}
	if m3.ContextLimit != nil {
		t.Errorf("m3.ContextLimit = %v, want nil", m3.ContextLimit)
	}
}

func TestResolveModels_Tier2_CatalogFallback(t *testing.T) {
	ctx := context.Background()

	// 1. Catalog server
	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer catServer.Close()

	// 2. Dead live server (fails with 500)
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "provider down"}`))
	}))
	defer liveServer.Close()

	reg := providers.NewRegistryWithClient(liveServer.Client())

	cacheDir, err := os.MkdirTemp("", "modelcatalog-tier2-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
		Registry:   reg,
	})

	// Gemini resolves to "google" catalog entry
	res, err := svc.ResolveModels(ctx, providers.Credential{
		Type:    providers.TypeGemini,
		BaseURL: liveServer.URL,
		APIKey:  "gemini-key",
	})
	if err != nil {
		t.Fatalf("ResolveModels failed: %v", err)
	}

	if res.Source != domain.ModelSourceCatalog {
		t.Fatalf("res.Source = %q, want 'catalog'", res.Source)
	}
	if len(res.Models) != 1 {
		t.Fatalf("res.Models len = %d, want 1", len(res.Models))
	}
	if res.Models[0].ID != "gemini-1.5-pro" || res.Models[0].Name != "Gemini 1.5 Pro" {
		t.Errorf("res.Models[0] = %+v, want gemini-1.5-pro", res.Models[0])
	}
	if res.Models[0].ContextLimit == nil || *res.Models[0].ContextLimit != 2097152 {
		t.Errorf("res.Models[0].ContextLimit = %v, want 2097152", res.Models[0].ContextLimit)
	}
}

func TestResolveModels_BothFail_CompatibleAndNone(t *testing.T) {
	ctx := context.Background()

	// 1. Catalog server
	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer catServer.Close()

	// 2. Dead live server
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer liveServer.Close()

	reg := providers.NewRegistryWithClient(liveServer.Client())

	cacheDir, err := os.MkdirTemp("", "modelcatalog-none-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
		Registry:   reg,
	})

	// Compatible type fails live -> skips catalog -> returns source: "none", models: []
	res, err := svc.ResolveModels(ctx, providers.Credential{
		Type:    providers.TypeOpenAICompatible,
		BaseURL: liveServer.URL,
	})
	if err != nil {
		t.Fatalf("ResolveModels failed: %v", err)
	}

	if res.Source != domain.ModelSourceNone {
		t.Errorf("res.Source = %q, want 'none'", res.Source)
	}
	if res.Models == nil || len(res.Models) != 0 {
		t.Errorf("res.Models = %+v, want empty non-nil slice", res.Models)
	}
}

func TestResolveEfforts(t *testing.T) {
	ctx := context.Background()

	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer catServer.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-efforts-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
	})

	// Case 1: Catalog knows the model with effort values (gpt-4o -> [minimal, low, medium, high])
	eff1 := svc.ResolveEfforts(ctx, providers.TypeOpenAI, "gpt-4o", "")
	if len(eff1) != 4 || eff1[0] != "minimal" {
		t.Errorf("ResolveEfforts(openai, gpt-4o) = %v, want [minimal low medium high]", eff1)
	}

	// Case 2: OpenAI model unknown to catalog -> static floor [low, medium, high]
	eff2 := svc.ResolveEfforts(ctx, providers.TypeOpenAI, "custom-gateway-model", "")
	if len(eff2) != 3 || eff2[0] != "low" {
		t.Errorf("ResolveEfforts(openai, custom) = %v, want static floor [low medium high]", eff2)
	}

	// Case 3: OpenAI-compatible -> static floor [low, medium, high]
	eff3 := svc.ResolveEfforts(ctx, providers.TypeOpenAICompatible, "local-model", "")
	if len(eff3) != 3 || eff3[0] != "low" {
		t.Errorf("ResolveEfforts(openai-compatible) = %v, want static floor [low medium high]", eff3)
	}

	// Case 4: Anthropic -> empty floor []
	eff4 := svc.ResolveEfforts(ctx, providers.TypeAnthropic, "claude-3-5-sonnet-20241022", "")
	if len(eff4) != 0 {
		t.Errorf("ResolveEfforts(anthropic) = %v, want empty slice", eff4)
	}
}

func TestResolveContextLimit(t *testing.T) {
	ctx := context.Background()

	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer catServer.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-context-limit-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
	})

	// 1. OpenAI gpt-4o -> published limit 128000
	cw1 := svc.ResolveContextLimit(ctx, providers.TypeOpenAI, "gpt-4o", "")
	if cw1 == nil || *cw1 != 128000 {
		t.Errorf("ResolveContextLimit(openai, gpt-4o) = %v, want 128000", cw1)
	}

	// 2. OpenAI o1 -> model known but no limit.context published -> returns nil
	cw2 := svc.ResolveContextLimit(ctx, providers.TypeOpenAI, "o1", "")
	if cw2 != nil {
		t.Errorf("ResolveContextLimit(openai, o1) = %v, want nil", cw2)
	}

	// 3. Anthropic claude-3-5-sonnet-20241022 -> published limit 200000
	cw3 := svc.ResolveContextLimit(ctx, providers.TypeAnthropic, "claude-3-5-sonnet-20241022", "")
	if cw3 == nil || *cw3 != 200000 {
		t.Errorf("ResolveContextLimit(anthropic, claude-3-5-sonnet) = %v, want 200000", cw3)
	}

	// 4. Compatible provider types (openai-compatible, anthropic-compatible) -> resolve nothing (return nil)
	cwCompatOpenAI := svc.ResolveContextLimit(ctx, providers.TypeOpenAICompatible, "gpt-4o", "")
	if cwCompatOpenAI != nil {
		t.Errorf("ResolveContextLimit(openai-compatible, gpt-4o) = %v, want nil", cwCompatOpenAI)
	}

	cwCompatAnthropic := svc.ResolveContextLimit(ctx, providers.TypeAnthropicCompatible, "claude-3-5-sonnet-20241022", "")
	if cwCompatAnthropic != nil {
		t.Errorf("ResolveContextLimit(anthropic-compatible, claude-3-5-sonnet) = %v, want nil", cwCompatAnthropic)
	}

	// 5. Unknown model / unknown provider
	cwUnknownModel := svc.ResolveContextLimit(ctx, providers.TypeOpenAI, "non-existent-model", "")
	if cwUnknownModel != nil {
		t.Errorf("ResolveContextLimit(openai, non-existent) = %v, want nil", cwUnknownModel)
	}

	cwUnknownProvider := svc.ResolveContextLimit(ctx, "unknown-provider", "gpt-4o", "")
	if cwUnknownProvider != nil {
		t.Errorf("ResolveContextLimit(unknown-provider, gpt-4o) = %v, want nil", cwUnknownProvider)
	}
}

// modalityCatalogJSON mirrors real models.dev entries (2026-09-11 cache):
// zai-coding-plan/glm-5.3-flash lists input modalities text/image/video/pdf
// while zai-coding-plan/glm-5.3 is text-only; textgateway hosts the SAME
// glm-5.3-flash id with a text-only input list (provider scoping).
const modalityCatalogJSON = `{
  "zai-coding-plan": {
    "id": "zai-coding-plan",
    "name": "Z.ai Coding Plan",
    "models": {
      "glm-5.3-flash": {
        "id": "glm-5.3-flash",
        "name": "GLM-5.3-Flash",
        "attachment": true,
        "reasoning": true,
        "tool_call": true,
        "limit": {"context": 200000, "output": 131072},
        "modalities": {"input": ["text", "image", "video", "pdf"], "output": ["text"]},
        "reasoning_options": [{"type": "effort", "values": ["low", "high", "max"]}]
      },
      "glm-5.3": {
        "id": "glm-5.3",
        "name": "GLM-5.3",
        "attachment": false,
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]}
      }
    }
  },
  "textgateway": {
    "id": "textgateway",
    "name": "Text-Only Gateway",
    "models": {
      "glm-5.3-flash": {
        "id": "glm-5.3-flash",
        "name": "GLM-5.3-Flash (text-only gateway)",
        "modalities": {"input": ["text"], "output": ["text"]}
      }
    }
  },
  "attachmentonly": {
    "id": "attachmentonly",
    "name": "Attachment Metadata Only",
    "models": {
      "legacy-attachment-model": {
        "id": "legacy-attachment-model",
        "name": "Legacy Attachment Model",
        "attachment": true
      }
    }
  }
}`

func newModalityCatalogService(t *testing.T) *services.ModelCatalog {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(modalityCatalogJSON))
	}))
	t.Cleanup(server.Close)

	cacheDir, err := os.MkdirTemp("", "modelcatalog-modality-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(cacheDir) })

	return services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})
}

func TestSupportsInput(t *testing.T) {
	ctx := context.Background()
	svc := newModalityCatalogService(t)

	tests := []struct {
		name         string
		providerType string
		hint         string
		modelID      string
		kind         domain.InputKind
		want         domain.InputSupport
	}{
		// Capable gateway entry → supported (image and pdf).
		{"capable gateway image", providers.TypeOpenAICompatible, "zai-coding-plan", "glm-5.3-flash", domain.InputKindImage, domain.InputSupported},
		{"capable gateway pdf", providers.TypeOpenAICompatible, "zai-coding-plan", "glm-5.3-flash", domain.InputKindPDF, domain.InputSupported},

		// Same gateway, text-only model id → unsupported (entry present, modality absent).
		{"text-only model image", providers.TypeOpenAICompatible, "zai-coding-plan", "glm-5.3", domain.InputKindImage, domain.InputUnsupported},
		{"text-only model pdf", providers.TypeOpenAICompatible, "zai-coding-plan", "glm-5.3", domain.InputKindPDF, domain.InputUnsupported},

		// Same model id on a text-only gateway → unsupported: resolution follows
		// the provider mapping, not the model name.
		{"same model id text-only gateway image", providers.TypeOpenAICompatible, "textgateway", "glm-5.3-flash", domain.InputKindImage, domain.InputUnsupported},

		// Canonical mapping wins; a hint for a different provider is ignored.
		{"mapped type ignores hint", providers.TypeOpenAI, "zai-coding-plan", "glm-5.3-flash", domain.InputKindImage, domain.InputUnknown},
		{"mapped type unknown model", providers.TypeOpenAI, "", "not-in-catalog", domain.InputKindImage, domain.InputUnknown},

		// Unmapped provider without hint → unknown.
		{"unmapped no hint image", providers.TypeOpenAICompatible, "", "glm-5.3-flash", domain.InputKindImage, domain.InputUnknown},
		{"unmapped no hint pdf", providers.TypeAnthropicCompatible, "", "glm-5.3-flash", domain.InputKindPDF, domain.InputUnknown},
		{"unknown provider type", "mystery-type", "", "glm-5.3-flash", domain.InputKindImage, domain.InputUnknown},

		// Hint pointing at an absent catalog provider → unknown.
		{"absent hint provider", providers.TypeOpenAICompatible, "no-such-provider", "glm-5.3-flash", domain.InputKindImage, domain.InputUnknown},

		// attachment==true fallback: pdf supported when modalities is absent;
		// image stays unknown (no evidence).
		{"attachment fallback pdf", providers.TypeOpenAICompatible, "attachmentonly", "legacy-attachment-model", domain.InputKindPDF, domain.InputSupported},
		{"attachment fallback image", providers.TypeOpenAICompatible, "attachmentonly", "legacy-attachment-model", domain.InputKindImage, domain.InputUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.SupportsInput(ctx, tt.providerType, tt.hint, tt.modelID, tt.kind)
			if got != tt.want {
				t.Errorf("SupportsInput(%q, hint=%q, %q, %q) = %q, want %q", tt.providerType, tt.hint, tt.modelID, tt.kind, got, tt.want)
			}
		})
	}

	// Canonically mapped provider types resolve through their own catalog
	// entry (sampleCatalogJSON carries gpt-4o with input [text image]).
	sampleSvc := newSampleCatalogService(t)
	if got := sampleSvc.SupportsInput(ctx, providers.TypeOpenAI, "", "gpt-4o", domain.InputKindImage); got != domain.InputSupported {
		t.Errorf("SupportsInput(openai, gpt-4o, image) = %q, want %q", got, domain.InputSupported)
	}
	if got := sampleSvc.SupportsInput(ctx, providers.TypeOpenAI, "", "gpt-4o", domain.InputKindPDF); got != domain.InputUnsupported {
		t.Errorf("SupportsInput(openai, gpt-4o, pdf) = %q, want %q (entry lists text+image only)", got, domain.InputUnsupported)
	}
}

// newSampleCatalogService serves sampleCatalogJSON.
func newSampleCatalogService(t *testing.T) *services.ModelCatalog {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	t.Cleanup(server.Close)

	cacheDir, err := os.MkdirTemp("", "modelcatalog-sample-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(cacheDir) })

	return services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})
}

func TestSupportsInput_CatalogUnavailable(t *testing.T) {
	ctx := context.Background()

	// Failing server, empty cache dir: FetchCatalog cannot serve anything.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-unavail-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: server.URL,
		TTL:        24 * time.Hour,
		Client:     server.Client(),
	})

	if got := svc.SupportsInput(ctx, providers.TypeOpenAICompatible, "zai-coding-plan", "glm-5.3-flash", domain.InputKindImage); got != domain.InputUnknown {
		t.Errorf("SupportsInput with unavailable catalog = %q, want %q", got, domain.InputUnknown)
	}
}

func TestEffectiveCatalogHint(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		storedHint   string
		baseURL      string
		want         string
	}{
		// Canonical mapping wins: the stored hint is ignored entirely.
		{"openai ignores hint", providers.TypeOpenAI, "zai-coding-plan", "https://api.z.ai/v1", ""},
		{"anthropic ignores hint", providers.TypeAnthropic, "openrouter", "https://api.zhipuai.cn/v1", ""},
		{"openrouter ignores hint", providers.TypeOpenRouter, "deepseek", "https://openrouter.ai/api/v1", ""},

		// Compatible types: stored hint wins over the host suggestion.
		{"stored hint wins", providers.TypeOpenAICompatible, "zai-coding-plan", "https://api.groq.com/openai/v1", "zai-coding-plan"},
		{"stored hint anthropic-compatible", providers.TypeAnthropicCompatible, " deepseek ", "https://api.z.ai/v1", "deepseek"},

		// Compatible types without hint: host suggestion.
		{"host z.ai", providers.TypeOpenAICompatible, "", "https://api.z.ai/api/paas/v4", "zai-coding-plan"},
		{"host zhipuai", providers.TypeOpenAICompatible, "", "https://api.zhipuai.cn/v1", "zhipuai-coding-plan"},
		{"host openrouter", providers.TypeAnthropicCompatible, "", "https://openrouter.ai/api/v1", "openrouter"},
		{"host deepseek", providers.TypeOpenAICompatible, "", "https://api.deepseek.com/v1", "deepseek"},
		{"host mistral", providers.TypeOpenAICompatible, "", "https://api.mistral.ai/v1", "mistral"},
		{"host groq", providers.TypeOpenAICompatible, "", "https://api.groq.com/openai/v1", "groq"},
		{"host fireworks", providers.TypeOpenAICompatible, "", "https://api.fireworks.ai/inference/v1", "fireworks-ai"},

		// Unknown host (or empty base URL) without hint → empty (unmapped).
		{"unknown host", providers.TypeOpenAICompatible, "", "https://llm.corp.example/internal/v1", ""},
		{"empty base url", providers.TypeOpenAICompatible, "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := services.EffectiveCatalogHint(tt.providerType, tt.storedHint, tt.baseURL)
			if got != tt.want {
				t.Errorf("EffectiveCatalogHint(%q, %q, %q) = %q, want %q", tt.providerType, tt.storedHint, tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestResolveModels_CompatibleGatewayWithHint(t *testing.T) {
	ctx := context.Background()

	// Dead live server: tier-1 fails, tier-2 catalog list must come from the hint.
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer liveServer.Close()

	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(modalityCatalogJSON))
	}))
	defer catServer.Close()

	cacheDir, err := os.MkdirTemp("", "modelcatalog-hint-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	reg := providers.NewRegistryWithClient(liveServer.Client())
	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
		Registry:   reg,
	})

	// Without a hint the compatible gateway stays unmapped → source "none".
	withoutHint, err := svc.ResolveModels(ctx, providers.Credential{
		Type:    providers.TypeOpenAICompatible,
		BaseURL: liveServer.URL,
	})
	if err != nil {
		t.Fatalf("ResolveModels without hint failed: %v", err)
	}
	if withoutHint.Source != domain.ModelSourceNone || len(withoutHint.Models) != 0 {
		t.Fatalf("without hint: source = %q, models = %d, want none/0", withoutHint.Source, len(withoutHint.Models))
	}

	// With the hint the catalog model list resolves, carrying capability fields.
	withHint, err := svc.ResolveModels(ctx, providers.Credential{
		Type:        providers.TypeOpenAICompatible,
		BaseURL:     liveServer.URL,
		CatalogHint: "zai-coding-plan",
	})
	if err != nil {
		t.Fatalf("ResolveModels with hint failed: %v", err)
	}
	if withHint.Source != domain.ModelSourceCatalog {
		t.Fatalf("with hint: source = %q, want %q", withHint.Source, domain.ModelSourceCatalog)
	}

	byID := make(map[string]domain.Model, len(withHint.Models))
	for _, m := range withHint.Models {
		byID[m.ID] = m
	}

	flash, ok := byID["glm-5.3-flash"]
	if !ok {
		t.Fatalf("glm-5.3-flash missing from catalog list: %+v", withHint.Models)
	}
	if !flash.ImageInput || !flash.PDFInput {
		t.Errorf("glm-5.3-flash capabilities = image:%v pdf:%v, want both true", flash.ImageInput, flash.PDFInput)
	}
	if flash.Reasoning == nil || !*flash.Reasoning || flash.ToolCall == nil || !*flash.ToolCall {
		t.Errorf("glm-5.3-flash reasoning/tool_call = %v/%v, want true/true", flash.Reasoning, flash.ToolCall)
	}
	if flash.ContextLimit == nil || *flash.ContextLimit != 200000 {
		t.Errorf("glm-5.3-flash context limit = %v, want 200000", flash.ContextLimit)
	}
	if len(flash.Efforts) != 3 || flash.Efforts[0] != "low" {
		t.Errorf("glm-5.3-flash efforts = %v, want [low high max]", flash.Efforts)
	}

	textOnly, ok := byID["glm-5.3"]
	if !ok {
		t.Fatalf("glm-5.3 missing from catalog list: %+v", withHint.Models)
	}
	if textOnly.ImageInput || textOnly.PDFInput {
		t.Errorf("glm-5.3 capabilities = image:%v pdf:%v, want both false (text-only)", textOnly.ImageInput, textOnly.PDFInput)
	}
	if textOnly.Reasoning == nil || !*textOnly.Reasoning {
		t.Errorf("glm-5.3 reasoning = %v, want true", textOnly.Reasoning)
	}
}

func TestResolveModels_Tier1_ProjectsCapabilities(t *testing.T) {
	ctx := context.Background()

	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalogJSON))
	}))
	defer catServer.Close()

	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4o", "name": "GPT-4o Live"}]}`))
	}))
	defer liveServer.Close()

	reg := providers.NewRegistryWithClient(liveServer.Client())

	cacheDir, err := os.MkdirTemp("", "modelcatalog-tier1-cap-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(cacheDir)

	svc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
		Registry:   reg,
	})

	res, err := svc.ResolveModels(ctx, providers.Credential{
		Type:    providers.TypeOpenAI,
		BaseURL: liveServer.URL,
		APIKey:  "sk-test",
	})
	if err != nil {
		t.Fatalf("ResolveModels failed: %v", err)
	}
	if res.Source != domain.ModelSourceLive || len(res.Models) != 1 {
		t.Fatalf("source = %q, models = %d, want live/1", res.Source, len(res.Models))
	}

	m := res.Models[0]
	if !m.ImageInput {
		t.Errorf("gpt-4o live model ImageInput = false, want true (catalog modalities)")
	}
	if m.PDFInput {
		t.Errorf("gpt-4o live model PDFInput = true, want false (catalog lists text+image only)")
	}
	// Catalog says reasoning:false — only affirmative true is projected.
	if m.Reasoning != nil {
		t.Errorf("gpt-4o live model Reasoning = %v, want nil (catalog reasoning:false)", *m.Reasoning)
	}
	if m.ToolCall == nil || !*m.ToolCall {
		t.Errorf("gpt-4o live model ToolCall = %v, want true", m.ToolCall)
	}
}

func TestResolveContextLimit_WithHint(t *testing.T) {
	ctx := context.Background()
	svc := newModalityCatalogService(t)

	// Compatible gateway + hint resolves the catalog limit.
	if got := svc.ResolveContextLimit(ctx, providers.TypeOpenAICompatible, "glm-5.3-flash", "zai-coding-plan"); got == nil || *got != 200000 {
		t.Errorf("ResolveContextLimit(compatible, glm-5.3-flash, hint) = %v, want 200000", got)
	}

	// Without the hint still nil.
	if got := svc.ResolveContextLimit(ctx, providers.TypeOpenAICompatible, "glm-5.3-flash", ""); got != nil {
		t.Errorf("ResolveContextLimit(compatible, glm-5.3-flash, no hint) = %v, want nil", got)
	}
}

func TestResolveEfforts_WithHint(t *testing.T) {
	ctx := context.Background()
	svc := newModalityCatalogService(t)

	got := svc.ResolveEfforts(ctx, providers.TypeOpenAICompatible, "glm-5.3-flash", "zai-coding-plan")
	if len(got) != 3 || got[0] != "low" {
		t.Errorf("ResolveEfforts(compatible, glm-5.3-flash, hint) = %v, want [low high max]", got)
	}

	// Without the hint: static floor for openai-compatible [low medium high].
	gotFloor := svc.ResolveEfforts(ctx, providers.TypeOpenAICompatible, "glm-5.3-flash", "")
	if len(gotFloor) != 3 || gotFloor[0] != "low" || gotFloor[2] != "high" {
		t.Errorf("ResolveEfforts(compatible, glm-5.3-flash, no hint) = %v, want static floor", gotFloor)
	}
}
