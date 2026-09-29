package openresponses

import (
	"encoding/json"
)

// ResponseRequest is the POST /v1/responses request body. Fields documented
// as accepted-and-ignored (instructions, temperature, max_output_tokens,
// store) are decoded so known fields bind cleanly but do not influence
// execution: the agent's composed instruction and per-agent configuration
// govern.
type ResponseRequest struct {
	Model              string            `json:"model"`
	Input              json.RawMessage   `json:"input"`
	Metadata           map[string]string `json:"metadata"`
	PreviousResponseID string            `json:"previous_response_id"`
	Tools              []RequestTool     `json:"tools"`
	ToolChoice         json.RawMessage   `json:"tool_choice"`
	Stream             bool              `json:"stream"`

	// Accepted and ignored.
	Instructions    json.RawMessage `json:"instructions"`
	Temperature     json.RawMessage `json:"temperature"`
	MaxOutputTokens json.RawMessage `json:"max_output_tokens"`
	Store           *bool           `json:"store"`
}

// RequestTool is one entry of the request-level tools array. Only function
// tools with a name are meaningful; the allowlist intersection drops anything
// the agent does not expose.
type RequestTool struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// ToolChoiceNone reports whether the request demanded a toolless turn
// (tool_choice: "none").
func (r *ResponseRequest) ToolChoiceNone() bool {
	if len(r.ToolChoice) == 0 {
		return false
	}
	var choice string
	if err := json.Unmarshal(r.ToolChoice, &choice); err != nil {
		return false
	}
	return choice == "none"
}

// RequestedToolNames returns the tool names the request asks for. Malformed
// entries are skipped.
func (r *ResponseRequest) RequestedToolNames() []string {
	names := make([]string, 0, len(r.Tools))
	for _, t := range r.Tools {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names
}

// InputPart is one content part of an input item. The attachment-part fields
// (input_image/input_file) stay raw JSON so their URL values can tolerate
// both the plain-string and the {url: "..."} object form; attachments.go
// decodes them. The document-mention fields (add-reference-documents 10.4)
// carry the composer's identity chip — kind/documentId/name/path — either as
// a typed input_document part or as the chip object verbatim (no type
// marker, only kind).
type InputPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	ImageURL json.RawMessage `json:"image_url"`
	FileURL  json.RawMessage `json:"file_url"`
	FileData string          `json:"file_data"`
	FileID   json.RawMessage `json:"file_id"`
	Filename string          `json:"filename"`
	Detail   string          `json:"detail"`

	// Document-mention chip fields (add-reference-documents 10.4). Kind is
	// the chip's own discriminator ("document"); Name mirrors the chip's
	// name field (Filename above stays the input_file form).
	Kind       string `json:"kind"`
	DocumentID string `json:"documentId"`
	Name       string `json:"name"`
	Path       string `json:"path"`
}

// InputItem is one entry of an item-array input.
type InputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// FlattenInput turns the request input into the turn's text input: a string
// passes through; an item array concatenates its input_text parts in order.
// It is the text-only view of the parts-aware FlattenInput (attachments
// design D2): malformed inputs and unsupported part types error with the
// offending type name, and valid image/file parts parse but their references
// are ignored here.
func FlattenInput(raw json.RawMessage) (string, error) {
	text, _, err := FlattenInputParts(raw)
	return text, err
}
