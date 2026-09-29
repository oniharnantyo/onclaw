package agents

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	bgtask "github.com/cloudwego/eino/adk/backgroundtask"
	backgroundlocal "github.com/cloudwego/eino/adk/backgroundtask/local"
	fsmw "github.com/cloudwego/eino/adk/middlewares/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/subagent"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

// TasksOutputDir is the canonical agent-dir-relative directory background
// task output files land in (add-agent-subagents-background D6/D11) — the
// single literal lives in the backend package next to the append opener that
// enforces it; the fs middleware's Background.OutputDir and the lanes' output
// paths all wire through this name.
const TasksOutputDir = backend.TasksOutputDir

// Eino task kinds the two v1 background lanes set on their specs: the
// subagent middleware's delegation lane and the fs middleware's shell lane.
// Referenced as constants (not literals) so upstream renames fail the build.
const (
	einoTaskKindSubagent = subagent.TaskKindSubagent // "subagent"
	einoTaskKindBash     = fsmw.ExecuteTaskKind      // "bash"
)

const (
	// notificationPollInterval paces the completion pump. The in-memory
	// outbox's Receive is non-blocking — it returns immediately with whatever
	// is visible — so the pump polls; 250ms keeps completion notices
	// near-live without busy-spinning the run.
	notificationPollInterval = 250 * time.Millisecond
	// notificationLease bounds one pump's hold on a leased notification. The
	// pump acks immediately after handling, so the lease only covers
	// processing latency; a pump that dies mid-lease lets the record
	// re-surface for the next drain instead of vanishing.
	notificationLease = 30 * time.Second
)

// foregroundTimerDisabledMs pins the Runner's foreground observation timer
// off (add-agent-subagents-background D11): nothing is ever auto-backgrounded
// — foreground commands run to completion exactly as they did before the
// lane existed, and an explicit run_in_background is the only path into the
// background. A non-positive Config.ForegroundTimeoutMs disables the timer.
var foregroundTimerDisabledMs = -1

// BackgroundTaskSpace is one run's process-local background task lane
// (add-agent-subagents-background D5): the in-memory store the Manager
// writes lifecycle into and the pump drains, the Manager itself (the
// task_output/task_stop control-tools' binding point), and the local Runner
// that executes the lanes' closures. Constructed per run — run ends or
// process dies, tasks and their records are gone (the honest-lifetime pin).
type BackgroundTaskSpace struct {
	Store   *bgtask.InMemoryStore
	Manager *bgtask.Manager
	Runner  *backgroundlocal.Runner
}

// newBackgroundTaskSpace builds one run's task space. Allocation-only: no
// store/database/disk access, no goroutines — the composition stays pure and
// each space's Manager mints task ids from its own store, so two spaces never
// share a task-id space.
func newBackgroundTaskSpace(ctx context.Context) (*BackgroundTaskSpace, error) {
	store := bgtask.NewInMemoryStore(nil)
	// One registry shared by Manager and Runner: the local Runner registers
	// its process-local executor into it, and the Manager resolves specs
	// through the same registry. backgroundlocal.New requires it non-nil.
	registry := bgtask.NewExecutorRegistry()
	manager, err := bgtask.New(ctx, &bgtask.Config{
		Tasks:      store,
		TaskEvents: store,
		Executors:  registry,
		// Every parent-session task (the lanes always notify) requires the
		// task-created sender. Eino's canonical sender is stateless — it
		// resolves the parent session from the send-time run ctx — so the
		// space wires it per message type without runner coupling; the
		// emitted x.eino.background_task.created event rides the parent's
		// normal session-event lane.
		SendTaskCreatedEvent: bgtask.TaskCreatedSessionEventSender[*schema.AgenticMessage](),
	})
	if err != nil {
		return nil, fmt.Errorf("agents: background task manager: %w", err)
	}
	runner, err := backgroundlocal.New(&backgroundlocal.Config{
		Manager:             manager,
		Executors:           registry,
		ForegroundTimeoutMs: &foregroundTimerDisabledMs,
	})
	if err != nil {
		return nil, fmt.Errorf("agents: background task runner: %w", err)
	}
	return &BackgroundTaskSpace{Store: store, Manager: manager, Runner: runner}, nil
}

// pumpBackgroundNotifications drains the space's notification outbox until
// ctx is done, emitting one TaskCompletedPayload per terminal task
// transition (add-agent-subagents-background D8). The emit callback is the
// runner worker's wiring point — it appends the x.task_completed session
// event and broadcasts the live TranscriptEvent. The space is per-run, so
// every notification it leases belongs to the run's session by construction;
// the pump is best-effort — store or enrichment errors log and continue, and
// nothing here can fail the run. It exits with the run (ctx done), matching
// the task space's lifetime.
func pumpBackgroundNotifications(ctx context.Context, space *BackgroundTaskSpace, emit func(TaskCompletedPayload)) {
	ticker := time.NewTicker(notificationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		result, err := space.Store.Receive(ctx, &bgtask.ReceiveNotificationsRequest{
			Limit:         100,
			LeaseDuration: notificationLease,
		})
		if err != nil {
			slog.WarnContext(ctx, "agents: background notification receive failed (best-effort)", "error", err)
			continue
		}
		for _, delivery := range result.Deliveries {
			space.dispatchNotification(ctx, delivery, emit)
		}
	}
}

// dispatchNotification handles one leased notification: lifecycle plumbing
// (task_created, waiting_input) is acked and dropped — only terminal
// transitions emit completion surface; terminal ones enrich from the
// authoritative task snapshot, emit, then ack the receipt. Skipping before
// acking would re-lease the same record on every poll, so every handled
// delivery is acked exactly once.
func (s *BackgroundTaskSpace) dispatchNotification(ctx context.Context, delivery bgtask.NotificationDelivery, emit func(TaskCompletedPayload)) {
	record := delivery.Record
	outcome, terminal := taskOutcomeFor(record.Kind)
	if !terminal {
		s.ackNotification(ctx, delivery.Receipt)
		return
	}
	payload := TaskCompletedPayload{TaskID: record.TaskID, Outcome: outcome}
	if task, err := s.Manager.Get(ctx, record.TaskID); err != nil {
		// The terminal notification is authoritative that the transition
		// happened; enrichment is additive, so emit with what the record
		// carries even when the snapshot is unreachable.
		slog.WarnContext(ctx, "agents: background task snapshot unavailable for completion notice (best-effort)",
			"task_id", record.TaskID, "error", err)
	} else {
		payload.Kind = taskKindFor(task.Spec.Kind)
		payload.OutputPath = task.Spec.OutputFile
		payload.Summary = taskSummary(task.Spec.Description)
	}
	emit(payload)
	s.ackNotification(ctx, delivery.Receipt)
}

// ackNotification acknowledges one leased receipt. A lost ack only delays
// redelivery — the pump's emit-per-transition contract is served by the
// terminal notification being consumed once; duplicates surface as repeated
// completion chips, so failures log loudly but never crash the run.
func (s *BackgroundTaskSpace) ackNotification(ctx context.Context, receipt bgtask.NotificationReceipt) {
	if err := s.Store.Ack(ctx, receipt); err != nil {
		slog.WarnContext(ctx, "agents: background notification ack failed (best-effort)", "error", err)
	}
}

// taskOutcomeFor maps an outbox notification kind onto the completion
// payload's outcome; ok is false for non-terminal lifecycle records.
func taskOutcomeFor(kind bgtask.NotificationKind) (outcome string, ok bool) {
	switch kind {
	case bgtask.NotificationCompleted:
		return "completed", true
	case bgtask.NotificationFailed:
		return "failed", true
	case bgtask.NotificationCanceled:
		return "canceled", true
	default:
		return "", false
	}
}

// taskKindFor maps an eino task kind onto the payload's lane kind:
// delegation for the subagent lane, shell for the fs shell lane. Unknown
// kinds pass through verbatim — a future lane stays honest on the transcript
// instead of collapsing into the wrong copy.
func taskKindFor(kind string) string {
	switch kind {
	case einoTaskKindSubagent:
		return "delegation"
	case einoTaskKindBash:
		return "shell"
	default:
		return kind
	}
}

// taskSummary derives the completion chip's one-line human summary from the
// task description: its first non-empty line, trimmed.
func taskSummary(description string) string {
	description = strings.TrimSpace(description)
	if line, _, found := strings.Cut(description, "\n"); found {
		description = line
	}
	return strings.TrimSpace(description)
}
