package domain

import (
	"strings"
	"time"
)

// ProviderConfig represents a workspace-scoped provider configuration and credential vault entry.
//
// KeyCiphertext and KeyHint are sensitive credential fields stored at rest.
// KeyCiphertext contains the AES-256-GCM envelope and must NEVER cross the HTTP boundary
// or be serialized in API responses. KeyHint contains only the last 4 characters of the key
// (or empty if no key is set) and is exposed in API responses for display only.
type ProviderConfig struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	Type            string    `json:"type"`
	Name            string    `json:"name"`
	BaseURL         string    `json:"base_url,omitempty"`
	CatalogProvider string    `json:"catalog_provider,omitempty"`
	KeyCiphertext   string    `json:"-"`
	KeyHint         string    `json:"key_hint,omitempty"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// HasKey reports whether the provider config has an encrypted key set.
func (p *ProviderConfig) HasKey() bool {
	return p != nil && p.KeyCiphertext != ""
}

// GenerateKeyHint returns the last 4 characters of the key for display purposes.
// If the key is empty, it returns an empty string. If the key length is 4 or fewer
// characters, the entire key is returned.
func GenerateKeyHint(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return trimmed
	}
	return trimmed[len(trimmed)-4:]
}

// ModelSource indicates the source of a resolved model list (live, catalog, or none).
type ModelSource string

const (
	ModelSourceLive    ModelSource = "live"
	ModelSourceCatalog ModelSource = "catalog"
	ModelSourceNone    ModelSource = "none"
)

// Model represents a resolved model with metadata. Capability fields are
// projected only when affirmatively true: ImageInput/PDFInput are bools set
// solely from an affirmative catalog modalities.input entry; Reasoning and
// ToolCall are pointers so explicit false from the catalog survives while an
// absent catalog entry stays omitted.
type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Efforts             []string `json:"efforts"`
	SupportsTemperature bool     `json:"supports_temperature"`
	ContextLimit        *int     `json:"context_limit,omitempty"`
	ImageInput          bool     `json:"image_input,omitempty"`
	PDFInput            bool     `json:"pdf_input,omitempty"`
	Reasoning           *bool    `json:"reasoning,omitempty"`
	ToolCall            *bool    `json:"tool_call,omitempty"`
}

// ModelsResult represents the result of model resolution.
type ModelsResult struct {
	Source ModelSource `json:"source"`
	Models []Model     `json:"models"`
}
