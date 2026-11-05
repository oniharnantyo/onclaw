package groups

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Summary maintenance (add-agent-groups design D3): when a member's unread
// window exceeds the threshold, the group service runs ONE asynchronous
// summarization over the older transcript range after the member's turn
// completes — via Manager.SummarizeText, which acquires neither the agent's
// lane nor any session's busy slot — and versions the result into
// agent_group_summaries. Below the threshold no summary is built.
const (
	// summaryThreshold is the unread-row count that arms a post-turn
	// summarization of the older range.
	summaryThreshold = 40
	// summaryInstruction leads the summarizer call.
	summaryInstruction = "Summarize this agent-group transcript excerpt for the group's members: keep decisions, task results and their owners, and open questions. Be concise."
)

// maybeSummarizeAsync spawns the post-turn summary build when the dispatch's
// unread window exceeded the threshold (design D3). Asynchronous and
// single-flight per group: a group with a build in flight skips. Never
// blocks the turn-end path.
func (s *Service) maybeSummarizeAsync(key groupKey, rec *inflightTurn) {
	if rec == nil || rec.UnreadCount <= summaryThreshold {
		return
	}
	s.mu.Lock()
	busy := s.summarizing[key.GroupID]
	s.summarizing[key.GroupID] = true
	s.mu.Unlock()
	if busy {
		return
	}

	go func(key groupKey, throughSeq int64) {
		defer func() {
			s.mu.Lock()
			delete(s.summarizing, key.GroupID)
			s.mu.Unlock()
		}()
		if err := s.buildSummary(context.Background(), key.GroupID, key.AgentID, throughSeq); err != nil {
			slog.Warn("groups: build transcript summary", "group", key.GroupID, "error", err)
		}
	}(key, rec.ThroughSeq)
}

// buildSummary summarizes the older transcript range — the rows at or below
// throughSeq-recentWindow, which have aged out of the recent window — and
// saves the next summary version. The caller runs off the turn-end path.
func (s *Service) buildSummary(ctx context.Context, groupID, agentID string, throughSeq int64) error {
	olderThrough := throughSeq - recentWindow
	if olderThrough <= 0 {
		return nil // everything is still inside the recent window
	}
	l := s.groupLock(groupID)
	l.Lock()
	rows, err := s.readTranscriptRange(ctx, groupID, 0, olderThrough)
	if err != nil {
		l.Unlock()
		return err
	}
	l.Unlock()
	if len(rows) == 0 {
		return nil
	}

	text, err := s.deps.Manager.SummarizeText(ctx, agentID, summaryInstruction, renderTranscriptRows(rows))
	if err != nil {
		return fmt.Errorf("build summary: %w", err)
	}
	if text == "" {
		return nil // an empty summary must not replace a good one
	}

	l = s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()
	version := int64(1)
	if cur, err := s.deps.Store.Groups().GetSummary(ctx, groupID); err == nil {
		version = cur.Version + 1
	}
	sum := &store.AgentGroupSummary{GroupID: groupID, Version: version, ThroughSeq: olderThrough, Text: text}
	if err := s.deps.Store.Groups().SaveSummary(ctx, sum); err != nil {
		return fmt.Errorf("build summary: %w", err)
	}
	slog.Debug("groups: transcript summary saved", "group", groupID, "version", version, "through", olderThrough)
	return nil
}
