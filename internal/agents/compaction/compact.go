package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// CompactionThreshold is the fraction of the model's context window at which
// a session's working context is compacted before the next turn.
const CompactionThreshold = 0.8

// CharsPerToken is the display-grade estimator (~4 chars/token) the donor
// uses for compaction before/after estimates. Never billing or trigger math.
const CharsPerToken = 4

// SummarizeSystemPrompt instructs the summarizer call of a compaction.
const SummarizeSystemPrompt = "You summarize conversation history so an assistant can continue the work with less context. " +
	"Produce a dense factual summary: the user's goals, decisions made, names, numbers, and open threads. " +
	"Output only the summary text."

// CompactionNeeded reports whether the session's used tokens crossed the
// compaction threshold of the model's limit.
func CompactionNeeded(used, limit int) bool {
	return limit > 0 && float64(used) >= CompactionThreshold*float64(limit)
}

// CompactableUpTo returns the log boundary a compaction can summarize up to:
// the highest event sequence currently in the log. The caller must have
// loaded events before appending the current turn's user message, so the
// boundary covers exactly the history older than this turn.
func CompactableUpTo(events []sessions.Event) int64 {
	var max int64
	for _, ev := range events {
		if ev.Seq > max {
			max = ev.Seq
		}
	}
	return max
}

// HasCompactableView reports whether any message-bearing event sits beyond
// the checkpoint boundary — a compaction with nothing to summarize is a
// quiet no-op.
func HasCompactableView(events []sessions.Event, upToSeq int64) bool {
	for _, ev := range events {
		if ev.Seq > upToSeq {
			continue
		}
		switch ev.Kind {
		case sessions.KindUserMessage, sessions.KindAssistantMessage:
			return true
		}
	}
	return false
}

// UsedTokens derives a session's used context tokens from its persisted
// events: the last completed turn's final input plus its output.
func UsedTokens(events []sessions.Event) int {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Kind == sessions.KindTurnCompleted && ev.Usage != nil {
			return ev.Usage.FinalInputTokens + ev.Usage.OutputTokens
		}
	}
	return 0
}

// RenderTranscript builds a plain text transcript from events up to upToSeq.
func RenderTranscript(events []sessions.Event, upToSeq int64) string {
	var sb strings.Builder
	for _, ev := range events {
		if ev.Seq > upToSeq {
			continue
		}
		switch ev.Kind {
		case sessions.KindUserMessage:
			sb.WriteString("User: " + ev.Text + "\n")
		case sessions.KindAssistantMessage:
			sb.WriteString("Assistant: " + ev.Text + "\n")
		}
	}
	return sb.String()
}

// RunCompaction summarizes the session's view up to upToSeq into a new
// checkpoint via the model, persists the checkpoint, and returns the
// compaction marker event. The stored transcript is never truncated:
// compaction affects the model's working context only.
func RunCompaction(
	ctx context.Context,
	sessionID, turnID string,
	mdl model.BaseChatModel,
	events []sessions.Event,
	upToSeq int64,
	sessionSvc sessions.Service,
) (sessions.Event, error) {
	transcript := RenderTranscript(events, upToSeq)

	summarized, err := mdl.Generate(ctx, []*schema.Message{
		schema.SystemMessage(SummarizeSystemPrompt),
		schema.UserMessage(transcript),
	})
	if err != nil {
		return sessions.Event{}, fmt.Errorf("compact: summarize: %w", err)
	}
	summary := ""
	var usage *schema.TokenUsage
	if summarized != nil {
		summary = summarized.Content
		if summarized.ResponseMeta != nil {
			usage = summarized.ResponseMeta.Usage
		}
	}

	before := UsedTokens(events)
	after := len(summary) / CharsPerToken

	checkpoint := sessions.Checkpoint{
		Summary:      summary,
		UpToSeq:      upToSeq,
		TokensBefore: before,
		TokensAfter:  after,
	}
	if err := sessionSvc.SaveCheckpoint(ctx, sessionID, checkpoint); err != nil {
		return sessions.Event{}, fmt.Errorf("compact: save checkpoint: %w", err)
	}

	// The summarizer call spent real tokens; the caller folds its usage into
	// the turn's accounting via the returned usage.
	markUsage := &sessions.Usage{}
	if usage != nil {
		markUsage.InputTokens = usage.PromptTokens
		markUsage.OutputTokens = usage.CompletionTokens
		markUsage.TotalTokens = usage.TotalTokens
		markUsage.FinalInputTokens = usage.PromptTokens
	}

	marker := sessions.Event{
		TurnID: turnID,
		Kind:   sessions.KindCompaction,
		Compaction: &sessions.Compaction{
			CheckpointID: sessions.CheckpointID(sessionID),
			Summary:      summary,
			TokensBefore: before,
			TokensAfter:  after,
		},
		Usage: markUsage,
	}
	return marker, nil
}
