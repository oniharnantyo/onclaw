package modelmeta_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/modelmeta"
)

func TestResolveLayersAndFallback(t *testing.T) {
	ctx := context.Background()

	// 1. Prepare Mock Catalog
	catalog := &modelmeta.ApiJSON{
		Providers: map[string]modelmeta.ProviderObj{
			"openai": {
				Models: map[string]modelmeta.ModelObj{
					"gpt-4o": {
						Limit:      modelmeta.LimitObj{Context: 128000},
						Reasoning:  false,
						Modalities: modelmeta.ModalitiesObj{Input: []string{"text", "image"}},
					},
					"gpt-3.5-turbo": {
						Limit:      modelmeta.LimitObj{Context: 16385},
						Reasoning:  false,
						Modalities: modelmeta.ModalitiesObj{Input: []string{"text"}},
					},
				},
			},
			"ollama": {
				Models: map[string]modelmeta.ModelObj{
					"llama3": {
						Limit:      modelmeta.LimitObj{Context: 8192},
						Reasoning:  false,
						Modalities: modelmeta.ModalitiesObj{Input: []string{"text"}},
					},
				},
			},
		},
	}

	// 2. Scenario: Unknown model falls back to defaults
	metaDefault := modelmeta.Resolve(ctx, "non-existent-model", "openai", "http://localhost", "", catalog)
	if metaDefault.ContextWindow != 0 {
		t.Errorf("expected context window 0, got %d", metaDefault.ContextWindow)
	}
	if metaDefault.Thinking != false {
		t.Errorf("expected thinking false")
	}
	if len(metaDefault.InputModalities) != 1 || metaDefault.InputModalities[0] != "text" {
		t.Errorf("expected modalities [text], got %v", metaDefault.InputModalities)
	}

	// 3. Scenario: Found in Catalog directly
	metaCatalog := modelmeta.Resolve(ctx, "gpt-4o", "openai", "http://localhost", "", catalog)
	if metaCatalog.ContextWindow != 128000 {
		t.Errorf("expected 128000, got %d", metaCatalog.ContextWindow)
	}
	if len(metaCatalog.InputModalities) != 2 || metaCatalog.InputModalities[0] != "text" || metaCatalog.InputModalities[1] != "image" {
		t.Errorf("expected [text, image], got %v", metaCatalog.InputModalities)
	}

	// 4. Scenario: Global search fallback (mismatched provider type, e.g. openai-compatible)
	metaGlobal := modelmeta.Resolve(ctx, "gpt-4o", "openai-compatible", "http://localhost", "", catalog)
	if metaGlobal.ContextWindow != 128000 {
		t.Errorf("expected 128000 from global search, got %d", metaGlobal.ContextWindow)
	}

	// 5. Scenario: Ollama native context window via POST /api/show
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return ollama show style model info
		_, _ = w.Write([]byte(`{
			"model_info": {
				"llama.context_length": 4096
			}
		}`))
	}))
	defer ollamaServer.Close()

	metaOllamaNative := modelmeta.Resolve(ctx, "llama3", "ollama", ollamaServer.URL, "", catalog)
	// Even though catalog has llama3 context_window as 8192, native provider-native source should take precedence!
	if metaOllamaNative.ContextWindow != 4096 {
		t.Errorf("expected native context length 4096, got %d", metaOllamaNative.ContextWindow)
	}
	// Other fields (modalities, thinking) should still be populated from the catalog!
	if len(metaOllamaNative.InputModalities) != 1 || metaOllamaNative.InputModalities[0] != "text" {
		t.Errorf("expected modalities [text], got %v", metaOllamaNative.InputModalities)
	}
}

func TestApiJSONUnmarshalRegression(t *testing.T) {
	jsonData := `{
		"openai": {
			"models": {
				"gpt-4o": {
					"limit": {
						"context": 128000
					},
					"reasoning": true,
					"modalities": {
						"input": ["text", "image"]
					}
				}
			}
		}
	}`

	var catalog modelmeta.ApiJSON
	if err := json.Unmarshal([]byte(jsonData), &catalog); err != nil {
		t.Fatalf("failed to unmarshal real-shaped JSON: %v", err)
	}

	prov, ok := catalog.Providers["openai"]
	if !ok {
		t.Fatalf("expected openai provider in catalog")
	}

	model, ok := prov.Models["gpt-4o"]
	if !ok {
		t.Fatalf("expected gpt-4o model in openai provider")
	}

	if model.Limit.Context != 128000 {
		t.Errorf("expected context limit 128000, got %d", model.Limit.Context)
	}
	if model.Reasoning != true {
		t.Errorf("expected reasoning true, got %t", model.Reasoning)
	}
	if len(model.Modalities.Input) != 2 || model.Modalities.Input[0] != "text" || model.Modalities.Input[1] != "image" {
		t.Errorf("expected modalities [text, image], got %v", model.Modalities.Input)
	}
}

func TestResolveReasoningOptions(t *testing.T) {
	jsonData := `{
		"openai": {
			"models": {
				"o1-preview": {
					"limit": {
						"context": 128000
					},
					"reasoning": true,
					"reasoning_options": [
						{
							"type": "effort",
							"values": ["low", "medium", "high"]
						}
					],
					"modalities": {
						"input": ["text"]
					}
				},
				"o3-mini": {
					"limit": {
						"context": 200000
					},
					"reasoning": true,
					"reasoning_options": [
						{
							"type": "effort",
							"values": ["low", "medium", "high"]
						},
						{
							"type": "budget_tokens",
							"min": 1024,
							"max": 65536
						}
					],
					"modalities": {
						"input": ["text"]
					}
				},
				"claude-3-7-sonnet": {
					"limit": {
						"context": 200000
					},
					"reasoning": true,
					"reasoning_options": [
						{
							"type": "toggle"
						},
						{
							"type": "budget_tokens",
							"min": 1024,
							"max": 65536
						}
					],
					"modalities": {
						"input": ["text"]
					}
				}
			}
		}
	}`

	var catalog modelmeta.ApiJSON
	if err := json.Unmarshal([]byte(jsonData), &catalog); err != nil {
		t.Fatalf("failed to unmarshal JSON with reasoning options: %v", err)
	}

	ctx := context.Background()

	// 1. Verify o1-preview (effort type option)
	metaO1 := modelmeta.Resolve(ctx, "o1-preview", "openai", "http://localhost", "", &catalog)
	if !metaO1.Thinking {
		t.Errorf("expected thinking true for o1-preview")
	}
	if len(metaO1.ReasoningOptions) != 1 {
		t.Errorf("expected 1 reasoning option, got %d", len(metaO1.ReasoningOptions))
	} else {
		opt := metaO1.ReasoningOptions[0]
		if opt.Type != "effort" {
			t.Errorf("expected type effort, got %s", opt.Type)
		}
		if len(opt.Values) != 3 || opt.Values[0] != "low" || opt.Values[1] != "medium" || opt.Values[2] != "high" {
			t.Errorf("expected values [low, medium, high], got %v", opt.Values)
		}
	}

	// 2. Verify o3-mini (effort and budget_tokens)
	metaO3 := modelmeta.Resolve(ctx, "o3-mini", "openai", "http://localhost", "", &catalog)
	if len(metaO3.ReasoningOptions) != 2 {
		t.Errorf("expected 2 reasoning options, got %d", len(metaO3.ReasoningOptions))
	} else {
		opt0 := metaO3.ReasoningOptions[0]
		opt1 := metaO3.ReasoningOptions[1]
		if opt0.Type != "effort" || opt1.Type != "budget_tokens" {
			t.Errorf("expected [effort, budget_tokens], got [%s, %s]", opt0.Type, opt1.Type)
		}
		if opt1.Min != 1024 || opt1.Max != 65536 {
			t.Errorf("expected min/max 1024/65536, got %d/%d", opt1.Min, opt1.Max)
		}
	}

	// 3. Verify claude-3-7-sonnet (toggle and budget_tokens)
	metaClaude := modelmeta.Resolve(ctx, "claude-3-7-sonnet", "openai", "http://localhost", "", &catalog)
	if len(metaClaude.ReasoningOptions) != 2 {
		t.Errorf("expected 2 reasoning options, got %d", len(metaClaude.ReasoningOptions))
	} else {
		opt0 := metaClaude.ReasoningOptions[0]
		if opt0.Type != "toggle" {
			t.Errorf("expected type toggle, got %s", opt0.Type)
		}
	}
}
