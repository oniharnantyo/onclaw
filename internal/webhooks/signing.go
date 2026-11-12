package webhooks

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// computeMAC returns the HMAC of msg under key with the configured hash.
// The caller must have validated algo (Verify rejects unknown algos before
// calling).
func computeMAC(algo HMACAlgo, key, msg []byte) []byte {
	var newHash func() hash.Hash
	switch algo {
	case AlgoSHA1:
		newHash = sha1.New
	default:
		newHash = sha256.New
	}
	mac := hmac.New(newHash, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

// decodeSignature decodes a presented signature string per the configured
// encoding, reporting false when it is not decodable in that encoding.
func decodeSignature(encoding HMACEncoding, sig string) ([]byte, bool) {
	switch encoding {
	case EncHex:
		decoded, err := hex.DecodeString(sig)
		return decoded, err == nil
	case EncBase64:
		decoded, err := base64.StdEncoding.DecodeString(sig)
		return decoded, err == nil
	default:
		return nil, false
	}
}

// decodeWebhookSecret decodes a Standard Webhooks secret: base64, optionally
// under the whsec_ prefix (the serialization the spec prescribes).
func decodeWebhookSecret(secret string) ([]byte, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(secret), "whsec_")
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	return decoded, err == nil && len(decoded) > 0
}

// decodeStripeSecret decodes a Stripe signing secret: the whsec_-prefixed
// form is base64 (the Dashboard serialization), an unprefixed secret is used
// as raw key bytes. This mirrors stripe-go's webhook key handling.
func decodeStripeSecret(secret string) ([]byte, bool) {
	rest, hasPrefix := strings.CutPrefix(secret, "whsec_")
	if !hasPrefix {
		return []byte(secret), secret != ""
	}
	decoded, err := base64.StdEncoding.DecodeString(rest)
	return decoded, err == nil && len(decoded) > 0
}

// withinWindow reports whether ts (unix seconds) lies within window seconds
// of now, tolerating drift in both directions.
func withinWindow(now time.Time, ts int64, window time.Duration) bool {
	delta := now.Unix() - ts
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(window/time.Second)
}

// signDelivery is the inverse of Verify for one engine kind
// (add-webhook-triggers tasks 6.2, design D8): it composes the credential
// headers a request needs to pass cfg against body, timestamping the
// timestamp-bearing engines at now. The path engine needs no headers — it
// authenticates through the URL segment — so it returns an empty header set
// with ok=true; an engine whose secret cannot sign (empty, or undecodable
// for the whsec_ forms) or an unknown kind returns ok=false. header engines
// are included so a self-test can ride a static header credential.
func signDelivery(cfg VerifierConfig, body []byte, now time.Time, deliveryID string) (http.Header, bool) {
	switch cfg.Kind {
	case KindPath:
		// No signature headers: the URL segment is the credential.
		return http.Header{}, true
	case KindHeader:
		if cfg.Secret == "" || cfg.HeaderName == "" {
			return nil, false
		}
		h := http.Header{}
		h.Set(cfg.HeaderName, cfg.Secret)
		return h, true
	case KindRawHMAC:
		if cfg.Secret == "" || cfg.HeaderName == "" {
			return nil, false
		}
		mac := computeMAC(cfg.Algo, []byte(cfg.Secret), body)
		var encoded string
		switch cfg.Encoding {
		case EncHex:
			encoded = hex.EncodeToString(mac)
		case EncBase64:
			encoded = base64.StdEncoding.EncodeToString(mac)
		default:
			return nil, false
		}
		h := http.Header{}
		h.Set(cfg.HeaderName, cfg.Prefix+encoded)
		return h, true
	case KindStdWeb:
		key, ok := decodeWebhookSecret(cfg.Secret)
		if !ok || deliveryID == "" {
			return nil, false
		}
		ts := strconv.FormatInt(now.Unix(), 10)
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(deliveryID))
		mac.Write([]byte("."))
		mac.Write([]byte(ts))
		mac.Write([]byte("."))
		mac.Write(body)
		h := http.Header{}
		h.Set(StdWebIDHeader, deliveryID)
		h.Set(StdWebTimestampHeader, ts)
		h.Set(StdWebSignatureHeader, stdWebSignatureVersion+","+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		return h, true
	case KindTsHMAC:
		return signTsHMAC(cfg, body, now)
	default:
		return nil, false
	}
}

// signTsHMAC composes the timestamp-composite signature header for the
// self-test: Stripe's self-contained "t=...,v1=..." or the Slack/Zoom v0
// composition with its separate timestamp header.
func signTsHMAC(cfg VerifierConfig, body []byte, now time.Time) (http.Header, bool) {
	if cfg.HeaderName == "" {
		return nil, false
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	h := http.Header{}
	switch cfg.Scheme {
	case SchemeStripe:
		key, ok := decodeStripeSecret(cfg.Secret)
		if !ok {
			return nil, false
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(ts))
		mac.Write([]byte("."))
		mac.Write(body)
		h.Set(cfg.HeaderName, "t="+ts+",v1="+hex.EncodeToString(mac.Sum(nil)))
		return h, true
	case SchemeV0:
		if cfg.Secret == "" || cfg.TimestampHeader == "" {
			return nil, false
		}
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write([]byte("v0:"))
		mac.Write([]byte(ts))
		mac.Write([]byte(":"))
		mac.Write(body)
		h.Set(cfg.TimestampHeader, ts)
		h.Set(cfg.HeaderName, "v0="+hex.EncodeToString(mac.Sum(nil)))
		return h, true
	default:
		return nil, false
	}
}

// signDeliverySet signs body with the endpoint's verifier rows: the first
// row that composes credential headers wins (later rows see no signature
// header of their own and skip — no fail-closed trip). A row that cannot
// sign (empty or undecodable secret, unknown engine) is a real defect the
// self-test must surface, so it errors. A path-token-only endpoint signs
// nothing: the URL builder carries its token.
func signDeliverySet(cfgs []VerifierConfig, body []byte, now time.Time, deliveryID string) (http.Header, error) {
	for _, cfg := range cfgs {
		h, ok := signDelivery(cfg, body, now, deliveryID)
		if !ok {
			return nil, fmt.Errorf("webhook self-test: the %s verifier cannot sign: check its secret and configuration", cfg.Kind)
		}
		if len(h) > 0 {
			return h, nil
		}
		// A path row (no signature headers): keep looking for a signable row.
	}
	return http.Header{}, nil
}
