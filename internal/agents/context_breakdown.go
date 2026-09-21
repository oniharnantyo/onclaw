package agents

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Estimator constants for the display-only context breakdown
// (adopt-assistant-ui-elements D7) — the same display-grade values the web
// estimator uses (web/src/lib/contextBreakdown.ts, design D2), so server and
// client numbers agree in shape and skew.
const (
	// contextCharPerToken mirrors the summarization middleware's default
	// display estimator (~4 chars/token) that estimateWindowTokens shares.
	contextCharPerToken = 4
	// contextImageTokens is the flat input-cost estimate per in-window image
	// attachment — the web estimator's IMAGE_TOKENS constant.
	contextImageTokens = 1100
	// contextMinFileTokens is the floor for byte-derived attachment cost —
	// the web estimator's minimum for non-image attachments.
	contextMinFileTokens = 64
)

// contextSizes carries the compose-time section byte counts of one run's
// composed context (D7): the composed instruction string and the marshaled
// tool schemas, both captured from the real composed artifacts at compose
// time. Written by composeAgent before the run goroutine starts (the `go`
// statement that launches the drain establishes the happens-before), read at
// turn end by the breakdown measurement — no locking required, the same
// single-writer shape as the compaction estimates.
type contextSizes struct {
	instructionBytes int
	toolSchemaBytes  int
}

// recordCompose stamps the compose-time byte counts. The composed
// instruction IS the instructions segment's real string: persona +
// workspace/user docs + memory sections (including the todo summary line) +
// channel docs + profiles, joined exactly as sent.
func (s *contextSizes) recordCompose(instructionBytes, toolBytes int) {
	s.instructionBytes = instructionBytes
	s.toolSchemaBytes = toolBytes
}

// toolSchemaBytes marshals every resolved registry tool's parameter schema
// the composition hands to the model node and reports the total byte length —
// the tools segment's compose-time measurement. Tools whose schema cannot be
// marshaled contribute nothing: their share stays in the server remainder
// rather than a fabricated count.
func toolSchemaBytes(ctx context.Context, tools []tool.BaseTool) int {
	var total int
	for _, t := range tools {
		if t == nil {
			continue
		}
		info, err := t.Info(ctx)
		if err != nil || info == nil {
			continue
		}
		js, err := info.ParamsOneOf.ToJSONSchema()
		if err != nil || js == nil {
			continue
		}
		b, err := json.Marshal(js)
		if err != nil {
			continue
		}
		total += len(b)
	}
	return total
}

// estimateWindowFileTokens estimates the attachment-payload share of a
// message window with the web estimator's constants: a flat 1100 tokens per
// in-window image attachment and a byte-derived cost (ceil(size/4), floored
// at 64) for other payload-carrying file attachments. Text-lane attachment
// content — fenced inline text and the degraded/drop-lane pointer notes — is
// plain text already counted by the conversation estimator, so it is never
// counted here twice. Unidentified payloads contribute nothing.
func estimateWindowFileTokens(msgs []*schema.AgenticMessage) int {
	var tokens int
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		for _, block := range msg.ContentBlocks {
			if block == nil {
				continue
			}
			switch {
			case block.UserInputImage != nil:
				tokens += contextImageTokens
			case block.UserInputFile != nil:
				meta, ok := attachmentBlockMetaOf(block)
				if !ok || meta.Size <= 0 {
					continue
				}
				n := (meta.Size + contextCharPerToken - 1) / contextCharPerToken
				if n < contextMinFileTokens {
					n = contextMinFileTokens
				}
				tokens += int(n)
			}
		}
	}
	return tokens
}

// measureContextBreakdown assembles a turn's display-only context breakdown
// (D7) from the compose-time section sizes and the true session window at
// turn end. It returns nil when the provider reported no usage — the block
// never appears without a usage block — or when nothing at all could be
// measured. Every unmeasurable segment is omitted (never zero): the struct's
// per-field omitempty drops zero segments on the wire. Display-only by
// contract: nothing here feeds billing, trigger math, or summarization.
func measureContextBreakdown(
	ctx context.Context,
	sessionStore adk.SessionEventStore[*schema.AgenticMessage],
	sessionID string,
	sizes *contextSizes,
	usage UsagePayload,
) *ContextBreakdown {
	if usageOf(usage) == nil {
		return nil
	}

	var cb ContextBreakdown
	if sizes != nil {
		cb.Instructions = sizes.instructionBytes / contextCharPerToken
		cb.Tools = sizes.toolSchemaBytes / contextCharPerToken
	}
	// The conversation and files segments measure the TRUE session window —
	// the same reconstruction the summarization middleware's window rules
	// produce: restart at the latest compaction boundary, never the full
	// transcript. The read detaches from the run's cancellation (a cancelled
	// turn still stamps a breakdown on its usage) and a read failure omits
	// both segments rather than failing the turn for a display-only
	// diagnostic.
	readCtx := context.WithoutCancel(ctx)
	if window, err := loadSessionWindow(readCtx, sessionStore, sessionID); err == nil {
		if n := estimateWindowTokens(window); n > 0 {
			cb.Conversation = n
		}
		if n := estimateWindowFileTokens(window); n > 0 {
			cb.Files = n
		}
	}
	// server = the composed share not attributable to the measured segments:
	// the final call's input minus the measured segments, floored at 0.
	// Present only when the provider reported the final call's input.
	if usage.FinalInputTokens > 0 {
		measured := cb.Instructions + cb.Tools + cb.Conversation + cb.Files
		if rest := usage.FinalInputTokens - measured; rest > 0 {
			cb.Server = rest
		}
	}
	if cb == (ContextBreakdown{}) {
		return nil
	}
	return &cb
}
