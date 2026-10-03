package driver

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCall is one function_call item from a turn's output array, paired with
// its onclaw.function_call_output counterpart by call_id.
type ToolCall struct {
	Name     string
	CallID   string
	ArgsJSON string
	Output   string
}

// Turn is the extracted transcript of one /v1/responses turn: the folded
// answer text, the run status, and every tool call the run exposed.
type Turn struct {
	AnswerText string
	Status     string
	ToolCalls  []ToolCall
}

// ExtractTurn generalizes the eval harness's extractAnswer +
// extractSearchEvidence (internal/memory/eval/run.go) from memory.search to
// all tools: it folds the Response output's message items into one answer
// text and pairs every function_call item with its onclaw.function_call_output
// counterpart by call_id across the same output array. Pairing is by call_id
// alone — call ids are unique per run, and the transcript's output item type
// ("onclaw.function_call_output") is the runner's constant for every tool.
func ExtractTurn(resp map[string]any) Turn {
	turn := Turn{}
	if st, ok := resp["status"].(string); ok {
		turn.Status = st
	}
	output, _ := resp["output"].([]any)

	var parts []string
	// callIndexes maps call_id → index into turn.ToolCalls, in first-seen order.
	callIndexes := map[string]int{}
	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "message":
			content, _ := m["content"].([]any)
			for _, part := range content {
				p, ok := part.(map[string]any)
				if !ok || p["type"] != "output_text" {
					continue
				}
				if t, ok := p["text"].(string); ok && strings.TrimSpace(t) != "" {
					parts = append(parts, t)
				}
			}
		case "function_call":
			name, _ := m["name"].(string)
			callID, _ := m["call_id"].(string)
			args, _ := m["arguments"].(string)
			callIndexes[callID] = len(turn.ToolCalls)
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{
				Name:     name,
				CallID:   callID,
				ArgsJSON: args,
			})
		}
	}
	turn.AnswerText = strings.Join(parts, "\n")

	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok || m["type"] != "onclaw.function_call_output" {
			continue
		}
		callID, _ := m["call_id"].(string)
		idx, tracked := callIndexes[callID]
		if !tracked {
			continue // an output with no matching call item in this transcript
		}
		// "result" is the recorded eval-shape field; "output" is tolerated as
		// the OpenResponses item-shape fallback so a transcript shape drift
		// degrades to empty outputs, not a hard parse failure.
		if v, ok := m["result"]; ok {
			turn.ToolCalls[idx].Output = stringify(v)
		} else if v, ok := m["output"]; ok {
			turn.ToolCalls[idx].Output = stringify(v)
		}
	}
	return turn
}

// stringify renders an arbitrary tool-result JSON value as readable text:
// strings verbatim, everything else re-encoded (or fmt-fell-back) — the same
// discipline the eval harness applies to search evidence.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
