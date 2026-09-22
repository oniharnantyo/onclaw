package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// The instance OAuth app's structural validation (add-connection-oauth
// tasks.md 1.4): provider and client id required; the secret is lifecycle
// state (write-only at the API, envelope at rest) and carries no shape rule
// here.
func TestInstanceOAuthAppValidate(t *testing.T) {
	valid := domain.InstanceOAuthApp{
		Provider:               "atlassian",
		ClientID:               "Aa1Bb2Cc3",
		ClientSecretCiphertext: "v1:bnB4:Y2lwaGVydGV4dA",
		ClientSecretHint:       "t4",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid app, got %v", err)
	}

	tests := []struct {
		name   string
		mutate func(a *domain.InstanceOAuthApp)
	}{
		{"empty provider", func(a *domain.InstanceOAuthApp) { a.Provider = "" }},
		{"blank provider", func(a *domain.InstanceOAuthApp) { a.Provider = "  " }},
		{"empty client id", func(a *domain.InstanceOAuthApp) { a.ClientID = "" }},
		{"blank client id", func(a *domain.InstanceOAuthApp) { a.ClientID = " " }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := valid
			tt.mutate(&a)
			if err := a.Validate(); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	if err := (*domain.InstanceOAuthApp)(nil).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil app, got %v", err)
	}
}

// The write-only contract is structural: the secret envelope must never be
// serialized, the hint is the only secret-shaped field on the wire.
func TestInstanceOAuthAppJSON(t *testing.T) {
	app := domain.InstanceOAuthApp{
		Provider:               "atlassian",
		ClientID:               "Aa1Bb2Cc3",
		ClientSecretCiphertext: "v1:bnB4:Y2lwaGVydGV4dA",
		ClientSecretHint:       "t4",
	}
	data, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	for _, key := range []string{"provider", "client_id", "client_secret_hint"} {
		if _, ok := wire[key]; !ok {
			t.Errorf("expected %q in the served app JSON, got %s", key, data)
		}
	}
	for _, key := range []string{"client_secret", "client_secret_ciphertext"} {
		if _, ok := wire[key]; ok {
			t.Errorf("expected %q to never serialize, got %s", key, data)
		}
	}
}
