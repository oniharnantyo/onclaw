package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The durable-approval tests (tasks.md 4.2/4.4, spec chat-approvals): a
// dangerous command parks the run with a persisted approval_required, the
// pending state survives a reload (derived from the log), a stale id is a
// conflict, an approval resumes the run so the command executes, a denial
// feeds the refusal to the model, a cancel clears the pending approval, and
// a cold resume works after the process lost the parked run.

// approvalFixture wires the fixture for shell-gate flows: a workspace-rooted
// agent whose model first calls the execute tool with command, then — once
// the tool result is in context — answers finally.
func approvalFixture(t *testing.T, root string, reply func(input []*schema.Message) fakeReply, opts ...Option) *fixture {
	t.Helper()
	opts = append(opts, WithWorkspaces(workspaces.NewManager(root)))
	f := newFixture(t, reply, opts...)
	agent := f.agent
	agent.Slug = "tester"
	require.NoError(t, f.store.Agents().Update(t.Context(), &agent))
	f.agent = agent
	return f
}

// dangerousCall is the model's first response: one execute call for a forced
// recursive deletion inside the workspace (mount contract spelling).
func dangerousCall(id, target string) fakeReply {
	return toolCallsReply(id, ToolExecute, `{"command":"rm -rf /workspace/`+target+`"}`)
}

// parkOnApproval drives a turn into the approval pause and returns the turn
// handle (it stays open until the decision or a cancel) with the pending
// event.
func parkOnApproval(t *testing.T, f *fixture, sess sessions.Session) (*Turn, sessions.Event) {
	t.Helper()
	turn, err := f.mgr.Send(t.Context(), sess.ID, "clean up build output")
	require.NoError(t, err)

	events := f.awaitEvent(t, sess.ID, sessions.KindApprovalRequired)
	require.Len(t, events, 2)
	require.Equal(t, string(sessions.KindUserMessage), string(events[0].Kind))
	pending := events[1]
	require.NotNil(t, pending.Approval)
	require.NotEmpty(t, pending.Approval.InterruptID)
	require.Contains(t, pending.Approval.Command, "doomed.txt")
	require.NotEmpty(t, pending.Approval.CheckpointID)
	require.Equal(t, turn.TurnID(), pending.TurnID)

	// The turn is parked: still busy, no terminal event, the Turn open.
	_, err = f.mgr.Send(t.Context(), sess.ID, "while parked")
	require.ErrorIs(t, err, ErrTurnInProgress)
	// The run's checkpoint is on disk.
	_, ok, err := f.store.Checkpoints().Get(t.Context(), pending.Approval.CheckpointID)
	require.NoError(t, err)
	require.True(t, ok, "the interrupted run's checkpoint is persisted")

	// The pending state derives from a fresh replay of the log.
	derived := pendingApprovalFromEvents(mustEvents(t, f, sess.ID))
	require.NotNil(t, derived)
	require.Equal(t, pending.Approval.InterruptID, derived.interruptID)
	require.Equal(t, pending.Approval.CheckpointID, derived.checkpointID)
	return turn, pending
}

// awaitEvent waits until the session's persisted log ends with an event of
// the kind (the parking happens on the turn goroutine).
func (f *fixture) awaitEvent(t *testing.T, sessionID string, kind sessions.Kind) []sessions.Event {
	t.Helper()
	require.Eventually(t, func() bool {
		events := mustEvents(t, f, sessionID)
		return len(events) > 0 && events[len(events)-1].Kind == kind
	}, 5*time.Second, 10*time.Millisecond, "waiting for a persisted %s event", kind)
	return mustEvents(t, f, sessionID)
}

// mustEvents replays the session log, failing the test on read errors.
func mustEvents(t *testing.T, f *fixture, sessionID string) []sessions.Event {
	t.Helper()
	events, err := f.sessions.Events(t.Context(), sessionID)
	require.NoError(t, err)
	return events
}

// TestApproval_PendingSurvivesReloadAndStaleIDsConflict covers the
// derivation contract: the pending approval is visible after a fresh store
// replay, a stale interrupt id is a conflict that changes nothing, and an
// already-resolved decision conflicts too.
func TestApproval_PendingSurvivesReloadAndStaleIDsConflict(t *testing.T) {
	f := approvalFixture(t, t.TempDir(), func(input []*schema.Message) fakeReply {
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "handled", promptTokens: 10, completionTokens: 2}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, pending := parkOnApproval(t, f, sess)

	// A stale id is a conflict; the pending state is untouched.
	err = f.mgr.ResolveApproval(t.Context(), sess.ID, "stale-interrupt-id", true)
	require.ErrorIs(t, err, ErrApprovalNotPending)
	derived := pendingApprovalFromEvents(mustEvents(t, f, sess.ID))
	require.NotNil(t, derived, "the conflict left the pending state intact")
	require.Equal(t, pending.Approval.InterruptID, derived.interruptID)

	// The decision resolves; the turn runs to completion.
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())

	// A second resolve of the same interrupt is a conflict.
	err = f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true)
	require.ErrorIs(t, err, ErrApprovalNotPending)

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindApprovalRequired),
		string(sessions.KindApprovalResolved),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, sessions.ApprovalDenied, events[2].Approval.Outcome)
}

// resolveGateStore makes the concurrent double-resolve interleaving
// deterministic: the first Events read parks after replaying the log (one
// resolve holds a pre-resolution snapshot), and the first Append parks
// before persisting (the other resolve sits mid-flight — guard claimed,
// running record not yet replaced — which is the window the resolving guard
// closes).
type resolveGateStore struct {
	sessions.Service
	eventsEntered  chan struct{}
	releaseEvents  chan struct{}
	appendCalls    chan struct{}
	releaseAppends chan struct{}
	spent          atomic.Bool
	appendSpent    atomic.Bool
}

func (g *resolveGateStore) Events(ctx context.Context, sessionID string) ([]sessions.Event, error) {
	if !g.spent.CompareAndSwap(false, true) {
		return g.Service.Events(ctx, sessionID)
	}
	events, err := g.Service.Events(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	g.eventsEntered <- struct{}{}
	<-g.releaseEvents
	return events, nil
}

func (g *resolveGateStore) Append(ctx context.Context, sessionID string, events []sessions.Event) error {
	g.appendCalls <- struct{}{}
	if !g.appendSpent.CompareAndSwap(false, true) {
		return g.Service.Append(ctx, sessionID, events)
	}
	<-g.releaseAppends
	return g.Service.Append(ctx, sessionID, events)
}

// TestResolveApproval_ConcurrentResolveConflicts pins the resolve guard: a
// resolve holding a pre-resolution snapshot of the log (read before another
// resolve's approval_resolved landed) conflicts like the sequential case,
// and exactly one approval_resolved is appended. The gate store makes the
// interleaving deterministic — no timing.
func TestResolveApproval_ConcurrentResolveConflicts(t *testing.T) {
	f := approvalFixture(t, t.TempDir(), func(input []*schema.Message) fakeReply {
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "handled", promptTokens: 10, completionTokens: 2}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, pending := parkOnApproval(t, f, sess)

	gate := &resolveGateStore{
		Service:        f.mgr.sessions,
		eventsEntered:  make(chan struct{}, 1),
		releaseEvents:  make(chan struct{}),
		appendCalls:    make(chan struct{}, 4),
		releaseAppends: make(chan struct{}),
	}
	f.mgr.sessions = gate

	// Racer A reads the log and parks holding the pre-resolution snapshot.
	racing := make(chan error, 1)
	go func() {
		racing <- f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false)
	}()
	<-gate.eventsEntered

	// Racer B runs to its approval_resolved append and parks there: its
	// guard is claimed, the running record not yet replaced — the exact
	// window the guard closes.
	winner := make(chan error, 1)
	go func() {
		winner <- f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false)
	}()
	<-gate.appendCalls

	// Release A into the window: it must conflict on the guard instead of
	// appending a second resolution.
	close(gate.releaseEvents)
	require.ErrorIs(t, <-racing, ErrApprovalNotPending,
		"the stale-snapshot resolve conflicts instead of appending a second resolution")

	// Let B land its resolution and finish the resumed turn.
	close(gate.releaseAppends)
	require.NoError(t, <-winner)
	require.NoError(t, turn.Wait())

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindApprovalRequired),
		string(sessions.KindApprovalResolved),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events), "exactly one approval_resolved lands")
}

// TestApproval_ApproveRunsCommand covers the approve path end to end: the
// decision resumes the run from the checkpoint, the command really executes
// (the file is gone), and the turn continues to a normal completion.
func TestApproval_ApproveRunsCommand(t *testing.T) {
	root := t.TempDir()
	f := approvalFixture(t, root, func(input []*schema.Message) fakeReply {
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "the build output is gone", promptTokens: 10, completionTokens: 2}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	doomed := filepath.Join(root, "tester", "doomed.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(doomed), 0o755))
	require.NoError(t, os.WriteFile(doomed, []byte("old build"), 0o644))

	turn, pending := parkOnApproval(t, f, sess)
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))
	require.NoError(t, turn.Wait())

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindApprovalRequired),
		string(sessions.KindApprovalResolved),
		string(sessions.KindToolActivity),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, sessions.ApprovalApproved, events[2].Approval.Outcome)

	// The command really executed inside the jail.
	_, err = os.Stat(doomed)
	require.True(t, os.IsNotExist(err), "the approved command ran")

	// The terminal tool record carries the full call arguments — the resumed
	// run seeded its drain from the interrupted run's call memory.
	tool := events[3].Tool
	require.NotNil(t, tool)
	require.Equal(t, "call-1", tool.CallID)
	require.Equal(t, ToolExecute, tool.Name)
	require.Equal(t, `{"command":"rm -rf /workspace/doomed.txt"}`, tool.Arguments)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status)
}

// TestApproval_DenyReportsRefusalToModel covers the deny path: the refusal
// is fed back as the tool's result, the command never runs, and the turn
// continues with the model informed.
func TestApproval_DenyReportsRefusalToModel(t *testing.T) {
	root := t.TempDir()
	f := approvalFixture(t, root, func(input []*schema.Message) fakeReply {
		var denial string
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				denial = msg.Content
			}
		}
		if denial == "" {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "understood, I will not run it", promptTokens: 10, completionTokens: 2}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	doomed := filepath.Join(root, "tester", "doomed.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(doomed), 0o755))
	require.NoError(t, os.WriteFile(doomed, []byte("keep me"), 0o644))

	turn, pending := parkOnApproval(t, f, sess)
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, sessions.ApprovalDenied, events[2].Approval.Outcome)

	// The refusal is the tool result the model read.
	tool := events[3].Tool
	require.NotNil(t, tool)
	require.Contains(t, tool.Result, "denied approval")
	require.Contains(t, tool.Result, "doomed.txt")

	// The command never executed.
	b, err := os.ReadFile(doomed)
	require.NoError(t, err)
	require.Equal(t, "keep me", string(b))

	// The model's final answer proves it read the refusal (the scripted
	// reply only fires when the denial was in context).
	require.Equal(t, "understood, I will not run it", events[4].Text)
}

// TestApproval_CancelClearsPending covers cancel-during-pending: the run is
// discarded, the decision is recorded as cancelled, the checkpoint and any
// unspent ledger entry are purged, and the session is released.
func TestApproval_CancelClearsPending(t *testing.T) {
	f := approvalFixture(t, t.TempDir(), func(input []*schema.Message) fakeReply {
		for _, msg := range input {
			if msg.Role == schema.User && strings.Contains(msg.Content, "after the cancel") {
				return fakeReply{text: "recovered", promptTokens: 1, completionTokens: 1}
			}
		}
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{err: errors.New("the parked run must never reach the model again")}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, pending := parkOnApproval(t, f, sess)

	// A decision is recorded but never spent (a resolve raced the cancel);
	// the cancel must purge it.
	require.NoError(t, backend.RecordDecision(t.Context(), f.store.Checkpoints(), pending.Approval.Command))

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindApprovalRequired),
		string(sessions.KindApprovalResolved),
		string(sessions.KindTurnCancelled),
	}, kindsOf(events))
	require.Equal(t, sessions.ApprovalCancelled, events[2].Approval.Outcome)

	// The checkpoint is gone and the ledger entry purged.
	_, ok, err := f.store.Checkpoints().Get(t.Context(), pending.Approval.CheckpointID)
	require.NoError(t, err)
	require.False(t, ok, "the discarded run's checkpoint is deleted")
	_, ok, err = f.store.Checkpoints().Get(t.Context(), backend.LedgerKey(pending.Approval.Command))
	require.NoError(t, err)
	require.False(t, ok, "the unspent decision is purged")

	// The session is released.
	next, err := f.mgr.Send(t.Context(), sess.ID, "after the cancel")
	require.NoError(t, err)
	require.NoError(t, next.Wait())
}

// TestApproval_CancelDuringResume covers the cancel on a resumed run: the
// resume carries its own two-level cancel surface, so stopping it lands the
// cancelled terminal and releases the session like any in-flight turn.
func TestApproval_CancelDuringResume(t *testing.T) {
	prev := cancelEscalationDelay
	cancelEscalationDelay = 100 * time.Millisecond
	t.Cleanup(func() { cancelEscalationDelay = prev })

	root := t.TempDir()
	gate := make(chan struct{})
	var streamCalls int
	f := approvalFixture(t, root, func(input []*schema.Message) fakeReply {
		for _, msg := range input {
			if msg.Role == schema.User && strings.Contains(msg.Content, "after the cancel") {
				return fakeReply{text: "recovered", promptTokens: 1, completionTokens: 1}
			}
		}
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "one two three", promptTokens: 10, completionTokens: 2}
	})
	// Only the RESUMED run's model call pauses mid-stream (the second Stream
	// of the turn): the original run must complete its interrupting call.
	f.model.midStream = func() {
		streamCalls++
		if streamCalls >= 2 {
			<-gate
		}
	}
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	collector := collect(t, f.mgr, sess.ID)

	turn, pending := parkOnApproval(t, f, sess)
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))

	// The resumed run is streaming again: stop it there.
	collector.waitDelta(t, 1)
	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)
	close(gate)

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, sessions.KindTurnCancelled, events[len(events)-1].Kind)
	require.Equal(t, turn.TurnID(), events[len(events)-1].TurnID)

	// The session is released.
	next, err := f.mgr.Send(t.Context(), sess.ID, "after the cancel")
	require.NoError(t, err)
	require.NoError(t, next.Wait())
}

// TestApproval_ColdResumeAfterProcessLoss covers the restart path: with the
// parked run's process state gone, a fresh runtime over the same store
// resumes the turn from the persisted checkpoint and the command executes.
func TestApproval_ColdResumeAfterProcessLoss(t *testing.T) {
	root := t.TempDir()
	f := approvalFixture(t, root, func(input []*schema.Message) fakeReply {
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "resumed and cleaned", promptTokens: 10, completionTokens: 2}
	})
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	doomed := filepath.Join(root, "tester", "doomed.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(doomed), 0o755))
	require.NoError(t, os.WriteFile(doomed, []byte("old build"), 0o644))

	_, pending := parkOnApproval(t, f, sess)

	// Process loss: a brand-new runtime over the same store. The parked
	// run's composed agent is gone; the resume rebuilds from the store and
	// drives Runner.Resume from the persisted checkpoint. The replacement
	// model answers the follow-up only when the tool result is in context.
	reg := &fakeRegistry{model: &fakeModel{reply: func(input []*schema.Message) fakeReply {
		for _, msg := range input {
			if msg.Role == schema.User && strings.Contains(msg.Content, "after the resume") {
				return fakeReply{text: "recovered", promptTokens: 1, completionTokens: 1}
			}
		}
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return fakeReply{err: errors.New("the resumed tool result never reached the model")}
		}
		return fakeReply{text: "resumed and cleaned", promptTokens: 10, completionTokens: 2}
	}}}
	// The browser is in the catalog: the resumed agent's open allowlist
	// constructs it on rebuild.
	m2 := newManager(f.store, f.sessions, reg, WithWorkspaces(workspaces.NewManager(root)), WithBrowserDir(t.TempDir()))

	require.NoError(t, m2.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))

	require.Eventually(t, func() bool {
		events := mustEvents(t, f, sess.ID)
		return len(events) > 0 && events[len(events)-1].Kind == sessions.KindTurnCompleted
	}, 10*time.Second, 20*time.Millisecond, "the cold-resumed turn must complete")

	events := mustEvents(t, f, sess.ID)
	require.Equal(t, sessions.ApprovalApproved, events[2].Approval.Outcome)
	tool := events[3].Tool
	require.NotNil(t, tool)
	require.Equal(t, sessions.ToolStatusCompleted, tool.Status)
	_, err = os.Stat(doomed)
	require.True(t, os.IsNotExist(err), "the approved command ran after the cold resume")

	// The session is released in the new runtime too.
	next, err := m2.Send(t.Context(), sess.ID, "after the resume")
	require.NoError(t, err)
	require.NoError(t, next.Wait())
}
