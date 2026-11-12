// Package webhooks is the webhook trigger surface (add-webhook-triggers):
// it verifies webhook sender authenticity and runs the receiver that turns
// authenticated deliveries into agent fire sessions or group transcript
// messages. It owns the five verifier engines (URL path token, static
// header equality, raw-body HMAC, Standard Webhooks HMAC, and the
// timestamp-composite HMAC schemes of Stripe and Slack/Zoom), the named
// sender presets that seed an endpoint's verifier list, and the fail-closed
// authorize rule: any verifier pass authenticates, while a present-but-
// invalid signature refuses the request with no fall-through to weaker
// credentials. On top of those it owns the accept pipeline (resolve
// endpoint → gates → verify → dedupe → dispatch, with the status map of
// design D5), the bounded delivery log's recording, the gateway-attached
// listener lifecycle, the endpoint self-test, and the narrow dispatch seams
// the composition root adapts (seams.go).
package webhooks
