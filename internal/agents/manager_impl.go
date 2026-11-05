package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/agents/tools/docgen"
	"github.com/oniharnantyo/buddies/internal/attachments"
	"github.com/oniharnantyo/buddies/internal/knowledge"
	"github.com/oniharnantyo/buddies/internal/scheduler"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// manager is the concrete Manager implementation.
type manager struct {
	store    store.Store
	sessions sessions.Service
	registry modelRegistry
	resolver *CapabilityResolver
	// prompts is the persona-document source; nil (the default) means turns
	// run without persona documents.
	prompts PromptDocs
	// workspaces is the workspace source the jailed tools root at; nil (the
	// default) means no workspace-rooted tool is offered.
	workspaces *workspaces.Manager
	// skillsDir is the global skills library directory the two-tier skill
	// backend reads from (design D6, see skills.go); empty (the default)
	// means turns carry no skills.
	skillsDir string
	// attachments is the uploaded-attachment store turns read content from
	// (design D5, see attachments.go); nil (the default) means turns reject
	// attachment keys.
	attachments *attachments.Store
	// knowledge is the durable document corpus store turns save into through
	// save_document and mount read-only at /knowledge (add-knowledge-corpus
	// design D3/D4, see knowledge_tool.go); nil (the default) withholds both.
	knowledge *knowledge.Store
	// browserDir is the browser tool's home directory: profile user-data
	// directories root at <browserDir>/profiles and screenshots at
	// <browserDir>/screenshots (spec add-browser-tool D10); empty (the
	// default) leaves the tool's own fallback location in place.
	browserDir string
	// scheduler is the schedule service the scheduler tool drives (spec
	// add-scheduler D1/D8, see scheduler_tool.go); nil (the default) withholds
	// the tool from the catalog.
	scheduler *scheduler.Service
	// docgen is the manager-held document converter the create_document tool
	// renders through (add-create-document design D3): created with the
	// manager — Chrome-free until the first conversion — serialized behind
	// the converter's own mutex, and closed with the manager (Close). Never
	// nil: the tool degrades no further than the library's defaults.
	docgen *docgen.Converter
	// mcpManager is the MCP connection cache the agent's opted-in servers'
	// tools are drawn from (see mcp.go). Never nil: the default no-op source
	// contributes zero MCP tools.
	mcpManager mcp.ToolSource
	// hooks is the lifecycle-hook dispatcher turns resolve their chain from
	// (see hooks.go). Never nil: the default no-op dispatcher resolves an
	// empty chain, so turns run without hooks.
	hooks *hooks.Dispatcher
	// onTransition is the in-flight transition callback (WithTurnTransition,
	// add-grouped-session-rail design D2); nil (the default) means
	// transitions go unobserved.
	onTransition func(sessionID string, busy bool)
	// groupTurns is the late-binding group turn source holder (WithGroupTurns,
	// add-agent-groups design D3); nil (the default) means group dispatching
	// is unavailable.
	groupTurns *GroupTurnRef
	// agentLanes is the per-agent group-dispatch lane state (add-agent-groups
	// design D6), keyed by agent id. Created lazily; guarded by mu.
	agentLanes map[string]*agentLane

	// baseCtx parents turn goroutines: turns survive the Send call and any
	// subscriber disconnect (design D7 — a disconnected client never loses
	// the turn's persisted events).
	baseCtx context.Context

	mu      sync.Mutex
	nextSub int
	subs    map[string]map[int]*streamSub // sessionID -> registration -> sub
	// running is the per-session in-flight turn record (design D3): the
	// per-turn cancel surface plus, while an approval is pending, the parked
	// run's resume state. A session whose turn parked on a pending approval
	// keeps its record — the session stays busy until the decision or a
	// cancel resolves it.
	running map[string]*runningTurn
	// resolving guards ResolveApproval's validate-then-append window (the
	// event-log derivation reads before the approval_resolved append lands):
	// interrupt ids whose resolve has begun or landed, so a second
	// concurrent resolve conflicts exactly like the sequential case. Entries
	// persist once the resolution lands — the log keeps it resolved forever
	// — and a resolve that fails before its append releases its entry so a
	// retry can proceed.
	resolving map[string]bool
	// pendingInputs is the live browser-attention registry
	// (add-browser-login-handover design D3), keyed by interrupt id. An entry
	// exists exactly while a parked attention tool call waits: ResolveInput
	// hands the typed value to it (in memory only — never persisted), and a
	// cold restart loses it, which is what makes a stale resolve fail with
	// ErrInputNotPending while the card folds expired client-side (the
	// frontend transcript fold normalizes a still-pending card once the
	// session is no longer busy — expireUnresolvedInputs in
	// frontend/src/features/chat/entries.ts). Guarded by mu.
	pendingInputs map[string]*pendingInput
	// resolvingInputs guards the input resolve methods' validate-then-append
	// window exactly like resolving does for approvals: interrupt ids whose
	// resolve has begun or landed, so a second concurrent resolve conflicts
	// exactly like the sequential already-resolved case. Entries persist once
	// the resolution lands; a resolve that fails before its append releases
	// its entry so a retry can proceed. Guarded by mu.
	resolvingInputs map[string]bool
	// sessionTasks is the delegated-task space per session (spec
	// agent-subagents "Background lane"): background tasks complete
	// independently of the turn that spawned them and stay addressable
	// through task_output/task_stop in later turns, so the space lives as
	// long as the session — not the turn.
	sessionTasks map[string]*subagents.TaskRegistry
}

// inFlightTool captures a currently executing tool call for the catch-up snapshot.
type inFlightTool struct {
	Name      string
	Arguments string
}

// groupTurnContext is a group dispatch's turn context (add-agent-groups
// design D3/D4): the packet the model input assembles from and the
// source-provided in-channel tools. A running turn's group is non-nil
// exactly for a group turn.
type groupTurnContext struct {
	packet GroupPacket
	tools  []tool.BaseTool
}

// runningTurn is one session's in-flight turn record (design D3): the two
// cancel levels — the ADK run's safe-point cancel and the turn context's
// hard cancel — plus the state a parked approval resume picks up. Created by
// Send before the turn goroutine starts; replaced when a parked run resumes
// (the resumed run needs a fresh cancel surface of its own).
type runningTurn struct {
	turn      *Turn
	sessionID string
	// agent and toolErrors carry the composed turn surface so a live resume
	// reuses it (design D4); nil after a process loss, where the resume
	// rebuilds phases 4–9 from the store.
	agent      adk.TypedAgent[*schema.AgenticMessage]
	toolErrors *toolErrorResult
	// usage accumulates the turn's token usage across the original run and
	// its resumes; the terminal event carries the total.
	usage *sessions.Usage
	// args remembers each tool call's arguments so a resumed run's terminal
	// tool records still carry the full call (the interrupted run's call
	// announcement was live-only).
	args map[string]string
	// artifactsBefore is the start-of-turn listing of the workspace's
	// artifacts directory (add-turn-artifacts design D3): the same lifetime
	// as usage, so a live approval resume keeps it and the terminal event
	// diffs against it. Nil when detection is disabled for the turn (the
	// artifacts path is unusable) — the turn still runs.
	artifactsBefore artifactRecord
	// artifactsSince is the cold-resume fallback predicate (add-turn-artifacts
	// design D3): with the process — and the start record — lost, the
	// terminal listing reports files whose mtime is at or after the turn's
	// persisted user-message time. Zero when a live record exists.
	artifactsSince time.Time
	// origin is the dispatch's run origin (design D6): stamped on the hook
	// events of the original run and of any approval resume of it. Empty on
	// a cold resume (process lost) — the resume reads as a user run.
	origin string
	// agentID is the session agent's id (add-agent-groups design D6): the
	// per-agent lane's busy key, installed with the record so lane scans read
	// it race-free.
	agentID string
	// group is the group dispatch's turn context (add-agent-groups design
	// D3); non-nil exactly for a group turn — the packet replaces the session
	// replay as the model input, and the source's in-channel tools ride
	// beside the ordinary catalog surface (fix-agent-group-member-tools
	// design D1).
	group *groupTurnContext
	// laneClaimed reports that this turn claimed its agent's group-dispatch
	// lane (add-agent-groups design D6); released when the turn finishes so
	// the queued dispatches drain. A group turn's ordinary surface can carry
	// the approval-gated shell (fix-agent-group-member-tools design D2), so a
	// claimed turn can park on a pending approval: the claim survives the
	// park and releases when the parked turn is resolved or cancelled (the
	// resume terminal paths and discardPending).
	laneClaimed bool

	// ctx is the turn's cancellable context (design D3) — the run's context,
	// created with the record; the escalation hard-stops through it.
	ctx context.Context

	// cancel is the turn context's hard cancel — the escalation target.
	cancel context.CancelFunc
	// adkCancel is the current run's ADK safe-point cancel; swapped in when
	// a run (original or resumed) starts.
	adkCancel adk.AgentCancelFunc
	// cancelRequested reports whether Cancel already fired for this turn.
	// A second Cancel is a no-op; the drain consults it to record the
	// cancelled terminal instead of an error.
	cancelRequested atomic.Bool
	// toolMu guards partial and runningTools against concurrent access
	// between the drain and Subscribe catch-up snapshots.
	toolMu       sync.Mutex
	runningTools map[string]inFlightTool
	// partial carries the in-flight model call's streamed text across an
	// abrupt end (design D3: a cancelled turn keeps its partial output); the
	// drain updates it, the cancelled terminal persists it.
	partial string
	// variantOf is the turn's regeneration marker (design D7): recorders
	// that bypass the runTurn record closure (the cancelled terminal, the
	// resumed run) still stamp it so the variant grouping survives.
	variantOf string
	// pending is non-nil while the run is parked on an unresolved approval.
	pending *pendingApproval
}

func (rt *runningTurn) appendPartial(text string) {
	rt.toolMu.Lock()
	rt.partial += text
	rt.toolMu.Unlock()
}

func (rt *runningTurn) resetPartial() {
	rt.toolMu.Lock()
	rt.partial = ""
	rt.toolMu.Unlock()
}

func (rt *runningTurn) addTool(callID, name, args string) {
	rt.toolMu.Lock()
	if rt.runningTools == nil {
		rt.runningTools = make(map[string]inFlightTool)
	}
	rt.runningTools[callID] = inFlightTool{Name: name, Arguments: args}
	rt.toolMu.Unlock()
}

func (rt *runningTurn) removeTool(callID string) {
	rt.toolMu.Lock()
	delete(rt.runningTools, callID)
	rt.toolMu.Unlock()
}

func (rt *runningTurn) snapshot() (string, map[string]inFlightTool) {
	rt.toolMu.Lock()
	defer rt.toolMu.Unlock()
	toolsCopy := make(map[string]inFlightTool, len(rt.runningTools))
	for k, v := range rt.runningTools {
		toolsCopy[k] = v
	}
	return rt.partial, toolsCopy
}

// pendingApproval is a parked run's unresolved approval: the interrupt and
// checkpoint coordinates ResolveApproval resumes from. Derived from the log
// on reload; held in memory while the process lives.
type pendingApproval struct {
	interruptID  string
	command      string
	checkpointID string
	turnID       string
}

// sessionTaskRegistry returns the session's delegated-task space, creating it
// on first use. record is installed for the calling turn (the per-session
// busy guard serializes turns, so the install is race-free against other
// turns of the same session).
func (m *manager) sessionTaskRegistry(sessionID string, record recordFn) *subagents.TaskRegistry {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionTasks == nil {
		m.sessionTasks = make(map[string]*subagents.TaskRegistry)
	}
	reg, ok := m.sessionTasks[sessionID]
	if !ok {
		reg = subagents.NewTaskRegistry("", func(ev sessions.Event, durable bool) {
			_ = record(&ev, durable)
		})
		m.sessionTasks[sessionID] = reg
		return reg
	}
	reg.SetRecord(func(ev sessions.Event, durable bool) {
		_ = record(&ev, durable)
	})
	return reg
}

func (m *manager) CreateSession(ctx context.Context, agentID string) (sessions.Session, error) {
	if _, err := m.store.Agents().ByID(ctx, agentID); err != nil {
		return sessions.Session{}, fmt.Errorf("create session: %w", err)
	}
	session, err := m.sessions.CreateSession(ctx, agentID, "")
	if err != nil {
		return sessions.Session{}, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

// Send submits one user message and returns promptly, while the turn
// proceeds in the background. It is the (sessionID, message) shorthand over
// Submit with the user origin (design D6) — the chat UI's path.
func (m *manager) Send(ctx context.Context, sessionID, message string, attachmentKeys ...string) (*Turn, error) {
	return m.Submit(ctx, SendRequest{
		SessionID:      sessionID,
		Message:        message,
		AttachmentKeys: attachmentKeys,
		Origin:         hooks.OriginUser,
	})
}

// Submit dispatches one turn from the full request shape (design D6): the
// turn proceeds in the background and req.Origin — who started the run — is
// stamped on every hook event it raises. Fails exactly like Send.
func (m *manager) Submit(ctx context.Context, req SendRequest) (*Turn, error) {
	if strings.TrimSpace(req.Message) == "" && len(req.AttachmentKeys) == 0 {
		return nil, fmt.Errorf("send message: message is empty")
	}
	// The per-message attachment cap and key registration (spec
	// chat-attachments): at most MaxPerMessage files ride one message, and
	// every key must name an upload. The transcript metadata comes from the
	// registry rows — the caller's keys never carry metadata.
	atts, err := m.resolveAttachments(ctx, req.AttachmentKeys)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	sess, err := m.sessions.Session(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	agent, err := m.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	rt, err := m.startTurn(req.SessionID, agent.ID, req.Origin, nil)
	if err != nil {
		return nil, err
	}

	// Effective turn configuration (design D1/D2): the session's overrides
	// win per field; empty inherits — the model falls back to the agent's
	// configured model at build time, the effort to the agent's stored
	// default here. Validation happens at build time, on the turn goroutine;
	// failures surface as the turn's terminal turn_error event.
	override := ModelOverride{
		Model:  sess.Model,
		Effort: sess.Effort,
	}
	if override.Effort == "" {
		override.Effort = agent.Effort
	}

	go func() {
		defer rt.cancel()
		err := m.runTurn(rt.ctx, rt, sess, *agent, override, turnRequest{
			message:     req.Message,
			attachments: atts,
			origin:      req.Origin,
		})
		m.finishTurn(req.SessionID, rt, err)
	}()
	return rt.turn, nil
}

// Regenerate re-runs a turn's prompt as a new alternative (design D7, spec
// chat-view "Regenerate and branch variants"): the original turn's persisted
// user message supplies the text and attachment metadata (content re-sent
// from disk, never re-uploaded), the new turn's events carry the variant_of
// marker, and the duplicate prompt persist is suppressed — the log keeps
// exactly one user message per prompt.
func (m *manager) Regenerate(ctx context.Context, sessionID, turnID string) (*Turn, error) {
	sess, err := m.sessions.Session(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("regenerate: %w", err)
	}
	agent, err := m.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		return nil, fmt.Errorf("regenerate: %w", err)
	}
	// The busy guard first (startTurn re-checks under the lock, so a race
	// still fails closed): a session with a turn in flight cannot
	// regenerate.
	if m.Busy(ctx, sessionID) {
		return nil, fmt.Errorf("regenerate: %w", ErrTurnInProgress)
	}
	events, err := m.sessions.Events(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("regenerate: %w", err)
	}
	var original *sessions.Event
	for i := range events {
		if events[i].Kind == sessions.KindUserMessage && events[i].TurnID == turnID {
			original = &events[i]
			break
		}
	}
	if original == nil {
		return nil, fmt.Errorf("regenerate: turn %s has no user message to re-run", turnID)
	}

	rt, err := m.startTurn(sessionID, agent.ID, hooks.OriginUser, nil)
	if err != nil {
		return nil, err
	}

	// The same effective configuration resolution as Send (design D1/D2).
	override := ModelOverride{Model: sess.Model, Effort: sess.Effort}
	if override.Effort == "" {
		override.Effort = agent.Effort
	}

	go func() {
		defer rt.cancel()
		err := m.runTurn(rt.ctx, rt, sess, *agent, override, turnRequest{
			message: original.Text,
			// The stored metadata re-sends content from disk (task 5.3).
			attachments:         original.Attachments,
			variantOf:           turnID,
			suppressUserPersist: true,
			// The replayed context cuts at the original prompt: its old
			// answer is what the new variant replaces, and the prompt itself
			// rides the fresh input.
			replayBefore: original.Seq,
			// A regeneration is a user action, whatever started the original
			// turn (design D6).
			origin: hooks.OriginUser,
		})
		m.finishTurn(sessionID, rt, err)
	}()
	return rt.turn, nil
}

// agentLane is one agent's group-dispatch lane (add-agent-groups design D6):
// the FIFO queue of group dispatches waiting for the agent's turn slot, plus
// the claim latch that keeps two group dispatches of the same agent from
// starting together. Ordinary sends never touch the lane — the per-session
// busy guard stays their only serializer.
type agentLane struct {
	// active reports that a group dispatch claimed the lane: it is starting
	// or running. Claimed by drainGroupLane before startTurn and released
	// when the claiming turn finishes.
	active bool
	// queue holds the waiting dispatches in arrival order.
	queue []*groupLaneEntry
}

// groupLaneEntry is one queued group dispatch: its request and the Turn
// handle its submitter waits on.
type groupLaneEntry struct {
	req  GroupTurnRequest
	turn *Turn
}

// laneFor returns the agent's lane, creating it on first use. The caller
// holds m.mu.
func (m *manager) laneFor(agentID string) *agentLane {
	lane, ok := m.agentLanes[agentID]
	if !ok {
		lane = &agentLane{}
		m.agentLanes[agentID] = lane
	}
	return lane
}

// agentBusyLocked reports whether the agent has any turn in flight — own
// chat or group thread (add-agent-groups design D6). The caller holds m.mu.
func (m *manager) agentBusyLocked(agentID string) bool {
	for _, rt := range m.running {
		if rt.agentID == agentID {
			return true
		}
	}
	return false
}

// SubmitGroup dispatches one group member turn (add-agent-groups design
// D3/D6): the packet is the turn's whole model input, and the dispatch
// queues on the agent's lane while the agent has any turn in flight — it
// never fails with ErrTurnInProgress. See the Manager interface for the full
// contract.
func (m *manager) SubmitGroup(ctx context.Context, req GroupTurnRequest) (*Turn, error) {
	// Fail fast when the seam is unwired; the dispatch itself re-resolves
	// the source at drain time, so a source bound between the two reads is
	// picked up.
	if _, err := m.groupSource(); err != nil {
		return nil, err
	}
	sess, err := m.sessions.Session(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("submit group turn: %w", err)
	}
	agent, err := m.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		return nil, fmt.Errorf("submit group turn: %w", err)
	}
	entry := &groupLaneEntry{
		req:  req,
		turn: &Turn{id: newTurnID(), done: make(chan struct{})},
	}
	m.enqueueGroupTurn(agent.ID, entry)
	// Kick the drain: on a free agent the dispatch starts right away, on a
	// busy one it waits for the lane.
	m.drainGroupLane(agent.ID)
	return entry.turn, nil
}

// groupSource resolves the wired group turn source; ErrGroupTurnsUnavailable
// when the ref is unwired or the groups service has not bound itself yet.
func (m *manager) groupSource() (GroupTurnSource, error) {
	if m.groupTurns == nil {
		return nil, ErrGroupTurnsUnavailable
	}
	if src := m.groupTurns.Source(); src != nil {
		return src, nil
	}
	return nil, ErrGroupTurnsUnavailable
}

// enqueueGroupTurn parks one dispatch at the lane's tail. The caller holds
// no locks.
func (m *manager) enqueueGroupTurn(agentID string, entry *groupLaneEntry) {
	m.mu.Lock()
	lane := m.laneFor(agentID)
	lane.queue = append(lane.queue, entry)
	m.mu.Unlock()
}

// drainGroupLane starts the lane's head dispatch when the agent is fully
// free — no group dispatch claimed or running, and no turn of the agent's in
// flight anywhere. Called whenever a group dispatch is submitted and when
// any turn of the agent finishes (add-agent-groups design D6: the lane frees
// on the terminal outcome, own chat and group turns alike). The entry is
// popped before its turn starts, so a drain can never double-start it.
func (m *manager) drainGroupLane(agentID string) {
	for {
		m.mu.Lock()
		lane := m.agentLanes[agentID]
		if lane == nil || lane.active || len(lane.queue) == 0 || m.agentBusyLocked(agentID) {
			m.mu.Unlock()
			return
		}
		lane.active = true
		entry := lane.queue[0]
		lane.queue = lane.queue[1:]
		m.mu.Unlock()

		if err := m.startGroupTurn(entry, agentID); err != nil {
			// The dispatch never claimed the member session — an ordinary
			// send raced onto it between the busy check and startTurn. The
			// group service holds the Turn handle, sees the error, and can
			// re-dispatch; the lane re-opens for the next entry.
			entry.turn.finish(fmt.Errorf("dispatch group turn: %w", err))
			m.releaseGroupLane(agentID)
			continue
		}
		return
	}
}

// releaseGroupLane re-opens the agent's lane claim. The caller holds no
// locks.
func (m *manager) releaseGroupLane(agentID string) {
	m.mu.Lock()
	if lane := m.agentLanes[agentID]; lane != nil {
		lane.active = false
	}
	m.mu.Unlock()
}

// groupLaneTurnDone releases a finished turn's lane claim and kicks the
// agent's lane drain (add-agent-groups design D6). Runs on every terminal
// path — the claiming group turn's own finish and any ordinary turn's finish
// that unblocks a waiting dispatch alike.
func (m *manager) groupLaneTurnDone(rt *runningTurn) {
	if rt.agentID == "" {
		return
	}
	if rt.laneClaimed {
		m.releaseGroupLane(rt.agentID)
	}
	m.drainGroupLane(rt.agentID)
}

// cancelQueuedGroupTurns drops the session's queued group dispatches
// (add-agent-groups design D6): each waiting Turn completes with
// ErrTurnCancelled and never runs. A session with nothing queued is a safe
// no-op.
func (m *manager) cancelQueuedGroupTurns(sessionID string) error {
	dropped := make([]*Turn, 0, 1)
	m.mu.Lock()
	for _, lane := range m.agentLanes {
		kept := make([]*groupLaneEntry, 0, len(lane.queue))
		for _, entry := range lane.queue {
			if entry.req.SessionID == sessionID {
				dropped = append(dropped, entry.turn)
				continue
			}
			kept = append(kept, entry)
		}
		lane.queue = kept
	}
	m.mu.Unlock()
	for _, turn := range dropped {
		turn.finish(ErrTurnCancelled)
	}
	return nil
}

// startGroupTurn claims the member session's busy slot and launches the
// dispatch's turn goroutine — the normal startTurn/finishTurn path (design
// D3: a group turn is a real turn on a real session). Called with the
// agent's lane claimed. An error return means the turn never started: the
// caller finishes the entry's Turn handle with it and re-opens the lane.
func (m *manager) startGroupTurn(entry *groupLaneEntry, agentID string) error {
	src, err := m.groupSource()
	if err != nil {
		return err
	}
	// The dispatch's reads ride a detached context: a queued dispatch runs
	// after its submitter's call returned, exactly like the turn goroutine
	// itself.
	setupCtx := context.WithoutCancel(m.baseCtx)
	sess, err := m.sessions.Session(setupCtx, entry.req.SessionID)
	if err != nil {
		return fmt.Errorf("dispatch group turn: %w", err)
	}
	agent, err := m.store.Agents().ByID(setupCtx, sess.AgentID)
	if err != nil {
		return fmt.Errorf("dispatch group turn: %w", err)
	}
	rt, err := m.startTurn(sess.ID, agent.ID, hooks.OriginUser, entry.turn)
	if err != nil {
		return err
	}
	rt.laneClaimed = true

	// The in-channel tools resolve at dispatch time (design D4): the
	// source-provided group tools ride the turn BESIDE the member's ordinary
	// catalog surface, which the runner resolves per turn exactly as for a
	// session turn (fix-agent-group-member-tools design D1). A failure here
	// takes the normal terminal path — the lane releases through finishTurn
	// like any finished turn.
	tools, err := src.GroupTurnTools(setupCtx, entry.req.Packet.GroupID, agent.ID)
	if err != nil {
		m.finishTurn(sess.ID, rt, fmt.Errorf("resolve group tools: %w", err))
		return nil
	}
	rt.group = &groupTurnContext{packet: entry.req.Packet, tools: tools}

	// The same effective configuration resolution as Submit (design D1/D2).
	override := ModelOverride{Model: sess.Model, Effort: sess.Effort}
	if override.Effort == "" {
		override.Effort = agent.Effort
	}
	packet := entry.req.Packet
	go func() {
		defer rt.cancel()
		err := m.runTurn(rt.ctx, rt, sess, *agent, override, turnRequest{
			message:     packet.Request,
			attachments: packet.Attachments,
			origin:      hooks.OriginUser,
		})
		m.finishTurn(sess.ID, rt, err)
		// The acceptance cursor advances to the packet's through_seq after
		// the terminal outcome (add-agent-groups design D3) — a full packet
		// rebuilds it, a delta advances it. Best-effort: the turn's outcome
		// is already terminal, and the next packet rebuilds from the store's
		// cursor when a commit is lost.
		if cerr := src.CommitCursor(context.WithoutCancel(m.baseCtx), packet.GroupID, agent.ID, packet.ThroughSeq); cerr != nil {
			slog.Warn("group turn: commit acceptance cursor", "group", packet.GroupID, "agent", agent.ID, "error", cerr)
		}
	}()
	return nil
}

// SummarizeText runs one direct summarizer call on the agent's configured
// model (add-agent-groups design D3). It touches neither the agent lane nor
// any session's busy slot. See the Manager interface for the full contract.
func (m *manager) SummarizeText(ctx context.Context, agentID, instruction, text string) (string, error) {
	agent, err := m.store.Agents().ByID(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("summarize text: %w", err)
	}
	mdl, err := m.registry.Build(ctx, *agent, ModelOverride{})
	if err != nil {
		return "", fmt.Errorf("summarize text: %w", err)
	}
	return summarizeTextWithModel(ctx, mdl, instruction, text)
}

// summarizeTextWithModel runs the summarizer call the compaction gate uses
// (design D3: the same prompt seam): the summarizer system prompt — with the
// caller's instruction appended when one rides — leads, the material is the
// user message, and the reply's text blocks concatenate into the summary.
func summarizeTextWithModel(ctx context.Context, mdl model.BaseModel[*schema.AgenticMessage], instruction, text string) (string, error) {
	sys := summarizeSystemPrompt
	if strings.TrimSpace(instruction) != "" {
		sys += "\n\n" + instruction
	}
	summarized, err := mdl.Generate(ctx, []*schema.AgenticMessage{
		schema.SystemAgenticMessage(sys),
		schema.UserAgenticMessage(text),
	})
	if err != nil {
		return "", fmt.Errorf("summarize text: %w", err)
	}
	var sb strings.Builder
	if summarized != nil {
		for _, b := range summarized.ContentBlocks {
			if b != nil && b.AssistantGenText != nil {
				sb.WriteString(b.AssistantGenText.Text)
			}
		}
	}
	return sb.String(), nil
}

// startTurn claims the session's busy slot and returns the new turn's
// running record; ErrTurnInProgress when a turn is already in flight. agentID
// is the session agent's id — the per-agent lane's busy key (add-agent-groups
// design D6). turn is the turn handle to install: nil creates a fresh one
// (the ordinary dispatches); a group dispatch passes the handle its submitter
// already holds, so a queued dispatch's caller waits on the turn that actually
// runs. origin is the dispatch's run origin (design D6), kept on the record so
// an approval resume stamps the same origin.
func (m *manager) startTurn(sessionID, agentID, origin string, turn *Turn) (*runningTurn, error) {
	m.mu.Lock()
	if _, inFlight := m.running[sessionID]; inFlight {
		m.mu.Unlock()
		return nil, ErrTurnInProgress
	}
	// The per-turn cancel surface (design D3): the run rides turnCtx so the
	// escalation can hard-stop it; the ADK safe-point cancel is handed
	// over by the run itself once it starts.
	turnCtx, cancel := context.WithCancel(m.baseCtx)
	if turn == nil {
		turn = &Turn{id: newTurnID(), done: make(chan struct{})}
	}
	rt := &runningTurn{
		turn:         turn,
		sessionID:    sessionID,
		agentID:      agentID,
		origin:       origin,
		ctx:          turnCtx,
		cancel:       cancel,
		usage:        &sessions.Usage{},
		runningTools: make(map[string]inFlightTool),
	}
	m.running[sessionID] = rt
	m.mu.Unlock()
	m.notifyTransition(sessionID, true)
	return rt, nil
}

// finishTurn clears the busy record and completes the Turn for a dispatched
// run — parked runs (pending approval) leave both to the resolve/cancel path.
func (m *manager) finishTurn(sessionID string, rt *runningTurn, err error) {
	if errors.Is(err, errTurnParked) {
		// Parked on a pending approval: the session stays busy and the Turn
		// stays open — ResolveApproval's resume or Cancel owns the outcome
		// from here.
		return
	}
	// The busy record clears strictly before the Turn completes: a caller
	// waking from Wait can immediately Send again.
	m.mu.Lock()
	delete(m.running, sessionID)
	m.mu.Unlock()
	m.notifyTransition(sessionID, false)
	rt.turn.finish(err)
	m.groupLaneTurnDone(rt)
}

// resolveAttachments maps send-time capability keys onto their registered
// metadata (design D5): the per-message cap holds, every key must name an
// upload, and an absent attachment store rejects any key. An empty key list
// is always fine.
func (m *manager) resolveAttachments(ctx context.Context, keys []string) ([]sessions.Attachment, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if len(keys) > attachments.MaxPerMessage {
		return nil, fmt.Errorf("%w: at most %d attachments per message (got %d)",
			attachments.ErrRejected, attachments.MaxPerMessage, len(keys))
	}
	if m.attachments == nil {
		return nil, fmt.Errorf("attachment support is not configured")
	}
	atts := make([]sessions.Attachment, 0, len(keys))
	for _, key := range keys {
		att, err := m.attachments.Resolve(ctx, key)
		if err != nil {
			return nil, err
		}
		atts = append(atts, att)
	}
	return atts, nil
}

// Busy reports whether the session has a turn in flight — running or parked
// on a pending approval.
func (m *manager) Busy(_ context.Context, sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, inFlight := m.running[sessionID]
	return inFlight
}

// TurnState reports whether the session has a turn in flight — running or
// parked on a pending approval — and the in-flight turn's ID.
func (m *manager) TurnState(_ context.Context, sessionID string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.running[sessionID]; ok && rt != nil && rt.turn != nil {
		return true, rt.turn.id
	}
	return false, ""
}

// RunningSessions reports the session IDs with a turn in flight — running or
// parked on a pending approval (parked records stay in the busy map, so they
// are included). The result is empty, never nil.
func (m *manager) RunningSessions(_ context.Context) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for sessionID := range m.running {
		out = append(out, sessionID)
	}
	return out
}

// notifyTransition fires the WithTurnTransition callback for one in-flight
// transition (add-grouped-session-rail design D2), outside the manager lock.
// A no-op when no callback was configured.
func (m *manager) notifyTransition(sessionID string, busy bool) {
	cb := m.onTransition
	if cb != nil {
		cb(sessionID, busy)
	}
}

// cancelEscalationDelay bounds the safe-point cancel (design D3): the ADK
// cancel asks the run to stop at its next safe point (after the model call /
// after the tool call, keeping in-flight tool writes intact); if the run has
// not stopped after this long, the turn context's hard cancel fires. A var so
// tests can shorten the wait.
var cancelEscalationDelay = 2 * time.Second

// Cancel stops the session's in-flight or parked turn (spec agent-runtime
// "Turn cancellation"). With no turn in flight it drops the session's queued
// group dispatches instead (add-agent-groups design D6) — the same stop, one
// step earlier on the lane — and with nothing queued it is a safe no-op.
func (m *manager) Cancel(_ context.Context, sessionID string) error {
	m.mu.Lock()
	rt, ok := m.running[sessionID]
	m.mu.Unlock()
	if !ok {
		return m.cancelQueuedGroupTurns(sessionID)
	}
	if rt.pending != nil {
		return m.discardPending(sessionID, rt)
	}
	// Request the cancel exactly once (a second Cancel is a no-op), then
	// escalate: the ADK safe-point cancel first, the hard context cancel
	// after the grace period (design D3).
	if !rt.cancelRequested.CompareAndSwap(false, true) {
		return nil
	}
	m.mu.Lock()
	adkCancel := rt.adkCancel
	m.mu.Unlock()
	if adkCancel != nil {
		// Safe points keep tool writes intact; whichever fires first stops
		// the run.
		_, _ = adkCancel(
			adk.WithAgentCancelMode(adk.CancelAfterChatModel | adk.CancelAfterToolCalls),
		)
	}
	time.AfterFunc(cancelEscalationDelay, rt.cancel)
	return nil
}

// discardPending cancels a turn parked on a pending approval (spec
// agent-runtime: cancelling discards the run and clears the pending
// approval): the decision is recorded as cancelled, the run's checkpoint is
// purged along with any unconsumed ledger entry, and the session is released.
func (m *manager) discardPending(sessionID string, rt *runningTurn) error {
	pend := rt.pending
	recordCtx := context.WithoutCancel(m.baseCtx)
	// The decision and the terminal event land together, in log order.
	resolved := sessions.Event{
		TurnID: pend.turnID,
		Kind:   sessions.KindApprovalResolved,
		Approval: &sessions.Approval{
			InterruptID: pend.interruptID,
			Outcome:     sessions.ApprovalCancelled,
		},
	}
	cancelled := sessions.Event{
		TurnID:    pend.turnID,
		Kind:      sessions.KindTurnCancelled,
		VariantOf: rt.variantOf,
	}
	batch := []sessions.Event{resolved, cancelled}
	if err := m.sessions.Append(recordCtx, sessionID, batch); err != nil {
		// The store is the failing component; the cancel still clears the
		// session — a wedged busy flag would be worse than a missing notice.
		// Best-effort, like failTurn.
		slog.Error("cancel parked turn: persist outcome", "session", sessionID, "error", err)
	}
	m.broadcast(sessionID, batch[0])
	m.broadcast(sessionID, batch[1])

	// Purge the parked run's disk state: the checkpoint can never be resumed
	// and a recorded-but-unspent approval must not buy a later execution.
	if pend.checkpointID != "" {
		if err := m.store.Checkpoints().Delete(recordCtx, pend.checkpointID); err != nil {
			slog.Warn("cancel parked turn: delete checkpoint", "checkpoint", pend.checkpointID, "error", err)
		}
	}
	if pend.command != "" {
		if err := m.store.Checkpoints().Delete(recordCtx, backend.LedgerKey(pend.command)); err != nil {
			slog.Debug("cancel parked turn: purge ledger entry", "error", err)
		}
	}

	m.mu.Lock()
	delete(m.running, sessionID)
	m.mu.Unlock()
	m.notifyTransition(sessionID, false)
	rt.turn.finish(ErrTurnCancelled)
	m.groupLaneTurnDone(rt)
	return nil
}

func (m *manager) Subscribe(ctx context.Context, sessionID string) (Subscription, error) {
	if _, err := m.sessions.Session(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("subscribe session: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	sub := newStreamSub()
	id := m.nextSub
	m.nextSub++
	if m.subs[sessionID] == nil {
		m.subs[sessionID] = make(map[int]*streamSub)
	}
	m.subs[sessionID][id] = sub

	// Catch-up snapshot (design D5): emit in-flight state to the new subscription.
	// Synthetic IDs ensure deduplication safety; direct channel send avoids persisting.
	if rt, ok := m.running[sessionID]; ok && rt != nil && rt.turn != nil {
		turnID := rt.turn.id
		partial, runningTools := rt.snapshot()
		if partial != "" {
			sub.send(sessions.Event{
				ID:         fmt.Sprintf("snap-%s-partial", turnID),
				TurnID:     turnID,
				Kind:       sessions.KindTextDelta,
				Text:       partial,
				OccurredAt: now(),
			})
		}
		toolKeys := make([]string, 0, len(runningTools))
		for callID := range runningTools {
			toolKeys = append(toolKeys, callID)
		}
		sort.Strings(toolKeys)
		for _, callID := range toolKeys {
			t := runningTools[callID]
			sub.send(sessions.Event{
				ID:     fmt.Sprintf("snap-%s-tool-%s", turnID, callID),
				TurnID: turnID,
				Kind:   sessions.KindToolActivity,
				Tool: &sessions.ToolActivity{
					CallID:    callID,
					Name:      t.Name,
					Arguments: t.Arguments,
					Status:    sessions.ToolStatusRunning,
				},
				OccurredAt: now(),
			})
		}
	}

	return &sessionSubscription{m: m, sessionID: sessionID, id: id, sub: sub}, nil
}

func (m *manager) ContextUsage(ctx context.Context, sessionID string) (ContextUsage, error) {
	sess, err := m.sessions.Session(ctx, sessionID)
	if err != nil {
		return ContextUsage{}, fmt.Errorf("context usage: %w", err)
	}

	events, err := m.sessions.Events(ctx, sessionID)
	if err != nil {
		return ContextUsage{}, fmt.Errorf("context usage: %w", err)
	}

	agent, err := m.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		return ContextUsage{}, fmt.Errorf("context usage: %w", err)
	}

	used, lastInput, lastOutput := turnUsage(events)
	return ContextUsage{
		Used:       used,
		Limit:      m.contextLimit(ctx, *agent),
		LastInput:  lastInput,
		LastOutput: lastOutput,
	}, nil
}

// providerTypeOf resolves the provider type a turn's agent runs on: "" when
// the provider row cannot be read — capability checks then degrade
// conservatively (an unknown model reads no attachment content).
func (m *manager) providerTypeOf(ctx context.Context, agent store.Agent) string {
	if provider, err := m.store.Providers().ByID(ctx, agent.ProviderID); err == nil {
		return provider.Type
	}
	return ""
}

// contextLimit resolves the context window the agent's turns account
// against: the agent's explicit override when set (design D8), otherwise the
// model's catalog window from the provider type and model id. An
// unresolvable provider degrades to the default window rather than failing
// usage display.
func (m *manager) contextLimit(ctx context.Context, agent store.Agent) int {
	if agent.ContextWindow > 0 {
		return agent.ContextWindow
	}
	resolver := m.resolver
	if resolver == nil {
		resolver = NewCapabilityResolver(m.store)
	}
	caps, _ := resolver.Resolve(ctx, agent.ProviderID, agent.Model)
	return caps.ContextWindow
}

// broadcast fans an event out to the session's live subscriptions. It never
// blocks and never fails: subscriptions are best-effort taps, durability
// lives in the store.
func (m *manager) broadcast(sessionID string, ev sessions.Event) {
	m.mu.Lock()
	registered := make([]*streamSub, 0, len(m.subs[sessionID]))
	for _, sub := range m.subs[sessionID] {
		registered = append(registered, sub)
	}
	m.mu.Unlock()

	for _, sub := range registered {
		sub.send(ev)
	}
}

// sessionSubscription pairs a streamSub with its registration so Close can
// unregister it.
type sessionSubscription struct {
	m         *manager
	sessionID string
	id        int
	sub       *streamSub
}

func (s *sessionSubscription) Events() <-chan sessions.Event { return s.sub.Events() }

func (s *sessionSubscription) Close() {
	s.m.mu.Lock()
	regs := s.m.subs[s.sessionID]
	if regs != nil {
		delete(regs, s.id)
		if len(regs) == 0 {
			delete(s.m.subs, s.sessionID)
		}
	}
	s.m.mu.Unlock()
	s.sub.Close()
}

// now is the single clock events are stamped from (and tool durations are
// measured against, design D5). A variable so tests can drive a
// deterministic clock.
var now = func() time.Time { return time.Now().UTC() }

// newTurnID returns a random 128-bit hex turn id, mirroring the store's id
// convention.
func newTurnID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("agents: generate turn id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// Close tears the runtime's process resources down (add-create-document
// design D3): today that is the document converter's headless Chrome, which
// the converter closes idempotently — a converter that never converted
// closes as a no-op. The application's shutdown path (internal/cli App
// teardown) is where it is invoked; a converter that is never closed loses
// only its idle Chrome at process exit, the same way the browser tool's
// profiles are reaped by their own janitor.
func (m *manager) Close() error {
	return m.docgen.Close()
}
