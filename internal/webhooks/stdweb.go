package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

// Standard Webhooks header names. Lookups go through VerifyInput.Header
// (typically http.Header.Get, which is case-insensitive), so the lowercase
// spec spelling works regardless of the wire casing.
const (
	StdWebIDHeader        = "webhook-id"
	StdWebTimestampHeader = "webhook-timestamp"
	StdWebSignatureHeader = "webhook-signature"
)

// stdWebSignatureVersion is the version prefix of the symmetric HMAC
// signatures this engine verifies ("v1,<base64>"). Other versions (v1a and
// beyond) are different schemes and are ignored, per the spec's
// space-separated candidate list for zero-downtime secret rotation.
const stdWebSignatureVersion = "v1"

// verifyStdWeb implements the Standard Webhooks signature scheme
// (https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md):
// HMAC-SHA256 over "{webhook-id}.{webhook-timestamp}.{body}" keyed with the
// base64-decoded whsec_ secret, checked against the space-separated,
// version-prefixed, base64-encoded candidate list of the webhook-signature
// header. GitLab's signed webhooks follow this scheme. A webhook-timestamp
// outside the verifier's replay window refuses (replay defense in both
// directions).
func verifyStdWeb(cfg VerifierConfig, in VerifyInput) Verdict {
	signatures := strings.TrimSpace(in.Header(StdWebSignatureHeader))
	if signatures == "" {
		return VerdictSkip
	}
	if cfg.Secret == "" {
		return VerdictFail
	}
	id := strings.TrimSpace(in.Header(StdWebIDHeader))
	ts := strings.TrimSpace(in.Header(StdWebTimestampHeader))
	key, ok := decodeWebhookSecret(cfg.Secret)
	if !ok || id == "" {
		return VerdictFail
	}
	tsUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return VerdictFail
	}
	window := cfg.Window
	if window <= 0 {
		window = DefaultWebhookWindow
	}
	if !withinWindow(in.Now, tsUnix, window) {
		return VerdictFail
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id))
	mac.Write([]byte("."))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(in.Body)
	expected := mac.Sum(nil)
	for _, candidate := range strings.Fields(signatures) {
		version, value, found := strings.Cut(candidate, ",")
		if !found || version != stdWebSignatureVersion {
			continue
		}
		presented, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		if hmac.Equal(expected, presented) {
			return VerdictPass
		}
	}
	return VerdictFail
}
