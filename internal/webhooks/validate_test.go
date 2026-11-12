package webhooks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateVerifierSet(t *testing.T) {
	tests := []struct {
		name    string
		cfgs    []VerifierConfig
		wantErr string // substring; empty means the set must validate
	}{
		{
			name:    "no verifiers rejected",
			cfgs:    nil,
			wantErr: "at least one verifier is required",
		},
		{
			name:    "empty slice rejected",
			cfgs:    []VerifierConfig{},
			wantErr: "at least one verifier is required",
		},
		{
			name: "single path verifier accepted",
			cfgs: []VerifierConfig{{Kind: KindPath}},
		},
		{
			name:    "unknown kind rejected",
			cfgs:    []VerifierConfig{{Kind: "sigv2"}},
			wantErr: `unknown kind "sigv2"`,
		},
		{
			name:    "header verifier needs a name",
			cfgs:    []VerifierConfig{{Kind: KindHeader}},
			wantErr: "header name is required",
		},
		{
			name: "header verifier accepted",
			cfgs: []VerifierConfig{{Kind: KindHeader, HeaderName: "X-Gitlab-Token"}},
		},
		{
			name:    "raw-hmac needs a signature header",
			cfgs:    []VerifierConfig{{Kind: KindRawHMAC, Algo: AlgoSHA256, Encoding: EncHex}},
			wantErr: "signature header name is required",
		},
		{
			name:    "raw-hmac needs a known algorithm",
			cfgs:    []VerifierConfig{{Kind: KindRawHMAC, HeaderName: "X", Algo: "md5", Encoding: EncHex}},
			wantErr: `algorithm must be "sha256" or "sha1"`,
		},
		{
			name:    "raw-hmac needs an algorithm set",
			cfgs:    []VerifierConfig{{Kind: KindRawHMAC, HeaderName: "X", Encoding: EncHex}},
			wantErr: `algorithm must be "sha256" or "sha1"`,
		},
		{
			name:    "raw-hmac needs a known encoding",
			cfgs:    []VerifierConfig{{Kind: KindRawHMAC, HeaderName: "X", Algo: AlgoSHA256, Encoding: "rot13"}},
			wantErr: `encoding must be "hex" or "base64"`,
		},
		{
			name: "raw-hmac accepted",
			cfgs: []VerifierConfig{{Kind: KindRawHMAC, HeaderName: "X-Hub-Signature-256", Algo: AlgoSHA256, Encoding: EncHex}},
		},
		{
			name: "stdweb verifier accepted",
			cfgs: []VerifierConfig{{Kind: KindStdWeb}},
		},
		{
			name:    "ts-hmac needs a known scheme",
			cfgs:    []VerifierConfig{{Kind: KindTsHMAC, HeaderName: "Stripe-Signature", Scheme: "hmac"}},
			wantErr: `scheme must be "stripe" or "v0"`,
		},
		{
			name: "stripe accepted without a timestamp header",
			cfgs: []VerifierConfig{{Kind: KindTsHMAC, HeaderName: "Stripe-Signature", Scheme: SchemeStripe}},
		},
		{
			name:    "v0 needs a timestamp header",
			cfgs:    []VerifierConfig{{Kind: KindTsHMAC, HeaderName: "X-Slack-Signature", Scheme: SchemeV0}},
			wantErr: "timestamp header is required",
		},
		{
			name: "v0 accepted with a timestamp header",
			cfgs: []VerifierConfig{{
				Kind: KindTsHMAC, HeaderName: "X-Slack-Signature",
				TimestampHeader: "X-Slack-Request-Timestamp", Scheme: SchemeV0,
			}},
		},
		{
			name: "every preset's seeded set validates",
			cfgs: allPresetVerifiers(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVerifierSet(tt.cfgs)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// allPresetVerifiers concatenates every preset's seeded rows: the whole
// catalog must be save-valid as one endpoint's credential set.
func allPresetVerifiers() []VerifierConfig {
	var all []VerifierConfig
	for _, preset := range Presets() {
		all = append(all, preset.Verifiers...)
	}
	return all
}
