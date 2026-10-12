package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"crypto/subtle"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// verifyNow is the clock the timestamped scheme reads the server time from.
// A package var so tests can pin the ±WorkspaceWebhookTimestampSkew boundary
// without an exported knob.
var verifyNow = time.Now

// VerifySignature checks one delivery's signature header value against the
// connection's secret over the raw body, per the recipe's declared scheme
// (ingress contract §2). The comparison is constant-time in both schemes:
//
//   - hmac_sha256: the header carries hex(HMAC-SHA256(secret, raw body)),
//     optionally prefixed "<algo>=" (GitHub's "sha256=...") — the prefix
//     handling belongs to the verifier, driven by the recipe scheme. The
//     verdict is hmac.Equal over the decoded bytes, so wrong-length and
//     wrong-content inputs are indistinguishable to a timing probe.
//   - secret_token, plain_token: the header carries the shared secret
//     verbatim (GitLab's X-Gitlab-Token); subtle.ConstantTimeCompare against
//     the secret. The two schemes share one comparison — plain_token is the
//     standalone endpoint's spelling of the same scheme (spec: "a
//     plain-token header compared in constant time").
//   - timestamped_hmac: the header carries "t=<unix-seconds>,v1=<hex>" where
//     the digest is HMAC-SHA256(secret, "<ts>.<body>"); the timestamp must
//     lie within ±WorkspaceWebhookTimestampSkew of the server clock, so a
//     captured header expires (spec: Timestamped HMAC rejects stale capture).
//     Replay inside the window is the durable dedupe's job — the handler
//     records TimestampedReplayKey via the delivery store, not the verifier.
func VerifySignature(scheme, provided string, secret, body []byte) bool {
	switch scheme {
	case domain.RecipeWebhookSchemeHMACSHA256:
		return verifyHMACSHA256(provided, secret, body)
	case domain.RecipeWebhookSchemeSecretToken, domain.WorkspaceWebhookSchemePlainToken:
		return subtle.ConstantTimeCompare([]byte(provided), secret) == 1
	case domain.WorkspaceWebhookSchemeTimestampedHmac:
		return verifyTimestampedHMAC(provided, secret, body)
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

// verifyTimestampedHMAC checks a "t=<unix-seconds>,v1=<hex>" header: the
// timestamp must sit within ±WorkspaceWebhookTimestampSkew of the server
// clock (a captured header expires; spec: Timestamped HMAC rejects stale
// capture), and the digest must match hex(HMAC-SHA256(secret, "<ts>.<body>"))
// — ASCII decimal timestamp, a literal dot, then the raw body. The comparison
// is hmac.Equal over the decoded bytes, so wrong-length and wrong-content
// inputs are indistinguishable to a timing probe.
func verifyTimestampedHMAC(provided string, secret, body []byte) bool {
	ts, sig, ok := parseTimestampedHeader(provided)
	if !ok {
		return false
	}

	skew := int64(domain.WorkspaceWebhookTimestampSkew / time.Second)
	age := verifyNow().Unix() - ts
	if age < 0 {
		age = -age
	}
	if age > skew {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}

// parseTimestampedHeader splits a timestamped signature header into its
// unix-seconds timestamp and decoded v1 digest. The grammar is exact —
// "t=<integer>,v1=<64 hex digits>" — and anything else (missing parts,
// non-integer timestamp, non-hex or wrong-length digest, trailing garbage)
// reports not-ok. Hex decoding is case-insensitive, matching hmac_sha256.
func parseTimestampedHeader(value string) (ts int64, sig []byte, ok bool) {
	rest, found := strings.CutPrefix(value, "t=")
	if !found {
		return 0, nil, false
	}
	tsPart, sigPart, found := strings.Cut(rest, ",")
	if !found {
		return 0, nil, false
	}
	if !strings.HasPrefix(sigPart, "v1=") {
		return 0, nil, false
	}

	ts, err := strconv.ParseInt(tsPart, 10, 64)
	if err != nil {
		return 0, nil, false
	}
	sig, err = hex.DecodeString(sigPart[len("v1="):])
	if err != nil || len(sig) != sha256.Size {
		return 0, nil, false
	}
	return ts, sig, true
}

// TimestampedReplayKey derives the durable replay-dedupe key for a
// timestamped signature header: "sig:" + lowercase hex of SHA-256 over the
// header value verbatim. After a delivery verifies, the ingress handler
// records this key through WorkspaceWebhookStore.RecordDelivery — the row
// lives only for the skew window, so a second identical signature (same
// timestamp, body, and secret) fails RecordDelivery and is rejected as a
// replay (spec: Timestamped replay within the window). It returns "" for
// values that are not a well-formed timestamped header; those never verify,
// so a handler that records only verified deliveries never keys on "".
func TimestampedReplayKey(headerValue string) string {
	if _, _, ok := parseTimestampedHeader(headerValue); !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(headerValue))
	return "sig:" + hex.EncodeToString(sum[:])
}
