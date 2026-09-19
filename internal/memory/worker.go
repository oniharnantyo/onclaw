// Package memory implements the background agent-memory ingestion pipeline
// and the read-only retrieval seam (integrate-agent-zero-memory): a bounded
// worker drains turn-end ingest jobs through the per-run windowed gister and
// the curation gate — both cheap-model side-calls that fail soft — while a
// Searcher serves scope-filtered reads. Ingestion never touches the run that
// produced the turn: any stage failure logs, counts, and leaves the raw
// session events intact for reprocessing.
package memory

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Run origins carried on IngestJob. The values mirror the ExecRequest Origin
// constants in internal/agents — duplicated as literals because the runner
// imports this package for the ingestion seam and the reverse import would
// cycle. Empty behaves as a direct user chat.
const (
	originUser      = "user"
	originScheduler = "scheduler"
	originChannel   = "channel"
	originTelegram  = "telegram"
	originHeartbeat = "heartbeat"
)

// IngestJob captures everything the pipeline needs at enqueue time (D2): the
// turn's identity coordinates, the triggering origin, the session shape
// driving the visibility ceiling, and the run-finish status. The raw
// material is never copied into the job — the worker loads the session's
// events itself through the store, so a pipeline failure leaves the raw log
// untouched and reprocessing possible.
type IngestJob struct {
	WorkspaceID string
	AgentID     string
	UserID      string
	SessionID   string
	TurnID      string

	// Origin is the finished run's origin: "user", "scheduler", "channel",
	// "telegram", or "heartbeat" (the ExecRequest Origin values).
	Origin string

	// HumanParticipants is the session-shape human count feeding both the
	// gister's participant rule and the gate's visibility ceiling. It is
	// counted at enqueue time, never re-derived from the transcript.
	HumanParticipants int

	// Status is the run-finish status literal ("completed"/"failed"). Both
	// statuses ingest (D2) — a failed run still contains a real user turn;
	// the field is attribution, never a filter.
	Status string
}

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

const (
	defaultQueueSize   = 256
	defaultConcurrency = 2
	// stopGrace bounds Stop's wait for in-flight jobs; a side-call past the
	// deadline logs and the process proceeds (fail-soft to the shutdown).
	stopGrace = 30 * time.Second
)

// Worker drains turn-end ingest jobs off a bounded queue through the gister
// and the curation gate, then emits the chip. Enqueueing never blocks the
// caller and nothing a job does can fail a run (D2, D10). Jobs on the same
// session are serialized — the gister's cursor is read-then-write — while
// different sessions process concurrently.
type Worker struct {
	gister    *Gister
	gate      *Gate
	log       *slog.Logger
	chip      ChipSink
	queue     chan IngestJob
	queueSize int
	workers   int
	wg        sync.WaitGroup
	stopCh    chan struct{}
	stopOnce  sync.Once

	locksMu     sync.Mutex
	sessionLock map[string]*sync.Mutex

	enqueued  atomic.Int64
	processed atomic.Int64
	succeeded atomic.Int64
	failed    atomic.Int64
	dropped   atomic.Int64
}

// WorkerOption configures the worker; every knob has a safe default.
type WorkerOption func(*Worker)

// WithQueueSize caps the ingest queue (default 256). The queue exists to
// absorb bursts, not to gate the turn path — overflow drops.
func WithQueueSize(n int) WorkerOption {
	return func(w *Worker) {
		if n > 0 {
			w.queueSize = n
		}
	}
}

// WithConcurrency sets how many background goroutines drain the queue
// (default 2).
func WithConcurrency(n int) WorkerOption {
	return func(w *Worker) {
		if n > 0 {
			w.workers = n
		}
	}
}

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

// NewWorker constructs the worker from its collaborators: the windowed
// gister and the curation gate, each already bound to their own granular
// stores. The composition root resolves every dependency non-nil.
func NewWorker(gister *Gister, gate *Gate, log *slog.Logger, opts ...WorkerOption) *Worker {
	w := &Worker{
		gister:      gister,
		gate:        gate,
		log:         log,
		queueSize:   defaultQueueSize,
		workers:     defaultConcurrency,
		stopCh:      make(chan struct{}),
		sessionLock: make(map[string]*sync.Mutex),
	}
	for _, opt := range opts {
		opt(w)
	}
	// Sized after the options so WithQueueSize takes effect.
	w.queue = make(chan IngestJob, w.queueSize)
	return w
}

// sessionMutex returns the per-session serialization lock: two jobs on one
// session must not window concurrently or the same material is processed
// twice against the incremental cursor (D3). Different sessions never
// contend.
func (w *Worker) sessionMutex(workspaceID, sessionID string) *sync.Mutex {
	w.locksMu.Lock()
	defer w.locksMu.Unlock()
	key := workspaceID + "/" + sessionID
	mu, ok := w.sessionLock[key]
	if !ok {
		mu = &sync.Mutex{}
		w.sessionLock[key] = mu
	}
	return mu
}

// Start launches the drain goroutines under the process-lifetime context:
// the loops exit when ctx is cancelled or Stop is called; in-flight jobs are
// not interrupted — Stop waits for them (the scheduler/heartbeat lifecycle).
func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-w.stopCh:
					return
				case job := <-w.queue:
					w.process(ctx, job)
				}
			}
		}()
	}
}

// Stop halts enqueue acceptance and waits for in-flight jobs to reach their
// terminal outcome, bounded by the stop grace. Queued-but-unstarted jobs are
// abandoned — the raw session events stay intact for reprocessing (D2).
// Idempotent.
func (w *Worker) Stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(stopGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		w.log.Warn("memory: stop deadline exceeded with jobs still in flight")
	}
}

// Enqueue submits one turn for background ingestion. It never blocks the
// caller: a full queue (or a stopped worker) drops the job with a warning
// and a QueueDropped count — the runner's enqueue sits on the turn path and
// must never stall a turn.
func (w *Worker) Enqueue(job IngestJob) {
	select {
	case <-w.stopCh:
		w.dropped.Add(1)
		return
	default:
	}
	select {
	case w.queue <- job:
		w.enqueued.Add(1)
	default:
		w.dropped.Add(1)
		w.log.Warn("memory: ingest queue full; dropping job",
			"workspace_id", job.WorkspaceID,
			"session_id", job.SessionID,
			"turn_id", job.TurnID)
	}
}

// IngestStats is the worker's monotonic counter snapshot; the morning
// report's extraction-failure count reads Failed from here.
type IngestStats struct {
	Enqueued     int64
	Processed    int64
	Succeeded    int64
	Failed       int64
	QueueDropped int64
}

// Stats returns the current counters.
func (w *Worker) Stats() IngestStats {
	return IngestStats{
		Enqueued:     w.enqueued.Load(),
		Processed:    w.processed.Load(),
		Succeeded:    w.succeeded.Load(),
		Failed:       w.failed.Load(),
		QueueDropped: w.dropped.Load(),
	}
}

// process runs one job: the windowed gister, then the curation gate, then
// the chip. The stages fail soft independently — a gist failure never
// blocks the gate, and nothing escapes to the caller (D10). A job with an
// empty unprocessed window is a quiet success: nothing to ingest. Jobs on
// the same session hold the session lock for the whole body — the window
// read, both stages, and the chip — so the incremental cursor never races.
func (w *Worker) process(ctx context.Context, job IngestJob) {
	mutex := w.sessionMutex(job.WorkspaceID, job.SessionID)
	mutex.Lock()
	defer mutex.Unlock()

	failed := false
	chip := MemoryIngestedPayload{NoteIDs: []string{}, EventIDs: []string{}}

	win, err := w.gister.Window(ctx, job)
	if err != nil {
		w.log.Warn("memory: gist window failed", "session_id", job.SessionID, "error", err)
		failed = true
	}

	if err == nil && len(win.events) > 0 {
		if gist, err := w.gister.Gist(ctx, job, win); err != nil {
			w.log.Warn("memory: gist failed", "session_id", job.SessionID, "error", err)
			failed = true
		} else {
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
