package modelmeta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 5 * time.Second,
}

type openaiModel struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"`
}

type openaiModelsResponse struct {
	Data []openaiModel `json:"data"`
}

type ollamaModel struct {
	Name string `json:"name"`
}

type ollamaTagsResponse struct {
	Models []ollamaModel `json:"models"`
}

type ollamaShowResponse struct {
	ModelInfo map[string]interface{} `json:"model_info"`
}

// FetchCatalog fetches the models.dev catalog from the given URL.
func FetchCatalog(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FetchOpenAIModels calls GET {base_url}/v1/models.
func FetchOpenAIModels(ctx context.Context, baseURL string, apiKey string, providerType string) (*openaiModelsResponse, error) {
	if cacheVal := ctx.Value(OpenaiModelsCacheKey); cacheVal != nil {
		if cache, ok := cacheVal.(*ModelCache); ok && cache.OpenaiResponse != nil {
			return cache.OpenaiResponse, nil
		}
	}

	url := fmt.Sprintf("%s/models", strings.TrimSuffix(baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if apiKey != "" {
		if strings.ToLower(providerType) == "anthropic" {
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", url, resp.StatusCode)
	}

	var result openaiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if cacheVal := ctx.Value(OpenaiModelsCacheKey); cacheVal != nil {
		if cache, ok := cacheVal.(*ModelCache); ok {
			cache.OpenaiResponse = &result
		}
	}

	return &result, nil
}

// FetchOllamaModels calls GET {base_url}/api/tags.
func FetchOllamaModels(ctx context.Context, baseURL string) (*ollamaTagsResponse, error) {
	u := strings.TrimSuffix(baseURL, "/")
	u = strings.TrimSuffix(u, "/v1")
	url := fmt.Sprintf("%s/api/tags", u)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", url, resp.StatusCode)
	}

	var result ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// FetchOllamaShow calls POST {base_url}/api/show with model name.
func FetchOllamaShow(ctx context.Context, baseURL string, modelID string) (*ollamaShowResponse, error) {
	u := strings.TrimSuffix(baseURL, "/")
	u = strings.TrimSuffix(u, "/v1")
	url := fmt.Sprintf("%s/api/show", u)
	body := map[string]string{"name": modelID}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST %s returned status %d", url, resp.StatusCode)
	}

	var result ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
