package domain_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestProviderConfig_HasKey(t *testing.T) {
	var nilCfg *domain.ProviderConfig
	if nilCfg.HasKey() {
		t.Errorf("nil config HasKey() = true, want false")
	}

	cfgNoKey := &domain.ProviderConfig{
		ID:   "p-1",
		Name: "OpenAI",
	}
	if cfgNoKey.HasKey() {
		t.Errorf("empty ciphertext HasKey() = true, want false")
	}

	cfgWithKey := &domain.ProviderConfig{
		ID:            "p-2",
		Name:          "OpenAI",
		KeyCiphertext: "v1:nonce:ciphertext",
	}
	if !cfgWithKey.HasKey() {
		t.Errorf("set ciphertext HasKey() = false, want true")
	}
}

func TestProviderConfig_JSONSecrecy(t *testing.T) {
	cfg := &domain.ProviderConfig{
		ID:            "prov-123",
		WorkspaceID:   "ws-456",
		Type:          "openai",
		Name:          "Production OpenAI",
		BaseURL:       "https://api.openai.com",
		KeyCiphertext: "v1:SECRET_NONCE:SECRET_CIPHERTEXT",
		KeyHint:       "abcd",
		Enabled:       true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	str := string(data)
	if strings.Contains(str, "SECRET_CIPHERTEXT") || strings.Contains(str, "key_ciphertext") {
		t.Errorf("json.Marshal(ProviderConfig) exposed KeyCiphertext: %s", str)
	}
}

func TestKeyHint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty key", input: "", expected: ""},
		{name: "whitespace only", input: "   ", expected: ""},
		{name: "short key 1 char", input: "a", expected: "a"},
		{name: "short key 3 chars", input: "abc", expected: "abc"},
		{name: "exact 4 chars", input: "1234", expected: "1234"},
		{name: "standard openai key", input: "sk-proj-abc123456789xyz", expected: "9xyz"},
		{name: "key with trailing whitespace", input: "  sk-secret-9999  ", expected: "9999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.GenerateKeyHint(tt.input)
			if got != tt.expected {
				t.Errorf("KeyHint(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
