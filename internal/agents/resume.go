package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The durable-approval runtime (spec chat-approvals, design D4): a run
// parked on a shell-gate interrupt keeps the session busy with its
// checkpoint on disk; pending state derives from the event log (no schema),
// the single decision rides the checkpoint KV ledger (consumed on read),
// and one resume path serves both the live-parked run and a run lost to a
// restart — a fresh Runner.Resume from the stored checkpoint.

// checkpointLedger adapts the store's checkpoint store onto the shell gate's
// ApprovalLedger seam: one spendable approval decision per command. Spending
// IS the delete — a decision that was found is also gone, so an identical
// command interrupts again (spec: a decision applies to exactly one
// occurrence). A store that cannot delete reports "no decision": the gate
// interrupts, which is the safe degradation.
type checkpointLedger struct {
	checkpoints store.CheckpointStore
}

// Consume spends the decision recorded under key (backend.ApprovalLedger).
func (l checkpointLedger) Consume(ctx context.Context, key string) (bool, error) {
	err := l.checkpoints.Delete(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("consume approval decision: %w", err)
	}
	return true, nil
}

// parkForApproval lands the approval_required event and parks the run (spec
// chat-approvals "Durable approval requests"): the record carries the
// interrupt and checkpoint coordinates, the session keeps its busy record
// with pending set, and the run ends WITHOUT a terminal event — the paused
// run is a busy session whose checkpoint is on disk. The record closure
// carries the cancellation-proof recorder context.
func (m *manager) parkForApproval(rt *runningTurn, record recordFn, hit *interruptHit) error {
	ev := sessions.Event{
		Kind: sessions.KindApprovalRequired,
		Approval: &sessions.Approval{
			InterruptID:  hit.interruptID,
			Command:      hit.command,
			CheckpointID: hit.checkpointID,
		},
	}
	if err := record(&ev, true); err != nil {
		return err
	}
	m.mu.Lock()
	rt.pending = &pendingApproval{
		interruptID:  hit.interruptID,
		command:      hit.command,
		checkpointID: hit.checkpointID,
		turnID:       rt.turn.id,
	}
	m.mu.Unlock()
	return nil
}

// pendingApprovalFromEvents derives the session's pending approval from its
// log (design D4 — no schema, replay-safe): the latest approval_required
// with no matching approval_resolved after it.
func pendingApprovalFromEvents(events []sessions.Event) *pendingApproval {
	resolved := make(map[string]bool)
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Approval == nil {
			continue
		}
		switch ev.Kind {
		case sessions.KindApprovalResolved:
			resolved[ev.Approval.InterruptID] = true
		case sessions.KindApprovalRequired:
			if !resolved[ev.Approval.InterruptID] {
				return &pendingApproval{
					interruptID:  ev.Approval.InterruptID,
					command:      ev.Approval.Command,
					checkpointID: ev.Approval.CheckpointID,
					turnID:       ev.TurnID,
				}
			}
		}
	}
	return nil
}

// ResolveApproval submits the user's decision for the session's pending
// approval (spec chat-approvals "Decision side-channel"). Validation runs
// against the derived pending set, so a stale id fails with
// ErrApprovalNotPending and nothing changes; a decision that cannot be
// recorded leaves the pending state intact. Approving stocks the decision
// ledger and resumes the turn so the command executes; denying feeds the
// refusal back to the interrupting tool as its resume result.
func (m *manager) ResolveApproval(ctx context.Context, sessionID, interruptID string, approved bool) error {
	events, err := m.sessions.Events(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resolve approval: %w", err)
	}
	pend := pendingApprovalFromEvents(events)
	if pend == nil || pend.interruptID != interruptID {
		return fmt.Errorf("resolve approval: %w: %s", ErrApprovalNotPending, interruptID)
	}

	// The live-parked run carries the composed surface to reuse (design D4);
	// its absence is the cold path — a restart lost the process state and
	// the resume rebuilds from the store. The resolving guard closes the
	// validate-then-append window: two concurrent resolves can both derive
	// the pending approval from the log before either append lands, so the
	// first one to take the lock claims the interrupt and the second
	// conflicts exactly like the sequential already-resolved case.
	m.mu.Lock()
	if m.resolving[interruptID] {
		m.mu.Unlock()
		return fmt.Errorf("resolve approval: %w: %s (already resolved)", ErrApprovalNotPending, interruptID)
	}
	m.resolving[interruptID] = true
	// A resolve that fails before its approval_resolved append leaves the
	// pending state intact — release the claim so a retry can proceed.
	abandon := func() {
		m.mu.Lock()
		delete(m.resolving, interruptID)
		m.mu.Unlock()
	}
	rt, live := m.running[sessionID]
	var (
		turn       *Turn
		agent      adk.TypedAgent[*schema.AgenticMessage]
		toolErrors *toolErrorResult
		usage      *sessions.Usage
		args       map[string]string
	)
	if live {
		if rt.pending == nil {
			m.mu.Unlock()
			return fmt.Errorf("resolve approval: %w: %s (already resolved)", ErrApprovalNotPending, interruptID)
		}
		turn, agent, toolErrors, usage, args = rt.turn, rt.agent, rt.toolErrors, rt.usage, rt.args
	}
	m.mu.Unlock()

	// The decision lands in the ledger BEFORE anything else moves: the
	// resumed run's gate consumes it to execute. A failure here leaves the
	// pending state intact (spec: resolve errors are conflict-safe).
	if approved {
		if err := backend.RecordDecision(ctx, m.store.Checkpoints(), pend.command); err != nil {
			abandon()
			return fmt.Errorf("resolve approval: %w", err)
		}
	}

	// The decision is now durable: the resolved event, then the resume.
	resolved := sessions.Event{
		TurnID: pend.turnID,
		Kind:   sessions.KindApprovalResolved,
		Approval: &sessions.Approval{
			InterruptID: pend.interruptID,
			Outcome:     outcomeOf(approved),
		},
	}
	recordCtx := context.WithoutCancel(m.baseCtx)
	batch := []sessions.Event{resolved}
	if err := m.sessions.Append(recordCtx, sessionID, batch); err != nil {
		// Roll the ledger entry back so an unspent approval cannot buy a
		// later execution, and leave the pending state intact.
		if approved {
			_ = m.store.Checkpoints().Delete(ctx, backend.LedgerKey(pend.command))
		}
		abandon()
		return fmt.Errorf("resolve approval: %w", err)
	}
	m.broadcast(sessionID, batch[0])

	resumed := &runningTurn{
		turn:       turn,
		sessionID:  sessionID,
		agent:      agent,
		toolErrors: toolErrors,
		usage:      usage,
		args:       args,
	}
	// The regeneration marker survives the resume (design D7): a live-parked
	// run carries it on the record; a cold resume recovers it from the
	// parked turn's persisted events.
	if live {
		resumed.variantOf = rt.variantOf
		// The resume is the same run: it stamps the original dispatch's
		// origin (design D6). A cold resume leaves it empty — the process
		// lost the record, and the resume reads as a user run.
		resumed.origin = rt.origin
	} else {
		for _, ev := range events {
			if ev.TurnID == pend.turnID && ev.VariantOf != "" {
				resumed.variantOf = ev.VariantOf
				break
			}
		}
	}
	if resumed.turn == nil {
		// Cold resume (design D4): the process lost the parked run; the
		// resumed run gets a fresh handle carrying the original turn id, so
		// the transcript keeps grouping under one turn.
		resumed.turn = &Turn{id: pend.turnID, done: make(chan struct{})}
		resumed.usage = &sessions.Usage{}
	}
	// The resumed run carries its own two-level cancel surface (design D3):
	// its context is a child of the manager's base context, so the escalation
	// hard-cancel tears down exactly this run.
	runCtx, cancel := context.WithCancel(m.baseCtx)
	resumed.cancel = cancel
	resumed.ctx = runCtx
	m.mu.Lock()
	m.running[sessionID] = resumed
	m.mu.Unlock()

	go m.resumeTurn(runCtx, resumed, pend, approved)
	return nil
}

// outcomeOf maps the decision to its event outcome.
func outcomeOf(approved bool) string {
	if approved {
		return sessions.ApprovalApproved
	}
	return sessions.ApprovalDenied
}

// resumedInstruction is the placeholder system instruction of a cold-resumed
// composition: ChatModelAgent's resume restores the checkpointed
// instruction, so the placeholder never reaches the model.
const resumedInstruction = "Resumed turn: continuing from a stored checkpoint."

// resumeTurn drives the parked turn's continuation from its checkpoint
// (design D4): with the composed agent still held, reuse it; after a loss,
// rebuild the turn's composition from the store (the phases of runTurn from
// the model build down — the user-message persist and the prompt gate are
// skipped, and the compaction gate stays out because the checkpoint restores
// the working context as it was) and let ChatModelAgent's resume restore the
// instruction/tool state from the checkpoint.
func (m *manager) resumeTurn(ctx context.Context, rt *runningTurn, pend *pendingApproval, approved bool) {
	defer rt.cancel()
	turnID := rt.turn.id
	recordCtx := context.WithoutCancel(ctx)

	// The turn's configuration is read fresh (design D1: the session row's
	// overrides win, the agent's defaults inherit).
	sess, err := m.sessions.Session(ctx, rt.sessionID)
	if err != nil {
		m.failResume(rt, recordCtx, nil, hooks.Event{}, fmt.Errorf("resume turn: %w", err))
		return
	}
	agent, err := m.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		m.failResume(rt, recordCtx, nil, hooks.Event{}, fmt.Errorf("resume turn: %w", err))
		return
	}

	// The record closure is runTurn's, bound to the resume: durable events
	// persist and broadcast on a recorder that outlives any cancellation. A
	// resumed regeneration keeps its variant marker (design D7).
	record := func(ev *sessions.Event, durable bool) error {
		ev.TurnID = turnID
		if rt.variantOf != "" {
			ev.VariantOf = rt.variantOf
		}
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = now()
		}
		if durable {
			batch := []sessions.Event{*ev}
			if err := m.sessions.Append(recordCtx, rt.sessionID, batch); err != nil {
				return err
			}
			*ev = batch[0]
		}
		m.broadcast(rt.sessionID, *ev)
		return nil
	}

	// The hook chain resolves fresh for the resumed run (the ported
	// lifecycle shape: run started, then finished with the terminal status);
	// the prompt gate is skipped — the prompt was accepted before the pause.
	hookChain, err := m.resolveTurnHooks(ctx, *agent)
	if err != nil {
		m.failResume(rt, recordCtx, nil, hooks.Event{}, fmt.Errorf("resume turn: %w", err))
		return
	}
	hookBase := hookBaseEvent(*agent, rt.sessionID, turnID, rt.origin)
	if hookChain.HasHooks() {
		hookChain.ObserveRunStarted(recordCtx, hookBase)
	}
	fail := func(err error) error {
		// A cancel converts any failure into the cancelled terminal, exactly
		// like runTurn's failTurn (design D3): the user's stop wins over how
		// the torn-down run errored.
		if rt.cancelRequested.Load() {
			m.recordCancelledTerminal(recordCtx, rt, hookChain, hookBase)
			return ErrTurnCancelled
		}
		turnErr := sessions.Event{Kind: sessions.KindTurnError, Error: err.Error()}
		_ = record(&turnErr, true)
		if hookChain.HasHooks() {
			hookChain.RunFinished(recordCtx, hookBase, hookRunStatusFailed)
		}
		return err
	}

	run, err := m.composeResumeRun(ctx, rt, sess, *agent, pend, approved, hookChain, hookBase, record)
	if err != nil {
		m.clearBusy(rt)
		rt.turn.finish(fail(err))
		return
	}
	err = m.executeTurnRun(ctx, recordCtx, rt, record, fail, hookChain, hookBase, run)
	if errors.Is(err, errTurnParked) {
		// The resumed run parked on another approval: the session stays busy
		// and the Turn stays open, exactly like the first pause.
		return
	}
	m.clearBusy(rt)
	rt.turn.finish(err)
}

// failResume ends a resume whose setup failed before the run could take the
// failure path: the turn_error is persisted best-effort and the session is
// released. The pending approval was already resolved — the resume owns the
// outcome now.
func (m *manager) failResume(rt *runningTurn, recordCtx context.Context, hookChain *hooks.Resolved, hookBase hooks.Event, err error) {
	turnErr := sessions.Event{TurnID: rt.turn.id, Kind: sessions.KindTurnError, Error: err.Error()}
	batch := []sessions.Event{turnErr}
	if aerr := m.sessions.Append(recordCtx, rt.sessionID, batch); aerr != nil {
		slog.Error("record resume failure", "session", rt.sessionID, "error", aerr)
	}
	m.broadcast(rt.sessionID, batch[0])
	if hookChain != nil && hookChain.HasHooks() {
		hookChain.RunFinished(recordCtx, hookBase, hookRunStatusFailed)
	}
	m.clearBusy(rt)
	rt.turn.finish(err)
}

// clearBusy removes the session's busy record.
func (m *manager) clearBusy(rt *runningTurn) {
	m.mu.Lock()
	delete(m.running, rt.sessionID)
	m.mu.Unlock()
}

// composeResumeRun builds the resumed run's execute inputs. With the parked
// process state intact the composed agent is reused as-is (design D4); a
// cold resume rebuilds the turn's composition from the store — model,
// persona documents, tool surface, delegated-task space, skills, MCP —
// mirroring runTurn's phases 3 and 5 through the 9-merge.
func (m *manager) composeResumeRun(ctx context.Context, rt *runningTurn, sess sessions.Session, agent store.Agent, pend *pendingApproval, approved bool, hookChain *hooks.Resolved, hookBase hooks.Event, _ recordFn) (turnRun, error) {
	deny := func() map[string]any {
		if approved {
			return nil
		}
		return map[string]any{pend.interruptID: backend.DenialResult(pend.command)}
	}

	// Live resume: the composed surface from the interrupted run is still
	// held — reuse it.
	if rt.agent != nil {
		return turnRun{
			agent:        rt.agent,
			toolErrors:   rt.toolErrors,
			checkpointID: pend.checkpointID,
			resume:       true,
			resumeData:   deny(),
			planDir:      m.planDirForSlug(agent.Slug),
		}, nil
	}

	// Cold rebuild.
	override := ModelOverride{Model: sess.Model, Effort: sess.Effort}
	if override.Effort == "" {
		override.Effort = agent.Effort
	}

	// Phase 3: the model from the agent's provider with the session's
	// effective overrides.
	mdl, err := m.registry.Build(ctx, agent, override)
	if err != nil {
		return turnRun{}, err
	}

	// Phase 5: the persona documents. The resumed run's instruction is
	// restored from the checkpoint (ChatModelAgent resume), so a read
	// failure only degrades the placeholder — the checkpoint's instruction
	// is what the model sees.
	instruction := resumedInstruction
	if identity, soul, bootstrap, derr := m.documents(ctx, agent); derr == nil {
		instruction = persona{identity: identity, soul: soul, bootstrap: bootstrap}.text()
	}

	// Phase 6: the tool surface through the three-way gate.
	surface, err := m.resolveTurnTools(ctx, agent)
	if err != nil {
		return turnRun{}, err
	}

	// Phase 7: the delegated-task space and the skill capability.
	recordCtx := context.WithoutCancel(m.baseCtx)
	tasks := m.sessionTaskRegistry(sess.ID, func(ev *sessions.Event, durable bool) error {
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = now()
		}
		if durable {
			batch := []sessions.Event{*ev}
			if err := m.sessions.Append(recordCtx, sess.ID, batch); err != nil {
				return err
			}
			*ev = batch[0]
		}
		m.broadcast(sess.ID, *ev)
		return nil
	})
	skillBackend, err := m.resolveSkills(ctx, agent)
	if err != nil {
		return turnRun{}, err
	}

	// Phase 8: compose the ADK agent. The instruction is a placeholder (the
	// checkpoint restores the real one); the capability middlewares and the
	// tool surface must match the interrupted composition's structure.
	cfg := &composeConfig{
		Name:        agent.Name,
		Instruction: instruction,
		ChatModel:   mdl,
	}
	var skillsCap *skillsCapability
	if skillBackend != nil {
		skillsCap = &skillsCapability{Backend: skillBackend}
	}
	var extraRoots []string
	if surface.agentDir != "" {
		fsCap := &filesystemCapability{AgentDir: surface.agentDir, DisabledTools: surface.fsDisabled}
		if surface.shell {
			fsCap.Shell = backend.NewJailedShell(surface.agentDir,
				backend.WithApprovalGate(checkpointLedger{checkpoints: m.store.Checkpoints()}))
		}
		if skillsCap != nil && m.skillsDir != "" {
			fsCap.ExtraRoots = []string{m.skillsDir}
		}
		// The read-only corpus mount rides the jail when the gate exposed it
		// (add-knowledge-corpus design D7), matching the fresh-turn shape.
		fsCap.Knowledge = surface.knowledge
		cfg.Filesystem = fsCap

		extraRoots = fsCap.ExtraRoots
		cfg.Reduction = &reductionCapability{
			AgentDir:     surface.agentDir,
			ExtraRoots:   extraRoots,
			ContextLimit: int64(m.contextLimit(ctx, agent)),
		}
		cfg.Plantask = &plantaskCapability{
			AgentDir:      surface.agentDir,
			ExtraRoots:    extraRoots,
			DisabledTools: surface.plantaskDisabled,
		}
		cfg.DisabledPlantaskTools = surface.plantaskDisabled
	}
	cfg.Summarization = &summarizationCapability{
		Model:        mdl,
		ContextLimit: int64(m.contextLimit(ctx, agent)),
	}
	if skillsCap != nil {
		cfg.Skills = skillsCap
	}
	var hookCap *hooksCapability
	if hookChain.HasHooks() {
		hookCap = &hooksCapability{Resolved: hookChain, Base: hookBase}
		cfg.Hooks = hookCap
	}

	// Phases 8.5 and 9-merge: the delegation tools and the MCP tools join
	// the surface exactly as a fresh turn resolves them.
	mcpTools, err := m.resolveMCPTools(ctx, agent)
	if err != nil {
		return turnRun{}, err
	}
	if agent.SubagentsEnabled {
		tasksDir := ""
		if surface.agentDir != "" && m.workspaces != nil && agent.Slug != "" {
			dir, err := m.workspaces.TasksDir(agent.Slug)
			if err != nil {
				return turnRun{}, fmt.Errorf("resolve tasks directory for agent %q: %w", agent.Slug, err)
			}
			tasksDir = dir
		}
		tasks.SetTurnID(rt.turn.id)
		subCaps, err := subagents.New(ctx, &subagents.Config{
			Instruction:      instruction,
			Model:            mdl,
			Tools:            surface.tools,
			MCPTools:         mcpTools,
			AgentDir:         surface.agentDir,
			ExtraRoots:       extraRoots,
			Knowledge:        surface.knowledge,
			Shell:            surface.shell,
			FSDisabled:       surface.fsDisabled,
			PlantaskDisabled: surface.plantaskDisabled,
			ContextLimit:     int64(m.contextLimit(ctx, agent)),
			TasksDir:         tasksDir,
			TurnID:           rt.turn.id,
			Skills:           skillsCap,
			Hooks:            hookCap,
			Checkpoints:      backend.WithApprovalGate(checkpointLedger{checkpoints: m.store.Checkpoints()}),
			Registry:         tasks,
		})
		if err != nil {
			return turnRun{}, err
		}
		cfg.Subagent = &subagentCapability{Middleware: subCaps.SubagentMiddleware, ArgsMiddleware: subCaps.DelegationArgsMiddleware}
		cfg.BackgroundTask = &backgroundTaskCapability{Middleware: subCaps.BackgroundTaskMiddleware}
	}
	if err := assertNoDelegationToolCollision(surface.tools); err != nil {
		return turnRun{}, err
	}
	cfg.Tools = append(append([]tool.BaseTool(nil), surface.tools...), mcpTools...)

	turnAgent, toolErrors, err := composeTurnAgent(ctx, cfg)
	if err != nil {
		return turnRun{}, err
	}
	return turnRun{
		agent:        turnAgent,
		toolErrors:   toolErrors,
		checkpointID: pend.checkpointID,
		resume:       true,
		resumeData:   deny(),
		planDir:      planDirFor(surface.agentDir),
	}, nil
}
