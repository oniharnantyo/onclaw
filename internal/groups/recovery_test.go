package groups

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
)

// Restart recovery tests (add-agent-groups design D8, spec "Group events and
// restart recovery"): queued dispatches survive a restart and drain when the
// app starts again; crashed mid-turn dispatches re-queue; paused tasks stay
// paused; stranded waiting rows convert by the dependency-wait rule
// (align-group-task-lifecycle design D7); Drain is idempotent and a no-op on
// empty state.

func TestDrain_EmptyStateIsNoOp(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.svc.Drain(h.ctx))
	require.Equal(t, 0, h.rt.submitCount())
}

func TestDrain_QueuedTaskReDispatches(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	// A budget-blocked queued task: two active assignments hold the third.
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@everyone go"}))
	require.Equal(t, 2, h.rt.submitCount())
	var queued store.AgentGroupTask
	for _, task := range h.groupTasks(g.ID) {
		if task.State == store.GroupTaskStateQueued {
			queued = task
		}
	}
	require.NotEmpty(t, queued.TaskID)

	// Restart: the in-memory dispatches are gone and the budget starts
	// empty. Drain re-queues the crashed running dispatches and re-dispatches
	// while the budget allows — the queued task drains either immediately or
	// as a re-dispatched turn finishes and frees its slot.
	h.restart()
	require.NoError(t, h.svc.Drain(h.ctx))
	require.GreaterOrEqual(t, h.rt.submitCount(), 2)

	requireEventually(t, "queued task drains", func() bool {
		if h.taskByID(queued.TaskID).State == store.GroupTaskStateRunning {
			return true
		}
		h.rt.finishOnePending(nil) // free a slot for the queued task
		return false
	})
}

func TestDrain_CrashedRunningTaskRequeues(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	task := h.groupTasks(g.ID)[0]
	require.Equal(t, store.GroupTaskStateRunning, task.State)

	// Restart mid-turn: no inflight record survives, so the persisted
	// running state is a crashed dispatch. Drain re-queues and re-dispatches.
	transcriptBefore := len(h.transcript(g.ID))
	h.restart()
	require.NoError(t, h.svc.Drain(h.ctx))

	// The transcript with its authorship survives the restart (spec: shared
	// authored transcript).
	require.Len(t, h.transcript(g.ID), transcriptBefore)
	require.Equal(t, 1, h.rt.submitCount())
	updated := h.taskByID(task.TaskID)
	require.Equal(t, store.GroupTaskStateRunning, updated.State)
	require.Greater(t, updated.Revision, task.Revision, "the re-dispatch bumps the revision")
	require.Equal(t, atlas.ID, h.submitAgentID(0), "the work returns to its owner")
}

func TestDrain_PausedTaskStaysPaused(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	task := h.groupTasks(g.ID)[0]
	require.NoError(t, h.svc.StopTask(h.ctx, task.TaskID, "on hold"))

	// A restart must not undo the user's Stop: Drain leaves paused tasks
	// alone.
	h.restart()
	require.NoError(t, h.svc.Drain(h.ctx))

	updated := h.taskByID(task.TaskID)
	require.Equal(t, store.GroupTaskStatePaused, updated.State)
	require.Equal(t, "on hold", deref(updated.StopReason))
	require.Equal(t, 0, h.rt.submitCount(), "no dispatch starts for a paused task")
}

// TestDrain_ChildlessWaitingParkCompletes pins the legacy-park conversion
// (align design D7, spec "Group task lifecycle"): a waiting row with no
// children was parked by the old turn-end semantics — under the dependency
// wait there is nothing left to wait for, so boot completes it.
func TestDrain_ChildlessWaitingParkCompletes(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	parent := &store.AgentGroupTask{
		TaskID:       "legacy-park",
		GroupID:      g.ID,
		RootTaskID:   "legacy-park",
		OwnerAgentID: &atlas.ID,
		Instruction:  "an old no-result turn",
		State:        store.GroupTaskStateWaiting,
		Revision:     1,
	}
	require.NoError(t, h.st.Groups().CreateTask(h.ctx, parent))

	h.restart()
	require.NoError(t, h.svc.Drain(h.ctx))

	updated := h.taskByID(parent.TaskID)
	require.Equal(t, store.GroupTaskStateCompleted, updated.State)
	require.Equal(t, int64(2), updated.Revision)
	require.Equal(t, 0, h.rt.submitCount(), "a converted park never re-dispatches")
}

// TestDrain_AllChildrenTerminalWaitingRequeues pins the missed-re-queue
// recovery (align design D7): the crash landed between the child's terminal
// write and the parent's re-queue — boot re-queues the waiting parent and
// the drain dispatches it.
func TestDrain_AllChildrenTerminalWaitingRequeues(t *testing.T) {
	h, g, atlas, _, _ := rosteredHarness(t)
	parent := &store.AgentGroupTask{
		TaskID:       "crashed-parent",
		GroupID:      g.ID,
		RootTaskID:   "crashed-parent",
		OwnerAgentID: &atlas.ID,
		Instruction:  "integrate the child",
		State:        store.GroupTaskStateWaiting,
		Revision:     1,
	}
	require.NoError(t, h.st.Groups().CreateTask(h.ctx, parent))
	child := &store.AgentGroupTask{
		TaskID:       "done-child",
		GroupID:      g.ID,
		ParentTaskID: &parent.TaskID,
		RootTaskID:   parent.RootTaskID,
		OwnerAgentID: &atlas.ID,
		Instruction:  "child work",
		State:        store.GroupTaskStateCompleted,
		Revision:     2,
	}
	require.NoError(t, h.st.Groups().CreateTask(h.ctx, child))

	h.restart()
	require.NoError(t, h.svc.Drain(h.ctx))

	updated := h.taskByID(parent.TaskID)
	require.Equal(t, store.GroupTaskStateRunning, updated.State, "the re-queued parent drains")
	require.Equal(t, int64(3), updated.Revision)
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, atlas.ID, h.submitAgentID(0))
}

// TestDrain_OpenChildWaitingUntouched pins the live dependency wait (align
// design D7): a waiting parent with an open child keeps waiting through a
// restart — only childless parks and all-children-terminal rows convert.
func TestDrain_OpenChildWaitingUntouched(t *testing.T) {
	h, g, atlas, scout, _ := rosteredHarness(t)
	parent := &store.AgentGroupTask{
		TaskID:       "waiting-parent",
		GroupID:      g.ID,
		RootTaskID:   "waiting-parent",
		OwnerAgentID: &atlas.ID,
		Instruction:  "await the child",
		State:        store.GroupTaskStateWaiting,
		Revision:     3,
	}
	require.NoError(t, h.st.Groups().CreateTask(h.ctx, parent))
	child := &store.AgentGroupTask{
		TaskID:       "open-child",
		GroupID:      g.ID,
		ParentTaskID: &parent.TaskID,
		RootTaskID:   parent.RootTaskID,
		OwnerAgentID: &scout.ID,
		Instruction:  "child work",
		State:        store.GroupTaskStateQueued,
		Revision:     1,
	}
	require.NoError(t, h.st.Groups().CreateTask(h.ctx, child))

	h.restart()
	require.NoError(t, h.svc.Drain(h.ctx))

	updated := h.taskByID(parent.TaskID)
	require.Equal(t, store.GroupTaskStateWaiting, updated.State, "the dependency wait is live")
	require.Equal(t, int64(3), updated.Revision, "Drain wrote nothing to the waiting parent")
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(child.TaskID).State, "the open child re-dispatched normally")
}

func TestDrain_DoubleDrainIsSafe(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "@Atlas work"}))
	h.restart()

	require.NoError(t, h.svc.Drain(h.ctx))
	require.Equal(t, 1, h.rt.submitCount())
	// The second Drain sees the live dispatch and writes nothing.
	require.NoError(t, h.svc.Drain(h.ctx))
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, store.GroupTaskStateRunning, h.taskByID(h.groupTasks(g.ID)[0].TaskID).State)
}

func TestDrain_LiveRunningTaskUntouched(t *testing.T) {
	h, g, _, _, _ := rosteredHarness(t)
	require.NoError(t, h.svc.Send(h.ctx, SendRequest{GroupID: g.ID, Text: "hello"}))
	task := h.groupTasks(g.ID)[0]
	revision := task.Revision

	// Drain while the dispatch is live: nothing re-queues.
	require.NoError(t, h.svc.Drain(h.ctx))
	require.Equal(t, 1, h.rt.submitCount())
	require.Equal(t, revision, h.taskByID(task.TaskID).Revision)
}
