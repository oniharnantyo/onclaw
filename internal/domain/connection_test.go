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
		name   string
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

// The persisted connection status catalog (add-connection-oauth design.md
// D6): connected and error mirror the server row's probe statuses, expired is
// the OAuth-only recovery state; empty stays valid as the pre-OAuth shape.
func TestConnectionStatuses(t *testing.T) {
	if domain.ConnectionStatusConnected == domain.ConnectionStatusExpired {
		t.Fatal("connected and expired must be distinct values")
	}
	for _, status := range []string{
		domain.ConnectionStatusConnected,
		domain.ConnectionStatusError,
		domain.ConnectionStatusExpired,
	} {
		if !domain.IsValidConnectionStatus(status) {
			t.Errorf("expected %q to be valid", status)
		}
		if err := domain.ValidateConnectionStatus(status); err != nil {
			t.Errorf("expected %q to validate, got %v", status, err)
		}
	}
	for _, status := range []string{"", "live", "EXPIRED", "revoked"} {
		if domain.IsValidConnectionStatus(status) {
			t.Errorf("expected %q to be invalid", status)
		}
		if status != "" {
			if err := domain.ValidateConnectionStatus(status); !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("expected ErrInvalid for %q, got %v", status, err)
			}
		}
	}
}

// Validate accepts the lifecycle fields only with a catalog status; the
// token-lifecycle fields themselves carry no structural constraints (an
// expired OAuth connection may legitimately have an empty envelope).
func TestConnectionValidateLifecycle(t *testing.T) {
	valid := domain.Connection{
		WorkspaceID:       "ws-1",
		Service:           "atlassian",
		AccessLevel:       domain.ConnectionAccessReadOnly,
		Status:            domain.ConnectionStatusExpired,
		RefreshCiphertext: "v1:bnB4:Y2lwaGVydGV4dA",
		GrantedScopes:     []string{"read:jira-work", "offline_access"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid lifecycle connection, got %v", err)
	}

	emptyStatus := valid
	emptyStatus.Status = ""
	if err := emptyStatus.Validate(); err != nil {
		t.Fatalf("expected empty status to stay valid (stored as connected), got %v", err)
	}

	badStatus := valid
	badStatus.Status = "revoked"
	if err := badStatus.Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for out-of-catalog status, got %v", err)
	}
}

// Status transitions (add-connection-oauth design.md D6): expired is entered
// from a live status by refresh failure and left only to connected by
// reauthorization; connected and error mirror probe outcomes.
func TestConnectionStatusTransitions(t *testing.T) {
	cases := []struct {
		from string
		to   string
		ok   bool
	}{
		{domain.ConnectionStatusConnected, domain.ConnectionStatusError, true},
		{domain.ConnectionStatusError, domain.ConnectionStatusConnected, true},
		{domain.ConnectionStatusConnected, domain.ConnectionStatusExpired, true},
		{domain.ConnectionStatusError, domain.ConnectionStatusExpired, true},
		{domain.ConnectionStatusExpired, domain.ConnectionStatusExpired, true},
		{domain.ConnectionStatusExpired, domain.ConnectionStatusConnected, true},
		{domain.ConnectionStatusExpired, domain.ConnectionStatusError, false},
		{domain.ConnectionStatusConnected, domain.ConnectionStatusConnected, true},
		{domain.ConnectionStatusError, domain.ConnectionStatusError, true},
		{"", domain.ConnectionStatusExpired, false},
		{domain.ConnectionStatusConnected, "", false},
		{"live", domain.ConnectionStatusExpired, false},
	}
	for _, tc := range cases {
		if got := domain.CanTransitionConnectionStatus(tc.from, tc.to); got != tc.ok {
			t.Errorf("CanTransitionConnectionStatus(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.ok)
		}
	}
}
