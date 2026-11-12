package webhooks

import "crypto/subtle"

// verifyPath compares the request URL's token segment with the configured
// path-token secret in constant time. A request whose URL carries no token
// segment never applies this verifier; a present-but-wrong token is an
// explicit failed authentication attempt and refuses.
func verifyPath(cfg VerifierConfig, in VerifyInput) Verdict {
	if in.PathToken == "" {
		return VerdictSkip
	}
	if cfg.Secret == "" {
		return VerdictFail
	}
	if subtle.ConstantTimeCompare([]byte(in.PathToken), []byte(cfg.Secret)) != 1 {
		return VerdictFail
	}
	return VerdictPass
}
