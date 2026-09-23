package oauth

import (
	"errors"
	"testing"
	"time"
)

// TestPeekStateVerifiesWithoutConsuming pins the callback's lookup step
// (tasks.md 6.1): PeekState validates signature, shape, and TTL exactly like
// the completion path, but the state stays consumable — the callback peeks to
// find the bound server row, then CompleteAuthorization validates and
// consumes the same state for real.
func TestPeekStateVerifiesWithoutConsuming(t *testing.T) {
	c := NewClient([]byte("test-master-key-0123456789abcdef"))

	begin, err := c.BeginAuthorization(BeginParams{
		Meta: &Metadata{
			ServerURL:              "https://mcp.example.test",
			AuthorizationServerURL: "https://as.example.test",
			AuthorizationServer: &AuthorizationServerMetadata{
				Issuer:                "https://as.example.test",
				AuthorizationEndpoint: "https://as.example.test/authorize",
				TokenEndpoint:         "https://as.example.test/token",
			},
		},
		Client:      &ResolvedClient{ClientID: "client-1", AuthMethod: AuthMethodNone, UsePKCE: true},
		RedirectURI: "https://onclaw.example.test/api/v1/mcp/oauth/callback",
		Claims:      StateClaims{WorkspaceID: "ws-1", ServerID: "srv-1"},
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	// Peek twice: valid both times, and the nonce survives.
	for i := 0; i < 2; i++ {
		claims, err := c.PeekState(begin.State)
		if err != nil {
			t.Fatalf("peek %d: %v", i, err)
		}
		if claims.WorkspaceID != "ws-1" || claims.ServerID != "srv-1" || claims.Nonce == "" {
			t.Fatalf("peek %d claims = %+v", i, claims)
		}
		if claims.Issuer != "https://as.example.test" {
			t.Fatalf("peek %d issuer = %q, want the begin-stamped authorization server", i, claims.Issuer)
		}
	}

	// A tampered state is the one generic rejection.
	if _, err := c.PeekState(begin.State + "x"); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("tampered peek err = %v, want ErrStateInvalid", err)
	}
	if _, err := c.PeekState(""); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("empty peek err = %v, want ErrStateInvalid", err)
	}

	// The peek left the flow consumable: CompleteAuthorization's state
	// validation (which consumes the nonce) accepts the same state.
	sessionClaims, err := c.openState(begin.State)
	if err != nil {
		t.Fatalf("openState after peeks: %v", err)
	}
	if sessionClaims.ServerID != "srv-1" {
		t.Fatalf("openState claims = %+v", sessionClaims)
	}

	// And the nonce is now consumed: a third peek still validates the
	// signature (peek never consumes), but the completion would reject the
	// replay.
	if _, err := c.PeekState(begin.State); err != nil {
		t.Fatalf("peek after consume: %v", err)
	}
	if !errors.Is(c.openStateErrForTest(begin.State), ErrStateInvalid) {
		t.Fatal("openState must reject the replayed state after the first completion")
	}
}

// openStateErrForTest wraps the unexported openState for the replay pin
// above.
func (c *Client) openStateErrForTest(raw string) error {
	_, err := c.openState(raw)
	return err
}

// TestPeekStateExpiredRejected pins the TTL half of the peek's validation.
func TestPeekStateExpiredRejected(t *testing.T) {
	c := NewClient([]byte("test-master-key-0123456789abcdef"))
	begin, err := c.BeginAuthorization(BeginParams{
		Meta: &Metadata{
			ServerURL:              "https://mcp.example.test",
			AuthorizationServerURL: "https://as.example.test",
			AuthorizationServer: &AuthorizationServerMetadata{
				Issuer:                "https://as.example.test",
				AuthorizationEndpoint: "https://as.example.test/authorize",
				TokenEndpoint:         "https://as.example.test/token",
			},
		},
		Client:      &ResolvedClient{ClientID: "client-1", AuthMethod: AuthMethodNone},
		RedirectURI: "https://onclaw.example.test/cb",
		Claims: StateClaims{
			WorkspaceID: "ws-1",
			ServerID:    "srv-1",
			ExpiresAt:   time.Now().Add(-time.Minute).Unix(),
		},
	})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := c.PeekState(begin.State); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expired peek err = %v, want ErrStateInvalid", err)
	}
}
