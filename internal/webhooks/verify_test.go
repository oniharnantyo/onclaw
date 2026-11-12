package webhooks

import (
	"encoding/base64"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// authorizeFixtures builds the verifier set and request shapes the
// fail-closed scenarios use: a Standard Webhooks credential (as a GitLab
// preset would seed) plus an independent URL path token on the same
// endpoint.
type authorizeFixtures struct {
	stdWeb   VerifierConfig
	path     VerifierConfig
	wrongTok VerifierConfig
}

func newAuthorizeFixtures() authorizeFixtures {
	return authorizeFixtures{
		stdWeb:   VerifierConfig{Kind: KindStdWeb, Preset: "gitlab", Secret: vecSecretStdWeb},
		path:     VerifierConfig{Kind: KindPath, Secret: vecPathToken},
		wrongTok: VerifierConfig{Kind: KindPath, Secret: "not-the-path-token"},
	}
}

func (f authorizeFixtures) signedHeaders(signature string, ts int64) map[string]string {
	return map[string]string{
		StdWebIDHeader:        vecID,
		StdWebTimestampHeader: strconv.FormatInt(ts, 10),
		StdWebSignatureHeader: signature,
	}
}

func TestAuthorize(t *testing.T) {
	fx := newAuthorizeFixtures()
	goodSig := refStdWebSign(vecSecretStdWeb, vecID, vecTS, vecBody)
	badSig := refStdWebSign(vecSecretStdWeb, vecID, vecTS, []byte("a different body"))
	staleTS := vecTS - 3600
	staleSig := refStdWebSign(vecSecretStdWeb, vecID, staleTS, vecBody)
	tampered := []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`)

	tests := []struct {
		name      string
		verifiers []VerifierConfig
		in        VerifyInput
		wantLabel string
		want      Verdict
	}{
		{
			name:      "valid signature passes with the preset label",
			verifiers: []VerifierConfig{fx.stdWeb},
			in:        signedInput(fx.signedHeaders(goodSig, vecTS), vecBody),
			wantLabel: "gitlab",
			want:      VerdictPass,
		},
		{
			name:      "tampered body refuses despite the valid path token",
			verifiers: []VerifierConfig{fx.stdWeb, fx.path},
			in:        signedInput(fx.signedHeaders(goodSig, vecTS), tampered),
			want:      VerdictFail,
		},
		{
			name: "tampered signature refuses despite the valid path token",
			// The signature header is present (intent), the path token is
			// correct (a pass): the fail-closed rule must refuse anyway.
			verifiers: []VerifierConfig{fx.stdWeb, fx.path},
			in:        signedInput(fx.signedHeaders(badSig, vecTS), vecBody),
			want:      VerdictFail,
		},
		{
			name:      "expired webhook-timestamp refuses despite the valid path token",
			verifiers: []VerifierConfig{fx.stdWeb, fx.path},
			in:        signedInput(fx.signedHeaders(staleSig, staleTS), vecBody),
			want:      VerdictFail,
		},
		{
			name:      "path token authenticates an unsigned request",
			verifiers: []VerifierConfig{fx.stdWeb, fx.path},
			in: func() VerifyInput {
				in := signedInput(nil, vecBody)
				in.PathToken = vecPathToken
				return in
			}(),
			wantLabel: "path",
			want:      VerdictPass,
		},
		{
			name:      "unsigned request with only signature credentials is unsigned",
			verifiers: []VerifierConfig{fx.stdWeb},
			in:        signedInput(nil, vecBody),
			wantLabel: "",
			want:      VerdictSkip,
		},
		{
			name:      "no credentials at all is unsigned",
			verifiers: nil,
			in:        signedInput(nil, vecBody),
			wantLabel: "",
			want:      VerdictSkip,
		},
		{
			name:      "manual row labels by kind",
			verifiers: []VerifierConfig{{Kind: KindPath, Secret: vecPathToken}},
			in: func() VerifyInput {
				in := signedInput(nil, vecBody)
				in.PathToken = vecPathToken
				return in
			}(),
			wantLabel: "path",
			want:      VerdictPass,
		},
		{
			name:      "first pass wins when several would pass",
			verifiers: []VerifierConfig{{Kind: KindHeader, Preset: "header", HeaderName: "X-Webhook-Token", Secret: vecHeaderVal}, fx.path},
			in:        signedInput(map[string]string{"X-Webhook-Token": vecHeaderVal}, vecBody),
			wantLabel: "header",
			want:      VerdictPass,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, verdict := Authorize(tt.verifiers, tt.in)
			require.Equal(t, tt.want, verdict)
			require.Equal(t, tt.wantLabel, label)
		})
	}
}

func TestAuthorizeFailDominatesRegardlessOfOrder(t *testing.T) {
	fx := newAuthorizeFixtures()
	goodSig := refStdWebSign(vecSecretStdWeb, vecID, vecTS, vecBody)
	badSig := refStdWebSign("whsec_"+base64.StdEncoding.EncodeToString([]byte("not-the-standard-secret")), vecID, vecTS, vecBody)

	// Bad signature + correct path token: both orderings must refuse.
	badSigIn := signedInput(fx.signedHeaders(badSig, vecTS), vecBody)
	for _, order := range [][]VerifierConfig{
		{fx.stdWeb, fx.path},
		{fx.path, fx.stdWeb},
	} {
		_, verdict := Authorize(order, badSigIn)
		require.Equal(t, VerdictFail, verdict, "bad signature must refuse in any order")
	}

	// Good signature + correct path token: both orderings must pass.
	goodIn := signedInput(fx.signedHeaders(goodSig, vecTS), vecBody)
	for _, order := range [][]VerifierConfig{
		{fx.stdWeb, fx.path},
		{fx.path, fx.stdWeb},
	} {
		label, verdict := Authorize(order, goodIn)
		require.Equal(t, VerdictPass, verdict, "good signature must pass in any order")
		require.Equal(t, "gitlab", label, "the signature verifier must claim the pass over the path token")
	}
}
