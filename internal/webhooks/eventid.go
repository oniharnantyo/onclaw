package webhooks

import (
	"encoding/json"
	"strings"
)

// DeriveEventID builds the catalog event id one delivery carries (ingress
// contract §3): the event-type header's value, lowercased with spaces folded
// to underscores, joined with "." to the payload's action. The action is
// probed at the two well-known payload paths — payload.action (GitHub) and
// payload.object_attributes.action (GitLab) — and the id stays bare when the
// payload carries no action ("push", "note"). The declared catalog ids are
// exactly these derived ids.
func DeriveEventID(eventTypeHeaderValue string, payload []byte) string {
	base := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(eventTypeHeaderValue), " ", "_"))
	if base == "" {
		return ""
	}
	if action := payloadAction(payload); action != "" {
		return base + "." + action
	}
	return base
}

// payloadAction returns the delivery payload's action segment: "action" at
// the top level when present, else "object_attributes.action" (the GitLab
// shape), else "" — actionless deliveries keep the bare event id.
func payloadAction(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var doc struct {
		Action           string `json:"action"`
		ObjectAttributes *struct {
			Action string `json:"action"`
		} `json:"object_attributes"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		// Malformed payloads derive the bare event id; rendering fails
		// closed later on the same payload.
		return ""
	}
	if doc.Action != "" {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(doc.Action), " ", "_"))
	}
	if doc.ObjectAttributes != nil && doc.ObjectAttributes.Action != "" {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(doc.ObjectAttributes.Action), " ", "_"))
	}
	return ""
}

// EventSelected reports whether the derived event id is in the connection's
// selected subset of the recipe catalog. Unselected events are acked without
// a turn (spec: "Unselected events ignored").
func EventSelected(events []string, eventID string) bool {
	if eventID == "" {
		return false
	}
	for _, e := range events {
		if e == eventID {
			return true
		}
	}
	return false
}
