package webhooks

import (
	"crypto/subtle"
	"strings"
)

// verifyHeader compares the named header's value with the configured secret
// in constant time. A request without the header never applies this
// verifier; a present-but-wrong value is an explicit failed attempt and
// refuses.
func verifyHeader(cfg VerifierConfig, in VerifyInput) Verdict {
	value := strings.TrimSpace(in.Header(cfg.HeaderName))
	if value == "" {
		return VerdictSkip
	}
	if cfg.Secret == "" {
		return VerdictFail
	}
	if subtle.ConstantTimeCompare([]byte(value), []byte(cfg.Secret)) != 1 {
		return VerdictFail
	}
	return VerdictPass
}
