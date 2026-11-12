package webhooks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyPath(t *testing.T) {
	const token = vecPathToken
	tests := []struct {
		name      string
		cfg       VerifierConfig
		pathToken string
		want      Verdict
	}{
		{
			name:      "correct token passes",
			cfg:       VerifierConfig{Kind: KindPath, Secret: token},
			pathToken: token,
			want:      VerdictPass,
		},
		{
			name:      "presented wrong token refuses",
			cfg:       VerifierConfig{Kind: KindPath, Secret: token},
			pathToken: "6f9e2f4cc1b841d3a0f4b7e6", // differs in the last char
			want:      VerdictFail,
		},
		{
			name:      "absent token skips",
			cfg:       VerifierConfig{Kind: KindPath, Secret: token},
			pathToken: "",
			want:      VerdictSkip,
		},
		{
			name:      "wrong configured secret never passes the right token",
			cfg:       VerifierConfig{Kind: KindPath, Secret: "other-token"},
			pathToken: token,
			want:      VerdictFail,
		},
		{
			name:      "empty configured secret fails closed on an applied token",
			cfg:       VerifierConfig{Kind: KindPath},
			pathToken: token,
			want:      VerdictFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := signedInput(nil, vecBody)
			in.PathToken = tt.pathToken
			require.Equal(t, tt.want, Verify(tt.cfg, in))
		})
	}
}
