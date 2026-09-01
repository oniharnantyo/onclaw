package modelcatalog_test

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
	"github.com/oniharnantyo/onclaw/internal/modelcatalog"
	"github.com/oniharnantyo/onclaw/internal/providers"
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
        "temperature": true
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
        "temperature": true
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
			id, mapped := modelcatalog.MapProviderType(tt.providerType)
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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
	cacheFilePath := filepath.Join(cacheDir, modelcatalog.CacheFileName)
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
	svc2 := modelcatalog.NewService(modelcatalog.Options{
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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

	// Check model 1 (gpt-4o): enriched with catalog effort values ["minimal", "low", "medium", "high"]
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

	// Check model 2 (o1): enriched with catalog temperature = false
	m2 := res.Models[1]
	if m2.ID != "o1" {
		t.Errorf("m2.ID = %q, want 'o1'", m2.ID)
	}
	if m2.SupportsTemperature {
		t.Errorf("m2.SupportsTemperature = true, want false (overridden by catalog)")
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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

	svc := modelcatalog.NewService(modelcatalog.Options{
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

	svc := modelcatalog.NewService(modelcatalog.Options{
		CacheDir:   cacheDir,
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
	})

	// Case 1: Catalog knows the model with effort values (gpt-4o -> [minimal, low, medium, high])
	eff1 := svc.ResolveEfforts(ctx, providers.TypeOpenAI, "gpt-4o")
	if len(eff1) != 4 || eff1[0] != "minimal" {
		t.Errorf("ResolveEfforts(openai, gpt-4o) = %v, want [minimal low medium high]", eff1)
	}

	// Case 2: OpenAI model unknown to catalog -> static floor [low, medium, high]
	eff2 := svc.ResolveEfforts(ctx, providers.TypeOpenAI, "custom-gateway-model")
	if len(eff2) != 3 || eff2[0] != "low" {
		t.Errorf("ResolveEfforts(openai, custom) = %v, want static floor [low medium high]", eff2)
	}

	// Case 3: OpenAI-compatible -> static floor [low, medium, high]
	eff3 := svc.ResolveEfforts(ctx, providers.TypeOpenAICompatible, "local-model")
	if len(eff3) != 3 || eff3[0] != "low" {
		t.Errorf("ResolveEfforts(openai-compatible) = %v, want static floor [low medium high]", eff3)
	}

	// Case 4: Anthropic -> empty floor []
	eff4 := svc.ResolveEfforts(ctx, providers.TypeAnthropic, "claude-3-5-sonnet-20241022")
	if len(eff4) != 0 {
		t.Errorf("ResolveEfforts(anthropic) = %v, want empty slice", eff4)
	}
}
