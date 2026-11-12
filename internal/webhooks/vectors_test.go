package webhooks

// Sender-side reference implementations for the pinned verification vectors.
// Each signer below is written straight from that sender's published
// verification recipe (doc link on the comment) and touches no webhooks
// code, so a wrongly composed engine cannot pass its own tests by matching
// itself.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Fixed vector fixtures shared by the engine and preset tests.
const (
	vecID        = "msg_2KWPBgLlAfxdpx2AI54pPJ85f4W"
	vecTS        = int64(1700000000)
	vecPathToken = "6f9e2f4cc1b841d3a0f4b7e5"
	vecHeaderVal = "static-header-secret"

	vecSecretGitHub  = "buddies-github-secret"
	vecSecretShopify = "buddies-shopify-secret"
	vecSecretLinear  = "buddies-linear-secret"
	vecSecretSlack   = "buddies-slack-signing"
	vecSecretZoom    = "buddies-zoom-secret-token"
)

// vecBody is the fixed delivery body every vector signs over; vecNow is the
// fixed verification clock.
var (
	vecBody = []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main"}`)
	vecNow  = time.Unix(vecTS, 0)
)

// whsec_-prefixed base64 secrets, the serialization the Standard Webhooks
// spec and Stripe's Dashboard prescribe.
var (
	vecSecretStdWeb = "whsec_" + base64.StdEncoding.EncodeToString([]byte("buddies-standard-secret"))
	vecSecretStripe = "whsec_" + base64.StdEncoding.EncodeToString([]byte("buddies-stripe-secret"))
)

// vecTSStr is vecTS as the unix-seconds header string every timestamp
// scheme signs.
var vecTSStr = strconv.FormatInt(vecTS, 10)

// signedInput builds a VerifyInput over a header map and body at the fixed
// vector clock.
func signedInput(headers map[string]string, body []byte) VerifyInput {
	return VerifyInput{
		Header: func(name string) string { return headers[name] },
		Body:   body,
		Now:    vecNow,
	}
}

// refGitHubSign implements GitHub's documented recipe
// (https://docs.github.com/webhooks/using-webhooks/validating-webhook-deliveries-from-github):
// the X-Hub-Signature-256 header carries "sha256=" followed by the lowercase
// hex HMAC-SHA256 of the raw body under the webhook secret.
func refGitHubSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// refShopifySign implements Shopify's documented recipe
// (https://shopify.dev/docs/apps/build/webhooks/verify-webhooks): the
// X-Shopify-Hmac-Sha256 header carries the standard base64 HMAC-SHA256 of
// the raw body under the app's API secret key.
func refShopifySign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// refLinearSign implements Linear's documented recipe
// (https://developers.linear.app/docs/graphql/webhooks): the
// Linear-Signature header carries the lowercase hex HMAC-SHA256 of the raw
// body under the webhook's signing secret.
func refLinearSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// refStdWebSign implements the Standard Webhooks signature scheme
// (https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md):
// "v1," followed by the standard base64 HMAC-SHA256 of
// "{id}.{timestamp}.{body}", keyed with the whsec_-stripped base64 decoding
// of the secret. The result is one candidate of the space-separated
// webhook-signature header.
func refStdWebSign(secret, id string, ts int64, body []byte) string {
	key, err := base64.StdEncoding.DecodeString(secret[len("whsec_"):])
	if err != nil {
		panic("vector fixture: " + err.Error())
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(fmt.Sprintf("%s.%d.", id, ts)))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// refStripeSign implements Stripe's documented manual verification recipe
// (https://docs.stripe.com/webhooks#verify-manually): "t=<ts>,v1=" followed
// by the hex HMAC-SHA256 of "<ts>.<body>", keyed with the whsec_-stripped
// base64 decoding of the signing secret. This is the Stripe-Signature
// header value.
func refStripeSign(secret string, ts int64, body []byte) string {
	key := []byte(secret)
	if rest, found := strings.CutPrefix(secret, "whsec_"); found {
		decoded, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			panic("vector fixture: " + err.Error())
		}
		key = decoded
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// refSlackSign implements Slack's documented recipe
// (https://api.slack.com/authentication/verifying-requests-from-slack): the
// X-Slack-Signature header carries "v0=" followed by the hex HMAC-SHA256 of
// "v0:{timestamp}:{body}" under the app's signing secret.
func refSlackSign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("v0:%d:", ts)))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

// refZoomSign implements Zoom's documented validate-with-signature recipe
// (https://developers.zoom.us/docs/api/webhooks/reference/validate-webhook/):
// the x-zm-signature header carries "v0=" followed by the hex HMAC-SHA256 of
// "v0:{timestamp}:{body}" under the webhook secret token.
func refZoomSign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("v0:%d:", ts)))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}
