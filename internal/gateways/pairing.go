package gateways

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// PairingTokenTTL is the bounded validity window of a minted pairing token
// (design D6): one hour, single use, consumed on success.
const PairingTokenTTL = time.Hour

// pairingTokenEntropy is the raw entropy of a minted token: 32 bytes
// base64url-encoded is 43 characters — inside the domain token shape window
// (ValidatePairingTokenShape) and far outside human-guessable range.
const pairingTokenEntropy = 32

// pairingTokenMintAttempts bounds the (astronomically unlikely) collision
// retry loop against the store's unique constraint.
const pairingTokenMintAttempts = 3

// PairingService implements the identity-pairing half of the gateway
// (design D6): single-use crypto-random tokens, `/start <token>`
// consumption, unpairing, and the default-deny refusal hint. Access is
// default-deny: only linked platform identities reach the runner, and every
// run executes under the linked member's user id.
type PairingService struct {
	links store.GatewayLinks
	now   func() time.Time
}

// PairingOption customizes a PairingService.
type PairingOption func(*PairingService)

// WithPairingClock overrides the clock used for token expiry (tests).
func WithPairingClock(now func() time.Time) PairingOption {
	return func(p *PairingService) { p.now = now }
}

// NewPairingService builds the pairing service. The links store is required
// (injected dependencies are never nil — the composition root resolves it).
func NewPairingService(links store.GatewayLinks, opts ...PairingOption) *PairingService {
	p := &PairingService{
		links: links,
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// MintToken creates a single-use pairing token binding the member to the
// workspace: crypto-random (32 bytes of entropy), expiring in one hour. The
// member sends it to the bot as `/start <token>` to pair their identity.
func (p *PairingService) MintToken(ctx context.Context, workspaceID, userID string) (*domain.PairingToken, error) {
	for attempt := 0; attempt < pairingTokenMintAttempts; attempt++ {
		token, err := newPairingTokenValue()
		if err != nil {
			return nil, fmt.Errorf("gateway pairing: mint token: %w", err)
		}
		pt := &domain.PairingToken{
			Token:       token,
			WorkspaceID: workspaceID,
			UserID:      userID,
			ExpiresAt:   p.now().Add(PairingTokenTTL),
		}
		err = p.links.CreatePairingToken(ctx, pt)
		if err == nil {
			return pt, nil
		}
		// A duplicate token (unique constraint) is the only retryable
		// condition; everything else — unknown workspace, unknown member —
		// is terminal.
		if !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("gateway pairing: token collision after %d attempts", pairingTokenMintAttempts)
}

// Pair consumes a pairing token on behalf of a platform identity and links
// it to the token's member. The token must be valid-shaped, unconsumed, and
// unexpired (ConsumePairingToken decides single-use atomically); the
// platform identity must not already be paired in the workspace. Keyed on
// the immutable platform user id — the username is display-only.
func (p *PairingService) Pair(ctx context.Context, workspaceID, platform, platformUserID, platformUsername, token string) (*domain.UserLink, error) {
	if err := domain.ValidatePairingTokenShape(token); err != nil {
		return nil, err
	}

	consumed, err := p.links.ConsumePairingToken(ctx, workspaceID, token, p.now())
	if err != nil {
		// Unknown, expired, and already-consumed tokens are indistinguishable
		// to the sender (spec: "the link is invalid or expired") and all
		// refuse without creating a link.
		return nil, err
	}

	link := &domain.UserLink{
		Platform:         platform,
		PlatformUserID:   platformUserID,
		WorkspaceID:      workspaceID,
		UserID:           consumed.UserID,
		PlatformUsername: platformUsername,
	}
	if err := p.links.CreateUserLink(ctx, workspaceID, link); err != nil {
		return nil, err
	}
	return link, nil
}

// Unpair revokes a platform identity's link to the workspace. Absent links
// return domain.ErrNotFound.
func (p *PairingService) Unpair(ctx context.Context, workspaceID, platform, platformUserID string) error {
	return p.links.DeleteUserLink(ctx, workspaceID, platform, platformUserID)
}

// UnpairedSenderHint is the default-deny refusal message sent to any
// unpaired sender (spec: "no run is minted and the sender receives a
// pairing hint"). It never leaks whether the workspace exists.
func UnpairedSenderHint() string {
	return "You're not paired with this workspace yet. " +
		"Generate a pairing token in your workspace settings " +
		"(Settings → Gateways → Pairing) and send it here as /start <token>."
}

// newPairingTokenValue generates the token material: 32 crypto-random bytes,
// base64url-encoded without padding (43 characters, URL-safe charset).
func newPairingTokenValue() (string, error) {
	buf := make([]byte, pairingTokenEntropy)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
