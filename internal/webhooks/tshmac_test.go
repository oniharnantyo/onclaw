package webhooks

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyTsHMACStripe(t *testing.T) {
	const name = "Stripe-Signature"
	stripeCfg := func(secret string) VerifierConfig {
		return VerifierConfig{
			Kind: KindTsHMAC, Preset: "stripe",
			Scheme: SchemeStripe, HeaderName: name,
			Secret: secret,
		}
	}
	tests := []struct {
		name    string
		cfg     VerifierConfig
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name: "documented vector passes",
			cfg:  stripeCfg(vecSecretStripe),
			headers: map[string]string{
				name: refStripeSign(vecSecretStripe, vecTS, vecBody),
			},
			body: vecBody,
			want: VerdictPass,
		},
		{
			name: "multiple v1 candidates pass when one matches",
			cfg:  stripeCfg(vecSecretStripe),
			headers: map[string]string{
				name: "t=" + vecTSStr +
					",v1=" + hex.EncodeToString([]byte("0000000000000000000000000000000000000000000000000000000000000000")) +
					"," + strings.TrimPrefix(refStripeSign(vecSecretStripe, vecTS, vecBody), "t="+vecTSStr+","),
			},
			body: vecBody,
			want: VerdictPass,
		},
		{
			name:    "unprefixed raw secret works (stripe-go key handling)",
			cfg:     stripeCfg("buddies-stripe-secret"),
			headers: map[string]string{name: refStripeSign("buddies-stripe-secret", vecTS, vecBody)},
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "tampered body refuses",
			cfg:     stripeCfg(vecSecretStripe),
			headers: map[string]string{name: refStripeSign(vecSecretStripe, vecTS, vecBody)},
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "wrong secret refuses",
			cfg:     stripeCfg("whsec_" + "wr9uZ25nLXN0cmlwZS1zZWNyZXQ="),
			headers: map[string]string{name: refStripeSign(vecSecretStripe, vecTS, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "stale t refuses",
			cfg:     stripeCfg(vecSecretStripe),
			headers: map[string]string{name: refStripeSign(vecSecretStripe, vecTS-3600, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "header without t= refuses",
			cfg:     stripeCfg(vecSecretStripe),
			headers: map[string]string{name: strings.TrimPrefix(refStripeSign(vecSecretStripe, vecTS, vecBody), "t="+vecTSStr+",")},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "header without v1= refuses",
			cfg:     stripeCfg(vecSecretStripe),
			headers: map[string]string{name: "t=" + vecTSStr},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "non-hex v1 refuses",
			cfg:     stripeCfg(vecSecretStripe),
			headers: map[string]string{name: "t=" + vecTSStr + ",v1=zz-not-hex"},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "whsec_ value that does not decode refuses",
			cfg:     stripeCfg("whsec_not-base64!!"),
			headers: map[string]string{name: refStripeSign(vecSecretStripe, vecTS, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent header skips",
			cfg:     stripeCfg(vecSecretStripe),
			headers: nil,
			body:    vecBody,
			want:    VerdictSkip,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(tt.cfg, signedInput(tt.headers, tt.body)))
		})
	}
}

func TestVerifyTsHMACSlack(t *testing.T) {
	slackCfg := func(secret string) VerifierConfig {
		return VerifierConfig{
			Kind: KindTsHMAC, Preset: "slack",
			Scheme: SchemeV0, HeaderName: "X-Slack-Signature",
			TimestampHeader: "X-Slack-Request-Timestamp",
			Secret:          secret,
		}
	}
	slackHeaders := func(sig string, tsStr string) map[string]string {
		return map[string]string{
			"X-Slack-Signature":         sig,
			"X-Slack-Request-Timestamp": tsStr,
		}
	}
	tests := []struct {
		name    string
		cfg     VerifierConfig
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name:    "documented vector passes",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS, vecBody), vecTSStr),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "comma-separated candidates pass when one matches",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders("v0="+strings.Repeat("0", 64)+","+refSlackSign(vecSecretSlack, vecTS, vecBody), vecTSStr),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "tampered body refuses",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS, vecBody), vecTSStr),
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "wrong signing secret refuses",
			cfg:     slackCfg("not-the-slack-signing-secret"),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS, vecBody), vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "stale timestamp refuses",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS-3600, vecBody), "1699996400"),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "timestamp exactly on the window edge passes",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS-300, vecBody), "1699999700"),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "missing timestamp header refuses",
			cfg:     slackCfg(vecSecretSlack),
			headers: map[string]string{"X-Slack-Signature": refSlackSign(vecSecretSlack, vecTS, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "non-numeric timestamp refuses",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS, vecBody), "now-ish"),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "non-hex signature refuses",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders("v0=not-hex!", vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "entry without the v0= prefix refuses",
			cfg:     slackCfg(vecSecretSlack),
			headers: slackHeaders(strings.Repeat("a", 64), vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent signature header skips",
			cfg:     slackCfg(vecSecretSlack),
			headers: map[string]string{"X-Slack-Request-Timestamp": vecTSStr},
			body:    vecBody,
			want:    VerdictSkip,
		},
		{
			name:    "empty configured secret fails closed",
			cfg:     slackCfg(""),
			headers: slackHeaders(refSlackSign(vecSecretSlack, vecTS, vecBody), vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(tt.cfg, signedInput(tt.headers, tt.body)))
		})
	}
}

func TestVerifyTsHMACZoom(t *testing.T) {
	zoomCfg := func(secret string) VerifierConfig {
		return VerifierConfig{
			Kind: KindTsHMAC, Preset: "zoom",
			Scheme: SchemeV0, HeaderName: "x-zm-signature",
			TimestampHeader: "x-zm-request-timestamp",
			Secret:          secret,
		}
	}
	zoomHeaders := func(sig string, tsStr string) map[string]string {
		return map[string]string{
			"x-zm-signature":         sig,
			"x-zm-request-timestamp": tsStr,
		}
	}
	tests := []struct {
		name    string
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name:    "documented vector passes",
			headers: zoomHeaders(refZoomSign(vecSecretZoom, vecTS, vecBody), vecTSStr),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "wrong secret token refuses",
			headers: zoomHeaders(refZoomSign("not-the-zoom-token", vecTS, vecBody), vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "tampered body refuses",
			headers: zoomHeaders(refZoomSign(vecSecretZoom, vecTS, vecBody), vecTSStr),
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "stale timestamp refuses",
			headers: zoomHeaders(refZoomSign(vecSecretZoom, vecTS-3600, vecBody), "1699996400"),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent signature header skips",
			headers: map[string]string{"x-zm-request-timestamp": vecTSStr},
			body:    vecBody,
			want:    VerdictSkip,
		},
	}
	cfg := zoomCfg(vecSecretZoom)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(cfg, signedInput(tt.headers, tt.body)))
		})
	}
}

func TestVerifyTsHMACUnknownScheme(t *testing.T) {
	cfg := VerifierConfig{
		Kind: KindTsHMAC, Scheme: "hmac",
		HeaderName: "X-Whatever", Secret: vecSecretSlack,
	}
	in := signedInput(map[string]string{"X-Whatever": "anything"}, vecBody)
	require.Equal(t, VerdictFail, Verify(cfg, in))
}
