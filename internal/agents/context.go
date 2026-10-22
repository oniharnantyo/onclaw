package agents

import (
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// turnUsage derives a session's context-window accounting from its persisted
// events (design D7): the last completed turn decides — its final model
// input (what the model's context last held) plus its output — and the same
// usage block yields the last turn's input and output counts beside the
// total. Display-grade accounting for the context ring, not a billing
// figure. All zeros before any completed turn.
func turnUsage(events []sessions.Event) (used, lastInput, lastOutput int) {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Kind == sessions.KindTurnCompleted && ev.Usage != nil {
			return ev.Usage.FinalInputTokens + ev.Usage.OutputTokens,
				ev.Usage.FinalInputTokens, ev.Usage.OutputTokens
		}
	}
	return 0, 0, 0
}

// usedTokens derives a session's used context tokens from its persisted
// events: the last completed turn's final input plus its output. The total
// slice of turnUsage (spec agent-runtime "Context-window accounting").
func usedTokens(events []sessions.Event) int {
	used, _, _ := turnUsage(events)
	return used
}

// accumulateTokenUsage folds one provider-reported usage block into the
// per-turn accumulator. FinalInputTokens is set, never summed: the
// chronologically last model call's input must win (what the context last
// held). Mirrors the donor's turn usage semantics.
func accumulateTokenUsage(acc *sessions.Usage, u *schema.TokenUsage) {
	if u == nil {
		return
	}
	acc.InputTokens += u.PromptTokens
	acc.OutputTokens += u.CompletionTokens
	acc.TotalTokens += u.TotalTokens
	acc.FinalInputTokens = u.PromptTokens
}

// foldUsage merges an already-aggregated usage block (e.g. the compaction
// summarizer call's, reported on its marker event) into a turn accumulator
// under the same semantics as accumulateTokenUsage.
func foldUsage(acc, add *sessions.Usage) {
	if add == nil {
		return
	}
	acc.InputTokens += add.InputTokens
	acc.OutputTokens += add.OutputTokens
	acc.TotalTokens += add.TotalTokens
	if add.FinalInputTokens != 0 {
		acc.FinalInputTokens = add.FinalInputTokens
	}
}
