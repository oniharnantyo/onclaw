package domain_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestConnectionValidate(t *testing.T) {
	valid := domain.Connection{
		WorkspaceID: "ws-1",
		Service:     "github",
		AccessLevel: domain.ConnectionAccessReadOnly,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid connection, got %v", err)
	}

	tests := []struct {
		name  string
		mutate func(c *domain.Connection)
	}{
		{
			name:   "empty workspace id",
			mutate: func(c *domain.Connection) { c.WorkspaceID = "" },
		},
		{
			name:   "empty service",
			mutate: func(c *domain.Connection) { c.Service = "" },
		},
		{
			name:   "blank service",
			mutate: func(c *domain.Connection) { c.Service = "   " },
		},
		{
			name:   "unknown access level",
			mutate: func(c *domain.Connection) { c.AccessLevel = "admin" },
		},
		{
			name:   "empty access level",
			mutate: func(c *domain.Connection) { c.AccessLevel = "" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid
			tt.mutate(&c)
			if err := c.Validate(); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	if err := (*domain.Connection)(nil).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil connection, got %v", err)
	}
}

func TestConnectionAccessLevels(t *testing.T) {
	if domain.ConnectionAccessReadOnly == domain.ConnectionAccessReadWrite {
		t.Fatal("read_only and read_write must be distinct values")
	}
	if !domain.IsValidConnectionAccessLevel(domain.ConnectionAccessReadOnly) {
		t.Errorf("expected %q to be valid", domain.ConnectionAccessReadOnly)
	}
	if !domain.IsValidConnectionAccessLevel(domain.ConnectionAccessReadWrite) {
		t.Errorf("expected %q to be valid", domain.ConnectionAccessReadWrite)
	}
	for _, level := range []string{"", "read", "write", "READ_ONLY", "admin", "read-only"} {
		if domain.IsValidConnectionAccessLevel(level) {
			t.Errorf("expected %q to be invalid", level)
		}
		if err := domain.ValidateConnectionAccessLevel(level); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("expected ErrInvalid for %q, got %v", level, err)
		}
	}
}

// The connection sentinels chain to the generic catalog sentinels so generic
// error mapping keeps working (tasks.md 1.1).
func TestConnectionErrorSentinels(t *testing.T) {
	if !errors.Is(domain.ErrUnknownRecipe, domain.ErrInvalid) {
		t.Error("expected ErrUnknownRecipe to chain to ErrInvalid")
	}
	if !errors.Is(domain.ErrConnectionExists, domain.ErrConflict) {
		t.Error("expected ErrConnectionExists to chain to ErrConflict")
	}
	if errors.Is(domain.ErrConnectionExists, domain.ErrInvalid) {
		t.Error("ErrConnectionExists must not chain to ErrInvalid")
	}
}
