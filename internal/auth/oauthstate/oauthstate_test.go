package oauthstate_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/auth/oauthstate"
)

func TestSealer_SealOpenRoundTrip(t *testing.T) {
	sealer := oauthstate.NewSealer([]byte("test-key-0123456789abcdef012345"), "onclaw:test-flow:v1", 10*time.Minute)
	payload := []byte(`{"w":"ws-1","n":"nonce-1"}`)

	state := sealer.Seal(payload)
	if state == "" || !strings.Contains(state, ".") {
		t.Fatalf("expected a dotted sealed state, got %q", state)
	}

	opened, err := sealer.Open(state)
	if err != nil {
		t.Fatalf("expected the sealed state to open, got %v", err)
	}
	if !bytes.Equal(opened, payload) {
		t.Fatalf("expected the payload byte-identical, got %q", opened)
	}
}

func TestSealer_RejectionsAreOneSentinel(t *testing.T) {
	sealer := oauthstate.NewSealer([]byte("test-key-0123456789abcdef012345"), "onclaw:test-flow:v1", 10*time.Minute)
	state := sealer.Seal([]byte(`{"w":"ws-1"}`))

	// A sealer over a DIFFERENT key (or context) sees forgeries.
	otherKey := oauthstate.NewSealer([]byte("another-key-0123456789abcdef01"), "onclaw:test-flow:v1", 10*time.Minute)
	otherContext := oauthstate.NewSealer([]byte("test-key-0123456789abcdef012345"), "onclaw:other-flow:v1", 10*time.Minute)

	cases := map[string]string{
		"unknown state":      "totally-made-up.state",
		"tampered payload":   state[:len(state)-4] + "zzzz",
		"tampered signature": state + "zzzz",
		"empty state":        "",
		"no signature":       "eyJ3Ijoid3MtMSJ9",
		"foreign key":        otherKey.Seal([]byte(`{"w":"ws-1"}`)),
		"foreign context":    otherContext.Seal([]byte(`{"w":"ws-1"}`)),
	}
	for name, raw := range cases {
		if _, err := sealer.Open(raw); !errors.Is(err, oauthstate.ErrInvalidState) {
			t.Errorf("%s: expected ErrInvalidState, got %v", name, err)
		}
	}
}

func TestNonceStore_SingleUse(t *testing.T) {
	sealer := oauthstate.NewSealer([]byte("test-key-0123456789abcdef012345"), "onclaw:test-flow:v1", time.Millisecond)
	nonces := sealer.Nonces()

	nonce := nonces.Issue()
	if nonce == "" {
		t.Fatal("expected a minted nonce")
	}
	if !nonces.Consume(nonce) {
		t.Fatal("expected the first consumption to succeed")
	}
	if nonces.Consume(nonce) {
		t.Fatal("expected the replayed nonce to be rejected")
	}
	if nonces.Consume("never-issued") {
		t.Fatal("expected an unknown nonce to be rejected")
	}

	// The TTL bounds the nonce: after it lapses the nonce is dead.
	live := oauthstate.NewSealer([]byte("test-key-0123456789abcdef012345"), "onclaw:test-flow:v1", time.Millisecond)
	ttlNonce := live.Nonces().Issue()
	time.Sleep(5 * time.Millisecond)
	if live.Nonces().Consume(ttlNonce) {
		t.Fatal("expected the expired nonce to be rejected")
	}
}

func TestDeriveRedirectURI(t *testing.T) {
	cases := []struct{ base, want string }{
		{"https://onclaw.example.com", "https://onclaw.example.com/api/v1/callback"},
		{"https://onclaw.example.com/", "https://onclaw.example.com/api/v1/callback"},
	}
	for _, tc := range cases {
		if got := oauthstate.DeriveRedirectURI(tc.base, "/api/v1/callback"); got != tc.want {
			t.Errorf("DeriveRedirectURI(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}
