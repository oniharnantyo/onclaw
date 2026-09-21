package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NameUIChart is the generative-UI chart tool's registry name
// (adopt-assistant-ui-elements D3): OnClaw's stand-in for a frontend tool —
// a real Go tool that validates its input schema and echoes the args back as
// the result envelope. The transcript renders the envelope through the
// frontend registry, which dispatches on `$type`.
const NameUIChart = "ui.chart"

// uiChartVariants are the allowed sparkline variants.
var uiChartVariants = map[string]struct{}{
	"area": {},
	"line": {},
	"bars": {},
}

// uiChartEnvelope is the echoed result. The field names mirror exactly what
// the frontend card parses (web/src/lib/generativeUi/ChartCard.tsx): points
// is the ordered sparkline series and visible optionally caps how many of
// those points draw. `$type` is deliberately the FIRST struct field so it
// marshals as the first JSON key — the frontend registry dispatches on it
// (adopt-assistant-ui-elements D4).
type uiChartEnvelope struct {
	Type    string    `json:"$type"`
	Label   string    `json:"label"`
	Value   float64   `json:"value"`
	Delta   *float64  `json:"delta,omitempty"`
	Variant string    `json:"variant,omitempty"`
	Points  []float64 `json:"points"`
	Visible *float64  `json:"visible,omitempty"`
}

// uiChartArgs is the deserialized tool-call argument shape.
type uiChartArgs struct {
	Label   string    `json:"label"`
	Value   *float64  `json:"value"`
	Delta   *float64  `json:"delta"`
	Variant string    `json:"variant"`
	Points  []float64 `json:"points"`
	Visible *float64  `json:"visible"`
}

// uiChartTool validates its schema and echoes the args as the result
// envelope. No persistence, no side effects: the call's value is the
// structured output the transcript renders.
type uiChartTool struct{}

// NewUIChart constructs the chart echo tool.
func NewUIChart() (tool.BaseTool, error) {
	return &uiChartTool{}, nil
}

// Info returns the tool schema surfaced to agentic models.
func (t *uiChartTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameUIChart,
		Desc: "Render a chart card in the conversation: a headline metric with label, current value, optional delta (the change against a previous value — sign is displayed), optional sparkline variant (\"area\", \"line\", or \"bars\"), and an ordered series of points. " +
			"The optional visible count draws only the first n points of the series (at least one point always draws). " +
			"Use it when a numeric result is clearer as a card — counts, measurements, trends — not for every number you mention. " +
			"The call validates its schema and renders exactly what you pass: nothing is fetched, stored, or computed.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"label": {
				Type:     schema.String,
				Desc:     "The chart's headline label.",
				Required: true,
			},
			"value": {
				Type:     schema.Number,
				Desc:     "The current headline value.",
				Required: true,
			},
			"delta": {
				Type:     schema.Number,
				Desc:     "Optional change against the previous value (negative for a decrease).",
				Required: false,
			},
			"variant": {
				Type:     schema.String,
				Desc:     `Optional sparkline shape: "area", "line", or "bars".`,
				Required: false,
			},
			"points": {
				Type: schema.Array,
				Desc: "Ordered sparkline values, in the order they should read.",
				ElemInfo: &schema.ParameterInfo{
					Type: schema.Number,
				},
				Required: true,
			},
			"visible": {
				Type:     schema.Number,
				Desc:     "Optional count of leading points to draw (whole number, at least 1).",
				Required: false,
			},
		}),
	}, nil
}

// InvokableRun satisfies tool.InvokableTool. A schema violation is a tool
// error the model reads and corrects — never a partial echo.
func (t *uiChartTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args uiChartArgs
	if err := strictDecodeArgs(NameUIChart, argumentsInJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Label) == "" {
		return "", fmt.Errorf("%s: label is required", NameUIChart)
	}
	if args.Value == nil {
		return "", fmt.Errorf("%s: value is required", NameUIChart)
	}
	if args.Variant != "" {
		if _, known := uiChartVariants[args.Variant]; !known {
			return "", fmt.Errorf(`%s: variant must be one of "area", "line", "bars", got %q`, NameUIChart, args.Variant)
		}
	}
	if len(args.Points) == 0 {
		return "", fmt.Errorf("%s: points must carry at least one value", NameUIChart)
	}
	if args.Visible != nil && *args.Visible < 1 {
		return "", fmt.Errorf("%s: visible must be a whole number of at least 1, got %v", NameUIChart, *args.Visible)
	}

	echo := uiChartEnvelope{
		Type:    "chart",
		Label:   args.Label,
		Value:   *args.Value,
		Delta:   args.Delta,
		Variant: args.Variant,
		Points:  args.Points,
		Visible: args.Visible,
	}
	return encodeEchoResult(NameUIChart, echo)
}

// encodeEchoResult marshals an echo envelope. The envelope structs keep
// `$type` as their first field so it marshals as the first JSON key — the
// frontend registry dispatches on it (adopt-assistant-ui-elements D4).
func encodeEchoResult(name string, envelope any) (string, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("%s: encode result: %w", name, err)
	}
	return string(encoded), nil
}
