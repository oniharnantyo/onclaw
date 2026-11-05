package groups

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/store"
)

// Task lifecycle tests (align-group-task-lifecycle design D1-D5, spec
// "Group task lifecycle" and "Group dispatch budgets"): the turn-end rule
// (a no-result turn completes; open children hold the dependency wait), the
// waiting parent's automatic re-queue, the revision guard, the Stop
// cascade, branch-scope Continue and Reassign, the stopped-chain Cancel
// gate, both dispatch budgets, and persist-before-dispatch with Drain
// recovery.

// assignThroughTool drives the group_assign_task in-channel tool as the
// calling member's turn would.
func assignThroughTool(t *testing.T, h *harness, groupID, callerID, member, instruction string) string {
	t.Helper()
	tools := toInvokable(t, mustTools(t, h, groupID, callerID))
	require.Len(t, tools, 3, "a group turn carries exactly the three in-channel tools")
	out := h.invokeTool(t, tools[1], fmt.Sprintf(`{"member":%q,"instruction":%q}`, member, instruction))
	require.NotEmpty(t, out)
	return out
}

// resultThroughTool drives the group_task_result in-channel tool.
func resultThroughTool(t *testing.T, h *harness, groupID, callerID, result string) {
	t.Helper()
	tools := toInvokable(t, mustTools(t, h, groupID, callerID))
	_ = h.invokeTool(t, tools[2], fmt.Sprintf(`{"result":%q}`, result))
}

// childOf returns the group task whose ParentTaskID names the given parent.
func childOf(t *testing.T, h *harness, groupID, parentTaskID string) store.AgentGroupTask {
	t.Helper()
	for _, task := range h.groupTasks(groupID) {
		if task.ParentTaskID != nil && *task.ParentTaskID == parentTaskID {
			return task
		}
	}
	t.Fatalf("no child task of %s in group %s", parentTaskID, groupID)
	return store.AgentGroupTask{}
}

func TestTasks_PersistBeforeDispatch(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))

	// At SubmitGroup time the task's persisted state was already running
	// (design D8): the fake snapshots the group's states at each submit.
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, []string{"running"}, h.rt.statesAtSubmit[0])
}

func TestTasks_TurnEndWithoutResultCompletes(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	h.rt.finishTurn(0, nil)
	requireEventually(t, "task settles to completed", func() bool {
		tasks := h.groupTasks(g.ID)
		return len(tasks) == 1 && tasks[0].State == store.GroupTaskStateCompleted
	})

	// A result reported on the now-terminal task still refuses (the result
	// tool completes a task once).
	task := h.groupTasks(g.ID)[0]
	tools := toInvokable(t, mustTools(t, h, g.ID, atlas.ID))
	_, err := tools[2].InvokableRun(h.ctx, fmt.Sprintf(`{"result":"late","task_id":%q}`, task.TaskID))
	require.Error(t, err)
	require.Contains(t, err.Error(), "already")
}

func TestTasks_TurnEndWithOpenChildrenWaits(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	require.Equal(t, 1, h.rt.submitCount())
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)

	// The delegating turn ends without a result: it waits on its open
	// child instead of completing (design D2).
	h.rt.finishTurn(0, nil)
	requireEventually(t, "delegating parent settles to waiting", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateWaiting
	})
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(child.TaskID).State, "the open child keeps running")
}

func TestTasks_WaitingParentResumesOnChildTurnEnd(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)
	h.rt.finishTurn(0, nil)
	requireEventually(t, "parent waits", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateWaiting
	})
	waitedRev := h.taskByID(root.TaskID).Revision

	// The child's turn ends without a result: it completes (no open
	// children of its own), which was the waiting parent's last open child
	// — the parent re-queues (design D3) and the drain dispatches it.
	h.rt.finishTurn(1, nil)
	requireEventually(t, "parent re-dispatched", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateRunning
	})
	require.Equal(t, store.GroupTaskStateCompleted, h.taskByID(child.TaskID).State)
	require.Greater(t, h.taskByID(root.TaskID).Revision, waitedRev, "the re-queue advances the revision")
	require.Equal(t, 3, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(2), "the parent's next turn integrates the child's result")
}

func TestTasks_WaitingParentResumesOnFailedChild(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)
	h.rt.finishTurn(0, nil)
	requireEventually(t, "parent waits", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateWaiting
	})
	waitedRev := h.taskByID(root.TaskID).Revision

	// A FAILED last sibling is terminal too: the parent re-queues (design
	// D3) and its next turn sees the failure in the transcript.
	h.rt.finishTurn(1, errors.New("child blew up"))
	requireEventually(t, "parent re-dispatched after the child's failure", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateRunning
	})
	require.Equal(t, store.GroupTaskStateFailed, h.taskByID(child.TaskID).State)
	require.Greater(t, h.taskByID(root.TaskID).Revision, waitedRev)
	require.Equal(t, 3, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(2))
}

func TestTasks_WaitingParentResumesOnChildResultTool(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)
	h.rt.finishTurn(0, nil)
	requireEventually(t, "parent waits", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateWaiting
	})
	waitedRev := h.taskByID(root.TaskID).Revision

	// The child reports its result through the tool: the completion
	// re-queues the parent (design D3, second terminal site) and the
	// tool's drain dispatches it with the result in the packet.
	resultThroughTool(t, h, g.ID, scout.ID, "child findings")
	requireEventually(t, "parent re-dispatched", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateRunning
	})
	require.Equal(t, store.GroupTaskStateCompleted, h.taskByID(child.TaskID).State)
	require.Greater(t, h.taskByID(root.TaskID).Revision, waitedRev)
	require.Equal(t, 3, h.rt.submitCount())
	req := h.rt.submitAt(2)
	require.Len(t, req.Packet.TaskResults, 1, "the parent's next turn integrates the child's result")
	require.Equal(t, child.TaskID, req.Packet.TaskResults[0].TaskID)
	require.Equal(t, "child findings", req.Packet.TaskResults[0].Result)
}

func TestTasks_WaitingParentHoldsUntilLastSiblingTerminal(t *testing.T) {
	h, g, atlas, scout, jono := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child one")
	assignThroughTool(t, h, g.ID, atlas.ID, "Jono", "child two")
	var child1, child2 store.AgentGroupTask
	for _, task := range h.groupTasks(g.ID) {
		if task.ParentTaskID == nil || *task.ParentTaskID != root.TaskID {
			continue
		}
		switch deref(task.OwnerAgentID) {
		case scout.ID:
			child1 = task
		case jono.ID:
			child2 = task
		}
	}
	require.NotEmpty(t, child1.TaskID)
	require.NotEmpty(t, child2.TaskID)

	// The parent waits once its turn ends; the second child drained into
	// the freed budget slot.
	h.rt.finishTurn(0, nil)
	requireEventually(t, "second child drains", func() bool {
		return h.taskByID(child2.TaskID).State == store.GroupTaskStateRunning
	})
	require.Equal(t, store.GroupTaskStateWaiting, h.taskByID(root.TaskID).State)

	// The first child completes: the second is still open — the parent
	// keeps waiting.
	resultThroughTool(t, h, g.ID, scout.ID, "child one done")
	require.Equal(t, store.GroupTaskStateWaiting, h.taskByID(root.TaskID).State)

	// The last sibling's terminal turn re-queues the parent (design D3).
	h.rt.finishTurn(2, nil)
	requireEventually(t, "parent re-dispatched after the last sibling", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateRunning
	})
	require.Equal(t, 4, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(3))
	_ = jono
}

func TestTasks_TurnErrorFailsTaskWithVisibleRow(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	h.rt.finishTurn(0, errors.New("model exploded"))
	// The failure row lands in the same turn-end handler as the state
	// write; waiting for the row settles the state too.
	requireEventually(t, "failed task shows its error row", func() bool {
		return len(h.messagesOf(g.ID, store.GroupMessageKindError)) == 1
	})
	task := h.taskByID(h.groupTasks(g.ID)[0].TaskID)
	require.Equal(t, store.GroupTaskStateFailed, task.State)
	require.NotNil(t, task.Error)
	require.Contains(t, *task.Error, "model exploded")

	// The failure is visible in the transcript (kind=error row).
	errs := h.messagesOf(g.ID, store.GroupMessageKindError)
	require.Len(t, errs, 1)
	require.Contains(t, payloadText(errs[0]), "model exploded")
}

func TestTasks_RevisionGuard_StaleDispatchCannotMoveTask(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	require.Equal(t, 1, h.rt.submitCount())
	dispatched := h.taskByID(h.groupTasks(g.ID)[0].TaskID)
	dispatchRev := dispatched.Revision

	// The user stops the task mid-turn: paused with a visible reason, and
	// the owner's active turn is interrupted.
	require.NoError(t, h.svc.StopTask(h.ctx, dispatched.TaskID, "changed my mind"))
	require.Equal(t, 1, h.rt.cancelCount(), "the active member turn is interrupted")

	// The stale dispatch's turn now completes — the write is rejected: the
	// task stays paused, at the revision Stop left it at.
	h.rt.finishTurn(0, nil)
	requireEventually(t, "stale turn end settles", func() bool {
		return true
	})
	task := h.taskByID(dispatched.TaskID)
	require.Equal(t, store.GroupTaskStatePaused, task.State)
	require.Equal(t, "changed my mind", deref(task.StopReason))
	require.Equal(t, dispatchRev+1, task.Revision)
}

func TestTasks_StopCascadesToDescendants(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	// The lead's turn is in flight; it delegates a child to Scout through
	// the tool.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	require.Equal(t, 1, h.rt.submitCount())
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")

	tasks := h.groupTasks(g.ID)
	require.Len(t, tasks, 2)
	var child store.AgentGroupTask
	for _, task := range tasks {
		if task.TaskID != root.TaskID {
			child = task
		}
	}
	require.Equal(t, root.TaskID, *child.ParentTaskID)
	require.Equal(t, root.RootTaskID, child.RootTaskID, "chained to the assigning task's root")
	require.Equal(t, scout.ID, deref(child.OwnerAgentID))

	require.NoError(t, h.svc.StopTask(h.ctx, root.TaskID, "project on hold"))

	// Both tasks paused with the visible reason; both owners interrupted.
	for _, taskID := range []string{root.TaskID, child.TaskID} {
		task := h.taskByID(taskID)
		require.Equal(t, store.GroupTaskStatePaused, task.State)
		require.Equal(t, "project on hold", deref(task.StopReason))
	}
	require.Equal(t, 2, h.rt.cancelCount(), "both active member turns are interrupted")

	// The reason is visible in the transcript.
	var found bool
	for _, r := range h.messagesOf(g.ID, store.GroupMessageKindChange) {
		if payloadText(r) == "Task paused: project on hold" {
			found = true
		}
	}
	require.True(t, found)
}

func TestTasks_ContinueResumesPausedTask(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	task := h.groupTasks(g.ID)[0]
	require.NoError(t, h.svc.StopTask(h.ctx, task.TaskID, "hold"))
	revision := h.taskByID(task.TaskID).Revision

	require.NoError(t, h.svc.ContinueTask(h.ctx, task.TaskID, "please resume"))

	// The task keeps its instruction and owner and is dispatched again.
	updated := h.taskByID(task.TaskID)
	require.Equal(t, atlas.ID, deref(updated.OwnerAgentID))
	require.Equal(t, task.Instruction, updated.Instruction)
	require.Equal(t, store.GroupTaskStateRunning, updated.State)
	require.Greater(t, updated.Revision, revision)
	require.Equal(t, 2, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(1))

	// The continue message rode the packet as a follow-up with the task
	// header (the member session's log is never replayed).
	req := h.rt.submitAt(1)
	require.Contains(t, req.Packet.Request, "[task "+task.TaskID+"]")
	require.Contains(t, req.Packet.Request, "please resume")

	// The continue message landed in the transcript as a user row linked
	// to the task.
	var found bool
	for _, r := range h.messagesOf(g.ID, store.GroupMessageKindMessage) {
		if r.AuthorKind == store.GroupAuthorKindUser && r.TaskID != nil && *r.TaskID == task.TaskID && payloadText(r) == "please resume" {
			found = true
		}
	}
	require.True(t, found)

	// Completing the resumed turn without a result settles it completed
	// (design D1).
	h.rt.finishTurn(1, nil)
	requireEventually(t, "resumed turn settles", func() bool {
		return h.taskByID(task.TaskID).State == store.GroupTaskStateCompleted
	})
}

func TestTasks_ContinueResumesStoppedBranch(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)

	// The stop pauses the whole branch; the continue restarts it.
	require.NoError(t, h.svc.StopTask(h.ctx, root.TaskID, "hold"))
	require.Equal(t, 2, h.rt.cancelCount(), "both active member turns are interrupted")

	require.NoError(t, h.svc.ContinueTask(h.ctx, root.TaskID, "please resume"))

	// Both re-queued and dispatched within the budget: the named task
	// first, the descendant through the drain loop (design D4).
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(root.TaskID).State)
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(child.TaskID).State)
	require.Nil(t, h.taskByID(child.TaskID).StopReason)
	require.Equal(t, 4, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(2))
	require.Equal(t, scout.ID, h.submitAgentID(3))

	// The continue message rode the root's packet as a follow-up; the
	// child re-dispatched on its standing instruction.
	rootReq := h.rt.submitAt(2)
	require.Contains(t, rootReq.Packet.Request, "[task "+root.TaskID+"]")
	require.Contains(t, rootReq.Packet.Request, "please resume")
	childReq := h.rt.submitAt(3)
	require.Contains(t, childReq.Packet.Request, "[task "+child.TaskID+"]")
	require.NotContains(t, childReq.Packet.Request, "please resume")
}

func TestTasks_ContinueResumesFailedBranch(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)

	// Both turns fail: the branch is stopped (paused or failed), so the
	// continue restarts it like a paused one.
	h.rt.finishTurn(0, errors.New("root blew up"))
	requireEventually(t, "root failed", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateFailed
	})
	h.rt.finishTurn(1, errors.New("child blew up"))
	requireEventually(t, "child failed", func() bool {
		return h.taskByID(child.TaskID).State == store.GroupTaskStateFailed
	})

	require.NoError(t, h.svc.ContinueTask(h.ctx, root.TaskID, "try again"))

	// Both re-queued and dispatched within the budget: the named task
	// first, the descendant through the drain loop (design D4).
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(root.TaskID).State)
	require.Nil(t, h.taskByID(root.TaskID).StopReason)
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(child.TaskID).State)
	require.Nil(t, h.taskByID(child.TaskID).StopReason)
	require.Equal(t, 4, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(2))
	require.Equal(t, scout.ID, h.submitAgentID(3))

	// The recorded failure stays until the next terminal outcome
	// overwrites it.
	require.NotNil(t, h.taskByID(root.TaskID).Error)
}

func TestTasks_ContinueRequiresStopped(t *testing.T) {
	// The continue gate follows the stopped definition (paused or failed,
	// align-group-task-lifecycle): active work and terminal states refuse.
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	task := h.groupTasks(g.ID)[0]

	err := h.svc.ContinueTask(h.ctx, task.TaskID, "resume")
	require.Error(t, err, "a running task is not continuable")
	require.Contains(t, err.Error(), "only a stopped task continues")

	// A turn without error and without a result completes the task.
	h.rt.finishTurn(0, nil)
	requireEventually(t, "task completes", func() bool {
		return h.taskByID(task.TaskID).State == store.GroupTaskStateCompleted
	})
	err = h.svc.ContinueTask(h.ctx, task.TaskID, "resume")
	require.Error(t, err, "a completed task is not continuable")

	// A failed dispatch leaves the next task queued: still active work.
	h.rt.mu.Lock()
	h.rt.submitErr = errors.New("runtime down")
	h.rt.mu.Unlock()
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	// The store orders tasks by random hex id — pick the queued one by
	// state, never by position.
	var queued store.AgentGroupTask
	for _, t := range h.groupTasks(g.ID) {
		if t.State == store.GroupTaskStateQueued {
			queued = t
		}
	}
	require.Equal(t, store.GroupTaskStateQueued, h.taskByID(queued.TaskID).State)
	err = h.svc.ContinueTask(h.ctx, queued.TaskID, "resume")
	require.Error(t, err, "a queued task is not continuable")

	// A waiting parent and a cancelled child are not continuable either.
	h2, g2, atlas2, _, _ := rosteredHarness(t)
	require.NoError(t, h2.svc.Send(h2.ctx, SendRequest{GroupID: g2.ID, Text: "@Atlas run the project"}))
	root := h2.groupTasks(g2.ID)[0]
	assignThroughTool(t, h2, g2.ID, atlas2.ID, "Scout", "child work")
	child := childOf(t, h2, g2.ID, root.TaskID)

	// The root fails while its child is still running; cancelling the
	// failed root ends the open child with it (design D5).
	h2.rt.finishTurn(0, errors.New("root blew up"))
	requireEventually(t, "root fails", func() bool {
		return h2.taskByID(root.TaskID).State == store.GroupTaskStateFailed
	})
	require.NoError(t, h2.svc.CancelTask(h2.ctx, root.TaskID))
	require.Equal(t, store.GroupTaskStateCancelled, h2.taskByID(child.TaskID).State)
	err = h2.svc.ContinueTask(h2.ctx, child.TaskID, "resume")
	require.Error(t, err, "a cancelled task is not continuable")

	// A root whose turn ended while its child is open waits, and waits
	// are active work too.
	h3, g3, atlas3, _, _ := rosteredHarness(t)
	require.NoError(t, h3.svc.Send(h3.ctx, SendRequest{GroupID: g3.ID, Text: "@Atlas run the project"}))
	root3 := h3.groupTasks(g3.ID)[0]
	assignThroughTool(t, h3, g3.ID, atlas3.ID, "Scout", "child work")
	h3.rt.finishTurn(0, nil)
	requireEventually(t, "root waits on its child", func() bool {
		return h3.taskByID(root3.TaskID).State == store.GroupTaskStateWaiting
	})
	err = h3.svc.ContinueTask(h3.ctx, root3.TaskID, "resume")
	require.Error(t, err, "a waiting task is not continuable")
}

func TestTasks_ReassignMovesOwnership(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	task := h.groupTasks(g.ID)[0]
	revision := task.Revision

	require.NoError(t, h.svc.ReassignTask(h.ctx, task.TaskID, scout.ID))

	updated := h.taskByID(task.TaskID)
	require.Equal(t, scout.ID, deref(updated.OwnerAgentID), "the task's owner changes")
	require.Greater(t, updated.Revision, revision, "its revision advances")
	require.Equal(t, store.GroupTaskStateRunning, updated.State)
	require.Equal(t, 2, h.rt.submitCount())
	require.Equal(t, scout.ID, h.submitAgentID(1), "the new owner receives the task")
	require.Equal(t, h.rt.sessionFor(atlas.ID), h.rt.cancelAt(0), "the previous owner's turn is interrupted")

	// The ownership change is visible in the transcript.
	assigned := h.messagesOf(g.ID, store.GroupMessageKindAssigned)
	require.Len(t, assigned, 1)
	require.Equal(t, "Assigned to Scout", payloadText(assigned[0]))

	// The new owner's packet carries the task header.
	req := h.rt.submitAt(1)
	require.Contains(t, req.Packet.Request, "[task "+task.TaskID+"]")
}

func TestTasks_ReassignRequiresOpenTaskAndRosterMember(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	task := h.groupTasks(g.ID)[0]

	err := h.svc.ReassignTask(h.ctx, task.TaskID, "ghost")
	require.Error(t, err)
	require.ErrorIs(t, err, store.ErrNotFound)

	// Terminal tasks are not reassignable: stop, then cancel (the
	// stopped-chain gate, design D5) to reach the terminal state.
	require.NoError(t, h.svc.StopTask(h.ctx, task.TaskID, "hold"))
	require.NoError(t, h.svc.CancelTask(h.ctx, task.TaskID))
	err = h.svc.ReassignTask(h.ctx, task.TaskID, "ghost")
	require.Error(t, err)
}

func TestTasks_ReassignRestartsStoppedDescendants(t *testing.T) {
	h, g, atlas, scout, jono := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	child := childOf(t, h, g.ID, root.TaskID)

	// The child's turn fails; the root is then stopped.
	h.rt.finishTurn(1, errors.New("child blew up"))
	requireEventually(t, "child failed", func() bool {
		return h.taskByID(child.TaskID).State == store.GroupTaskStateFailed
	})
	require.NoError(t, h.svc.StopTask(h.ctx, root.TaskID, "hold"))

	require.NoError(t, h.svc.ReassignTask(h.ctx, root.TaskID, jono.ID))

	// The root moves and re-dispatches; the failed child re-queues with it
	// and drains back to its own owner (design D4).
	updated := h.taskByID(root.TaskID)
	require.Equal(t, jono.ID, deref(updated.OwnerAgentID))
	require.Equal(t, store.GroupTaskStateRunning, updated.State)
	restarted := h.taskByID(child.TaskID)
	require.Equal(t, store.GroupTaskStateRunning, restarted.State)
	require.Nil(t, restarted.StopReason)
	require.Equal(t, 4, h.rt.submitCount())
	require.Equal(t, jono.ID, h.submitAgentID(2), "the new owner receives the task")
	require.Equal(t, scout.ID, h.submitAgentID(3), "the restarted child returns to its own owner")
}

func TestTasks_CancelEndsRootChain(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run it"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	var child store.AgentGroupTask
	for _, task := range h.groupTasks(g.ID) {
		if task.TaskID != root.TaskID {
			child = task
		}
	}

	// The root's turn fails; the child keeps running its own turn.
	h.rt.finishTurn(0, errors.New("root blew up"))
	requireEventually(t, "root failed", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateFailed
	})

	// Cancelling the failed root ends every open task of the same root.
	require.NoError(t, h.svc.CancelTask(h.ctx, root.TaskID))
	require.Equal(t, store.GroupTaskStateFailed, h.taskByID(root.TaskID).State, "terminal tasks stay terminal")
	require.Equal(t, store.GroupTaskStateCancelled, h.taskByID(child.TaskID).State, "the open child is cancelled too")
	require.Equal(t, 1, h.rt.cancelCount(), "the child's active turn is interrupted; no further member turns start")
}

func TestTasks_CancelRefusesRunning(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	task := h.groupTasks(g.ID)[0]

	// The cancel is refused and the task keeps running (spec "Cancel
	// refuses active work"): Stop remains the control that pauses it.
	err := h.svc.CancelTask(h.ctx, task.TaskID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancel is for stopped chains")
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(task.TaskID).State)
}

func TestTasks_CancelRefusesWaiting(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas run the project"}))
	root := h.groupTasks(g.ID)[0]
	assignThroughTool(t, h, g.ID, atlas.ID, "Scout", "child work")
	h.rt.finishTurn(0, nil)
	requireEventually(t, "root waits on its child", func() bool {
		return h.taskByID(root.TaskID).State == store.GroupTaskStateWaiting
	})

	// A waiting task is open work (a dependency wait), not a stopped chain.
	err := h.svc.CancelTask(h.ctx, root.TaskID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancel is for stopped chains")
	require.Equal(t, store.GroupTaskStateWaiting, h.taskByID(root.TaskID).State)
}

func TestTasks_CancelRefusesQueued(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@everyone go"}))
	require.Equal(t, 2, h.rt.submitCount())
	var queued store.AgentGroupTask
	for _, task := range h.groupTasks(g.ID) {
		if task.State == store.GroupTaskStateQueued {
			queued = task
		}
	}
	require.NotEmpty(t, queued.TaskID)

	err := h.svc.CancelTask(h.ctx, queued.TaskID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cancel is for stopped chains")
	require.Equal(t, store.GroupTaskStateQueued, h.taskByID(queued.TaskID).State)
}

func TestTasks_CancelAllowsPaused(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	task := h.groupTasks(g.ID)[0]
	require.NoError(t, h.svc.StopTask(h.ctx, task.TaskID, "hold"))

	// A paused task is a stopped chain: the cancel ends it.
	require.NoError(t, h.svc.CancelTask(h.ctx, task.TaskID))
	require.Equal(t, store.GroupTaskStateCancelled, h.taskByID(task.TaskID).State)
}

func TestTasks_Budget_ThirdAssignmentQueues(t *testing.T) {
	h, g, atlas, scout, jono := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@everyone go"}))
	require.Len(t, h.groupTasks(g.ID), 3)
	require.Equal(t, 2, h.rt.submitCount(), "two member assignments are active; the third waits")
	// The first dispatch saw only itself running (persist-before-dispatch
	// is visible at submit time); the second saw two running.
	require.Equal(t, []string{"running"}, h.rt.statesAtSubmit[0])
	require.Equal(t, []string{"running", "running"}, h.rt.statesAtSubmit[1])
	_ = atlas
	_ = scout
	_ = jono

	// One active assignment finishes; the queued task drains.
	h.rt.finishTurn(0, nil)
	requireEventually(t, "third assignment drains", func() bool {
		return h.rt.submitCount() == 3
	})
	// The third dispatch's submit-time snapshot: the finished turn left its
	// task completed (design D1: a no-result turn completes), two running
	// now (the state multiset, in store order).
	states := append([]string(nil), h.rt.statesAtSubmit[2]...)
	require.Len(t, states, 3)
	counts := map[string]int{}
	for _, st := range states {
		counts[st]++
	}
	require.Equal(t, 2, counts[store.GroupTaskStateRunning])
	require.Equal(t, 1, counts[store.GroupTaskStateCompleted])
	require.Equal(t, 0, counts[store.GroupTaskStateWaiting])
	require.Equal(t, 0, counts[store.GroupTaskStateQueued])
}

func TestTasks_Budget_EightChildrenPerRoot(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas delegate"}))

	for i := 0; i < 8; i++ {
		assignThroughTool(t, h, g.ID, atlas.ID, "Scout", fmt.Sprintf("child %d", i))
	}
	require.Len(t, h.groupTasks(g.ID), 9, "the root plus eight children")

	// The ninth automatic child assignment exceeds the per-root budget and
	// is refused to the model.
	tools := toInvokable(t, mustTools(t, h, g.ID, atlas.ID))
	_, err := tools[1].InvokableRun(h.ctx, `{"member":"Scout","instruction":"one too many"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "budget")
}

func TestTasks_ResultToolCompletesTaskAndWritesResult(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	task := h.groupTasks(g.ID)[0]

	resultThroughTool(t, h, g.ID, atlas.ID, "the corpus is summarized")
	_ = task

	updated := h.taskByID(h.groupTasks(g.ID)[0].TaskID)
	require.Equal(t, store.GroupTaskStateCompleted, updated.State)

	// The result landed in the transcript as the member's authored result
	// row, linked to the task.
	results := h.messagesOf(g.ID, store.GroupMessageKindResult)
	require.Len(t, results, 1)
	require.Equal(t, store.GroupAuthorKindAgent, results[0].AuthorKind)
	require.Equal(t, atlas.ID, deref(results[0].AuthorAgentID))
	require.NotNil(t, results[0].TaskID)
	require.Equal(t, updated.TaskID, *results[0].TaskID)
	require.Equal(t, "the corpus is summarized", payloadText(results[0]))

	// The turn's later end is stale-guarded: the completed task stays
	// completed.
	h.rt.finishTurn(0, nil)
	require.Equal(t, store.GroupTaskStateCompleted, h.taskByID(updated.TaskID).State)
}

func TestTasks_ResultToolGuards(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))

	tools := toInvokable(t, mustTools(t, h, g.ID, atlas.ID))

	// A result for a task outside the group is refused.
	_, err := tools[2].InvokableRun(h.ctx, `{"result":"x","task_id":"elsewhere"}`)
	require.Error(t, err)

	// A second completion of the same task is refused.
	resultThroughTool(t, h, g.ID, atlas.ID, "done")
	_, err = tools[2].InvokableRun(h.ctx, `{"result":"again"}`)
	require.Error(t, err, "the task is already completed")
}

func TestTasks_HistoryTool(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))

	tools := toInvokable(t, mustTools(t, h, g.ID, atlas.ID))
	out := h.invokeTool(t, tools[0], `{"limit":2}`)
	require.Contains(t, out, "[user]", "the transcript renders author-labeled rows")
	require.Contains(t, out, "@Atlas work")

	// Paging: the next page reaches the older rows.
	rows := h.transcript(g.ID)
	out = h.invokeTool(t, tools[0], fmt.Sprintf(`{"limit":2,"before_seq":%d}`, rows[len(rows)-1].Seq))
	require.NotEmpty(t, out)
	require.Contains(t, out, "[coordinator]", "the older page holds the roster change lines")
}

func TestTasks_AssignToolRequiresRosterMember(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	tools := toInvokable(t, mustTools(t, h, g.ID, atlas.ID))
	_, err := tools[1].InvokableRun(h.ctx, `{"member":"Ghost","instruction":"x"}`)
	require.Error(t, err)
}

func TestTasks_AssignToolWithoutCallingTaskFails(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	tools := toInvokable(t, mustTools(t, h, g.ID, atlas.ID))
	_, err := tools[1].InvokableRun(h.ctx, `{"member":"Scout","instruction":"x"}`)
	require.Error(t, err, "no group turn is in flight")
}

func TestTasks_FailingDispatchLeavesQueuedAndDrainPicksItUp(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	h.rt.mu.Lock()
	h.rt.submitErr = errors.New("runtime down")
	h.rt.mu.Unlock()

	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	require.Equal(t, 0, h.rt.submitCount())
	task := h.groupTasks(g.ID)[0]
	require.Equal(t, store.GroupTaskStateQueued, task.State, "the failing dispatch leaves the task queued")

	h.rt.mu.Lock()
	h.rt.submitErr = nil
	h.rt.mu.Unlock()
	require.NoError(t, h.svc.Drain(h.ctx))

	require.Equal(t, 1, h.rt.submitCount(), "Drain picks the queued task up")
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(task.TaskID).State)
	require.Equal(t, atlas.ID, h.submitAgentID(0))
}

func TestTasks_SubmitGroupUnavailableIsQueuedNotFailed(t *testing.T) {
	// A source-less runtime (the groups service not yet bound) must not
	// fail the task: the ref bound here is empty, so the fake manager
	// surfaces agents.ErrGroupTurnsUnavailable only in the real runtime —
	// this test pins the revert-to-queued path through the fake's error.
	h, g, _, _, _ := rosteredHarness(t)
	h.rt.mu.Lock()
	h.rt.submitErr = agents.ErrGroupTurnsUnavailable
	h.rt.mu.Unlock()
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	require.Equal(t, store.GroupTaskStateQueued, h.groupTasks(g.ID)[0].State)
}
