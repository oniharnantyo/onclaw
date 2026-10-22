package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// compactionThreshold is the fraction of the model's context window at which
// a session's working context is compacted before the next turn.
const compactionThreshold = 0.8

// charsPerToken is the display-grade estimator (~4 chars/token) the donor
// uses for compaction before/after estimates. Never billing or trigger math.
const charsPerToken = 4

// summarizeSystemPrompt instructs the summarizer call of a compaction.
const summarizeSystemPrompt = "You summarize conversation history so an assistant can continue the work with less context. " +
	"Produce a dense factual summary: the user's goals, decisions made, names, numbers, and open threads. " +
	"Output only the summary text."

// compactionNeeded reports whether the session's used tokens crossed the
// compaction threshold of the model's limit.
func compactionNeeded(used, limit int) bool {
	return limit > 0 && float64(used) >= compactionThreshold*float64(limit)
}

// compactableUpTo returns the log boundary a compaction can summarize up to:
// the highest event sequence currently in the log. The caller must have
// loaded events before appending the current turn's user message, so the
// boundary covers exactly the history older than this turn.
func compactableUpTo(events []sessions.Event) int64 {
	var max int64
	for _, ev := range events {
		if ev.Seq > max {
			max = ev.Seq
		}
	}
	return max
}

// hasCompactableView reports whether any message-bearing event sits beyond
// the checkpoint boundary — a compaction with nothing to summarize is a
// quiet no-op.
func hasCompactableView(events []sessions.Event, upToSeq int64) bool {
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

// runCompaction summarizes the session's view up to upToSeq into a new
// checkpoint via the model, persists the checkpoint, and emits the
// compaction marker event. It returns the marker. The stored transcript is
// never truncated: compaction affects the model's working context only.
func (m *manager) runCompaction(
	ctx context.Context,
	sessionID, turnID string,
	mdl model.BaseChatModel,
	events []sessions.Event,
	upToSeq int64,
) (sessions.Event, error) {
	transcript := renderTranscript(events, upToSeq)

	summarized, err := mdl.Generate(ctx, []*schema.Message{
		schema.SystemMessage(summarizeSystemPrompt),
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

	before := usedTokens(events)
	after := len(summary) / charsPerToken

	checkpoint := sessions.Checkpoint{
		Summary:      summary,
		UpToSeq:      upToSeq,
		TokensBefore: before,
		TokensAfter:  after,
	}
	if err := m.sessions.SaveCheckpoint(ctx, sessionID, checkpoint); err != nil {
		return sessions.Event{}, fmt.Errorf("compact: save checkpoint: %w", err)
	}

	// The summarizer call spent real tokens; the caller folds its usage into
	// the turn's accounting via the returned usage.
	markUsage := &sessions.Usage{}
	accumulateTokenUsage(markUsage, usage)

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

// renderTranscript flattens the session's message events up to upToSeq into
// the text the summarizer model reads.
func renderTranscript(events []sessions.Event, upToSeq int64) string {
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
