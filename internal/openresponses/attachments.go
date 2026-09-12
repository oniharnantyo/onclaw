package openresponses

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// CapabilityPathPrefix is the onclaw capability-URL path prefix: the wire
// token shape every attachment reference takes (upload response url and /v1
// input parts alike). The suffix after the prefix is the raw capability key
// the attachment store resolves by (a global bearer-token lookup).
const CapabilityPathPrefix = "/api/v1/files/"

// InputAttachment is one attachment candidate parsed from an input content
// part (attachments design D2). Kind is "image" (input_image) or "file"
// (input_file); URL is the inline data: URL when Inline is set, otherwise the
// normalized onclaw capability path; Filename is the part's filename when
// given; Detail carries the OpenAI image detail hint — the OpenAI backend may
// use it later, other backends ignore it. It is a wire-level candidate:
// resolving it to an agents.AttachmentRef is the /v1 handler's job.
type InputAttachment struct {
	Kind     string // "image" | "file"
	URL      string
	Filename string
	Detail   string
	Inline   bool
}

// CapabilityKey returns the capability key carried by a non-inline input
// attachment and whether the reference is an onclaw capability path at all.
func (a InputAttachment) CapabilityKey() (string, bool) {
	if a.Inline {
		return "", false
	}
	key, ok := strings.CutPrefix(a.URL, CapabilityPathPrefix)
	if !ok || key == "" {
		return "", false
	}
	return key, true
}

// FlattenInputParts turns the request input into the turn's text input plus
// the attachment candidates its content parts reference (attachments design
// D2): a string passes through; an item array concatenates its input_text
// parts in order and collects input_image/input_file parts. image_url and
// file_url accept a plain string or an object with a url field; file_data is
// tolerated as the inline file form. Every URL must be an inline data: URL or
// an onclaw capability URL — file_id and remote third-party URLs error. The
// text join is byte-for-byte identical to FlattenInput's.
func FlattenInputParts(raw json.RawMessage) (string, []InputAttachment, error) {
	if len(raw) == 0 {
		return "", nil, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil, nil
	}

	var items []InputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", nil, fmt.Errorf("input: expected a string or an array of items")
	}

	var parts []string
	var atts []InputAttachment
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
			return "", nil, fmt.Errorf("input: malformed content on item of type %q", item.Type)
		}
		for _, p := range ps {
			switch p.Type {
			case "", "input_text":
				parts = append(parts, p.Text)
			case "input_image":
				att, err := imageAttachment(p)
				if err != nil {
					return "", nil, err
				}
				atts = append(atts, att)
			case "input_file":
				att, err := fileAttachment(p)
				if err != nil {
					return "", nil, err
				}
				atts = append(atts, att)
			default:
				return "", nil, fmt.Errorf("input: unsupported content part type %q", p.Type)
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
	return out, atts, nil
}

// imageAttachment parses one input_image part: image_url (string or {url})
// plus the optional detail hint.
func imageAttachment(p InputPart) (InputAttachment, error) {
	raw, err := urlField(p.ImageURL)
	if err != nil {
		return InputAttachment{}, fmt.Errorf("input: input_image: %v", err)
	}
	u, inline, err := partURL(raw)
	if err != nil {
		return InputAttachment{}, fmt.Errorf("input: input_image: %v", err)
	}
	return InputAttachment{Kind: "image", URL: u, Detail: p.Detail, Inline: inline}, nil
}

// fileAttachment parses one input_file part: file_url (string or {url}) or
// the inline file_data alias. file_id is always rejected — there is no OpenAI
// files backend to resolve against (design D2).
func fileAttachment(p InputPart) (InputAttachment, error) {
	if len(p.FileID) > 0 && string(p.FileID) != "null" {
		return InputAttachment{}, fmt.Errorf("input: input_file: file_id is not supported; reference the attachment's onclaw capability URL or an inline data URL")
	}

	raw := p.FileData
	if len(p.FileURL) > 0 && string(p.FileURL) != "null" {
		s, err := urlField(p.FileURL)
		if err != nil {
			return InputAttachment{}, fmt.Errorf("input: input_file: %v", err)
		}
		raw = s
	}

	u, inline, err := partURL(raw)
	if err != nil {
		return InputAttachment{}, fmt.Errorf("input: input_file: %v", err)
	}
	return InputAttachment{Kind: "file", URL: u, Filename: p.Filename, Inline: inline}, nil
}

// urlField decodes a URL-valued part field that may be a plain string or an
// object carrying a url field (the OpenResponses convention tolerates both).
func urlField(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("a URL is required")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var obj struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.URL != "" {
		return obj.URL, nil
	}
	return "", fmt.Errorf("expected a URL string or an object with a url field")
}

// partURL classifies a part URL: a data: URL is inline; an onclaw capability
// URL (path prefix /api/v1/files/, possibly behind an absolute origin)
// normalizes to its path; anything else — including remote http(s) URLs — is
// rejected (D2: fetching client-supplied remote URLs is out of scope).
func partURL(raw string) (u string, inline bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("a URL is required")
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		return raw, true, nil
	}
	if parsed, perr := url.Parse(raw); perr == nil && strings.HasPrefix(parsed.Path, CapabilityPathPrefix) {
		return parsed.Path, false, nil
	}
	return "", false, fmt.Errorf("remote URLs are not accepted; reference the attachment's onclaw capability URL or an inline data URL")
}
