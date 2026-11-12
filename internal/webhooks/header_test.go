package webhooks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyHeader(t *testing.T) {
	const name = "X-Gitlab-Token"
	tests := []struct {
		name    string
		cfg     VerifierConfig
		headers map[string]string
		want    Verdict
	}{
		{
			name:    "exact value passes",
			cfg:     VerifierConfig{Kind: KindHeader, HeaderName: name, Secret: vecHeaderVal},
			headers: map[string]string{name: vecHeaderVal},
			want:    VerdictPass,
		},
		{
			name:    "presented wrong value refuses",
			cfg:     VerifierConfig{Kind: KindHeader, HeaderName: name, Secret: vecHeaderVal},
			headers: map[string]string{name: "static-header-secreu"}, // differs in the last char
			want:    VerdictFail,
		},
		{
			name:    "absent header skips",
			cfg:     VerifierConfig{Kind: KindHeader, HeaderName: name, Secret: vecHeaderVal},
			headers: nil,
			want:    VerdictSkip,
		},
		{
			name:    "wrong configured secret never passes the right value",
			cfg:     VerifierConfig{Kind: KindHeader, HeaderName: name, Secret: "other-value"},
			headers: map[string]string{name: vecHeaderVal},
			want:    VerdictFail,
		},
		{
			name:    "empty configured secret fails closed on an applied header",
			cfg:     VerifierConfig{Kind: KindHeader, HeaderName: name},
			headers: map[string]string{name: vecHeaderVal},
			want:    VerdictFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(tt.cfg, signedInput(tt.headers, vecBody)))
		})
	}
}
