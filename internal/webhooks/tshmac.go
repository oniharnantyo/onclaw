package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// verifyTsHMAC dispatches the timestamp-composite schemes: Stripe's
// self-contained "t=...,v1=..." header and the Slack/Zoom v0 composition
// ("v0:{timestamp}:{body}" with the timestamp in its own header). Both are
// bounded by the verifier's replay window in both directions.
func verifyTsHMAC(cfg VerifierConfig, in VerifyInput) Verdict {
	signatures := strings.TrimSpace(in.Header(cfg.HeaderName))
	if signatures == "" {
		return VerdictSkip
	}
	if cfg.Secret == "" {
		return VerdictFail
	}
	switch cfg.Scheme {
	case SchemeStripe:
		return verifyStripe(signatures, cfg, in)
	case SchemeV0:
		return verifyV0(signatures, cfg, in)
	default:
		// Unknown composition: the row cannot authenticate anything.
		return VerdictFail
	}
}

// verifyStripe implements Stripe's manual verification recipe
// (https://docs.stripe.com/webhooks#verify-manually): the Stripe-Signature
// header lists "t=<unix seconds>" plus one or more "v1=<hex>" HMAC-SHA256
// signatures over "<t>.<body>", keyed with the whsec_-decoded signing
// secret. A stale t refuses (Stripe re-signs every retry, so live
// deliveries always carry a fresh timestamp).
func verifyStripe(signatures string, cfg VerifierConfig, in VerifyInput) Verdict {
	var ts string
	var candidates []string
	for _, part := range strings.Split(signatures, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch name {
		case "t":
			ts = value
		case "v1":
			candidates = append(candidates, value)
		}
	}
	if ts == "" || len(candidates) == 0 {
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
	key, ok := decodeStripeSecret(cfg.Secret)
	if !ok {
		return VerdictFail
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(in.Body)
	expected := mac.Sum(nil)
	for _, candidate := range candidates {
		presented, err := hex.DecodeString(candidate)
		if err != nil {
			continue
		}
		if hmac.Equal(expected, presented) {
			return VerdictPass
		}
	}
	return VerdictFail
}

// verifyV0 implements the Slack/Zoom composition — Slack:
// https://api.slack.com/authentication/verifying-requests-from-slack,
// Zoom: https://developers.zoom.us/docs/api/webhooks/ (validate with
// signature). HMAC-SHA256 over "v0:{timestamp}:{body}" under the plain
// signing secret; the timestamp rides cfg.TimestampHeader and the signature
// header carries comma-separated "v0=<hex>" values (Slack may list several
// during key rotation).
func verifyV0(signatures string, cfg VerifierConfig, in VerifyInput) Verdict {
	ts := strings.TrimSpace(in.Header(cfg.TimestampHeader))
	if ts == "" {
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
	mac := hmac.New(sha256.New, []byte(cfg.Secret))
	mac.Write([]byte("v0:"))
	mac.Write([]byte(ts))
	mac.Write([]byte(":"))
	mac.Write(in.Body)
	expected := mac.Sum(nil)
	for _, part := range strings.Split(signatures, ",") {
		value, ok := strings.CutPrefix(strings.TrimSpace(part), "v0=")
		if !ok {
			continue
		}
		presented, err := hex.DecodeString(value)
		if err != nil {
			continue
		}
		if hmac.Equal(expected, presented) {
			return VerdictPass
		}
	}
	return VerdictFail
}
