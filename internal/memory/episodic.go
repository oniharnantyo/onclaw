package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// SummarizeSession produces an episodic summary for a completed session.
// When compactionSummary is non-empty, it is reused directly (no LLM call).
// When compactionSummary is empty, a single LLM call is made against the
// provided messages to produce the summary.
// In both cases, l0Abstract and keyTopics are computed extractively.
func SummarizeSession(
	ctx context.Context,
	chatModel model.AgenticModel,
	compactionSummary string,
	messages []*schema.AgenticMessage,
) (summary, l0Abstract, keyTopics string, err error) {
	if compactionSummary != "" {
		summary = compactionSummary
	} else {
		if chatModel == nil || len(messages) == 0 {
			return "", "", "", fmt.Errorf("no compaction summary and no chatModel to summarize")
		}
		summary, err = llmSummarizeSession(ctx, chatModel, messages)
		if err != nil {
			return "", "", "", fmt.Errorf("llm summarize session: %w", err)
		}
	}

	l0Abstract = extractL0Abstract(summary)
	keyTopics = extractKeyTopics(summary)
	return summary, l0Abstract, keyTopics, nil
}

// llmSummarizeSession makes a single LLM call to summarize a session's messages.
func llmSummarizeSession(ctx context.Context, chatModel model.AgenticModel, messages []*schema.AgenticMessage) (string, error) {
	convoText := formatMessages(messages)
	if strings.TrimSpace(convoText) == "" {
		return "", fmt.Errorf("no message content to summarize")
	}

	prompt := fmt.Sprintf(`Summarize the following conversation session concisely.
Focus on: what the user asked, what the agent did, key decisions made, files modified, and any important outcomes.
Keep the summary to 3-5 sentences.

Conversation:
%s`, convoText)

	resp, err := chatModel.Generate(ctx, []*schema.AgenticMessage{
		schema.UserAgenticMessage(prompt),
	})
	if err != nil {
		return "", fmt.Errorf("generate summary: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("empty response from model")
	}
	return getAgenticMessageText(resp), nil
}

// extractL0Abstract returns the first meaningful sentence from text,
// limited to ~120 characters. No LLM call — purely extractive.
func extractL0Abstract(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Split into sentences by common delimiters.
	sentenceEnd := -1
	for _, delim := range []string{". ", "! ", "? ", ".\n", "!\n", "?\n"} {
		idx := strings.Index(text, delim)
		if idx >= 0 && (sentenceEnd < 0 || idx < sentenceEnd) {
			sentenceEnd = idx
		}
	}

	var firstSentence string
	if sentenceEnd >= 0 {
		firstSentence = text[:sentenceEnd]
	} else {
		firstSentence = text
	}

	// Fallback to first line if no sentence boundary found
	if sentenceEnd < 0 {
		if nl := strings.IndexByte(text, '\n'); nl >= 0 {
			firstSentence = text[:nl]
		}
	}

	firstSentence = strings.TrimSpace(firstSentence)
	if len(firstSentence) > 160 {
		firstSentence = firstSentence[:157] + "..."
	}
	return firstSentence
}

// extractKeyTopics extracts key topics from a summary using simple heuristics.
// Finds capitalized multi-word phrases and significant terms. No LLM call.
func extractKeyTopics(text string) string {
	var topics []string
	seen := make(map[string]bool)

	// Extract capitalized phrases (likely proper nouns / project terms)
	words := strings.Fields(text)
	for i, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		if w == "" {
			continue
		}
		// Check if word starts with uppercase
		if len(w) > 1 && w[0] >= 'A' && w[0] <= 'Z' {
			// Build multi-word phrase
			phrase := w
			for j := i + 1; j < len(words); j++ {
				nw := strings.Trim(words[j], ".,;:!?()[]{}\"'")
				if nw == "" || (nw[0] >= 'A' && nw[0] <= 'Z') || looksLikeStopWord(nw) {
					break
				}
				phrase += " " + nw
			}
			lower := strings.ToLower(phrase)
			if !seen[lower] && len(phrase) > 2 {
				seen[lower] = true
				topics = append(topics, phrase)
			}
		}
	}

	if len(topics) > 8 {
		topics = topics[:8]
	}
	return strings.Join(topics, ", ")
}

func looksLikeStopWord(w string) bool {
	short := strings.ToLower(strings.Trim(w, ".,;:!?()[]{}\"'"))
	switch short {
	case "the", "a", "an", "is", "are", "was", "were", "in", "on", "at",
		"to", "for", "of", "with", "and", "or", "but", "not", "by", "from":
		return true
	}
	return false
}

// ComputeEpisodicTTL returns the expiry time string based on a TTL in days.
func ComputeEpisodicTTL(ttlDays int) string {
	if ttlDays <= 0 {
		ttlDays = 90
	}
	return time.Now().AddDate(0, 0, ttlDays).Format(time.RFC3339)
}

// FormatDreamDigest builds a digest string from a batch of unpromoted episodes.
// Recent episodes (up to 3) are included verbatim; older ones use their l0_abstract.
func FormatDreamDigest(episodes []*EpisodicSummary) string {
	if len(episodes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Dreaming Digest (%d recent episodes)\n\n", len(episodes)))

	for i, ep := range episodes {
		if ep == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("### Episode %d\n", i+1))
		if ep.L0Abstract != "" {
			sb.WriteString(fmt.Sprintf("Abstract: %s\n", ep.L0Abstract))
		}
		if ep.KeyTopics != "" {
			sb.WriteString(fmt.Sprintf("Topics: %s\n", ep.KeyTopics))
		}
		// Recent episodes (first 3) include the full summary
		if i < 3 && ep.Summary != "" {
			sb.WriteString(fmt.Sprintf("Summary: %s\n", ep.Summary))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
