// Package memory implements the background agent-memory pipeline
// (integrate-agent-zero-memory) and the read-only retrieval seam: the
// per-turn extraction pipeline — the windowed gister and the curation gate,
// both cheap-model side-calls that fail soft, plus the vector channel — runs
// as consumer #1 of the neutral turn-ingest seam (internal/ingest,
// add-skill-curation-from-traces D1), while a Searcher serves scope-filtered
// reads. Ingestion never touches the run that produced the turn: any stage
// failure logs, counts, and leaves the raw session events intact for
// reprocessing.
package memory

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/ingest"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// IngestJob is the turn-end ingest job. The type itself moved to the neutral
// seam (ingest.Job, add-skill-curation-from-traces D1); the alias keeps the
// pipeline's signatures — and the chip's wire contract — stable.
type IngestJob = ingest.Job

// Run origins carried on the job. The values moved to internal/ingest with
// the job type; the aliases keep the pipeline's internal references stable.
// Empty behaves as a direct user chat.
const (
	originUser      = ingest.OriginUser
	originScheduler = ingest.OriginScheduler
	originChannel   = ingest.OriginChannel
	originTelegram  = ingest.OriginTelegram
	originHeartbeat = ingest.OriginHeartbeat
)

// SessionEventKindMemoryIngested is the application-owned session-event kind
// (the ADK extension namespace, the x.prompt_blocked convention) that
// persists the post-turn memory chip. The ADK runner never produces it; the
// runner-side wiring appends it from the payload this package computes.
const SessionEventKindMemoryIngested = adk.SessionEventKind("x.memory_ingested")

// MemoryIngestedCounts is the visibility breakdown of one turn's committed
// memory writes — counts only, never content.
type MemoryIngestedCounts struct {
	Shared int `json:"shared"`
	User   int `json:"user"`
	Agent  int `json:"agent"`
}

// MemoryIngestedPayload is the durable chip payload: the committed note and
// gist ids plus the visibility breakdown. The JSON tags are the web
// contract, identical for the live emission and the hydrated read; content
// is deliberately absent (D11) — the chip counts what was stored, channels
// disclose private extractions as counts, and hydration is free because the
// chip is just another session event.
type MemoryIngestedPayload struct {
	NoteIDs  []string             `json:"note_ids"`
	EventIDs []string             `json:"event_ids"`
	Counts   MemoryIngestedCounts `json:"counts"`
}

// The chip payload rides the session-event Extension any field; registering
// the concrete type is what lets the serializer round-trip it as the struct
// instead of a generic map (the promptBlockedEvent precedent).
func init() {
	schema.Register[MemoryIngestedPayload]()
}

// ChipSink receives the committed chip payload after the gate's ops commit;
// the runner-side wiring appends it to the session's event stream under
// SessionEventKindMemoryIngested. An absent sink is the unwired chip
// capability (the trace-handler precedent): payloads are computed and
// dropped, nothing else changes.
type ChipSink func(ctx context.Context, job IngestJob, payload MemoryIngestedPayload)

// Worker is the memory ingestion pipeline — consumer #1 of the ingest seam
// (add-skill-curation-from-traces D1): one turn-end job drains through the
// raw evidence embed, the windowed gister, and the curation gate, then emits
// the chip. Queue mechanics — bounding, drain goroutines, and per-session
// serialization — live on ingest.Worker; this type owns only the per-job
// pipeline and its counters. Nothing a job does can fail a run (D2, D10):
// every stage fails soft inside Ingest, which always returns nil.
type Worker struct {
	gister     *Gister
	gate       *Gate
	embedder   Embedder
	embeddings store.MemoryEmbeddingStore
	log        *slog.Logger
	chip       ChipSink

	// rawEnabled resolves the workspace's raw-embedding toggle (the
	// settings record's storage-pressure switch, D3 risk register); nil
	// behaves as enabled — absence is ON.
	rawEnabled RawEmbeddingFunc

	processed     atomic.Int64
	succeeded     atomic.Int64
	failed        atomic.Int64
	embedFailures atomic.Int64
}

// WorkerOption configures the memory pipeline; every knob has a safe default.
type WorkerOption func(*Worker)

// WithChipSink wires the chip emission. The composition root applies it only
// when the session-event seam exists; inside the package the sink is simply
// called when set.
func WithChipSink(fn ChipSink) WorkerOption {
	return func(w *Worker) {
		w.chip = fn
	}
}

// Posture is the workspace's memory visibility posture (D4's policy
// switch): Narrow keeps the gate's narrowest-tier defaulting; OrgShared
// raises the gate's DEFAULT proposed visibility to shared — still clamped
// by the session ceiling, so a DM births at most user-visibility facts.
type Posture string

const (
	PostureNarrow    Posture = "narrow"
	PostureOrgShared Posture = "org-shared"
)

// PostureFunc resolves a workspace's posture at job time. Any outcome other
// than PostureOrgShared — including a nil func — behaves as Narrow (the
// fail-safe default: the pipeline never widens on absent or failing
// settings).
type PostureFunc func(ctx context.Context, workspaceID string) Posture

// WithPostureFunc wires the workspace posture source the gate's
// visibility-defaulting consumes. Unset, the gate defaults Narrow.
func WithPostureFunc(fn PostureFunc) WorkerOption {
	return func(w *Worker) {
		if fn != nil && w.gate != nil {
			w.gate.posture = fn
		}
	}
}

// RawEmbeddingFunc resolves the workspace's raw-embedding toggle at job
// time — the per-workspace switch for turning off raw-turn vectors under
// storage pressure (wave3 risk register). Anything other than true skips
// the raw stage; a nil func behaves as enabled (absence = ON).
type RawEmbeddingFunc func(ctx context.Context, workspaceID string) bool

// WithRawEmbeddingEnabled wires the raw-evidence toggle source. Unset, raw
// turns embed whenever an embedding model is configured (absence = ON).
func WithRawEmbeddingEnabled(fn RawEmbeddingFunc) WorkerOption {
	return func(w *Worker) {
		if fn != nil {
			w.rawEnabled = fn
		}
	}
}

// NewWorker constructs the memory pipeline consumer from its collaborators:
// the windowed gister, the curation gate, the embeddings lane (the vector
// channel's port), and the vector-index store the embedding stages persist
// through — each an explicitly injected, granular dependency the composition
// root resolves non-nil. The consumer registers into an ingest.Worker via
// WithConsumers; the queue it drains is the seam's, not this type's.
func NewWorker(gister *Gister, gate *Gate, embedder Embedder, embeddings store.MemoryEmbeddingStore, log *slog.Logger, opts ...WorkerOption) *Worker {
	w := &Worker{
		gister:     gister,
		gate:       gate,
		embedder:   embedder,
		embeddings: embeddings,
		log:        log,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// The memory pipeline satisfies the ingest seam's consumer contract.
var _ ingest.Consumer = (*Worker)(nil)

// Ingest runs one job: the raw evidence embed, the windowed gister, then
// the curation gate, then the row embed, then the chip. The stages fail
// soft independently — a gist failure never blocks the gate, and an
// embedding failure never blocks anything (D3/D4: the vector channel is the
// additive lane) — and nothing escapes to the caller (D10): the method
// always returns nil, with every stage failure counted. A job with an empty
// unprocessed window is a quiet success: nothing to ingest. Per-session
// serialization is the ingest.Worker's job (jobs on one session never
// dispatch concurrently), so the incremental cursor never races.
func (w *Worker) Ingest(ctx context.Context, job IngestJob) error {
	failed := false
	chip := MemoryIngestedPayload{NoteIDs: []string{}, EventIDs: []string{}}

	win, err := w.gister.Window(ctx, job)
	if err != nil {
		w.log.Warn("memory: gist window failed", "session_id", job.SessionID, "error", err)
		failed = true
	}

	if err == nil && len(win.events) > 0 {
		// Raw evidence first (D3 index-before-extract): the turn is
		// retrievable through the vector channel even when every extraction
		// stage below fails or skips.
		w.embedRawTurn(ctx, job, win)

		var gist *domain.MemoryEvent
		if committed, err := w.gister.Gist(ctx, job, win); err != nil {
			w.log.Warn("memory: gist failed", "session_id", job.SessionID, "error", err)
			failed = true
		} else {
			gist = &committed
			chip.EventIDs = append(chip.EventIDs, gist.ID)
			bumpCount(&chip.Counts, gist.Visibility)
		}

		result, err := w.gate.Curate(ctx, job, turnEvents(win.events, job.TurnID), win.start, win.endID)
		if err != nil {
			w.log.Warn("memory: curation gate failed", "session_id", job.SessionID, "error", err)
			failed = true
		} else {
			chip.NoteIDs = append(chip.NoteIDs, result.NoteIDs...)
			addCounts(&chip.Counts, result.Counts)
		}

		// Row embeddings last (D4): the committed rows leave this job
		// vector-retrievable, batched into one embeddings call.
		w.embedCommittedRows(ctx, job, gist, result)
	}

	w.processed.Add(1)
	if failed {
		w.failed.Add(1)
	} else {
		w.succeeded.Add(1)
	}

	// The chip rides only committed memory: nothing stored, nothing emitted.
	if w.chip != nil && (len(chip.NoteIDs) > 0 || len(chip.EventIDs) > 0) {
		w.chip(ctx, job, chip)
	}
	return nil
}

// IngestStats is the memory pipeline's monotonic counter snapshot; the
// morning report's extraction-failure count reads Failed from here and its
// embedding-failure count reads EmbedFailures (wave3 D3's fail-soft
// visibility — silent degradation surfaces in the report). The queue-level
// counters (Enqueued, dispatched, QueueDropped) live on ingest.Worker's
// Stats.
type IngestStats struct {
	Processed     int64
	Succeeded     int64
	Failed        int64
	EmbedFailures int64
}

// Stats returns the current counters.
func (w *Worker) Stats() IngestStats {
	return IngestStats{
		Processed:     w.processed.Load(),
		Succeeded:     w.succeeded.Load(),
		Failed:        w.failed.Load(),
		EmbedFailures: w.embedFailures.Load(),
	}
}

// turnEvents narrows the window to the triggering turn's rows; a window with
// no row carrying the turn id (a failed run's user turn, a stale pointer)
// yields the whole window — the material is still unprocessed fact-bearing
// content and must not be silently skipped.
func turnEvents(events []domain.SessionEvent, turnID string) []domain.SessionEvent {
	var turn []domain.SessionEvent
	for _, ev := range events {
		if ev.TurnID == turnID {
			turn = append(turn, ev)
		}
	}
	if len(turn) > 0 {
		return turn
	}
	return events
}

// bumpCount adds one stored row's tier to the chip breakdown.
func bumpCount(counts *MemoryIngestedCounts, visibility domain.MemoryVisibility) {
	switch visibility {
	case domain.MemoryVisibilityShared:
		counts.Shared++
	case domain.MemoryVisibilityUser:
		counts.User++
	case domain.MemoryVisibilityAgent:
		counts.Agent++
	}
}

// addCounts merges a gate result's breakdown into the chip's.
func addCounts(counts *MemoryIngestedCounts, other MemoryIngestedCounts) {
	counts.Shared += other.Shared
	counts.User += other.User
	counts.Agent += other.Agent
}

// sessionCeiling derives the visibility ceiling from the session shape (D4):
// scheduled and heartbeat runs birth at most agent-visibility facts, direct
// chats (user/telegram) at most user-visibility, and channels the tier their
// human count supports. The gate clamps proposed tiers to this ceiling; the
// store re-validates it as the last line of defense.
func sessionCeiling(job IngestJob) domain.MemoryVisibility {
	switch job.Origin {
	case originScheduler, originHeartbeat:
		return domain.MemoryVisibilityAgent
	case originChannel:
		switch {
		case job.HumanParticipants >= 2:
			return domain.MemoryVisibilityShared
		case job.HumanParticipants == 1:
			return domain.MemoryVisibilityUser
		default:
			return domain.MemoryVisibilityAgent
		}
	default:
		return domain.MemoryVisibilityUser
	}
}

// participantVisibility stamps a gist by the participant rule (D4): no human
// participant → agent-visibility, exactly one → user-visibility owned by
// that user, two or more → shared. The count comes from the job's session
// shape, never from the transcript.
func participantVisibility(job IngestJob) (domain.MemoryVisibility, *string) {
	switch {
	case job.HumanParticipants >= 2:
		return domain.MemoryVisibilityShared, nil
	case job.HumanParticipants == 1:
		userID := job.UserID
		return domain.MemoryVisibilityUser, &userID
	default:
		return domain.MemoryVisibilityAgent, nil
	}
}
