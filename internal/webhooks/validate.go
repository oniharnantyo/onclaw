package webhooks

import "fmt"

// ValidateVerifierSet checks an endpoint's verifier list before save: at
// least one verifier is required (the >=1 credential rule), and every row
// must carry the structural fields its engine needs. Secret values are not
// required here — a blank secret means "keep the stored secret" at the
// service layer — but rows the engines cannot run (unknown kind, missing
// header names, or an invalid algorithm, encoding, or scheme) are rejected.
func ValidateVerifierSet(verifiers []VerifierConfig) error {
	if len(verifiers) == 0 {
		return fmt.Errorf("at least one verifier is required")
	}
	for i, cfg := range verifiers {
		switch cfg.Kind {
		case KindPath:
			// No structural fields beyond the secret.
		case KindHeader:
			if cfg.HeaderName == "" {
				return fmt.Errorf("verifier %d: header name is required", i)
			}
		case KindRawHMAC:
			if cfg.HeaderName == "" {
				return fmt.Errorf("verifier %d: signature header name is required", i)
			}
			if cfg.Algo != AlgoSHA256 && cfg.Algo != AlgoSHA1 {
				return fmt.Errorf("verifier %d: algorithm must be %q or %q", i, AlgoSHA256, AlgoSHA1)
			}
			if cfg.Encoding != EncHex && cfg.Encoding != EncBase64 {
				return fmt.Errorf("verifier %d: encoding must be %q or %q", i, EncHex, EncBase64)
			}
		case KindStdWeb:
			// Secret and Window are validated by Verify (a stale or
			// malformed secret fails closed; Window defaults).
		case KindTsHMAC:
			if cfg.HeaderName == "" {
				return fmt.Errorf("verifier %d: signature header name is required", i)
			}
			switch cfg.Scheme {
			case SchemeStripe:
				// The timestamp rides the signature header itself.
			case SchemeV0:
				if cfg.TimestampHeader == "" {
					return fmt.Errorf("verifier %d: timestamp header is required for the v0 scheme", i)
				}
			default:
				return fmt.Errorf("verifier %d: scheme must be %q or %q", i, SchemeStripe, SchemeV0)
			}
		default:
			return fmt.Errorf("verifier %d: unknown kind %q", i, cfg.Kind)
		}
	}
	return nil
}
