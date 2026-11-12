package webhooks

import (
	"encoding/json"
	"io"
	"net/http"
)

// DefaultBodyLimit is the delivery size cap: 1 MiB of raw body, the package
// default the receiver applies when no WithBodyLimit option narrows it. A
// body beyond the cap is refused with 413 before any verification work.
const DefaultBodyLimit = 1 << 20

// readBody reads the request's raw body bytes — the exact bytes the sender
// signed — capped cap-first: it reads at most limit+1 bytes and reports
// false when the body reaches limit+1, meaning the cap is exceeded (the
// caller answers 413). The request body is always closed.
func readBody(req *http.Request, limit int64) ([]byte, bool) {
	defer req.Body.Close()
	body, err := io.ReadAll(io.LimitReader(req.Body, limit+1))
	if err != nil {
		return nil, false
	}
	if int64(len(body)) > limit {
		return nil, false
	}
	return body, true
}

// PayloadBlock renders a delivery body as the fenced block that rides the
// prompt or group message (add-webhook-triggers spec "Payload block carries
// the body only"): a ```json fence when the body parses as JSON, a plain
// fence otherwise. Request headers are never included (v1 scope).
func PayloadBlock(body []byte) string {
	if json.Valid(body) {
		return "```json\n" + string(body) + "\n```"
	}
	return "```\n" + string(body) + "\n```"
}

// RenderPrompt appends the delivery's payload block to the endpoint's
// template — the full text an agent-target turn submits or a group-target
// send delivers. An empty template degrades to the payload block alone.
func RenderPrompt(template string, body []byte) string {
	block := PayloadBlock(body)
	if template == "" {
		return block
	}
	return template + "\n\n" + block
}
