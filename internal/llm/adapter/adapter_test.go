package adapter_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestIsKeyless(t *testing.T) {
	if !adapter.IsKeyless("ollama") {
		t.Error("expected ollama to be keyless")
	}
	if adapter.IsKeyless("openai") {
		t.Error("expected openai not to be keyless")
	}
}

func TestRegistryAndStub(t *testing.T) {
	r := adapter.NewRegistry()
	_, err := r.Get("stub")
	if err == nil {
		t.Error("expected Get before Register to fail")
	}

	adapter.DefaultAdapters(r)

	ad, err := r.Get("stub")
	if err != nil {
		t.Fatalf("failed to get stub: %v", err)
	}

	ctx := context.Background()
	p := &store.Profile{
		Name:         "test",
		ProviderType: "stub",
		Enabled:      1,
	}

	m, err := ad.Build(ctx, p, "model", "")
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil model")
	}

	resp, err := m.Generate(ctx, nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp == nil {
		t.Error("expected non-nil response")
	}

	sr, err := m.Stream(ctx, nil)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if sr == nil {
		t.Error("expected non-nil StreamReader")
	}
	defer sr.Close()
}

func TestAdapterBuildErrors(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)

	providers := []string{"openai", "anthropic", "google", "deepseek", "qwen", "ark"}
	ctx := context.Background()

	for _, provider := range providers {
		ad, err := r.Get(provider)
		if err != nil {
			t.Fatalf("failed to get adapter %s: %v", provider, err)
		}

		// 1. Profile is disabled
		pDisabled := &store.Profile{Name: "test", ProviderType: provider, Enabled: 0}
		_, err = ad.Build(ctx, pDisabled, "model", "key")
		if err == nil {
			t.Errorf("expected error for disabled profile on provider %s", provider)
		}

		// 2. Model name is empty
		pEnabled := &store.Profile{Name: "test", ProviderType: provider, Enabled: 1}
		_, err = ad.Build(ctx, pEnabled, "", "key")
		if err == nil {
			t.Errorf("expected error for empty model name on provider %s", provider)
		}

		// 3. API key is empty
		_, err = ad.Build(ctx, pEnabled, "model", "")
		if err == nil {
			t.Errorf("expected error for empty API key on provider %s", provider)
		}

		// 4. Invalid settings JSON
		pBadSettings := &store.Profile{Name: "test", ProviderType: provider, Enabled: 1, Settings: "bad-json"}
		_, err = ad.Build(ctx, pBadSettings, "model", "key")
		if err == nil {
			t.Errorf("expected error for invalid settings JSON on provider %s", provider)
		}
	}
}

func TestAdapterBuildSuccess(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)

	ctx := context.Background()

	// 1. OpenAI
	openaiAd, _ := r.Get("openai")
	openaiProfile := &store.Profile{
		Name:         "test",
		ProviderType: "openai",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "top_p": 0.9, "stop": ["\n"], "reasoning_effort": "high"}`,
	}
	_, err := openaiAd.Build(ctx, openaiProfile, "gpt-4", "test-key")
	if err != nil {
		t.Errorf("failed to build openai: %v", err)
	}

	// 2. Anthropic
	claudeAd, _ := r.Get("anthropic")
	claudeProfile := &store.Profile{
		Name:         "test",
		ProviderType: "anthropic",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "top_p": 0.9, "reasoning_effort": "medium", "reasoning_budget_tokens": 2048}`,
	}
	_, err = claudeAd.Build(ctx, claudeProfile, "claude-3-opus", "test-key")
	if err != nil {
		t.Errorf("failed to build anthropic: %v", err)
	}

	// 3. Gemini
	geminiAd, _ := r.Get("gemini")
	geminiProfile := &store.Profile{
		Name:         "test",
		ProviderType: "gemini",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "top_p": 0.9, "reasoning_effort": "medium", "reasoning_budget_tokens": 1024}`,
	}
	_, err = geminiAd.Build(ctx, geminiProfile, "gemini-1.5-pro", "test-key")
	if err != nil {
		t.Errorf("failed to build gemini: %v", err)
	}

	// 4. DeepSeek
	dsAd, _ := r.Get("deepseek")
	dsProfile := &store.Profile{
		Name:         "test",
		ProviderType: "deepseek",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100}`,
	}
	_, err = dsAd.Build(ctx, dsProfile, "deepseek-chat", "test-key")
	if err != nil {
		t.Errorf("failed to build deepseek: %v", err)
	}

	// 5. Qwen
	qwenAd, _ := r.Get("qwen")
	qwenProfile := &store.Profile{
		Name:         "test",
		ProviderType: "qwen",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "reasoning_effort": "on"}`,
	}
	_, err = qwenAd.Build(ctx, qwenProfile, "qwen-max", "test-key")
	if err != nil {
		t.Errorf("failed to build qwen: %v", err)
	}

	// 6. Ark
	arkAd, _ := r.Get("ark")
	arkProfile := &store.Profile{
		Name:         "test",
		ProviderType: "ark",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "reasoning_effort": "medium"}`,
	}
	_, err = arkAd.Build(ctx, arkProfile, "ark-model", "test-key")
	if err != nil {
		t.Errorf("failed to build ark: %v", err)
	}
}

// TestStubStreamEmitsIndexedDeltas verifies the stub adapter emits at least two
// delta blocks that share a stable streaming_meta.index, so streaming + the
// client-side delta merge are exercisable without a real provider.
func TestStubStreamEmitsIndexedDeltas(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)
	ad, err := r.Get("stub")
	if err != nil {
		t.Fatalf("get stub adapter: %v", err)
	}

	ctx := context.Background()
	m, err := ad.Build(ctx, &store.Profile{Name: "test", ProviderType: "stub", Enabled: 1}, "model", "")
	if err != nil {
		t.Fatalf("build stub model: %v", err)
	}

	sr, err := m.Stream(ctx, nil)
	if err != nil {
		t.Fatalf("stub Stream: %v", err)
	}
	defer sr.Close()

	var blocks []*schema.ContentBlock
	for {
		msg, err := sr.Recv()
		if err != nil {
			break
		}
		blocks = append(blocks, msg.ContentBlocks...)
	}

	if len(blocks) < 2 {
		t.Fatalf("expected >=2 delta blocks, got %d", len(blocks))
	}

	firstIdx := blocks[0].StreamingMeta.Index
	for i, b := range blocks {
		if b.StreamingMeta == nil || b.StreamingMeta.Index != firstIdx {
			t.Errorf("block %d has unstable streaming index (want %d)", i, firstIdx)
		}
	}
}
