package webhooks

import (
	"encoding/json"
	"fmt"
	"time"
)

// lastErrorEnvelope is the persisted shape of the connection's last render
// failure (add-connection-webhooks task 2.4, design.md risks): the event id
// whose delivery dropped, the render error, and the drop timestamp (RFC3339
// with nanoseconds). The store column is a nullable text carrying this JSON;
// the domain carries the raw string, this package owns the format — the
// ingress marshals it, BuildView parses it back for the manage surface.
type lastErrorEnvelope struct {
	Event string `json:"event"`
	Error string `json:"error"`
	At    string `json:"at"`
}

// formatLastError renders the residue one fail-closed render drop persists.
func formatLastError(eventID string, renderErr error, at time.Time) string {
	encoded, err := json.Marshal(lastErrorEnvelope{
		Event: eventID,
		Error: renderErr.Error(),
		At:    at.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		// Marshaling three strings cannot fail; the fallback keeps the write
		// path total regardless.
		return fmt.Sprintf(`{"event":%q,"error":%q,"at":%q}`, eventID, renderErr.Error(), at.UTC().Format(time.RFC3339Nano))
	}
	return string(encoded)
}

// parseLastError decodes a stored residue. ok is false for the clear shape
// ("") and for anything undecodable — a corrupt residue degrades to "no
// last error" on the view, never a serving failure.
func parseLastError(raw string) (lastErrorEnvelope, bool) {
	if raw == "" {
		return lastErrorEnvelope{}, false
	}
	var env lastErrorEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return lastErrorEnvelope{}, false
	}
	return env, true
}
