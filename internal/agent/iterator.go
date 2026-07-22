package agent

import "github.com/cloudwego/eino/schema"

// Event is one item produced by the agent run iterator. Exactly one variant is
// populated: a streamed/buffered message, or a summarization compaction signal
// parsed from the summarization middleware's CustomizedAction.
type Event struct {
	// Message is non-nil for a regular agent message (user/assistant/tool).
	Message *schema.AgenticMessage

	// Compaction is non-nil when the summarization middleware emitted a
	// compaction progress step (trigger met, generation attempt, or completion).
	Compaction *CompactionSignal
}

// CompactionSignal describes one step of a context-compaction progress sequence,
// parsed from the summarization middleware's internal events.
type CompactionSignal struct {
	// Status reports the compaction lifecycle phase. Empty for a progress-only
	// step (a summary generation attempt).
	Status CompactionStatus

	// Progress is the coarse percent complete (0-100).
	Progress int
}

// CompactionStatus reports the lifecycle phase of a context-compaction event.
type CompactionStatus string

const (
	CompactionStarted   CompactionStatus = "started"
	CompactionCompleted CompactionStatus = "completed"
)

// EventIterator defines the iterator returned by Run.
type EventIterator interface {
	Next() (Event, bool)
	Err() error
	CollectedTurn() []*schema.AgenticMessage
}
