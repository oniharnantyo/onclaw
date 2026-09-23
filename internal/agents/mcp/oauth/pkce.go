package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE S256 (RFC 7636 §4.2) — the only code-challenge method this client
// uses. The verifier is high-entropy base64url; the challenge sent in the
// authorize URL is BASE64URL-ENCODE(SHA256(ASCII(code_verifier))), the S256
// transform, and the verifier rides only the token-endpoint request body.

// pkceVerifierBytes sizes the verifier: 64 random bytes encode to 86 base64url
// characters, inside the RFC 7636 §4.1 window of 43–128 characters.
const pkceVerifierBytes = 64

// newPKCE mints a fresh verifier/challenge pair. The verifier must reach the
// token endpoint unobserved by whoever sees the authorize URL — it travels in
// the sealed session bundle (authcode.go), never in the state.
func newPKCE() (verifier, challenge string, err error) {
	buf := make([]byte, pkceVerifierBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("pkce verifier entropy: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	challenge, err = ChallengeS256(verifier)
	if err != nil {
		return "", "", err
	}
	return verifier, challenge, nil
}

// ChallengeS256 computes the RFC 7636 §4.2 S256 code challenge for a
// verifier: BASE64URL-ENCODE(SHA256(ASCII(code_verifier))), without padding.
// Exported so callers (and tests) can verify the shape end to end.
func ChallengeS256(verifier string) (string, error) {
	if l := len(verifier); l < 43 || l > 128 {
		return "", fmt.Errorf("code verifier length %d is outside the RFC 7636 window of 43-128 characters", l)
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
