package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NameUITimeline is the generative-UI timeline tool's registry name
// (adopt-assistant-ui-elements D3): validate the schema, echo the args as
// the result envelope, render through the frontend registry.
const NameUITimeline = "ui.timeline"

// uiTimelineStates are the allowed event states: settled events happened;
// reference events mark planned or external anchors. The frontend card
// (web/src/lib/generativeUi/TimelineCard.tsx) reads exactly this `state` key.
var uiTimelineStates = map[string]struct{}{
	"settled":   {},
	"reference": {},
}

// uiTimelineEnvelope is the echoed result; `$type` marshals first.
type uiTimelineEnvelope struct {
	Type   string                `json:"$type"`
	Title  string                `json:"title,omitempty"`
	Events []uiTimelineEventEcho `json:"events"`
}

// uiTimelineEventEcho is one echoed event. The field names mirror exactly
// what the frontend card parses: label, at, state (settled vs reference),
// and an optional detail line.
type uiTimelineEventEcho struct {
	Label  string `json:"label"`
	At     string `json:"at,omitempty"`
	State  string `json:"state,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// uiTimelineArgs is the deserialized tool-call argument shape.
type uiTimelineArgs struct {
	Title  string                `json:"title"`
	Events []uiTimelineEventEcho `json:"events"`
}

// uiTimelineTool validates its schema and echoes the args as the result
// envelope. No persistence, no side effects.
type uiTimelineTool struct{}

// NewUITimeline constructs the timeline echo tool.
func NewUITimeline() (tool.BaseTool, error) {
	return &uiTimelineTool{}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *uiTimelineTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameUITimeline,
		Desc: "Render a timeline card in the conversation: an ordered vertical sequence of events under an optional title. " +
			"Each event carries a label, an optional at (its timestamp or time label, displayed verbatim), an optional state — \"settled\" for events that already happened, \"reference\" for planned or external anchors — and an optional detail line rendered beneath the event. " +
			"Pass events in the order they should read (earliest first reads naturally). " +
			"Use it for incident sequences, migration steps, rollout phases — any where things line up in time. " +
			"The call validates its schema and renders exactly what you pass: nothing is fetched, stored, or computed.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"title": {
				Type:     schema.String,
				Desc:     "Optional timeline title.",
				Required: false,
			},
			"events": {
				Type:     schema.Array,
				Desc:     `Ordered events, each an object {"label": string (required), "at": string (optional), "state": "settled"|"reference" (optional), "detail": string (optional)}.`,
				Required: true,
			},
		}),
	}, nil
}

// InvokableRun satisfies tool.InvokableTool. A schema violation is a tool
// error the model reads and corrects — never a partial echo.
func (t *uiTimelineTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args uiTimelineArgs
	if err := strictDecodeArgs(NameUITimeline, argumentsInJSON, &args); err != nil {
		return "", err
	}
	if len(args.Events) == 0 {
		return "", fmt.Errorf("%s: events must carry at least one event", NameUITimeline)
	}
	for i, event := range args.Events {
		if strings.TrimSpace(event.Label) == "" {
			return "", fmt.Errorf("%s: event %d: label is required", NameUITimeline, i+1)
		}
		if event.State != "" {
			if _, known := uiTimelineStates[event.State]; !known {
				return "", fmt.Errorf(`%s: event %d: state must be "settled" or "reference", got %q`, NameUITimeline, i+1, event.State)
			}
		}
	}

	echo := uiTimelineEnvelope{
		Type:   "timeline",
		Title:  args.Title,
		Events: args.Events,
	}
	return encodeEchoResult(NameUITimeline, echo)
}
