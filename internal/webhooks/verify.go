package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"crypto/subtle"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// VerifySignature checks one delivery's signature header value against the
// connection's secret over the raw body, per the recipe's declared scheme
// (ingress contract §2). The comparison is constant-time in both schemes:
//
//   - hmac_sha256: the header carries hex(HMAC-SHA256(secret, raw body)),
//     optionally prefixed "<algo>=" (GitHub's "sha256=...") — the prefix
//     handling belongs to the verifier, driven by the recipe scheme. The
//     verdict is hmac.Equal over the decoded bytes, so wrong-length and
//     wrong-content inputs are indistinguishable to a timing probe.
//   - secret_token: the header carries the shared secret verbatim (GitLab's
//     X-Gitlab-Token); subtle.ConstantTimeCompare against the secret.
func VerifySignature(scheme, provided string, secret, body []byte) bool {
	switch scheme {
	case domain.RecipeWebhookSchemeHMACSHA256:
		return verifyHMACSHA256(provided, secret, body)
	case domain.RecipeWebhookSchemeSecretToken:
		return subtle.ConstantTimeCompare([]byte(provided), secret) == 1
	default:
		// Registration validation admits only the catalog schemes; an
		// unknown scheme here is a wiring bug — verify nothing.
		return false
	}
}

// verifyHMACSHA256 computes the expected MAC over the raw body and compares
// it with the provided header value in constant time. The optional
// "<algo>=" prefix (lowercase or uppercase) is stripped before hex decoding;
// undecodable or wrong-length values fail the comparison without a
// distinguishable timing profile (hmac.Equal's early length exit is constant
// in the secret).
func verifyHMACSHA256(provided string, secret, body []byte) bool {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expected := mac.Sum(nil)

	value := stripSignaturePrefix(provided)
	// hex.DecodeString is case-insensitive; a mismatched length yields an
	// error that maps to "no match", never a panic.
	actual, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return hmac.Equal(expected, actual)
}

// stripSignaturePrefix removes a leading "<algo>=" prefix (e.g. "sha256=")
// from a signature header value. Anything without the separator is returned
// whole, so bare-hex providers keep working.
func stripSignaturePrefix(value string) string {
	if i := strings.IndexByte(value, '='); i >= 0 && isAlphanumericBlob(value[:i]) {
		return value[i+1:]
	}
	return value
}

// isAlphanumericBlob reports whether s is a non-empty run of ASCII letters
// or digits — the shape of an algorithm label ("sha256", "sha1").
func isAlphanumericBlob(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
