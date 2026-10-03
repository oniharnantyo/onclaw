package skillcuration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// The run window's serializer mirrors the runner's ADK session-event
// serializer: raw session rows parse back into typed events with the same
// HumanReadableSerializer the adapter persists with (the memory
// materialSerializer precedent).
var runSerializer = &schema.HumanReadableSerializer{}

// runTally is the structured projection of one run's transcript window that
// the five hard gates and the shape signature read. Everything here comes
// off existing persisted payload fields — no model call, ever (D2).
//
// Field provenance (mirroring the History hydration joins in
// internal/agents/history.go, which the live TranscriptEvent projection is
// derived from):
//   - tool calls: adk.SessionEventSpanToolCallStart / End spans carry
//     Span.Tool (ToolUseID + Name);
//   - the error flag: the end span's Status=="error" or non-empty Err — the
//     same predicate history's toolResult join applies (the live stream's
//     ToolResultPayload.IsError is filled from exactly these fields);
//   - latency: the span pair's timestamps, end.EndedAt (fallback: the
//     event's occurrence) minus start (the end span's own StartedAt
//     snapshot, falling back to the start span located via
//     ToolCallStartEventID or ToolUseID);
//   - the final assistant message: persisted message events with a
//     renderable assistant text (the extractAgenticText block set).
type runTally struct {
	// ToolCalls counts tool-call start spans in the window.
	ToolCalls int
	// OrderedTools lists the tool names in log order, one entry per call —
	// the shape signature's input.
	OrderedTools []string
	// DistinctTools is the unique tool-name count.
	DistinctTools int
	// Recoveries counts error→success sequences on the same tool name
	// (spec: "a tool result flagged as error followed by a later non-error
	// result for the same tool or task step" — a retried call with changed
	// arguments is the same signal, so the match is on the tool name within
	// the run window, never on call ids).
	Recoveries int
	// ErrorResults counts the window's error-flagged tool results.
	ErrorResults int
	// TotalToolLatency sums the tool spans' derived latencies.
	TotalToolLatency time.Duration
	// FinalAssistant reports that the run ended with a final assistant
	// message: an assistant message with renderable text that issues no
	// tool calls, with no interrupt/cancel after it.
	FinalAssistant bool
	// WindowEndEventID cites the window's last event — the membership row's
	// evidence pointer.
	WindowEndEventID string
	// HasEvents reports whether the window carried any rows at all.
	HasEvents bool
}

// tallyRun scans one run's session window in log order and returns the
// structured tally. Unparseable rows contribute nothing — the raw log is
// read-only evidence either way (the renderMaterial precedent).
func tallyRun(events []domain.SessionEvent) runTally {
	tally := runTally{OrderedTools: []string{}}

	// Recovery matcher state: per tool name, whether the latest result so
	// far was error-flagged.
	lastWasError := map[string]bool{}
	// Latency join state: start timestamps by ToolUseID and by start-event
	// id (the historyJoins.toolStartAt precedent).
	toolStartAt := map[string]time.Time{}

	// Final-assistant tracking: the last assistant message's shape, and
	// whether an interrupt/cancel has occurred since it (a turn that paused
	// on an approval and never resumed did not end with an assistant reply).
	assistant := struct {
		seen      bool
		withCalls bool
		trailing  bool
	}{}

	for _, row := range events {
		tally.HasEvents = true
		tally.WindowEndEventID = row.EventID

		var se adk.SessionEvent[*schema.AgenticMessage]
		if err := runSerializer.Unmarshal(row.Payload, &se); err != nil {
			continue
		}
		_ = adk.NormalizeSessionEventKind(&se)
		kind := se.Kind
		if kind == "" {
			kind = adk.SessionEventKind(row.Kind)
		}
		occurredAt := se.Timestamp
		if occurredAt.IsZero() {
			occurredAt = row.OccurredAt
		}

		switch kind {
		case adk.SessionEventSpanToolCallStart:
			if se.Span == nil || se.Span.Tool == nil {
				continue
			}
			startAt := se.Span.StartedAt
			if startAt.IsZero() {
				startAt = occurredAt
			}
			toolStartAt[se.Span.Tool.ToolUseID] = startAt
			if row.EventID != "" {
				toolStartAt[row.EventID] = startAt
			}
			tally.ToolCalls++
			tally.OrderedTools = append(tally.OrderedTools, se.Span.Tool.Name)

		case adk.SessionEventSpanToolCallEnd:
			if se.Span == nil || se.Span.Tool == nil {
				continue
			}
			isError := se.Span.Status == "error" || se.Span.Err != ""
			latency := spanLatency(se.Span, occurredAt, toolStartAt)
			tally.TotalToolLatency += latency
			name := se.Span.Tool.Name
			if isError {
				tally.ErrorResults++
				lastWasError[name] = true
				continue
			}
			if lastWasError[name] {
				tally.Recoveries++
				lastWasError[name] = false
			}

		case adk.SessionEventMessage:
			if se.Message == nil {
				continue
			}
			role := strings.ToLower(strings.TrimSpace(string(se.Message.Role)))
			if role != "assistant" {
				continue
			}
			if strings.TrimSpace(agenticRenderableText(se.Message)) == "" {
				continue
			}
			assistant.seen = true
			assistant.withCalls = messageIssuesToolCalls(se.Message)
			// A later assistant reply supersedes any interrupt before it.
			assistant.trailing = false

		case adk.SessionEventInterrupt, adk.SessionEventCancel:
			assistant.trailing = true
		}
	}

	tally.DistinctTools = distinctNames(tally.OrderedTools)
	tally.FinalAssistant = assistant.seen && !assistant.withCalls && !assistant.trailing
	return tally
}

// spanLatency derives one finished tool call's latency from the persisted
// span pair's timestamps — verbatim history.toolLatency semantics: the end
// span's EndedAt (fallback: the event's occurrence), minus its own
// StartedAt snapshot, falling back to the start span located via
// ToolCallStartEventID or ToolUseID. A span with no derivable start (or a
// negative pair) contributes zero, never a negative latency.
func spanLatency(span *adk.SpanEvent, endedAt time.Time, toolStartAt map[string]time.Time) time.Duration {
	if span == nil {
		return 0
	}
	end := span.EndedAt
	if end.IsZero() {
		end = endedAt
	}
	if end.IsZero() {
		return 0
	}
	start := span.StartedAt
	if start.IsZero() {
		key := ""
		if span.Tool != nil {
			if span.Tool.ToolCallStartEventID != "" {
				key = span.Tool.ToolCallStartEventID
			} else {
				key = span.Tool.ToolUseID
			}
		}
		start = toolStartAt[key]
	}
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

// agenticRenderableText extracts the renderable text from one agentic
// message: generated and user-input blocks only (the memory
// agenticText precedent).
func agenticRenderableText(msg *schema.AgenticMessage) string {
	var sb []byte
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.AssistantGenText != nil {
			sb = append(sb, block.AssistantGenText.Text...)
		}
		if block.UserInputText != nil {
			sb = append(sb, block.UserInputText.Text...)
		}
	}
	return string(sb)
}

// messageIssuesToolCalls reports whether the assistant message requests any
// function tool call — a text-bearing message that also issues calls is a
// mid-procedure step, never the run's final assistant message.
func messageIssuesToolCalls(msg *schema.AgenticMessage) bool {
	for _, block := range msg.ContentBlocks {
		if block != nil && block.FunctionToolCall != nil {
			return true
		}
	}
	return false
}

// distinctNames counts unique non-empty names, preserving no order.
func distinctNames(names []string) int {
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		seen[n] = struct{}{}
	}
	return len(seen)
}

// ClusterKey computes the v1 similarity-cluster id for a run: a SHA-256
// digest over the workspace, the agent, and the run's normalized ordered
// tool-name signature (each name lowercased and trimmed). Deterministic
// shape hashing, not embeddings — the design allows reusing the memory
// clustering signals but does not mandate them, and the embeddings lane is
// a memory-notes contract. Same agent + same procedure (same tools in the
// same order) converges on one cluster; a different agent, workspace, or
// call order is a different task family. Call counts ride the sequence
// implicitly (a longer sequence hashes differently).
func ClusterKey(workspaceID, agentID string, orderedTools []string) string {
	h := sha256.New()
	h.Write([]byte(workspaceID))
	h.Write([]byte{0})
	h.Write([]byte(agentID))
	h.Write([]byte{0})
	for _, name := range orderedTools {
		h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
		h.Write([]byte{0x1f}) // unit separator between sequence members
	}
	return "cl-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// ClusterGateOpen reports whether a cluster reached the proposal minimum
// ("Cluster gate before drafting"): the proposer drafts only when the
// cluster holds at least cfg.ClusterMinimum qualifying runs. A first-ever
// qualifying run opens the cluster without proposing (count 1 < 2).
func ClusterGateOpen(qualifyingCount, minimum int) bool {
	if minimum < 1 {
		minimum = 1
	}
	return qualifyingCount >= minimum
}

// ClusterQualifyingCount reads the cluster's qualifying-run count from the
// store — the cluster gate's number. Wraps the membership store read so the
// proposer (task 5) and the cycle (task 7) share one call site.
func ClusterQualifyingCount(ctx context.Context, clusters store.SkillCandidateStore, workspaceID, clusterID string) (int, error) {
	count, err := clusters.CountQualifyingByCluster(ctx, workspaceID, clusterID)
	if err != nil {
		return 0, fmt.Errorf("skillcuration: count qualifying runs: %w", err)
	}
	return count, nil
}

// ClusterContrastRuns returns the cluster's non-qualifying membership rows —
// failed runs and gate-failing runs alike (spec: "Failed runs join clusters
// without triggering"): the contrast evidence a proposal cites when it
// encodes the procedure's failure modes. The result is never nil.
func ClusterContrastRuns(ctx context.Context, clusters store.SkillCandidateStore, workspaceID, clusterID string) ([]domain.SkillClusterRun, error) {
	runs, err := clusters.ListClusterRunsByCluster(ctx, workspaceID, clusterID)
	if err != nil {
		return nil, fmt.Errorf("skillcuration: list cluster runs: %w", err)
	}
	contrast := make([]domain.SkillClusterRun, 0, len(runs))
	for _, run := range runs {
		if !run.Qualifying {
			contrast = append(contrast, run)
		}
	}
	return contrast, nil
}
