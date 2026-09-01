package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name        string
		slug        string
		expectError bool
	}{
		{name: "valid simple", slug: "acme", expectError: false},
		{name: "valid with hyphens", slug: "my-awesome-workspace", expectError: false},
		{name: "valid single char", slug: "a", expectError: false},
		{name: "valid single digit", slug: "1", expectError: false},
		{name: "valid numeric slug", slug: "12345", expectError: false},
		{name: "valid alphanum mix", slug: "team-42", expectError: false},
		{name: "valid 63 chars max length", slug: strings.Repeat("a", 63), expectError: false},

		// Invalid format
		{name: "empty slug", slug: "", expectError: true},
		{name: "too long (64 chars)", slug: strings.Repeat("a", 64), expectError: true},
		{name: "leading hyphen", slug: "-acme", expectError: true},
		{name: "trailing hyphen", slug: "acme-", expectError: true},
		{name: "uppercase letters", slug: "Acme", expectError: true},
		{name: "uppercase with hyphen", slug: "My-Team", expectError: true},
		{name: "spaces", slug: "my team", expectError: true},
		{name: "underscores", slug: "my_team", expectError: true},
		{name: "dots", slug: "my.team", expectError: true},
		{name: "special characters", slug: "acme!", expectError: true},

		// Reserved slugs
		{name: "reserved api", slug: "api", expectError: true},
		{name: "reserved auth", slug: "auth", expectError: true},
		{name: "reserved new", slug: "new", expectError: true},
		{name: "reserved settings", slug: "settings", expectError: true},
		{name: "reserved workspaces", slug: "workspaces", expectError: true},
		{name: "reserved login", slug: "login", expectError: true},
		{name: "reserved logout", slug: "logout", expectError: true},
		{name: "reserved master", slug: "master", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateSlug(tt.slug)
			if tt.expectError && err == nil {
				t.Errorf("ValidateSlug(%q) expected error, got nil", tt.slug)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateSlug(%q) unexpected error: %v", tt.slug, err)
			}
			if tt.expectError && err != nil && !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("ValidateSlug(%q) error = %v, want ErrInvalid sentinel wrapped", tt.slug, err)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"User@Example.COM", "user@example.com"},
		{"  john.doe@domain.co.uk  ", "john.doe@domain.co.uk"},
		{"ALICE+TAG@SUB.DOMAIN.ORG", "alice+tag@sub.domain.org"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := domain.NormalizeEmail(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		expectError bool
	}{
		{name: "valid standard", email: "user@example.com", expectError: false},
		{name: "valid with dots", email: "first.last@domain.co.uk", expectError: false},
		{name: "valid with tag", email: "user+tag@example.org", expectError: false},
		{name: "valid numbers", email: "user123@domain456.io", expectError: false},

		// Invalid emails
		{name: "empty", email: "", expectError: true},
		{name: "whitespace only", email: "   ", expectError: true},
		{name: "no @", email: "plainaddress", expectError: true},
		{name: "no local part", email: "@domain.com", expectError: true},
		{name: "no domain part", email: "user@", expectError: true},
		{name: "no tld dot", email: "user@domain", expectError: true},
		{name: "spaces in local", email: "user name@example.com", expectError: true},
		{name: "spaces in domain", email: "user@example .com", expectError: true},
		{name: "double @", email: "user@@example.com", expectError: true},
		{name: "exceeds 254 chars", email: strings.Repeat("a", 250) + "@domain.com", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateEmail(tt.email)
			if tt.expectError && err == nil {
				t.Errorf("ValidateEmail(%q) expected error, got nil", tt.email)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateEmail(%q) unexpected error: %v", tt.email, err)
			}
			if tt.expectError && err != nil && !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("ValidateEmail(%q) error = %v, want ErrInvalid sentinel wrapped", tt.email, err)
			}
		})
	}
}

func TestValidateTimezone(t *testing.T) {
	tests := []struct {
		name        string
		timezone    string
		expectError bool
	}{
		{name: "valid UTC", timezone: "UTC", expectError: false},
		{name: "valid Asia/Jakarta", timezone: "Asia/Jakarta", expectError: false},
		{name: "valid America/New_York", timezone: "America/New_York", expectError: false},
		{name: "valid Europe/London", timezone: "Europe/London", expectError: false},
		{name: "valid with whitespace", timezone: "  Asia/Jakarta  ", expectError: false},
		{name: "empty timezone", timezone: "", expectError: true},
		{name: "whitespace only", timezone: "   ", expectError: true},
		{name: "invalid timezone identifier", timezone: "Invalid/Timezone", expectError: true},
		{name: "random string", timezone: "Mars/Crater", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateTimezone(tt.timezone)
			if tt.expectError && err == nil {
				t.Errorf("ValidateTimezone(%q) expected error, got nil", tt.timezone)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateTimezone(%q) unexpected error: %v", tt.timezone, err)
			}
			if tt.expectError && err != nil && !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("ValidateTimezone(%q) error = %v, want ErrInvalid sentinel wrapped", tt.timezone, err)
			}
		})
	}
}

func TestValidateProviderBaseURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		required    bool
		expectError bool
	}{
		{name: "empty not required", baseURL: "", required: false, expectError: false},
		{name: "empty required", baseURL: "", required: true, expectError: true},
		{name: "valid https origin", baseURL: "https://api.openai.com", required: false, expectError: false},
		{name: "valid https with path", baseURL: "https://proxy.example.com/v1", required: false, expectError: false},
		{name: "valid http localhost", baseURL: "http://localhost:8000", required: true, expectError: false},
		{name: "invalid ftp scheme", baseURL: "ftp://example.com", required: false, expectError: true},
		{name: "invalid file scheme", baseURL: "file:///etc/passwd", required: false, expectError: true},
		{name: "missing host", baseURL: "https://", required: false, expectError: true},
		{name: "relative path only", baseURL: "/api/v1", required: false, expectError: true},
		{name: "invalid URL format", baseURL: "://bad", required: false, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateProviderBaseURL(tt.baseURL, tt.required)
			if tt.expectError && err == nil {
				t.Errorf("ValidateProviderBaseURL(%q, %v) expected error, got nil", tt.baseURL, tt.required)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateProviderBaseURL(%q, %v) unexpected error: %v", tt.baseURL, tt.required, err)
			}
			if tt.expectError && err != nil && !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("ValidateProviderBaseURL(%q, %v) error = %v, want ErrInvalid sentinel wrapped", tt.baseURL, tt.required, err)
			}
		})
	}
}
