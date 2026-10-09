package agents

import (
	"errors"
	"sort"
)

// ErrTaskNotRunning is the typed conflict the task cancel path returns for
// anything that is not a running task: an unknown id, a task of a session
// with no live run, and a task that already reached a terminal status. The
// session tasks handler maps it to 409.
var ErrTaskNotRunning = errors.New("agents: task is not running")

// Pinned merged-list statuses (add-subagent-runs-surface 2.4). TaskStatusCreated
// is the process-local honesty status: persisted created evidence with no
// completion and no live task — never presented as running.
const (
	TaskStatusRunning  = "running"
	TaskStatusCreated  = "created"
	taskStatusComplete = "completed"
	taskStatusFailed   = "failed"
	taskStatusCanceled = "canceled"
)

// LiveTask is the plain live-side input of MergeSessionTasks: one task of a
// live run's task space, snapshotted from the authoritative task store. Status
// carries the eino lifecycle status verbatim (pending, running, waiting_input,
// suspended, completed, failed, canceled); Kind carries the lane kind already
// mapped through taskKindFor.
type LiveTask struct {
	TaskID      string
	Kind        string
	Status      string
	Description string
	OutputPath  string
}

// TaskCreatedRecord is the plain persisted-side input of MergeSessionTasks:
// one decoded x.eino.background_task.created session event (the eino
// TaskCreatedSessionEvent payload — the only fields that event carries).
type TaskCreatedRecord struct {
	TaskID      string
	Description string
}

// TaskListEntry is one row of the merged session task list — the entry type
// the session tasks handler marshals. The JSON tags are the wire contract.
// Kind is empty only for created-only entries: the persisted created event
// carries no lane kind, and honesty forbids guessing one.
type TaskListEntry struct {
	TaskID      string `json:"task_id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"` // running | completed | failed | canceled | created
	Description string `json:"description"`
	OutputPath  string `json:"output_path,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

// taskStatusFor maps a live task's eino lifecycle status onto the pinned
// merged-list status set. Every non-terminal status maps to running: pending,
// waiting_input, and suspended tasks are alive in the run's process-local
// space, and the pinned set has no other honest bucket for them. Created is
// never produced here — it is reserved for persisted created evidence without
// a live task (the restart-lost case).
func taskStatusFor(status string) string {
	switch status {
	case taskStatusComplete:
		return taskStatusComplete
	case taskStatusFailed:
		return taskStatusFailed
	case taskStatusCanceled:
		return taskStatusCanceled
	default:
		return TaskStatusRunning
	}
}

// taskListEntryFromLive builds the list entry for one live task snapshot —
// the cancel path's acceptance document shares the merge's entry derivation.
func taskListEntryFromLive(live LiveTask) TaskListEntry {
	return TaskListEntry{
		TaskID:      live.TaskID,
		Kind:        live.Kind,
		Status:      taskStatusFor(live.Status),
		Description: live.Description,
		OutputPath:  live.OutputPath,
		Summary:     taskSummary(live.Description),
	}
}

// MergeSessionTasks joins a live run's task snapshot with the session's
// persisted task-created and task-completed session events into the merged
// task list (add-subagent-runs-surface 2.4). Pure: plain inputs, no I/O —
// the handler gathers the live snapshot and decodes the persisted events.
//
// Precedence rules (process-local honesty): a persisted terminal outcome
// beats every other evidence for the task; a created record with no
// completion and no live task renders as created — never running — because a
// task lost to a restart must not present as running; live tasks render from
// their authoritative snapshot. Duplicate persisted payloads (redelivery)
// collapse to the first. The result is sorted by task id for determinism and
// is an empty slice, never nil.
func MergeSessionTasks(live []LiveTask, created []TaskCreatedRecord, completed []TaskCompletedPayload) []TaskListEntry {
	createdBy := make(map[string]TaskCreatedRecord, len(created))
	for _, record := range created {
		if _, ok := createdBy[record.TaskID]; !ok {
			createdBy[record.TaskID] = record
		}
	}
	completedBy := make(map[string]TaskCompletedPayload, len(completed))
	for _, payload := range completed {
		if payload.TaskID == "" {
			continue
		}
		if _, ok := completedBy[payload.TaskID]; !ok {
			completedBy[payload.TaskID] = payload
		}
	}

	entries := make([]TaskListEntry, 0, len(live)+len(created))
	seen := make(map[string]bool, len(live)+len(created))

	for _, task := range live {
		entry := taskListEntryFromLive(task)
		if payload, ok := completedBy[task.TaskID]; ok {
			entry = entryFromCompleted(payload, entry)
		}
		entries = append(entries, entry)
		seen[task.TaskID] = true
	}

	for id, record := range createdBy {
		if seen[id] {
			continue
		}
		entry := TaskListEntry{
			TaskID:      id,
			Status:      TaskStatusCreated,
			Description: record.Description,
		}
		if payload, ok := completedBy[id]; ok {
			entry = entryFromCompleted(payload, entry)
		}
		entries = append(entries, entry)
		seen[id] = true
	}

	for id, payload := range completedBy {
		if seen[id] {
			continue
		}
		// Persisted completion with neither live task nor created event: the
		// created evidence is gone but the outcome was durably recorded.
		entries = append(entries, entryFromCompleted(payload, TaskListEntry{TaskID: id}))
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].TaskID < entries[j].TaskID })
	return entries
}

// entryFromCompleted applies a persisted terminal outcome onto a base entry:
// the durable evidence wins for status, lane kind, output location, and
// summary; description falls back to the base's when it has one (the live
// snapshot or the created event carries the full task description, the
// completion chip only its first line).
func entryFromCompleted(payload TaskCompletedPayload, base TaskListEntry) TaskListEntry {
	entry := TaskListEntry{
		TaskID:      base.TaskID,
		Status:      payload.Outcome,
		Description: base.Description,
		OutputPath:  payload.OutputPath,
		Summary:     payload.Summary,
	}
	if entry.Description == "" {
		entry.Description = payload.Summary
	}
	if payload.Kind != "" {
		entry.Kind = payload.Kind
	} else {
		entry.Kind = base.Kind
	}
	return entry
}
