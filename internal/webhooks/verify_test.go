// The internal test package: the timestamped scheme's ±skew boundary is
// pinned through the verifyNow clock var, which external tests cannot reach.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// signingFixture is one raw body + secret pair shared by both schemes' tests.
var (
	signBody   = []byte(`{"action":"opened","zen":"Keep it simple."}`)
	signSecret = []byte(" hunter2-secret-for-the-mac ")
)

// hmacHeader computes the GitHub-style signature header value.
func hmacHeader(t *testing.T, secret, body []byte, prefixed bool) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sum := hex.EncodeToString(mac.Sum(nil))
	if prefixed {
		return "sha256=" + sum
	}
	return sum
}

// fixedNow pins the timestamped scheme's clock so skew boundaries are
// deterministic regardless of when the test runs.
var fixedNow = time.Unix(1_700_000_000, 0).UTC()

// pinClock swaps the package clock for a fixed one, restored on cleanup.
func pinClock(t *testing.T, at time.Time) {
	t.Helper()
	orig := verifyNow
	verifyNow = func() time.Time { return at }
	t.Cleanup(func() { verifyNow = orig })
}

// tsSignature computes hex(HMAC-SHA256(secret, "<ts>.<body>")) per the
// timestamped scheme's contract — ASCII decimal timestamp, literal dot, raw
// body.
func tsSignature(secret, body []byte, ts int64) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(fmt.Sprintf("%s.%s", strconv.FormatInt(ts, 10), body)))
	return hex.EncodeToString(mac.Sum(nil))
}

// tsHeader builds a well-formed "t=<unix>,v1=<hex>" header value.
func tsHeader(ts int64, secret, body []byte) string {
	return fmt.Sprintf("t=%d,v1=%s", ts, tsSignature(secret, body, ts))
}

func TestVerifySignature_HMACSHA256(t *testing.T) {
	cases := []struct {
		name   string
		header string
		secret []byte
		body   []byte
		want   bool
	}{
		{"valid with sha256= prefix", hmacHeader(t, signSecret, signBody, true), signSecret, signBody, true},
		{"valid bare hex", hmacHeader(t, signSecret, signBody, false), signSecret, signBody, true},
		{"valid uppercase hex", "sha256=" + strings.ToUpper(hmacHeader(t, signSecret, signBody, false)), signSecret, signBody, true},
		{"tampered body", hmacHeader(t, signSecret, signBody, true), signSecret, []byte(`{"action":"opened","zen":"Keep it simple!"}`), false},
		{"wrong secret", hmacHeader(t, []byte("other-secret"), signBody, true), signSecret, signBody, false},
		{"garbage value", "sha256=zzzz-not-hex", signSecret, signBody, false},
		{"empty value", "", signSecret, signBody, false},
		{"truncated mac", hmacHeader(t, signSecret, signBody, true)[:20], signSecret, signBody, false},
		{"sha1-labeled prefix carrying the right sha256 mac", "sha1=" + hmacHeader(t, signSecret, signBody, false), signSecret, signBody, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(domain.RecipeWebhookSchemeHMACSHA256, tc.header, tc.secret, tc.body); got != tc.want {
				t.Fatalf("VerifySignature(hmac_sha256) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifySignature_SecretToken(t *testing.T) {
	const token = "gitlab-shared-token-9f2c"
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"verbatim match", token, true},
		{"wrong token", "gitlab-shared-token-9f2d", false},
		{"prefix of the token", token[:10], false},
		{"padded token", token + " ", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(domain.RecipeWebhookSchemeSecretToken, tc.header, []byte(token), signBody); got != tc.want {
				t.Fatalf("VerifySignature(secret_token) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVerifySignature_PlainToken routes the standalone endpoint's scheme
// through the same constant-time compare as secret_token (spec: "a
// plain-token header compared in constant time") — the table mirrors
// TestVerifySignature_SecretToken verbatim over the domain constant.
func TestVerifySignature_PlainToken(t *testing.T) {
	const token = "gitlab-shared-token-9f2c"
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"verbatim match", token, true},
		{"wrong token", "gitlab-shared-token-9f2d", false},
		{"prefix of the token", token[:10], false},
		{"padded token", token + " ", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(domain.WorkspaceWebhookSchemePlainToken, tc.header, []byte(token), signBody); got != tc.want {
				t.Fatalf("VerifySignature(plain_token) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifySignature_TimestampedHMAC(t *testing.T) {
	ts := fixedNow.Unix()
	valid := tsHeader(ts, signSecret, signBody)
	otherKey := []byte("a-different-endpoint-secret")

	cases := []struct {
		name   string
		header string
		secret []byte
		body   []byte
		want   bool
	}{
		{"valid signature", valid, signSecret, signBody, true},
		{"valid uppercase hex digest", fmt.Sprintf("t=%d,v1=%s", ts, strings.ToUpper(tsSignature(signSecret, signBody, ts))), signSecret, signBody, true},
		{"boundary: exactly the skew old", tsHeader(ts-300, signSecret, signBody), signSecret, signBody, true},
		{"boundary: exactly the skew ahead", tsHeader(ts+300, signSecret, signBody), signSecret, signBody, true},
		{"one second beyond the skew, past", tsHeader(ts-301, signSecret, signBody), signSecret, signBody, false},
		{"one second beyond the skew, future", tsHeader(ts+301, signSecret, signBody), signSecret, signBody, false},
		{"stale capture, an hour old", tsHeader(ts-3600, signSecret, signBody), signSecret, signBody, false},
		{"far-future timestamp", tsHeader(ts+3600, signSecret, signBody), signSecret, signBody, false},
		{"wrong key", tsHeader(ts, otherKey, signBody), signSecret, signBody, false},
		{"tampered body", tsHeader(ts, signSecret, []byte(`{"action":"opened","zen":"Keep it simple!"}`)), signSecret, signBody, false},
		{"digest over the raw body, timestamp omitted", fmt.Sprintf("t=%d,v1=%s", ts, hmacHeader(t, signSecret, signBody, false)), signSecret, signBody, false},
		{"signature bound to a different timestamp", fmt.Sprintf("t=%d,v1=%s", ts, tsSignature(signSecret, signBody, ts+1)), signSecret, signBody, false},
		{"missing t= prefix", "v1=" + tsSignature(signSecret, signBody, ts), signSecret, signBody, false},
		{"missing v1 label", fmt.Sprintf("t=%d,%s", ts, tsSignature(signSecret, signBody, ts)), signSecret, signBody, false},
		{"missing v1 part", fmt.Sprintf("t=%d", ts), signSecret, signBody, false},
		{"leading whitespace", " " + valid, signSecret, signBody, false},
		{"non-integer timestamp", "t=not-a-time,v1=" + tsSignature(signSecret, signBody, ts), signSecret, signBody, false},
		{"float timestamp", fmt.Sprintf("t=%d.5,v1=%s", ts, tsSignature(signSecret, signBody, ts)), signSecret, signBody, false},
		{"odd-length hex digest", "t=1700000000,v1=abc", signSecret, signBody, false},
		{"wrong-length digest", "t=1700000000,v1=" + hex.EncodeToString([]byte("0123456789abcdef")), signSecret, signBody, false},
		{"trailing non-hex garbage", valid + "&extra", signSecret, signBody, false},
		{"trailing hex garbage", valid + "ab", signSecret, signBody, false},
		{"empty header", "", signSecret, signBody, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinClock(t, fixedNow)
			if got := VerifySignature(domain.WorkspaceWebhookSchemeTimestampedHmac, tc.header, tc.secret, tc.body); got != tc.want {
				t.Fatalf("VerifySignature(timestamped_hmac) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTimestampedHmacSchemeConstant(t *testing.T) {
	// The verifier routes on the domain constant; the exact string is the
	// cross-worker contract with the migration's CHECK and the endpoint form.
	if got, want := domain.WorkspaceWebhookSchemeTimestampedHmac, "timestamped_hmac"; got != want {
		t.Fatalf("domain.WorkspaceWebhookSchemeTimestampedHmac = %q, want %q", got, want)
	}
}

func TestTimestampedReplayKey(t *testing.T) {
	valid := tsHeader(1700000000, signSecret, signBody)

	if got, again := TimestampedReplayKey(valid), TimestampedReplayKey(valid); got != again {
		t.Fatalf("TimestampedReplayKey not stable: %q vs %q", got, again)
	}

	key := TimestampedReplayKey(valid)
	digest, ok := strings.CutPrefix(key, "sig:")
	if !ok {
		t.Fatalf("TimestampedReplayKey = %q, want the \"sig:\" prefix", key)
	}
	if _, err := hex.DecodeString(digest); err != nil || len(digest) != sha256.Size*2 {
		t.Fatalf("TimestampedReplayKey digest = %q, want %d lowercase hex chars", digest, sha256.Size*2)
	}
	if digest != strings.ToLower(digest) {
		t.Fatalf("TimestampedReplayKey digest %q must be lowercase hex", digest)
	}

	other := tsHeader(1700000001, signSecret, signBody)
	if TimestampedReplayKey(valid) == TimestampedReplayKey(other) {
		t.Fatal("different signature headers must derive different replay keys")
	}

	for _, malformed := range []string{
		"",
		"t=1",
		"v1=" + strings.Repeat("a", 64),
		"t=abc,v1=" + strings.Repeat("a", 64),
		"t=1,v1=abc",
		valid + "zz",
	} {
		if got := TimestampedReplayKey(malformed); got != "" {
			t.Errorf("TimestampedReplayKey(%q) = %q, want \"\" for a malformed header", malformed, got)
		}
	}
}

func TestVerifySignature_UnknownScheme(t *testing.T) {
	// Registration validation admits only the catalog schemes; an unknown
	// scheme must verify nothing.
	if VerifySignature("plain_text", "anything", signSecret, signBody) {
		t.Fatal("an unknown scheme must never verify")
	}
}
