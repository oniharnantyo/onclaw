package webhooks

import "time"

// VerifierKind names one of the five credential engines a verifier row runs.
type VerifierKind string

const (
	// KindPath authenticates the secret URL path token segment.
	KindPath VerifierKind = "path"
	// KindHeader authenticates a static header name/value pair.
	KindHeader VerifierKind = "header"
	// KindRawHMAC authenticates an HMAC carried in one signature header and
	// computed over the exact raw body bytes (GitHub, Shopify, Linear).
	KindRawHMAC VerifierKind = "raw-hmac"
	// KindStdWeb authenticates the Standard Webhooks signature scheme
	// (GitLab signing, Svix-style senders).
	KindStdWeb VerifierKind = "stdweb"
	// KindTsHMAC authenticates a timestamp-composite HMAC (Stripe, or the
	// Slack/Zoom v0 composition).
	KindTsHMAC VerifierKind = "ts-hmac"
)

// HMACAlgo selects the hash function of a raw-hmac verifier.
type HMACAlgo string

const (
	// AlgoSHA256 is HMAC-SHA-256.
	AlgoSHA256 HMACAlgo = "sha256"
	// AlgoSHA1 is HMAC-SHA-1.
	AlgoSHA1 HMACAlgo = "sha1"
)

// HMACEncoding selects the string encoding of a raw-hmac signature header.
type HMACEncoding string

const (
	// EncHex is hexadecimal (either case).
	EncHex HMACEncoding = "hex"
	// EncBase64 is standard padded base64.
	EncBase64 HMACEncoding = "base64"
)

// TsHMACScheme selects the composition of a ts-hmac verifier.
type TsHMACScheme string

const (
	// SchemeStripe is Stripe's composition: the signature header carries
	// "t=<unix seconds>,v1=<hex>" and the MAC covers "<t>.<body>" under the
	// whsec_-decoded signing secret.
	SchemeStripe TsHMACScheme = "stripe"
	// SchemeV0 is the Slack/Zoom composition: the timestamp rides a
	// separate header and the MAC covers "v0:<timestamp>:<body>".
	SchemeV0 TsHMACScheme = "v0"
)

// DefaultWebhookWindow is the replay tolerance a timestamp-bearing verifier
// applies when its Window is not set: five minutes in both directions, the
// guidance of the Standard Webhooks, Stripe, and Slack verification docs.
const DefaultWebhookWindow = 5 * time.Minute

// VerifierConfig is one stored verifier: the engine to run, the preset label
// that seeded it ("" for a manually built row), the engine's non-secret
// fields, and the shared secret. It is plain data so a later change can
// persist it as ordered verifier rows.
//
// Field use by kind (fields of other kinds are ignored):
//
//	path:     Secret (the URL path token).
//	header:   HeaderName + Secret (the exact header value).
//	raw-hmac: HeaderName, Algo, Encoding, Prefix, Secret.
//	stdweb:   Secret (whsec_-prefixed base64), Window.
//	ts-hmac:  Scheme, HeaderName, TimestampHeader (v0 only), Secret, Window.
type VerifierConfig struct {
	Kind   VerifierKind
	Preset string
	Secret string
	// HeaderName is the header the engine reads: the value header for
	// KindHeader, the signature header for KindRawHMAC and KindTsHMAC.
	HeaderName string
	// TimestampHeader names the header carrying the unix timestamp for the
	// SchemeV0 composition (Stripe embeds it in the signature header).
	TimestampHeader string
	// Algo and Encoding configure the raw-hmac engine; Prefix is the
	// literal string stripped from the signature value before decoding
	// (GitHub's "sha256=").
	Algo     HMACAlgo
	Encoding HMACEncoding
	Prefix   string
	// Scheme selects the ts-hmac composition.
	Scheme TsHMACScheme
	// Window bounds the timestamp recency of the stdweb and ts-hmac
	// engines in both directions; zero or negative selects
	// DefaultWebhookWindow.
	Window time.Duration
}
