package groups

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Restart recovery (add-agent-groups design D8): group state persists in
// SQLite and the app restart re-drains it. Idempotency comes from the
// state-machine guards — persist-before-dispatch and the revision guard —
// so a Drain is safe to call repeatedly and after every start.
//
// Drain's worklist (documented decision):
//   - waiting tasks convert by the dependency-wait rule (align design D7,
//     spec "Group task lifecycle"): waiting now means the turn ended while
//     open child tasks remain, so a childless waiting row is a legacy park
//     under the old turn-end semantics and completes; a waiting row whose
//     children are all terminal missed its re-queue at a crash (between the
//     child's terminal write and the parent re-queue) and re-queues; a
//     waiting row with an open child keeps waiting;
//   - queued tasks re-dispatch (budget-guarded, in store order);
//   - running tasks whose dispatch is no longer live (the process died
//     mid-turn — a task is live exactly while its (group, member) inflight
//     record holds) re-queue first, then re-dispatch; a live running task is
//     left alone, making a double Drain a no-op;
//   - paused tasks stay paused (the visible StopReason awaits the user's
//     Continue — a restart must not undo a user's Stop).

// Drain re-drains the persisted dispatches at service start (add-agent-groups
// task 4.3 wires the call). Failures are logged per task, never fatal, and
// Drain never panics.
func (s *Service) Drain(ctx context.Context) error {
	tasks, err := s.deps.Store.Groups().ListOpenTasks(ctx)
	if err != nil {
		return fmt.Errorf("drain: %w", err)
	}
	byGroup := make(map[string][]store.AgentGroupTask)
	for _, t := range tasks {
		byGroup[t.GroupID] = append(byGroup[t.GroupID], t)
	}
	for groupID, groupTasks := range byGroup {
		l := s.groupLock(groupID)
		l.Lock()

		// Pass 0: stranded waiting rows convert (design D7). The child
		// check needs the group's FULL task list — a waiting row's terminal
		// children are not open tasks, yet their terminality is exactly
		// what closes the wait.
		allTasks, listErr := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
		if listErr != nil {
			slog.Error("groups: drain read tasks", "group", groupID, "error", listErr)
		}
		for i := range groupTasks {
			t := groupTasks[i]
			if listErr != nil || t.State != store.GroupTaskStateWaiting {
				continue
			}
			hasChild := false
			childOpen := false
			for j := range allTasks {
				if allTasks[j].ParentTaskID != nil && *allTasks[j].ParentTaskID == t.TaskID {
					hasChild = true
					if taskOpen(&allTasks[j]) {
						childOpen = true
					}
				}
			}
			if childOpen {
				continue // the dependency wait is live; nothing to convert
			}
			if hasChild {
				t.State = store.GroupTaskStateQueued // the missed re-queue re-drains below
			} else {
				t.State = store.GroupTaskStateCompleted // a legacy park under the old turn-end semantics
			}
			t.Revision++
			if err := s.deps.Store.Groups().UpdateTask(ctx, &t); err != nil {
				slog.Error("groups: drain waiting convert", "task", t.TaskID, "error", err)
				continue
			}
			s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: t.GroupID, TaskID: t.TaskID})
			groupTasks[i] = t
		}

		// Pass 1: crashed running dispatches re-queue. A task is live
		// exactly while an inflight record names it.
		live := make(map[string]bool)
		s.mu.Lock()
		for _, rec := range s.inflight {
			if rec != nil {
				live[rec.TaskID] = true
			}
		}
		s.mu.Unlock()
		for i := range groupTasks {
			t := groupTasks[i]
			if t.State == store.GroupTaskStateRunning && !live[t.TaskID] {
				t.State = store.GroupTaskStateQueued
				t.Revision++
				if err := s.deps.Store.Groups().UpdateTask(ctx, &t); err != nil {
					slog.Error("groups: drain requeue", "task", t.TaskID, "error", err)
					continue
				}
				s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: t.GroupID, TaskID: t.TaskID})
				groupTasks[i] = t
			}
		}

		// Pass 2: queued dispatches re-dispatch while the budget allows.
		for i := range groupTasks {
			t := groupTasks[i]
			if t.State != store.GroupTaskStateQueued {
				continue
			}
			if _, err := s.dispatchTask(ctx, &t, "", nil, false); err != nil {
				slog.Error("groups: drain dispatch", "task", t.TaskID, "error", err)
			}
		}
		l.Unlock()
	}
	return nil
}
