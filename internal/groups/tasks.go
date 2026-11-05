package groups

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Dispatch budgets (add-agent-groups design D6, spec "Group dispatch
// budgets"): a group runs at most two member assignments concurrently, and a
// root request is limited to at most eight automatic child assignments.
// Overflow persists as queued and drains as active work completes.
const (
	// maxActiveAssignmentsPerGroup is the group's running-assignment cap;
	// dispatches beyond it stay queued until an active assignment leaves the
	// running state.
	maxActiveAssignmentsPerGroup = 2
	// maxAutoChildrenPerRoot caps the automatic child assignments one root
	// request can spawn through the delegation tool.
	maxAutoChildrenPerRoot = 8
)

// newID returns a random 128-bit hex id, mirroring the store's identity
// scheme. Root tasks assign their id up front so the row's immutable
// RootTaskID can self-reference in the single insert.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// taskOpen reports whether the task is in a non-terminal state (design D8:
// queued/running/waiting/paused are open; completed/failed/cancelled are
// terminal).
func taskOpen(t *store.AgentGroupTask) bool {
	for _, state := range store.GroupTaskOpenStates {
		if t.State == state {
			return true
		}
	}
	return false
}

// openTasks returns the group's open tasks as routing task refs. The result
// is never nil.
func (s *Service) openTasks(ctx context.Context, groupID string) ([]taskRef, error) {
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group tasks: %w", err)
	}
	out := make([]taskRef, 0, len(tasks))
	for i := range tasks {
		if taskOpen(&tasks[i]) {
			out = append(out, taskRef{TaskID: tasks[i].TaskID, OwnerAgentID: deref(tasks[i].OwnerAgentID)})
		}
	}
	return out, nil
}

// composeTaskRequest renders a member dispatch's request text for work that
// already has a task: the task's instruction header (the member's packet
// never replays the session log, so the instruction rides the request),
// followed by the follow-up text when one rides. Initial dispatches (a task
// freshly created from a user message) carry the raw message text instead —
// the transcript's unread rows supply the context.
func composeTaskRequest(t *store.AgentGroupTask, followUp string) string {
	req := "[task " + t.TaskID + "] " + t.Instruction
	if t.ExpectedResult != "" {
		req += "\nExpected result: " + t.ExpectedResult
	}
	if followUp != "" {
		req += "\n\nFollow-up request:\n" + followUp
	}
	return req
}

// dispatchTask dispatches one task's member turn (design D8: state is
// persisted BEFORE the dispatch, so a crash never double-dispatches):
// budget check (a group runs at most maxActiveAssignmentsPerGroup running
// assignments — overflow stays queued), the packet builds, the running state
// persists, and SubmitGroup hands the packet to the runtime. The caller must
// hold the group's write lock. A SubmitGroup failure leaves the task queued
// (logged, retried by Drain); a packet or store failure returns an error and
// leaves the task queued. Reports whether the dispatch started.
func (s *Service) dispatchTask(ctx context.Context, task *store.AgentGroupTask, requestText string, attachments []sessions.Attachment, initial bool) (bool, error) {
	if !taskOpen(task) {
		return false, fmt.Errorf("dispatch task %s: state %s is terminal", task.TaskID, task.State)
	}
	owner := deref(task.OwnerAgentID)
	if owner == "" {
		return false, fmt.Errorf("dispatch task %s: no owner", task.TaskID)
	}

	// Budget gate: count the group's other running assignments.
	active, err := s.runningCount(ctx, task.GroupID, task.TaskID)
	if err != nil {
		return false, err
	}
	if active >= maxActiveAssignmentsPerGroup {
		slog.Debug("groups: dispatch deferred to the group budget", "group", task.GroupID, "task", task.TaskID, "active", active)
		return false, nil
	}

	agentRow, err := s.deps.Store.Agents().ByID(ctx, owner)
	if err != nil {
		return false, fmt.Errorf("dispatch task %s: load owner: %w", task.TaskID, err)
	}
	agent := *agentRow
	text := requestText
	if !initial {
		text = composeTaskRequest(task, requestText)
	}
	packet, sessionID, err := s.buildPacket(ctx, task, agent, text, attachments)
	if err != nil {
		return false, err
	}

	// Persist before dispatch (design D8): the running state lands first;
	// a crash between the write and SubmitGroup leaves the task running,
	// which restart recovery re-queues.
	task.State = store.GroupTaskStateRunning
	task.StopReason = nil
	task.Revision++
	task.AssignmentCount++
	if err := s.deps.Store.Groups().UpdateTask(ctx, task); err != nil {
		return false, fmt.Errorf("dispatch task %s: persist running: %w", task.TaskID, err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: task.GroupID, TaskID: task.TaskID})

	key := groupKey{GroupID: task.GroupID, AgentID: owner}
	s.setInflight(key, &inflightTurn{TaskID: task.TaskID, DispatchRev: task.Revision, ThroughSeq: packet.ThroughSeq, UnreadCount: len(packet.Unread)})

	// Re-seed the live copy-back pump before the turn can emit (design D7):
	// pumps are in-memory and die with the process, so every dispatch —
	// including Drain recovery after a restart — must restore its member's
	// pump or the turn's copy-back is silently lost.
	s.ensurePump(task.GroupID, owner, sessionID)

	turn, err := s.deps.Manager.SubmitGroup(ctx, agents.GroupTurnRequest{SessionID: sessionID, Packet: packet})
	if err != nil {
		// The dispatch never started: revert to queued so Drain retries,
		// and release the inflight record.
		s.clearInflight(key, task.TaskID, task.Revision)
		task.State = store.GroupTaskStateQueued
		task.Revision++
		if uerr := s.deps.Store.Groups().UpdateTask(ctx, task); uerr != nil {
			slog.Error("groups: revert dispatch to queued", "task", task.TaskID, "error", uerr)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: task.GroupID, TaskID: task.TaskID})
		slog.Warn("groups: submit group turn failed; task stays queued", "task", task.TaskID, "error", err)
		return false, nil
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: task.GroupID, TaskID: task.TaskID})

	go func(turn *agents.Turn, key groupKey, taskID string, rev int64) {
		err := turn.Wait()
		s.handleTurnEnd(key, taskID, rev, err)
	}(turn, key, task.TaskID, task.Revision)
	return true, nil
}

// runningCount counts the group's running assignments, excluding one task
// (the dispatch's own when it is already running).
func (s *Service) runningCount(ctx context.Context, groupID, excludeTaskID string) (int, error) {
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
	if err != nil {
		return 0, fmt.Errorf("count running tasks: %w", err)
	}
	n := 0
	for i := range tasks {
		if tasks[i].State == store.GroupTaskStateRunning && tasks[i].TaskID != excludeTaskID {
			n++
		}
	}
	return n, nil
}

// setInflight records a dispatch's inflight turn.
func (s *Service) setInflight(key groupKey, rec *inflightTurn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight[key] = rec
}

// clearInflight drops the inflight record only when it still names the
// same dispatch — a Continue or Reassign may have replaced it with a newer
// dispatch of the same task.
func (s *Service) clearInflight(key groupKey, taskID string, rev int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.inflight[key]; ok && rec.TaskID == taskID && rec.DispatchRev == rev {
		delete(s.inflight, key)
	}
}

// callingTask resolves the task whose turn is executing for (groupID,
// agentID) — the in-channel tools' calling context (design D4). The caller
// must hold the group's write lock.
func (s *Service) callingTask(ctx context.Context, groupID, agentID string) (*store.AgentGroupTask, error) {
	s.mu.Lock()
	rec := s.inflight[groupKey{GroupID: groupID, AgentID: agentID}]
	s.mu.Unlock()
	if rec == nil {
		return nil, fmt.Errorf("no group turn is in flight for agent %s in group %s", agentID, groupID)
	}
	t, err := s.deps.Store.Groups().TaskByID(ctx, rec.TaskID)
	if err != nil {
		return nil, fmt.Errorf("resolve calling task: %w", err)
	}
	return t, nil
}

// handleTurnEnd applies the terminal outcome of one dispatched member turn
// to its task, revision-guarded (design D8: a dispatch from a stale revision
// cannot complete — or otherwise move — the task). A turn that ends without
// error and without a reported result completes its task, unless it
// delegated open child tasks, in which case it enters the dependency wait
// (design D1/D2); a terminal task whose parent waits on it re-queues the
// parent (design D3). Then the group's queued dispatches drain and the
// member's unread window may summarize.
func (s *Service) handleTurnEnd(key groupKey, taskID string, dispatchRev int64, turnErr error) {
	// The handler outlives the dispatch's caller; the detached background
	// context drives the terminal writes.
	ctx := context.Background()

	l := s.groupLock(key.GroupID)
	l.Lock()

	// The dispatch's own record — captured before the clear, so the summary
	// path keys off the packet this turn consumed. A nil record means a
	// newer dispatch replaced ours; that turn summarizes for itself.
	s.mu.Lock()
	rec := s.inflight[key]
	if rec != nil && (rec.TaskID != taskID || rec.DispatchRev != dispatchRev) {
		rec = nil
	}
	s.mu.Unlock()
	s.clearInflight(key, taskID, dispatchRev)

	task, err := s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		l.Unlock()
		slog.Warn("groups: turn end for missing task", "task", taskID, "error", err)
		return
	}
	if task.Revision != dispatchRev {
		// Stale dispatch: a Stop, Continue, Reassign, Cancel, or a reported
		// result moved the task while the turn ran. The newer revision owns
		// the state; this handler writes nothing.
		l.Unlock()
		slog.Debug("groups: stale dispatch ignored", "task", taskID, "dispatchRev", dispatchRev, "currentRev", task.Revision)
		s.drainGroup(context.Background(), key.GroupID)
		return
	}

	switch {
	case errors.Is(turnErr, agents.ErrTurnCancelled):
		reason := "member turn cancelled"
		task.State = store.GroupTaskStatePaused
		task.StopReason = &reason
	case turnErr != nil:
		reason := turnErr.Error()
		task.State = store.GroupTaskStateFailed
		task.Error = &reason
	default:
		// A turn that ends without error and without a reported result
		// completes its task (design D1, spec "Turn without a result
		// completes"). The exception is the dependency wait (design D2,
		// spec "Waiting parent resumes after its children"): the turn
		// delegated child tasks that are still open, so the task waits for
		// their results instead of completing. The edge is implicit through
		// ParentTaskID — the delegation tool is the sole child writer, so
		// "waits on every assigned child" needs no schema.
		openChild := false
		groupTasks, gerr := s.deps.Store.Groups().ListTasksByGroup(ctx, task.GroupID)
		if gerr != nil {
			l.Unlock()
			slog.Error("groups: turn end child check", "task", taskID, "error", gerr)
			return
		}
		for i := range groupTasks {
			if groupTasks[i].ParentTaskID != nil && *groupTasks[i].ParentTaskID == task.TaskID && taskOpen(&groupTasks[i]) {
				openChild = true
				break
			}
		}
		if openChild {
			task.State = store.GroupTaskStateWaiting
		} else {
			task.State = store.GroupTaskStateCompleted
		}
	}
	task.Revision++
	if err := s.deps.Store.Groups().UpdateTask(ctx, task); err != nil {
		l.Unlock()
		slog.Error("groups: persist turn-end state", "task", taskID, "error", err)
		return
	}
	if task.State == store.GroupTaskStateFailed {
		if _, err := s.appendErrorLine(ctx, key.GroupID, task.TaskID, "Task failed: "+deref(task.Error)); err != nil {
			slog.Error("groups: append error line", "task", taskID, "error", err)
		}
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: key.GroupID, TaskID: task.TaskID})

	// The terminal outcome may complete the task's parent's dependency wait
	// (design D3): a waiting parent whose last open child just went terminal
	// re-queues before the drain dispatches it under the budget. Paused is
	// not terminal — the paused child itself keeps the wait open.
	s.resumeWaitingParent(ctx, key.GroupID, task.TaskID)

	l.Unlock()

	// The unread window this turn consumed may now warrant a summary
	// (design D3) — asynchronous, after the terminal state is persisted.
	s.maybeSummarizeAsync(key, rec)
	s.drainGroup(context.Background(), key.GroupID)
}

// resumeWaitingParent re-queues a waiting parent task when the child task
// that just reached a terminal state was its last open child (design D3,
// spec "Waiting parent resumes after its children"): the parent's next turn
// integrates the children's results from the packet's task results. The
// re-queue advances the parent's revision — an in-flight dispatch from a
// stale revision cannot move it — and the caller's drain dispatches it under
// the group budget. No-op when the child has no parent, the parent is not
// waiting, or an open sibling still runs. The caller must hold the group's
// write lock.
func (s *Service) resumeWaitingParent(ctx context.Context, groupID, childTaskID string) {
	child, err := s.deps.Store.Groups().TaskByID(ctx, childTaskID)
	if err != nil {
		slog.Warn("groups: resume waiting parent: load child", "task", childTaskID, "error", err)
		return
	}
	if child.ParentTaskID == nil {
		return
	}
	parent, err := s.deps.Store.Groups().TaskByID(ctx, *child.ParentTaskID)
	if err != nil {
		slog.Warn("groups: resume waiting parent: load parent", "task", *child.ParentTaskID, "error", err)
		return
	}
	if parent.State != store.GroupTaskStateWaiting {
		return
	}
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
	if err != nil {
		slog.Warn("groups: resume waiting parent: list tasks", "group", groupID, "error", err)
		return
	}
	for i := range tasks {
		if tasks[i].ParentTaskID != nil && *tasks[i].ParentTaskID == parent.TaskID && taskOpen(&tasks[i]) {
			return // a sibling is still open; the dependency wait continues
		}
	}
	parent.State = store.GroupTaskStateQueued
	parent.Revision++
	if err := s.deps.Store.Groups().UpdateTask(ctx, parent); err != nil {
		slog.Error("groups: resume waiting parent", "task", parent.TaskID, "error", err)
		return
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: parent.GroupID, TaskID: parent.TaskID})
}

// appendErrorLine writes a coordinator error row linked to the task (design
// D8: failures are visible in the transcript).
func (s *Service) appendErrorLine(ctx context.Context, groupID, taskID, text string) (string, error) {
	payload, err := makePayload(text, nil)
	if err != nil {
		return "", fmt.Errorf("append error line: %w", err)
	}
	msg := &store.AgentGroupMessage{
		GroupID:    groupID,
		Kind:       store.GroupMessageKindError,
		AuthorKind: store.GroupAuthorKindCoordinator,
		TaskID:     &taskID,
		Payload:    payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(ctx, msg); err != nil {
		return "", fmt.Errorf("append error line: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: groupID, MessageID: msg.MessageID, Seq: msg.Seq, Message: msg})
	return msg.MessageID, nil
}

// drainGroup starts the group's queued dispatches while the budget allows,
// in store order (task id order — stable; the per-agent lane's FIFO is the
// pinned ordering, design D6). Safe to call repeatedly: dispatchTask
// re-reads state and the budget re-counts each round.
func (s *Service) drainGroup(ctx context.Context, groupID string) {
	l := s.groupLock(groupID)
	l.Lock()
	defer l.Unlock()
	s.drainGroupLocked(ctx, groupID)
}

// drainGroupLocked is drainGroup with the group's write lock already held.
func (s *Service) drainGroupLocked(ctx context.Context, groupID string) {
	for {
		tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
		if err != nil {
			slog.Error("groups: drain read tasks", "group", groupID, "error", err)
			return
		}
		var next *store.AgentGroupTask
		active := 0
		for i := range tasks {
			if tasks[i].State == store.GroupTaskStateRunning {
				active++
				continue
			}
			if tasks[i].State == store.GroupTaskStateQueued && next == nil {
				next = &tasks[i]
			}
		}
		if next == nil || active >= maxActiveAssignmentsPerGroup {
			return
		}
		dispatched, err := s.dispatchTask(ctx, next, "", nil, false)
		if err != nil {
			slog.Error("groups: drain dispatch", "task", next.TaskID, "error", err)
			return
		}
		if !dispatched {
			return // budget full; the next terminal outcome re-drains
		}
	}
}

// StopTask pauses the task AND its descendant tasks with a visible reason
// (design D8), interrupting the owners' active member turns. The reason is
// stored on every paused task and written into the transcript as a change
// line; an empty reason reads as "stopped by user".
func (s *Service) StopTask(ctx context.Context, taskID, reason string) error {
	if reason == "" {
		reason = "stopped by user"
	}
	task, err := s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("stop task: %w", err)
	}
	l := s.groupLock(task.GroupID)
	l.Lock()
	defer l.Unlock()

	// Re-read under the group lock: the mutation is revision-guarded below.
	task, err = s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("stop task: %w", err)
	}

	// The cascade: the task and every descendant in its chain.
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, task.GroupID)
	if err != nil {
		return fmt.Errorf("stop task: %w", err)
	}
	targets := descendantTasks(tasks, task)

	var interrupt []string
	for _, t := range targets {
		if !taskOpen(&t) {
			continue
		}
		if t.State == store.GroupTaskStateRunning {
			if owner := deref(t.OwnerAgentID); owner != "" {
				interrupt = append(interrupt, owner)
			}
		}
		t.State = store.GroupTaskStatePaused
		t.StopReason = &reason
		t.Revision++
		if err := s.deps.Store.Groups().UpdateTask(ctx, &t); err != nil {
			return fmt.Errorf("stop task: %w", err)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: t.GroupID, TaskID: t.TaskID})
	}
	if _, err := s.appendChangeLine(ctx, task.GroupID, "Task paused: "+reason); err != nil {
		return err
	}
	for _, owner := range interrupt {
		s.interruptMember(ctx, task.GroupID, owner)
	}
	s.drainGroupLocked(ctx, task.GroupID)
	return nil
}

// descendantTasks returns the task itself and every task that descends from
// it (parent chain walk over the group's task list).
func descendantTasks(tasks []store.AgentGroupTask, root *store.AgentGroupTask) []store.AgentGroupTask {
	children := make(map[string][]int)
	for i := range tasks {
		if p := tasks[i].ParentTaskID; p != nil {
			children[*p] = append(children[*p], i)
		}
	}
	out := []store.AgentGroupTask{*root}
	queue := []string{root.TaskID}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, i := range children[parent] {
			out = append(out, tasks[i])
			queue = append(queue, tasks[i].TaskID)
		}
	}
	return out
}

// requeueStoppedDescendants re-queues the task's own paused/failed
// descendants (design D4, spec "Continue resumes" and "Reassign moves
// ownership"): a resumed or reassigned task must not wait on a stopped child
// forever, so the descendants re-queue with it — StopReason cleared,
// revision advanced, event fired each. Descendants already dispatching
// (queued/running/waiting) keep their state. The named task itself is
// skipped: the caller re-queues it. The caller must hold the group's write
// lock.
func (s *Service) requeueStoppedDescendants(ctx context.Context, tasks []store.AgentGroupTask, task *store.AgentGroupTask) error {
	for _, t := range descendantTasks(tasks, task) {
		if t.TaskID == task.TaskID {
			continue
		}
		if t.State != store.GroupTaskStatePaused && t.State != store.GroupTaskStateFailed {
			continue
		}
		t.State = store.GroupTaskStateQueued
		t.StopReason = nil
		t.Revision++
		if err := s.deps.Store.Groups().UpdateTask(ctx, &t); err != nil {
			return fmt.Errorf("requeue stopped descendant %s: %w", t.TaskID, err)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: t.GroupID, TaskID: t.TaskID})
	}
	return nil
}

// ContinueTask re-dispatches a stopped task — paused or failed, per the
// align-group-task-lifecycle delta ("continue a stopped task (re-queuing it
// and its descendant tasks with it)") — with the continue message as a
// follow-up (design D4 + D8): the task keeps its instruction and owner, the
// message lands in the transcript as a user row linked to the task, and the
// dispatch path (budget-guarded) runs again. The branch resumes with the
// named task (design D4): its stopped descendants re-queue with it and
// start through the drain loop, so the group budget holds. Only a stopped
// task continues. task.Error is left as-is: the stop notice only renders
// failed/paused roots, and the resumed turn's next terminal outcome
// overwrites it.
func (s *Service) ContinueTask(ctx context.Context, taskID, message string) error {
	task, err := s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	l := s.groupLock(task.GroupID)
	l.Lock()
	defer l.Unlock()

	task, err = s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	if task.State != store.GroupTaskStatePaused && task.State != store.GroupTaskStateFailed {
		return fmt.Errorf("continue task %s: state is %s, only a stopped task continues", taskID, task.State)
	}

	payload, err := makePayload(message, nil)
	if err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	msg := &store.AgentGroupMessage{
		GroupID:    task.GroupID,
		Kind:       store.GroupMessageKindMessage,
		AuthorKind: store.GroupAuthorKindUser,
		TaskID:     &taskID,
		Payload:    payload,
	}
	if err := s.deps.Store.Groups().AppendMessage(ctx, msg); err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventMessageAppended, GroupID: task.GroupID, MessageID: msg.MessageID, Seq: msg.Seq, Message: msg})

	task.State = store.GroupTaskStateQueued
	task.StopReason = nil
	task.Revision++
	if err := s.deps.Store.Groups().UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: task.GroupID, TaskID: task.TaskID})

	// The branch resumes with the named task (design D4): every stopped
	// descendant re-queues with it.
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, task.GroupID)
	if err != nil {
		return fmt.Errorf("continue task: %w", err)
	}
	if err := s.requeueStoppedDescendants(ctx, tasks, task); err != nil {
		return fmt.Errorf("continue task: %w", err)
	}

	if _, err := s.dispatchTask(ctx, task, message, nil, false); err != nil {
		return err
	}
	// The re-queued descendants start through the drain loop, not direct
	// dispatch, so the group budget holds (design D4).
	s.drainGroupLocked(ctx, task.GroupID)
	return nil
}

// ReassignTask moves an open task to another roster member, bumps its
// revision, and re-dispatches it to the new owner (design D8). A running
// task's previous owner turn is interrupted first. The moved task's own
// paused/failed descendants re-queue with it and drain under the budget —
// a restarted task must not wait on a stopped child forever (design D4).
// The ownership change is written into the transcript as an assigned line.
func (s *Service) ReassignTask(ctx context.Context, taskID, newOwnerAgentID string) error {
	task, err := s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	l := s.groupLock(task.GroupID)
	l.Lock()
	defer l.Unlock()

	task, err = s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	if !taskOpen(task) {
		return fmt.Errorf("reassign task %s: state %s is terminal", taskID, task.State)
	}
	agent, err := s.deps.Store.Agents().ByID(ctx, newOwnerAgentID)
	if err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	members, err := s.deps.Store.Groups().ListMembers(ctx, task.GroupID)
	if err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	inRoster := false
	for _, m := range members {
		if m.AgentID == newOwnerAgentID {
			inRoster = true
			break
		}
	}
	if !inRoster {
		return fmt.Errorf("reassign task: agent %s is not a member of group %s: %w", newOwnerAgentID, task.GroupID, store.ErrNotFound)
	}

	prevOwner := deref(task.OwnerAgentID)
	task.OwnerAgentID = &newOwnerAgentID
	task.State = store.GroupTaskStateQueued
	task.StopReason = nil
	task.Revision++
	if err := s.deps.Store.Groups().UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: task.GroupID, TaskID: task.TaskID})

	if _, err := s.appendAssignedLine(ctx, task.GroupID, task.TaskID, "Assigned to "+agent.Name); err != nil {
		return err
	}

	if prevOwner != "" && prevOwner != newOwnerAgentID {
		// The previous owner's turn, if still in flight, is interrupted;
		// its turn-end handler is stale-guarded against the bumped
		// revision.
		s.interruptMember(ctx, task.GroupID, prevOwner)
	}

	// The moved task's own stopped descendants re-queue with it (design
	// D4): the restart reaches the whole stopped branch.
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, task.GroupID)
	if err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}
	if err := s.requeueStoppedDescendants(ctx, tasks, task); err != nil {
		return fmt.Errorf("reassign task: %w", err)
	}

	if _, err := s.dispatchTask(ctx, task, "", nil, false); err != nil {
		return err
	}
	// The re-queued descendants start through the drain loop, not direct
	// dispatch, so the group budget holds (design D4).
	s.drainGroupLocked(ctx, task.GroupID)
	return nil
}

// CancelTask ends every open task of the named task's root chain (design
// D8) — the named task need not be open itself — and interrupts the active
// turns of the cancelled tasks' owners. Cancel is the stopped-chain verb
// (design D5, spec "Cancel refuses active work"): it refuses a queued,
// running, or waiting task — that is active work a Stop pauses — and is
// allowed only when the named task is paused or failed. A change line
// records the cancel in the transcript.
func (s *Service) CancelTask(ctx context.Context, taskID string) error {
	task, err := s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	l := s.groupLock(task.GroupID)
	l.Lock()
	defer l.Unlock()

	task, err = s.deps.Store.Groups().TaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	// The gate (design D5): cancel never ends active work — Stop stays the
	// one verb for it.
	if task.State != store.GroupTaskStatePaused && task.State != store.GroupTaskStateFailed {
		return fmt.Errorf("cancel task %s: state %s is active — stop the run instead; cancel is for stopped chains", taskID, task.State)
	}

	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, task.GroupID)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	var interrupt []string
	for _, t := range tasks {
		if t.RootTaskID != task.RootTaskID || !taskOpen(&t) {
			continue
		}
		t.State = store.GroupTaskStateCancelled
		t.Revision++
		if err := s.deps.Store.Groups().UpdateTask(ctx, &t); err != nil {
			return fmt.Errorf("cancel task: %w", err)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: t.GroupID, TaskID: t.TaskID})
		if owner := deref(t.OwnerAgentID); owner != "" {
			interrupt = append(interrupt, owner)
		}
	}
	if _, err := s.appendChangeLine(ctx, task.GroupID, "Cancelled task chain: "+task.RootTaskID); err != nil {
		return err
	}
	for _, owner := range interrupt {
		s.interruptMember(ctx, task.GroupID, owner)
	}
	s.drainGroupLocked(ctx, task.GroupID)
	return nil
}

// memberSessionID returns the member's group session id from its acceptance
// cursor; empty when the member has none.
func (s *Service) memberSessionID(ctx context.Context, groupID, agentID string) string {
	c, err := s.deps.Store.Groups().GetCursor(ctx, groupID, agentID)
	if err != nil {
		return ""
	}
	return c.SessionID
}

// interruptMember cancels the member's group session turn — the runtime's
// safe-point cancel with escalation — and drops the session's queued group
// dispatches (design D8). Errors are logged, never fatal: the interrupt is
// best-effort alongside the state transition that prompted it.
func (s *Service) interruptMember(ctx context.Context, groupID, agentID string) {
	sessionID := s.memberSessionID(ctx, groupID, agentID)
	if sessionID == "" {
		return
	}
	if err := s.deps.Manager.Cancel(ctx, sessionID); err != nil {
		slog.Warn("groups: interrupt member turn", "group", groupID, "agent", agentID, "error", err)
	}
}

// pauseMemberTasks pauses every open task the member owns with a visible
// reason (member removal, agent cleanup). The caller must hold the group's
// write lock.
func (s *Service) pauseMemberTasks(ctx context.Context, groupID, agentID, reason string) error {
	tasks, err := s.deps.Store.Groups().ListTasksByGroup(ctx, groupID)
	if err != nil {
		return fmt.Errorf("pause member tasks: %w", err)
	}
	for i := range tasks {
		t := tasks[i]
		if t.OwnerAgentID == nil || *t.OwnerAgentID != agentID || !taskOpen(&t) {
			continue
		}
		t.State = store.GroupTaskStatePaused
		t.StopReason = &reason
		t.Revision++
		if err := s.deps.Store.Groups().UpdateTask(ctx, &t); err != nil {
			return fmt.Errorf("pause member tasks: %w", err)
		}
		s.fireGroupEvent(GroupEvent{Kind: GroupEventTaskUpdated, GroupID: groupID, TaskID: t.TaskID})
	}
	return nil
}
