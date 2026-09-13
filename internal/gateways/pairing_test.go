package gateways

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestPairingMintTokenShapeAndTTL(t *testing.T) {
	f := newFixture(t)

	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if err := domain.ValidatePairingTokenShape(token.Token); err != nil {
		t.Fatalf("minted token violates the domain shape: %v", err)
	}
	if len(token.Token) != 43 {
		t.Fatalf("expected 43-char base64url token (32 bytes), got %d", len(token.Token))
	}
	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 || ttl > PairingTokenTTL+5*time.Second {
		t.Fatalf("expected ~1h expiry, got %v", ttl)
	}
}

func TestPairingTokenSingleUse(t *testing.T) {
	f := newFixture(t)
	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	if _, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092", "onih", token.Token); err != nil {
		t.Fatalf("first pair: %v", err)
	}

	// A second use fails and links nothing new.
	link2, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "555", "other", token.Token)
	if err == nil {
		t.Fatalf("second use must fail")
	}
	if !errors.Is(err, domain.ErrPairingTokenExpired) {
		t.Fatalf("expected ErrPairingTokenExpired, got %v", err)
	}
	if link2 != nil {
		t.Fatalf("second use created a link")
	}
}

func TestPairingExpiryRefused(t *testing.T) {
	f := newFixture(t)
	token, err := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// Consumed well past the TTL: the token is expired.
	expiredService := NewPairingService(f.st.GatewayLinks(), WithPairingClock(func() time.Time {
		return time.Now().Add(2 * PairingTokenTTL)
	}))
	if _, err := expiredService.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092", "onih", token.Token); !errors.Is(err, domain.ErrPairingTokenExpired) {
		t.Fatalf("expected expired-token error, got %v", err)
	}
	link, _ := f.st.GatewayLinks().GetUserLink(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092")
	if link != nil {
		t.Fatalf("expired token must not link")
	}
}

func TestPairingMalformedTokenRefusedBeforeStore(t *testing.T) {
	f := newFixture(t)
	for _, bad := range []string{"", "short", "with space and more chars to pass length check!!", "invalid+chars/here+and+more+chars+to+pass"} {
		if _, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092", "onih", bad); !errors.Is(err, domain.ErrInvalidPairingToken) {
			t.Fatalf("token %q: expected ErrInvalidPairingToken, got %v", bad, err)
		}
	}
}

func TestPairingRePairConflictSurfaces(t *testing.T) {
	f := newFixture(t)
	tok1, _ := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if _, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092", "onih", tok1.Token); err != nil {
		t.Fatalf("first pair: %v", err)
	}

	// A second token for the same member, used from the same platform
	// identity: the link already exists — the store refuses.
	tok2, _ := f.pairing.MintToken(f.ctx, f.ws.ID, f.user.ID)
	if _, err := f.pairing.Pair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092", "onih", tok2.Token); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected duplicate-pair conflict, got %v", err)
	}
}

func TestPairingUnpair(t *testing.T) {
	f := newFixture(t)
	f.pairUser("593821092", "onih")

	if err := f.pairing.Unpair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092"); err != nil {
		t.Fatalf("unpair: %v", err)
	}
	link, _ := f.st.GatewayLinks().GetUserLink(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092")
	if link != nil {
		t.Fatalf("link survived unpair")
	}
	// Unknown links are ErrNotFound.
	if err := f.pairing.Unpair(f.ctx, f.ws.ID, domain.GatewayPlatformTelegram, "593821092"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double unpair, got %v", err)
	}
}

func TestUnpairedSenderHintMentionsStart(t *testing.T) {
	hint := UnpairedSenderHint()
	if !strings.Contains(hint, "/start") {
		t.Fatalf("hint must teach /start <token>: %q", hint)
	}
}
