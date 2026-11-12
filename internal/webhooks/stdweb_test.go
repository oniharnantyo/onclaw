package webhooks

import (
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerifyStdWeb(t *testing.T) {
	baseCfg := func(secret string, window time.Duration) VerifierConfig {
		return VerifierConfig{Kind: KindStdWeb, Preset: "gitlab", Secret: secret, Window: window}
	}
	sig := func(secret string, id string, ts int64, body []byte) string {
		return refStdWebSign(secret, id, ts, body)
	}
	stdHeaders := func(signature string, id string, tsStr string) map[string]string {
		return map[string]string{
			StdWebIDHeader:        id,
			StdWebTimestampHeader: tsStr,
			StdWebSignatureHeader: signature,
		}
	}
	pastEdge := vecTS - int64(DefaultWebhookWindow/time.Second)   // exactly on the window edge
	futureEdge := vecTS + int64(DefaultWebhookWindow/time.Second) // drift in the other direction
	farPast := vecTS - 3600
	farFuture := vecTS + 3600

	tests := []struct {
		name    string
		cfg     VerifierConfig
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name: "documented vector passes",
			cfg:  baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(
				sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, vecTSStr,
			),
			body: vecBody,
			want: VerdictPass,
		},
		{
			name: "signature candidates list: junk then real passes",
			cfg:  baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(
				"v1,K5oZfzN95Z9UVu1EsfQmfVNQhnkZ2pj9o9NDN/HP4= "+
					"v1a,hnO3f9T8Ytu9HwrXslvumlUpqtNVqkhqw/enGzPCXe5BdqzCInXqYXFymVJaA7AZdpXwVLPo3mNl8EM+m7TBAg== "+
					sig(vecSecretStdWeb, vecID, vecTS, vecBody),
				vecID, vecTSStr,
			),
			body: vecBody,
			want: VerdictPass,
		},
		{
			name: "unprefixed base64 secret works (spec allows both serializations)",
			cfg:  baseCfg(base64.StdEncoding.EncodeToString([]byte("buddies-standard-secret")), 0),
			headers: stdHeaders(
				sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, vecTSStr,
			),
			body: vecBody,
			want: VerdictPass,
		},
		{
			name:    "custom window admits an older timestamp",
			cfg:     baseCfg(vecSecretStdWeb, time.Hour),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, farPast, vecBody), vecID, strconv.FormatInt(farPast, 10)),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "tampered body refuses",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, vecTSStr),
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "wrong secret refuses",
			cfg:     baseCfg("whsec_"+base64.StdEncoding.EncodeToString([]byte("not-the-standard-secret")), 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "timestamp exactly on the past window edge passes",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, pastEdge, vecBody), vecID, strconv.FormatInt(pastEdge, 10)),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "timestamp exactly on the future window edge passes",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, futureEdge, vecBody), vecID, strconv.FormatInt(futureEdge, 10)),
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "expired timestamp refuses with a valid signature",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, farPast, vecBody), vecID, strconv.FormatInt(farPast, 10)),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "future timestamp refuses with a valid signature",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, farFuture, vecBody), vecID, strconv.FormatInt(farFuture, 10)),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "custom window still bounds the timestamp",
			cfg:     baseCfg(vecSecretStdWeb, time.Minute),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, farPast, vecBody), vecID, strconv.FormatInt(farPast, 10)),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "missing webhook-id refuses",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, vecTS, vecBody), "", vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "missing webhook-timestamp refuses",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, ""),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "non-numeric timestamp refuses",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, "1700000000; drop table"),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "garbage signature value refuses",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders("v1,%%not-base64%%", vecID, vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "signature candidates without a v1 entry refuse",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: stdHeaders("v1a,hnO3f9T8Ytu9HwrXslvumlUpqtNVqkhqw/enGzPCXe5BdqzCInXqYXFymVJaA7AZdpXwVLPo3mNl8EM+m7TBAg==", vecID, vecTSStr),
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent signature header skips",
			cfg:     baseCfg(vecSecretStdWeb, 0),
			headers: map[string]string{StdWebIDHeader: vecID, StdWebTimestampHeader: vecTSStr},
			body:    vecBody,
			want:    VerdictSkip,
		},
		{
			name:    "undecodable configured secret fails closed",
			cfg:     baseCfg("whsec_not-base64!!", 0),
			headers: stdHeaders(sig(vecSecretStdWeb, vecID, vecTS, vecBody), vecID, vecTSStr),
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
