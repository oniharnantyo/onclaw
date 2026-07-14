package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// estimateFloorTokens returns the minimum input token cost that is always
// present regardless of conversation history: the system instruction plus the
// marshaled tool schemas. It reuses estimateTokenCount (chars/4) and clears
// Extra before marshaling each tool info so runtime-injected fields are not
// counted toward the fixed floor. This is the lower bound the input-safety
// guard compares against FloorSafetyLimit.
func estimateFloorTokens(_ context.Context, instruction string, tools []*schema.ToolInfo) (int, error) {
	tokens := estimateTokenCount(len(instruction))
	for _, tl := range tools {
		if tl == nil {
			continue
		}
		tl_ := *tl
		tl_.Extra = nil
		text, err := json.Marshal(tl_)
		if err != nil {
			return 0, fmt.Errorf("marshal tool info: %w", err)
		}
		tokens += estimateTokenCount(len(text))
	}
	return tokens, nil
}
