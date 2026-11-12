package webhooks

import "time"

// Verdict is a verifier's three-valued outcome, so callers can apply the
// fail-closed rule exactly.
type Verdict int

const (
	// VerdictSkip means the verifier did not apply to the request: its
	// signature header is absent, or the URL carries no path token. Only a
	// request every verifier skips is treated as unsigned.
	VerdictSkip Verdict = iota
	// VerdictFail means the verifier applied but the request is invalid:
	// a present signature is bad, malformed, stale, or the verifier's own
	// configuration cannot authenticate anything. Any Fail refuses the
	// request outright — signature presence is intent, so a failed
	// signature never falls through to weaker credentials such as the
	// path token.
	VerdictFail
	// VerdictPass means this verifier authenticated the request.
	VerdictPass
)

// VerifyInput carries the method-agnostic request facts verifiers inspect.
//
// PathToken is the request URL's token segment, empty when the URL carries
// none (the receiver extracts it when resolving the endpoint). Header looks
// up a header by its configured name (the receiver typically passes
// http.Header.Get); it must be non-nil whenever the verifier set contains a
// header or HMAC engine. Body is the exact raw request body — the bytes the
// sender signed, read before any parsing. Now is the verification clock used
// by the replay windows.
type VerifyInput struct {
	PathToken string
	Header    func(name string) string
	Body      []byte
	Now       time.Time
}

// Verify runs one verifier against the request and returns its three-valued
// verdict. A verifier whose configured secret is empty never authenticates
// (a row without a credential is a storage defect and must fail closed).
func Verify(cfg VerifierConfig, in VerifyInput) Verdict {
	switch cfg.Kind {
	case KindPath:
		return verifyPath(cfg, in)
	case KindHeader:
		return verifyHeader(cfg, in)
	case KindRawHMAC:
		return verifyRawHMAC(cfg, in)
	case KindStdWeb:
		return verifyStdWeb(cfg, in)
	case KindTsHMAC:
		return verifyTsHMAC(cfg, in)
	default:
		// An unknown kind can only come from a corrupted row: fail closed.
		return VerdictFail
	}
}

// Authorize applies the fail-closed rule to an endpoint's ordered verifier
// set. Every verifier is evaluated: the first Fail refuses the request with
// VerdictFail (a valid path token must not rescue a bad signature), the
// first Pass authenticates with VerdictPass and the verifier's label (its
// preset label, or its kind for manual rows, for the delivery log), and a
// request no verifier applied to returns VerdictSkip — the unsigned refusal
// the caller answers with 401.
//
// The outcome is independent of the verifiers' order: Fail dominates Pass.
func Authorize(verifiers []VerifierConfig, in VerifyInput) (label string, verdict Verdict) {
	var authenticated string
	for _, cfg := range verifiers {
		switch Verify(cfg, in) {
		case VerdictFail:
			return "", VerdictFail
		case VerdictPass:
			if authenticated == "" {
				authenticated = verifierLabel(cfg)
			}
		}
	}
	if authenticated != "" {
		return authenticated, VerdictPass
	}
	return "", VerdictSkip
}

// verifierLabel names a verifier for the delivery log: its preset label when
// one seeded it, its engine kind for manually built rows.
func verifierLabel(cfg VerifierConfig) string {
	if cfg.Preset != "" {
		return cfg.Preset
	}
	return string(cfg.Kind)
}
