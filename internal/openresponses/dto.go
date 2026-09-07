package openresponses

import (
	"encoding/json"
	"fmt"
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

// InputPart is one content part of an input item.
type InputPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// InputItem is one entry of an item-array input.
type InputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// FlattenInput turns the request input into the turn's text input: a string
// passes through; an item array concatenates its input_text parts in order.
// Unsupported part types error with the offending type name.
func FlattenInput(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var items []InputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("input: expected a string or an array of items")
	}

	var parts []string
	for _, item := range items {
		if len(item.Content) == 0 {
			continue
		}
		// Content is either a plain string or an array of parts.
		var cs string
		if err := json.Unmarshal(item.Content, &cs); err == nil {
			parts = append(parts, cs)
			continue
		}
		var ps []InputPart
		if err := json.Unmarshal(item.Content, &ps); err != nil {
			return "", fmt.Errorf("input: malformed content on item of type %q", item.Type)
		}
		for _, p := range ps {
			switch p.Type {
			case "", "input_text":
				parts = append(parts, p.Text)
			default:
				return "", fmt.Errorf("input: unsupported content part type %q", p.Type)
			}
		}
	}

	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out, nil
}
