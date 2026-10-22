package agents

import (
	"context"

	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/buddies/internal/agents/compaction"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

const (
	compactionThreshold   = compaction.CompactionThreshold
	charsPerToken         = compaction.CharsPerToken
	summarizeSystemPrompt = compaction.SummarizeSystemPrompt
)

func compactionNeeded(used, limit int) bool {
	return compaction.CompactionNeeded(used, limit)
}

func compactableUpTo(events []sessions.Event) int64 {
	return compaction.CompactableUpTo(events)
}

func hasCompactableView(events []sessions.Event, upToSeq int64) bool {
	return compaction.HasCompactableView(events, upToSeq)
}

func (m *manager) runCompaction(
	ctx context.Context,
	sessionID, turnID string,
	mdl model.BaseChatModel,
	events []sessions.Event,
	upToSeq int64,
) (sessions.Event, error) {
	return compaction.RunCompaction(ctx, sessionID, turnID, mdl, events, upToSeq, m.sessions)
}

func renderTranscript(events []sessions.Event, upToSeq int64) string {
	return compaction.RenderTranscript(events, upToSeq)
}
