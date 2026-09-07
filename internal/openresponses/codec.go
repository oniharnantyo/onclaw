// Package openresponses implements the OpenResponses wire contract for the
// /v1 surface: response identity encoding, request DTOs with input
// flattening, and the TranscriptEvent → wire-event translator.
package openresponses

import (
	"fmt"
	"strings"
)

// ResponseIDPrefix prefixes every minted response ID.
const ResponseIDPrefix = "resp_"

// MintResponseID encodes a response ID as resp_<sessionID>_<turnID>. Turn IDs
// are ADK session event IDs (UUIDs — they never contain underscores), so the
// decoding split is the LAST underscore of the payload; session IDs are
// client-chosen and may themselves contain underscores.
func MintResponseID(sessionID, turnID string) string {
	return ResponseIDPrefix + sessionID + "_" + turnID
}

// DecodeResponseID splits a minted response ID back into its session and
// turn. Malformed IDs return an error, which the endpoint surfaces as an
// invalid_request_error.
func DecodeResponseID(id string) (sessionID, turnID string, err error) {
	if !strings.HasPrefix(id, ResponseIDPrefix) {
		return "", "", fmt.Errorf("malformed response id %q", id)
	}
	payload := strings.TrimPrefix(id, ResponseIDPrefix)
	idx := strings.LastIndex(payload, "_")
	if idx <= 0 || idx == len(payload)-1 {
		return "", "", fmt.Errorf("malformed response id %q", id)
	}
	return payload[:idx], payload[idx+1:], nil
}
