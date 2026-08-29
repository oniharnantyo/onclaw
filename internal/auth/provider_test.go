package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

type mockTestProvider struct {
	name       string
	provisions bool
}

func (m *mockTestProvider) Name() string {
	return m.name
}

func (m *mockTestProvider) Authenticate(ctx context.Context, credentials map[string]string) (*auth.Identity, error) {
	if credentials["email"] == "valid@example.com" {
		return &auth.Identity{
			Email: "valid@example.com",
			Name:  "Valid User",
		}, nil
	}
	return nil, domain.ErrUnauthenticated
}

func (m *mockTestProvider) Begin(ctx context.Context, params map[string]string) (string, string, error) {
	return "https://example.com/oauth", "state123", nil
}

func (m *mockTestProvider) Complete(ctx context.Context, state string, params map[string]string) (*auth.Identity, error) {
	if state == "state123" {
		return &auth.Identity{
			Email: "oauth@example.com",
			Name:  "OAuth User",
		}, nil
	}
	return nil, domain.ErrUnauthenticated
}

func (m *mockTestProvider) ProvisionsUsers() bool {
	return m.provisions
}

func TestRegistry(t *testing.T) {
	reg := auth.NewRegistry()

	mock := &mockTestProvider{name: "mock-oauth", provisions: true}
	reg.Register(mock)

	// Duplicate registration panics
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected duplicate registration to panic")
		}
	}()
	reg.Register(&mockTestProvider{name: "mock-oauth"})
}

func TestRegistry_GetAndList(t *testing.T) {
	reg := auth.NewRegistry()

	mock1 := &mockTestProvider{name: "sso-google", provisions: true}
	mock2 := &mockTestProvider{name: "sso-github", provisions: true}

	reg.Register(mock1)
	reg.Register(mock2)

	p, err := reg.Get("sso-google")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "sso-google" {
		t.Errorf("got name %q, want %q", p.Name(), "sso-google")
	}

	_, err = reg.Get("unknown")
	if err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid for unknown provider, got: %v", err)
	}

	list := reg.List()
	if len(list) != 2 || list[0] != "sso-github" || list[1] != "sso-google" {
		t.Errorf("unexpected list: %v", list)
	}
}
