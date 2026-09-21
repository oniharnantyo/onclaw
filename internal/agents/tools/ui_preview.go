package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NameUIPreview is the generative-UI preview tool's registry name
// (adopt-assistant-ui-elements D3): validate the schema, echo the args as
// the result envelope, render through the frontend registry. The rendered
// iframe is sandboxed WITHOUT allow-same-origin; the card fixes the height
// and remounts on reload.
const NameUIPreview = "ui.preview"

// uiPreviewEnvelope is the echoed result; `$type` marshals first. The field
// names mirror exactly what the frontend card parses
// (web/src/lib/generativeUi/PreviewCard.tsx): url, html, and an optional
// title. The card fixes the frame height itself — no height field exists.
type uiPreviewEnvelope struct {
	Type  string `json:"$type"`
	URL   string `json:"url"`
	HTML  string `json:"html"`
	Title string `json:"title,omitempty"`
}

// uiPreviewArgs is the deserialized tool-call argument shape.
type uiPreviewArgs struct {
	URL   string `json:"url"`
	HTML  string `json:"html"`
	Title string `json:"title"`
}

// uiPreviewTool validates its schema and echoes the args as the result
// envelope. No persistence, no fetch, no side effects — the tool never
// touches the URL it echoes; the browser renders it.
type uiPreviewTool struct{}

// NewUIPreview constructs the preview echo tool.
func NewUIPreview() (tool.BaseTool, error) {
	return &uiPreviewTool{}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *uiPreviewTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameUIPreview,
		Desc: "Render a live web preview card in the conversation: a URL bar with the address, a sandboxed inline frame, and a reload control. " +
			"Pass the url to display and the html shown while the frame loads (a one-paragraph description of what the page is, or a loading note). " +
			"Optionally name the preview with a short title for the frame. " +
			"Use it when the user should see a page, not a description of it — a dashboard, a deployed draft, a report. " +
			"The call validates its schema and renders exactly what you pass: the tool never fetches the URL or stores anything.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "The absolute http(s) URL the frame displays.",
				Required: true,
			},
			"html": {
				Type:     schema.String,
				Desc:     "Short text shown while the frame loads — what the page is, in one sentence.",
				Required: true,
			},
			"title": {
				Type:     schema.String,
				Desc:     "Optional short title for the preview frame.",
				Required: false,
			},
		}),
	}, nil
}

// InvokableRun satisfies tool.InvokableTool. A schema violation is a tool
// error the model reads and corrects — never a partial echo. The URL must be
// an absolute http(s) address: the card loads it in a sandboxed frame, and
// anything else (javascript:, data:) has no business auto-rendering.
func (t *uiPreviewTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args uiPreviewArgs
	if err := strictDecodeArgs(NameUIPreview, argumentsInJSON, &args); err != nil {
		return "", err
	}
	if !uiPreviewURLAllowed(args.URL) {
		return "", fmt.Errorf("%s: url must be an absolute http(s) URL, got %q", NameUIPreview, args.URL)
	}
	if strings.TrimSpace(args.HTML) == "" {
		return "", fmt.Errorf("%s: html is required", NameUIPreview)
	}

	echo := uiPreviewEnvelope{
		Type:  "preview",
		URL:   args.URL,
		HTML:  args.HTML,
		Title: args.Title,
	}
	return encodeEchoResult(NameUIPreview, echo)
}

// uiPreviewURLAllowed reports whether the URL is an absolute http(s)
// address.
func uiPreviewURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
