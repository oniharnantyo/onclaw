package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/sessions"
)

// The notification-hook tests (add-desktop-notifications tasks.md 3.1/3.2,
// design D2): a non-group turn's terminal fires WithTurnFinished exactly once
// — completed, failed, and cancelled alike, whatever the origin — a group-lane
// turn never fires it (the group reports at task level), and the park
// installs fire WithSessionParked with their details while the resolve paths
// never do.

// finishedCall is one WithTurnFinished callback's captured arguments.
type finishedCall struct {
	sessionID string
	origin    hooks.Origin
	outcome   TurnOutcome
	turnID    string
	// busyAtFire is the session's Busy state read inside the callback: the
	// callback must fire after the busy record cleared.
	busyAtFire bool
}

// finishedCollector records WithTurnFinished callbacks. Safe for the turn
// goroutine's writes and the test goroutine's reads.
type finishedCollector struct {
	mu  sync.Mutex
	got []finishedCall
}

func (c *finishedCollector) record(sessionID string, origin hooks.Origin, outcome TurnOutcome, turnID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, finishedCall{sessionID: sessionID, origin: origin, outcome: outcome, turnID: turnID})
}

// recordFull appends one fully built call — the Completed test's variant that
// also captures the Busy state read inside the callback.
func (c *finishedCollector) recordFull(call finishedCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, call)
}

func (c *finishedCollector) all() []finishedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]finishedCall(nil), c.got...)
}

// parkedCall is one WithSessionParked callback's captured arguments.
type parkedCall struct {
	sessionID   string
	kind        ParkKind
	interruptID string
	detail      string
}

// parkedCollector records WithSessionParked callbacks.
type parkedCollector struct {
	mu  sync.Mutex
	got []parkedCall
}

func (c *parkedCollector) record(sessionID string, kind ParkKind, interruptID, detail string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, parkedCall{sessionID: sessionID, kind: kind, interruptID: interruptID, detail: detail})
}

func (c *parkedCollector) all() []parkedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]parkedCall(nil), c.got...)
}

// awaitOneParked waits until exactly one parked callback has landed (the
// install fires after the park event persisted, on the parking goroutine) and
// returns it.
func awaitOneParked(t *testing.T, parked *parkedCollector) parkedCall {
	t.Helper()
	var call parkedCall
	require.Eventually(t, func() bool {
		got := parked.all()
		if len(got) != 1 {
			return false
		}
		call = got[0]
		return true
	}, 5*time.Second, 10*time.Millisecond, "exactly one parked callback for the park install")
	return call
}

// TestTurnFinished_Completed covers the user turn's terminal: exactly one
// callback, the user origin, the completed outcome, the turn id, and the
// busy-cleared ordering — the session is already free when the callback runs.
func TestTurnFinished_Completed(t *testing.T) {
	var f *fixture
	fin := &finishedCollector{}
	f = newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	}, WithTurnFinished(func(sessionID string, origin hooks.Origin, outcome TurnOutcome, turnID string) {
		fin.recordFull(finishedCall{
			sessionID:  sessionID,
			origin:     origin,
			outcome:    outcome,
			turnID:     turnID,
			busyAtFire: f.mgr.Busy(context.Background(), sessionID),
		})
	}))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	calls := fin.all()
	require.Len(t, calls, 1, "exactly one finished callback for one completed turn")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, hooks.OriginUser, calls[0].origin)
	require.Equal(t, TurnCompleted, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
	require.False(t, calls[0].busyAtFire, "the callback fires after the busy record cleared")
}

// TestTurnFinished_Cancelled covers the mid-stream cancel: the terminal is
// the cancelled outcome, reported once.
func TestTurnFinished_Cancelled(t *testing.T) {
	shortenEscalation(t)
	gate := make(chan struct{})
	fin := &finishedCollector{}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "one two three", promptTokens: 5, completionTokens: 1}
	}, WithTurnFinished(fin.record))
	f.model.midStream = func() { <-gate }
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	collector.waitDelta(t, 1)

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)
	close(gate) // nothing is listening anymore; release the fake stream

	calls := fin.all()
	require.Len(t, calls, 1, "exactly one finished callback for one cancelled turn")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, TurnCancelled, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
}

// TestTurnFinished_Failed covers the model failure: the terminal error lands
// as the failed outcome, reported once.
func TestTurnFinished_Failed(t *testing.T) {
	fin := &finishedCollector{}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{err: errors.New("model exploded")}
	}, WithTurnFinished(fin.record))
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err, "model failures do not fail Send; they surface as the terminal outcome")
	require.ErrorContains(t, turn.Wait(), "model exploded")

	calls := fin.all()
	require.Len(t, calls, 1, "exactly one finished callback for one failed turn")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, TurnFailed, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
}

// TestTurnFinished_SchedulerOriginStillFires pins the truthful reporting: a
// scheduler-origin run's terminal fires the callback with its own origin —
// filtering scheduler runs out is the engine's job (design D2's matrix), not
// the manager's.
func TestTurnFinished_SchedulerOriginStillFires(t *testing.T) {
	fin := &finishedCollector{}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ran", promptTokens: 1, completionTokens: 1}
	}, WithTurnFinished(fin.record))
	sess := f.newSession(t)

	turn, err := f.mgr.Submit(t.Context(), SendRequest{
		SessionID: sess.ID,
		Message:   "scheduled work",
		Origin:    hooks.OriginScheduler,
	})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	calls := fin.all()
	require.Len(t, calls, 1, "the manager reports the terminal truthfully")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, hooks.OriginScheduler, calls[0].origin)
	require.Equal(t, TurnCompleted, calls[0].outcome)
}

// TestTurnFinished_GroupTurnNeverFires pins the group skip: a group-lane turn
// — completed or dispatch-failed — never fires the callback, so a group
// member turn never double-reports beside the group task-level report; an
// ordinary send on the same fixture still reports.
func TestTurnFinished_GroupTurnNeverFires(t *testing.T) {
	fin := &finishedCollector{}
	src := &fakeGroupTurnSource{}
	for _, name := range []string{"group_history", "group_assign_task", "group_task_result"} {
		gt, err := newFakeGroupTool(name)
		require.NoError(t, err)
		src.tools = append(src.tools, gt)
	}
	ref := NewGroupTurnRef()
	ref.Set(src)
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithGroupTurns(ref), WithTurnFinished(fin.record))
	member := f.newSession(t)

	turn, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.NoError(t, turn.Wait())
	require.Empty(t, fin.all(), "a group-lane turn never fires TurnFinished")

	// A dispatch whose group tools fail to resolve takes the same terminal
	// path — and is equally a group turn, equally silent.
	src.toolsErr = errors.New("group store down")
	failed, err := f.mgr.SubmitGroup(t.Context(), GroupTurnRequest{SessionID: member.ID, Packet: groupPacket()})
	require.NoError(t, err)
	require.ErrorContains(t, failed.Wait(), "group store down")
	require.Empty(t, fin.all(), "a dispatch-failed group turn never fires TurnFinished either")

	// The skip is the group-lane marking, not the fixture: an ordinary send
	// on the same session still reports.
	ordinary, err := f.mgr.Send(t.Context(), member.ID, "plain message")
	require.NoError(t, err)
	require.NoError(t, ordinary.Wait())
	calls := fin.all()
	require.Len(t, calls, 1, "an ordinary turn on the same fixture still fires")
	require.Equal(t, member.ID, calls[0].sessionID)
	require.Equal(t, hooks.OriginUser, calls[0].origin)
	require.Equal(t, TurnCompleted, calls[0].outcome)
}

// TestSessionParked_ApprovalPark covers the shell-approval park: the install
// reports the command awaiting approval, and neither the resolve nor the
// resumed turn's completion adds a parked callback.
func TestSessionParked_ApprovalPark(t *testing.T) {
	parked := &parkedCollector{}
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
	}, WithSessionParked(parked.record))
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, pending := parkOnApproval(t, f, sess)

	call := awaitOneParked(t, parked)
	require.Equal(t, sess.ID, call.sessionID)
	require.Equal(t, ParkApproval, call.kind)
	require.Equal(t, pending.Approval.InterruptID, call.interruptID, "the interrupt id rides the callback")
	require.Equal(t, pending.Approval.Command, call.detail, "the detail is the command awaiting approval")

	// The resolve path never fires the parked callback: denying resumes and
	// completes the turn with the count unchanged.
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())
	require.Len(t, parked.all(), 1, "the resolve path adds no parked callback")
}

// TestSessionParked_InputPark covers the browser attention park: the install
// reports the prompt/method describing the request, and the resolve adds no
// parked callback.
func TestSessionParked_InputPark(t *testing.T) {
	src := newSystemMCPSource()
	request := &scriptedChildTool{raw: "__request_secret", reply: staticReply(
		`{"status":"ready","requestID":"req-1","origin":"https://example.com","method":"password","username":"sam@example.com"}`)}
	deliver := &scriptedChildTool{raw: "__deliver_secret", reply: staticReply(`{"status":"completed"}`)}
	src.tools[mcp.SystemServerID] = []tool.BaseTool{request, deliver, fakeMCPTool{raw: "submit_secret"}}

	parked := &parkedCollector{}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok", promptTokens: 1, completionTokens: 1}
	}, WithMCPManager(src), WithSessionParked(parked.record))
	sess := f.newSession(t)
	const turnID = "turn-park-1"

	wrapper := resolveAttentionSurface(t, f, src, sess.ID, turnID, "mcp__browser-use__submit_secret")
	resultCh := invokeAsync(t.Context(), wrapper, `{"targets":["password"],"method":"password","username":"sam@example.com"}`)

	pending := awaitInputRequired(t, f, sess.ID)
	call := awaitOneParked(t, parked)
	require.Equal(t, sess.ID, call.sessionID)
	require.Equal(t, ParkInput, call.kind)
	require.Equal(t, pending.Input.InterruptID, call.interruptID, "the interrupt id rides the callback")
	require.Equal(t, "The agent asks you to sign in at https://example.com. (password)", call.detail,
		"the detail carries the prompt and the method")

	// The resolve path never fires the parked callback.
	require.NoError(t, f.mgr.ResolveInput(t.Context(), sess.ID, pending.Input.InterruptID, "s3cret-value", "", false, ""))
	res := <-resultCh
	require.NoError(t, res.err)
	require.Len(t, parked.all(), 1, "the resolve path adds no parked callback")
}

// failSessionStore fails the session lookup the resumed run's setup performs,
// driving the failResume terminal deterministically; everything else passes
// through.
type failSessionStore struct {
	sessions.Service
	err error
}

func (s failSessionStore) Session(ctx context.Context, sessionID string) (sessions.Session, error) {
	if s.err != nil {
		return sessions.Session{}, s.err
	}
	return s.Service.Session(ctx, sessionID)
}

// TestTurnFinished_ApprovalResumeCompletes pins the resume-terminal fire: an
// approval-resolved turn ends through resumeTurn's terminal, never
// finishTurn — and its completion reports exactly like any turn terminal.
func TestTurnFinished_ApprovalResumeCompletes(t *testing.T) {
	root := t.TempDir()
	fin := &finishedCollector{}
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
		return fakeReply{text: "handled", promptTokens: 10, completionTokens: 2}
	}, WithTurnFinished(fin.record))
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)
	doomed := root + "/tester/doomed.txt"
	require.NoError(t, os.MkdirAll(filepath.Dir(doomed), 0o755))
	require.NoError(t, os.WriteFile(doomed, []byte("old build"), 0o644))

	turn, pending := parkOnApproval(t, f, sess)
	require.Empty(t, fin.all(), "the park is not a terminal — nothing fired yet")

	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))
	require.NoError(t, turn.Wait())

	calls := fin.all()
	require.Len(t, calls, 1, "exactly one finished callback for the resumed turn's terminal")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, hooks.OriginUser, calls[0].origin)
	require.Equal(t, TurnCompleted, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
}

// TestTurnFinished_ReParkStaysSilentUntilTerminal pins the re-park case: the
// resumed run interrupting again is not a terminal — the turn stays busy with
// no finished callback — and only the final completion reports, exactly once.
func TestTurnFinished_ReParkStaysSilentUntilTerminal(t *testing.T) {
	fin := &finishedCollector{}
	parked := &parkedCollector{}
	f := approvalFixture(t, t.TempDir(), func(input []*schema.Message) fakeReply {
		saw1, saw2 := false, false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				saw1 = true
			}
			if msg.Role == schema.Tool && msg.ToolCallID == "call-2" {
				saw2 = true
			}
		}
		if !saw1 {
			return dangerousCall("call-1", "doomed.txt")
		}
		if !saw2 {
			return dangerousCall("call-2", "doomed2.txt")
		}
		return fakeReply{text: "both handled", promptTokens: 10, completionTokens: 2}
	}, WithTurnFinished(fin.record), WithSessionParked(parked.record))
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, first := parkOnApproval(t, f, sess)
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, first.Approval.InterruptID, true))

	// The resumed run interrupts on a second dangerous command.
	events := f.awaitEvent(t, sess.ID, sessions.KindApprovalRequired)
	second := events[len(events)-1]
	require.NotEqual(t, first.Approval.InterruptID, second.Approval.InterruptID, "a fresh interrupt")
	require.Empty(t, fin.all(), "the re-park is not a terminal — nothing fired")
	require.True(t, f.mgr.Busy(t.Context(), sess.ID), "the re-parked turn stays busy")

	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, second.Approval.InterruptID, true))
	require.NoError(t, turn.Wait())

	calls := fin.all()
	require.Len(t, calls, 1, "the final terminal fires exactly once despite two parks")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, TurnCompleted, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
	require.Len(t, parked.all(), 2, "both parks reported")
}

// TestTurnFinished_FailedResume pins the failResume terminal: a resume whose
// setup fails reports the failed outcome once and releases the session.
func TestTurnFinished_FailedResume(t *testing.T) {
	fin := &finishedCollector{}
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
		return fakeReply{text: "never reached", promptTokens: 1, completionTokens: 1}
	}, WithTurnFinished(fin.record))
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, pending := parkOnApproval(t, f, sess)

	// The resumed run's setup fails: the session lookup errors, and the
	// resume takes the failResume terminal.
	f.mgr.sessions = failSessionStore{Service: f.mgr.sessions, err: errors.New("store gone")}
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, true))
	require.ErrorContains(t, turn.Wait(), "resume turn")

	calls := fin.all()
	require.Len(t, calls, 1, "the failed resume's terminal fires exactly once")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, TurnFailed, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
	require.False(t, f.mgr.Busy(t.Context(), sess.ID), "the failed resume released the session")
}

// TestTurnFinished_ResumedCancelled pins the resumed run's cancelled terminal:
// stopping a resumed turn mid-stream reports the cancelled outcome once.
func TestTurnFinished_ResumedCancelled(t *testing.T) {
	shortenEscalation(t)
	gate := make(chan struct{})
	var streamCalls atomic.Int32
	fin := &finishedCollector{}
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
		return fakeReply{text: "one two three", promptTokens: 10, completionTokens: 2}
	}, WithTurnFinished(fin.record))
	// Only the RESUMED run's model call pauses mid-stream (the second Stream
	// of the turn): the original run must complete its interrupting call.
	f.model.midStream = func() {
		if streamCalls.Add(1) >= 2 {
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

	calls := fin.all()
	require.Len(t, calls, 1, "exactly one finished callback for the resumed turn's cancelled terminal")
	require.Equal(t, sess.ID, calls[0].sessionID)
	require.Equal(t, hooks.OriginUser, calls[0].origin)
	require.Equal(t, TurnCancelled, calls[0].outcome)
	require.Equal(t, turn.TurnID(), calls[0].turnID)
	require.False(t, f.mgr.Busy(t.Context(), sess.ID), "the cancelled resume released the session")
}
