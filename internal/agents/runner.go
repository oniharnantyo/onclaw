package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/agents/backend"
	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// runTurn executes one turn end to end and returns its terminal outcome
// (nil on success, ErrTurnCancelled after a cancellation, errTurnParked when
// the turn parked on a pending approval). The caller clears the session's
// busy record and finishes the Turn with the returned error — in that order,
// so a completed Turn never coexists with a still-busy session flag. A
// parked turn leaves BOTH to the resolve/cancel path (design D4).
//
// ctx is the turn's cancellable context (design D3): the cancel escalation
// hard-stops the run through it.
//
// Ordering contract: the user message is persisted first (an errored turn
// still shows what was asked), the hook chain is resolved fresh and the
// prompt gated on user_prompt_submit hooks BEFORE any provider work (spec
// agent-hooks: a blocked prompt makes no model call), the compaction gate
// runs next (it must decide on the log BEFORE this turn's user message
// lands), then the turn's tool surface is resolved and the ADK agent
// composed for this turn (design D1: resolve → compose → execute), and the
// turn's outputs are persisted and broadcast as the agent's tool loop
// streams. Persistence is unconditional: nothing here reads or waits on any
// subscriber.
func (m *manager) runTurn(ctx context.Context, rt *runningTurn, sess sessions.Session, agent store.Agent, override ModelOverride, req turnRequest) error {
	turnID := rt.turn.id
	// The regeneration marker rides the running record so every recorder —
	// the record closure below, the cancelled terminal, a resumed run —
	// stamps it (design D7).
	rt.variantOf = req.variantOf
	// recordCtx serves recorders that must outlive the turn: the terminal
	// recording after a cancellation killed the turn's context (design D3),
	// and a background delegation completing after its parent turn returned
	// still appends to the session transcript (spec agent-subagents "Task
	// completion surfacing").
	recordCtx := context.WithoutCancel(ctx)

	// The turn's hook chain and event identity (see hooks.go): resolved
	// once, below, and read by failTurn's run_finished observer whenever a
	// turn fails after resolution. Nil until resolution succeeds — the first
	// failTurn call site is the resolution error itself.
	var hookChain *hooks.Resolved
	hookBase := hooks.Event{}

	// record persists (when durable) and broadcasts. A persistence failure
	// is a turn failure: durability is the contract, so the turn aborts
	// rather than continue silently unpersisted. Appended events are copied
	// back so the broadcast carries the store-assigned identity. Recording
	// rides recordCtx: the terminal events of a cancelled turn must land
	// after the turn's context died (design D3). A regeneration's events all
	// carry the variant_of marker (design D7) so replay reconstructs the
	// branch grouping.
	record := func(ev *sessions.Event, durable bool) error {
		ev.TurnID = turnID
		if req.variantOf != "" {
			ev.VariantOf = req.variantOf
		}
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
	}
	// failTurn persists the failure notice best-effort — if the store is the
	// failing component there is nothing left to do — fires the
	// run_finished observer with the failed status once the terminal event
	// settled (a no-op until the chain resolved), and hands the outcome to
	// the caller. A cancel request converts any failure into the cancelled
	// terminal instead: the user's stop wins over however the torn-down run
	// errored (design D3).
	failTurn := func(err error) error {
		if rt.cancelRequested.Load() {
			m.recordCancelledTerminal(recordCtx, rt, hookChain, hookBase)
			return ErrTurnCancelled
		}
		turnErr := sessions.Event{Kind: sessions.KindTurnError, Error: err.Error()}
		_ = record(&turnErr, true)
		if hookChain != nil && hookChain.HasHooks() {
			hookChain.RunFinished(recordCtx, hookBase, hookRunStatusFailed)
		}
		return err
	}

	// Turn-boundary artifact detection (add-turn-artifacts design D3): the
	// start record rides the running turn — the same lifetime as usage — so
	// a live approval resume keeps it and the completion site diffs against
	// it. A record already held (a re-entered run) or a cold-resume
	// predicate skips the fresh snapshot. Any failure here logs and leaves
	// the record empty: the turn runs with detection disabled, never fails
	// (spec chat-artifacts "Artifacts path blocked by a regular file").
	if rt.artifactsBefore == nil && rt.artifactsSince.IsZero() && m.workspaces != nil {
		dir, err := m.workspaces.ArtifactsDir(agent.Slug)
		if err != nil {
			slog.Warn("artifacts: resolve artifacts directory", "session", rt.sessionID, "agent", agent.Slug, "error", err)
		} else if before, err := snapshotArtifacts(dir); err != nil {
			slog.Warn("artifacts: snapshot at turn start", "session", rt.sessionID, "agent", agent.Slug, "error", err)
		} else {
			rt.artifactsBefore = before
		}
	}

	// 1. Load persisted state: the full log decides compaction and context.
	// The compaction boundary is the log position before this turn's input.
	events, err := m.sessions.Events(ctx, sess.ID)
	if err != nil {
		return err
	}
	upToSeq := compactableUpTo(events)
	checkpoint, _, err := m.sessions.Checkpoint(ctx, sess.ID)
	if err != nil {
		return err
	}

	// 2. Persist the user message before anything can fail on the provider
	// path; an errored turn still shows what was asked. The payload carries
	// the send's attachment metadata for replay (design D5) and the resolved
	// dispatch origin (add-telegram-channel 9.2) so a replayed transcript
	// names who started the turn. A regeneration suppresses the persist
	// (design D7 — the log keeps exactly one user message per prompt) and
	// broadcasts the marker instead.
	userMsg := sessions.Event{
		Kind:        sessions.KindUserMessage,
		Text:        req.message,
		Attachments: req.attachments,
		VariantOf:   req.variantOf,
		Origin:      dispatchOrigin(req.origin),
	}
	if req.suppressUserPersist {
		userMsg.TurnID = turnID
		userMsg.OccurredAt = now()
		m.broadcast(sess.ID, userMsg)
	} else {
		if err := record(&userMsg, true); err != nil {
			return err
		}
		// Auto-title: an untitled session adopts its first message's first
		// line — or, on an attachment-only send, the first file's name.
		// Cosmetic: a failed rename must not fail the turn.
		if sess.Title == "" {
			_ = m.sessions.SetTitle(ctx, sess.ID, titleFromMessage(titleSource(req.message, req.attachments)))
		}
	}

	// 2.5 Resolve the hook chain fresh for this turn (design D4, spec
	// agent-hooks) and gate the prompt on user_prompt_submit hooks BEFORE
	// any model or provider work: a block ends the turn with a durable,
	// user-visible notice and NO model call. On allow, run_started is
	// observed (detached) and the chain rides the composition.
	hookChain, err = m.resolveTurnHooks(ctx, agent)
	if err != nil {
		return failTurn(err)
	}
	if hookChain.HasHooks() {
		hookBase = hookBaseEvent(agent, sess.ID, turnID, req.origin)
		if blocked, hookName, reason := hookChain.EvaluatePromptSubmission(ctx, hookBase); blocked {
			return blockedPromptTurn(recordCtx, record, hookChain, hookBase, hookName, reason)
		}
		hookChain.ObserveRunStarted(recordCtx, hookBase)
	}

	// 3. Build the model from the agent's configured provider with the
	// session's effective model/effort overrides applied (design D1/D2: the
	// caller resolved them from the session row). Unconfigured providers and
	// rejected overrides fail the turn with a typed error; the turn_error
	// event persisted above carries the message to the chat UI (spec: clear
	// error instead of silently failing).
	mdl, err := m.registry.Build(ctx, agent, override)
	if err != nil {
		return failTurn(err)
	}

	// The turn's usage accumulator is the running record's own (design D3):
	// a resumed run folds its usage into the same accumulator, so the
	// terminal event carries the whole turn's total.
	usage := rt.usage

	// 4. Compaction gate: when the working context crossed the threshold of
	// the model's limit, summarize everything before this turn into a fresh
	// checkpoint and mark it. The stored transcript is never truncated.
	if compactionNeeded(usedTokens(events), m.contextLimit(ctx, agent)) &&
		hasCompactableView(events, upToSeq) {
		marker, err := m.runCompaction(ctx, sess.ID, turnID, mdl, events, upToSeq)
		if err != nil {
			return failTurn(err)
		}
		foldUsage(usage, marker.Usage)
		if err := record(&marker, true); err != nil {
			return failTurn(err)
		}
		checkpoint = sessions.Checkpoint{
			Summary: marker.Compaction.Summary,
			UpToSeq: upToSeq,
		}
	}

	// 5. Compose the persona from the agent's workspace documents, read
	// fresh from disk for this turn (design D11/D12: an external edit applies
	// to the next turn for free). A real read failure omits the documents
	// entirely and fails the turn through the error path — no half-read
	// persona is ever composed, and the transcript records the failure, not
	// corrupted content.
	identity, soul, bootstrap, err := m.documents(ctx, agent)
	if err != nil {
		return failTurn(err)
	}

	// 6. Resolve the turn's tool surface through the three-way gate: catalog
	// × global enable × agent allowlist (spec agent-tools "Tool gate"),
	// workspace-rooted tools rooted at the agent's workspace.
	surface, err := m.resolveTurnTools(ctx, agent)
	if err != nil {
		return failTurn(err)
	}

	// 7. Delegated-task space and skill capability (design D3/D6). The task
	// registry is the session's task-ID space (spec agent-subagents
	// "Background lane"): background tasks complete independently of the
	// turn that spawned them — they outlive it, stay addressable through
	// task_output/task_stop in later turns, and their terminal records land
	// on the session transcript whenever they finish. The recorder is bound
	// to a cancellation-proof context for exactly that reason, and the
	// per-session busy guard makes each turn's install race-free.
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

	// The skill capability (design D6): the two-tier backend reads both skill
	// trees fresh for this turn and carries the eligibility gate. A nil
	// backend (capability off, or an agent without a workspace) composes a
	// skill-free turn.
	skillBackend, err := m.resolveSkills(ctx, agent)
	if err != nil {
		return failTurn(err)
	}
	// $name explicit invocation: eligible tokens become blocking
	// instructions in the MODEL input only — the persisted user message
	// above keeps the user's raw words, and the session title was derived
	// from them already. The expanded text becomes the fresh input's text
	// below (req is a value copy; the request's own fields stay as
	// dispatched).
	req.message = injectSkillInvocations(ctx, skillBackend, req.message)

	// 8. Compose the ADK agent for this turn: system instruction + tools →
	// ChatModelAgent with the tool loop (design D1). The instruction is the
	// composed persona, the operating contract, and the compaction summary;
	// capability middlewares append their own guidance (the fs workspace
	// mount, the skills guidance)
	// to it. The model input is assembled with the effective model's
	// attachment capabilities (task 5.3): the override's model — or the
	// agent's configured one — decides what rides as content.
	resolver := m.resolver
	if resolver == nil {
		resolver = NewCapabilityResolver(m.store)
	}
	effModel := effectiveModel(agent, override)
	caps, _ := resolver.Resolve(ctx, agent.ProviderID, effModel)

	instruction, inputMsgs, err := assembleTurnInput(
		persona{identity: identity, soul: soul, bootstrap: bootstrap}, checkpoint, events, req,
		turnComposer{
			providerType: m.providerTypeOf(ctx, agent),
			model:        effModel,
			atts:         m.attachments,
			caps:         caps,
			hasCaps:      true,
			knowledge:    surface.knowledge != nil,
			skills:       skillBackend != nil,
			presets:      m.presetGuidance(ctx, surface.createDocument),
			memory:       m.memoryBlock(ctx, agent),
		})
	if err != nil {
		return failTurn(err)
	}
	cfg := &composeConfig{
		Name:        agent.Name,
		Instruction: instruction,
		ChatModel:   mdl,
	}
	var skillsCap *skillsCapability
	// The global skills tree rides the jail as a read-only extra root
	// (design D6): skill bodies may reference supporting files beside them;
	// the agent tier lives inside the workspace already. It is also what the
	// delegated child's jail mounts.
	if skillBackend != nil {
		skillsCap = &skillsCapability{Backend: skillBackend}
	}
	var extraRoots []string
	if surface.agentDir != "" {
		fsCap := &filesystemCapability{AgentDir: surface.agentDir, DisabledTools: surface.fsDisabled}
		if surface.shell {
			// The dangerous-command gate rides the shell (design D4): a
			// safety-pattern match interrupts the run for a human decision;
			// the decision ledger lives in the checkpoint store under the
			// approval/ prefix.
			fsCap.Shell = backend.NewJailedShell(surface.agentDir,
				backend.WithApprovalGate(checkpointLedger{checkpoints: m.store.Checkpoints()}))
		}
		if skillsCap != nil && m.skillsDir != "" {
			fsCap.ExtraRoots = []string{m.skillsDir}
		}
		// The read-only corpus mount rides the jail when the gate exposed it
		// (add-knowledge-corpus design D7): rootable agent, fs tools enabled.
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

	// 8.5 Subagents (spec agent-subagents, design D3, D4): when the agent has the
	// capability enabled, the turn is offered the delegation tools via eino middlewares.
	mcpTools, err := m.resolveMCPTools(ctx, agent)
	if err != nil {
		return failTurn(err)
	}
	if agent.SubagentsEnabled {
		tasksDir := ""
		if surface.agentDir != "" && m.workspaces != nil && agent.Slug != "" {
			dir, err := m.workspaces.TasksDir(agent.Slug)
			if err != nil {
				return failTurn(fmt.Errorf("resolve tasks directory for agent %q: %w", agent.Slug, err))
			}
			tasksDir = dir
		}
		tasks.SetTurnID(turnID)
		// The child gets persona-only text: the full instruction carries the
		// contract's user-facing mode guidance, which never rides a child
		// turn (spec agent-prompts "Subagent child operating notes"); the
		// child's own reduced notes are composed in the subagents package.
		subCaps, err := subagents.New(ctx, &subagents.Config{
			Instruction:      persona{identity: identity, soul: soul, bootstrap: bootstrap}.text(),
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
			TurnID:           turnID,
			Skills:           skillsCap,
			Hooks:            hookCap,
			Checkpoints:      backend.WithApprovalGate(checkpointLedger{checkpoints: m.store.Checkpoints()}),
			Registry:         tasks,
		})
		if err != nil {
			return failTurn(err)
		}
		cfg.Subagent = &subagentCapability{Middleware: subCaps.SubagentMiddleware, ArgsMiddleware: subCaps.DelegationArgsMiddleware}
		cfg.BackgroundTask = &backgroundTaskCapability{Middleware: subCaps.BackgroundTaskMiddleware}
	}
	if err := assertNoDelegationToolCollision(surface.tools); err != nil {
		return failTurn(err)
	}
	cfg.Tools = append(append([]tool.BaseTool(nil), surface.tools...), mcpTools...)

	turnAgent, toolErrors, err := composeTurnAgent(ctx, cfg)
	if err != nil {
		return failTurn(err)
	}

	// 9–10. Execute the turn through the checkpointed Runner and record the
	// terminal outcome (design D4: the Runner is the interrupt/resume
	// substrate; the drain mapping and the terminal recording are the
	// runTurn contract).
	return m.executeTurnRun(ctx, recordCtx, rt, record, failTurn, hookChain, hookBase, agent, turnRun{
		agent:        turnAgent,
		toolErrors:   toolErrors,
		input:        inputMsgs,
		checkpointID: "adk_" + turnID,
		planDir:      planDirFor(surface.agentDir),
	})
}

// dispatchOrigin resolves the origin a turn's user_message event records
// (design D6): an empty dispatch origin reads as the user default — the same
// normalization the hook base event and the environment block apply.
func dispatchOrigin(origin string) string {
	if origin == "" {
		return hooks.OriginUser
	}
	return origin
}

// turnRun carries the execute phase's composed inputs (design D4): the
// executable agent, its tool-failure registry, the fresh-run model input
// (empty on a resume — the checkpoint restores the working context), the
// checkpoint id the run persists its interrupt state under, and — on a
// resumed run — the resume shape: which checkpoint to drive and, for a
// denial, the resume data fed back to the interrupting tool.
type turnRun struct {
	agent        adk.TypedAgent[*schema.AgenticMessage]
	toolErrors   *toolErrorResult
	input        []*schema.AgenticMessage
	checkpointID string
	resume       bool
	resumeData   map[string]any
	// planDir is the agent's plan directory (the plantask tools' task files);
	// empty when the turn has no rooted workspace. The drain snapshots it
	// after each plan change.
	planDir string
}

func assertNoDelegationToolCollision(tools []tool.BaseTool) error {
	for _, t := range tools {
		if t == nil {
			continue
		}
		info, err := t.Info(context.Background())
		if err != nil {
			continue
		}
		if info.Name == subagents.ToolAgent || info.Name == subagents.ToolTaskOutput || info.Name == subagents.ToolTaskStop {
			return fmt.Errorf("tool name %q collides with delegation tool", info.Name)
		}
	}
	return nil
}

// executeTurnRun runs the composed agent through the checkpointed ADK Runner
// (design D4) and maps the drain onto the transcript exactly as before: a
// run that ends on the iteration cap leaves a visible notice, any other run
// error takes the failure path, a tool interrupt parks the run WITHOUT a
// terminal event (the session stays busy, its checkpoint on disk), and a
// clean end persists the turn's completion carrying the accumulated usage.
// agent is the turn's agent row — the artifacts directory resolves through
// its slug at the terminal (add-turn-artifacts design D3).
func (m *manager) executeTurnRun(ctx context.Context, recordCtx context.Context, rt *runningTurn, record recordFn, failTurn func(error) error, hookChain *hooks.Resolved, hookBase hooks.Event, agent store.Agent, run turnRun) error {
	runner := adk.NewTypedRunner[*schema.AgenticMessage](adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           run.agent,
		SessionID:       rt.sessionID,
		EnableStreaming: true,
		CheckPointStore: m.store.Checkpoints(),
	})
	// The two-level cancel (design D3): the safe-point cancel rides the run
	// options — the runtime stops the run at its next safe point (after the
	// model call / after the tool call, keeping in-flight tool writes
	// intact) — and Cancel arms the escalation to the turn context's hard
	// cancel. The record hands the cancel func to the session's running-turn
	// entry before the run starts, so a cancel racing the composition is
	// still routed through.
	cancelOpt, adkCancel := adk.WithCancel()
	m.mu.Lock()
	rt.adkCancel = adkCancel
	alreadyRequested := rt.cancelRequested.Load()
	m.mu.Unlock()
	if alreadyRequested {
		// Cancel fired while the run was composing; the escalation timer is
		// already armed — route the request through the just-registered
		// safe-point cancel as well.
		_, _ = adkCancel(adk.WithAgentCancelMode(adk.CancelAfterChatModel | adk.CancelAfterToolCalls))
	}
	runOpts := []adk.AgentRunOption{cancelOpt, adk.WithCheckPointID(run.checkpointID)}

	// 9. Execute the turn: the agent's tool loop streams; every model tool
	// call emits a live-only running ToolActivity, executes through the
	// jailed surface, and lands its terminal record (completed/failed + real
	// Result) as a persisted event whose broadcast is the live finish
	// (design D10). Deltas stay live-only — the durable form of a response is
	// the assistant message event persisted as each model call completes.
	var iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]
	if run.resume {
		var rerr error
		if run.resumeData != nil {
			// A denial resumes WITH data: the interrupting tool receives the
			// refusal as its resume result and reports it to the model (spec
			// chat-approvals "Deny informs the model").
			iter, rerr = runner.ResumeWithParams(ctx, run.checkpointID,
				&adk.ResumeParams{Targets: run.resumeData}, runOpts...)
		} else {
			iter, rerr = runner.Resume(ctx, run.checkpointID, runOpts...)
		}
		if rerr != nil {
			return failTurn(rerr)
		}
	} else {
		iter = runner.Run(ctx, run.input, runOpts...)
	}
	res, drainErr := drainTurn(iter, record, rt.usage, run.toolErrors, rt.args, rt, run.planDir)
	// The drain's observations land on the record BEFORE any error takes the
	// failure path: the partial output persists on a cancelled terminal
	// (design D3) and the call-argument memory seeds a resumed run's terminal
	// tool records (the interrupted run's announcements were live-only).
	rt.partial = res.partialText
	rt.args = res.args
	if drainErr != nil {
		return failTurn(drainErr)
	}
	if res.interrupt != nil {
		if perr := m.parkForApproval(rt, record, res.interrupt); perr != nil {
			return failTurn(perr)
		}
		return errTurnParked
	}

	// 10. Persist the turn's completion carrying the accumulated usage, then
	// fire the run_finished observer with the completed status (detached —
	// spec agent-hooks: the run never waits on a hook). A cancel that raced
	// a clean end leaves the completion standing: the run was never
	// interrupted, so there is nothing to cancel.
	usageCopy := sessions.Usage{}
	if rt.usage != nil {
		usageCopy = *rt.usage
	}
	completed := sessions.Event{Kind: sessions.KindTurnCompleted, Usage: &usageCopy}
	// The artifact delta rides the terminal event (add-turn-artifacts
	// design D4): a live start record diffs against a fresh listing, a cold
	// resume falls back to the mtime predicate, a disabled detection carries
	// none. A listing failure reports no artifacts — it never fails the
	// turn.
	completed.Artifacts, completed.ArtifactsTruncated = m.turnArtifacts(agent, rt)
	if err := record(&completed, true); err != nil {
		return failTurn(err)
	}
	if hookChain.HasHooks() {
		hookChain.RunFinished(recordCtx, hookBase, hookRunStatusCompleted)
	}
	return nil
}

// turnArtifacts computes the artifact list a turn_completed event carries
// (add-turn-artifacts design D3/D4): with the start record held — a fresh
// turn or a live approval resume — the completion re-lists and diffs;
// after a process loss the mtime predicate (the persisted user message's
// occurrence time) takes over with the change kind omitted; detection that
// was disabled carries nothing. A listing failure logs and reports nothing:
// detection must never fail the turn.
func (m *manager) turnArtifacts(agent store.Agent, rt *runningTurn) ([]sessions.Artifact, bool) {
	if m.workspaces == nil {
		return nil, false
	}
	switch {
	case rt.artifactsBefore != nil:
		dir, err := m.workspaces.ArtifactsDir(agent.Slug)
		if err != nil {
			slog.Warn("artifacts: resolve artifacts directory", "session", rt.sessionID, "agent", agent.Slug, "error", err)
			return nil, false
		}
		after, err := snapshotArtifacts(dir)
		if err != nil {
			slog.Warn("artifacts: listing at turn completion", "session", rt.sessionID, "agent", agent.Slug, "error", err)
			return nil, false
		}
		return diffArtifacts(rt.artifactsBefore, after)
	case !rt.artifactsSince.IsZero():
		dir, err := m.workspaces.ArtifactsDir(agent.Slug)
		if err != nil {
			slog.Warn("artifacts: resolve artifacts directory", "session", rt.sessionID, "agent", agent.Slug, "error", err)
			return nil, false
		}
		arts, truncated, err := listArtifactsSince(dir, rt.artifactsSince)
		if err != nil {
			slog.Warn("artifacts: listing at turn completion", "session", rt.sessionID, "agent", agent.Slug, "error", err)
			return nil, false
		}
		return arts, truncated
	default:
		return nil, false
	}
}

// recordCancelledTerminal lands the cancelled terminal (design D3, spec
// agent-runtime "Turn cancellation"): the turn_cancelled event — with the
// in-flight model call's partial output, when one was streaming — persists
// through recordCtx after the turn's context died, and the run_finished
// observer sees the cancelled status. Best-effort like failTurn: a store
// failure cannot block the cancellation.
func (m *manager) recordCancelledTerminal(recordCtx context.Context, rt *runningTurn, hookChain *hooks.Resolved, hookBase hooks.Event) {
	usageCopy := sessions.Usage{}
	if rt.usage != nil {
		usageCopy = *rt.usage
	}
	cancelled := sessions.Event{
		TurnID:    rt.turn.id,
		Kind:      sessions.KindTurnCancelled,
		Text:      rt.partial,
		Usage:     &usageCopy,
		VariantOf: rt.variantOf,
	}
	batch := []sessions.Event{cancelled}
	if err := m.sessions.Append(recordCtx, rt.sessionID, batch); err != nil {
		slog.Error("record cancelled terminal", "session", rt.sessionID, "error", err)
	}
	m.broadcast(rt.sessionID, batch[0])
	if hookChain != nil && hookChain.HasHooks() {
		hookChain.RunFinished(recordCtx, hookBase, hookRunStatusCancelled)
	}
}

// blockedPromptTurn ends a hook-blocked submission (spec agent-hooks
// "Decision contract"): the reason rides a durable, user-visible notice, the
// turn completes normally, and the observers see the full lifecycle (run
// started, run finished completed — onclaw's blocked-turn shape). The model
// is never called, so a blocked prompt spends zero tokens.
func blockedPromptTurn(ctx context.Context, record recordFn, chain *hooks.Resolved, base hooks.Event, hookName, reason string) error {
	notice := sessions.Event{Kind: sessions.KindAssistantMessage, Text: blockedPromptNotice(hookName, reason)}
	if err := record(&notice, true); err != nil {
		return err
	}
	completed := sessions.Event{Kind: sessions.KindTurnCompleted, Usage: &sessions.Usage{}}
	if err := record(&completed, true); err != nil {
		return err
	}
	chain.ObserveRunStarted(ctx, base)
	chain.RunFinished(ctx, base, hookRunStatusCompleted)
	return nil
}

// recordFn persists (when durable) and broadcasts one turn event; the runTurn
// closure implements it.
type recordFn func(ev *sessions.Event, durable bool) error

// errTurnParked ends a run whose turn parked on a pending approval: the run
// loop is over, the session stays busy, and the Turn stays open until the
// approval is resolved or the turn is cancelled (design D4 — the paused run
// is a busy session whose checkpoint is on disk).
var errTurnParked = errors.New("turn parked on pending approval")

// interruptHit is the drain's interrupt observation: the coordinates the
// approval flow parks the run on (design D4).
type interruptHit struct {
	interruptID  string
	command      string
	checkpointID string
}

// interruptOf extracts the approval coordinates from an interrupt event: the
// root-cause context's id (the resume target key) and, when the interrupt
// came from the shell gate, the requested command.
func interruptOf(info *adk.InterruptInfo) *interruptHit {
	if info == nil {
		return nil
	}
	hit := &interruptHit{checkpointID: info.CheckPointID}
	for _, ic := range info.InterruptContexts {
		if !ic.IsRootCause {
			continue
		}
		hit.interruptID = ic.ID
		if shellInfo, ok := ic.Info.(backend.ShellApprovalInfo); ok {
			hit.command = shellInfo.Command
		}
		break
	}
	return hit
}

// drainResult carries what one run's drain observed beyond the mapped
// events: a tool interrupt that parked the run, the in-flight model call's
// partial text when the run ended abruptly (a cancellation mid-call), and
// the call-argument memory a resumed run seeds its drain from.
type drainResult struct {
	interrupt   *interruptHit
	partialText string
	args        map[string]string
}

// drainTurn consumes the ADK run's event iterator and maps it onto the
// session transcript. A run that ends on the iteration cap is not a failure:
// the turn gets a final visible notice (spec agent-tools "Runaway loop is
// bounded"). Any other run error propagates to the caller's failure path. A
// tool interrupt parks the run: the coordinates come back in the result and
// the drain ends WITHOUT a terminal event (design D4).
//
// seedArgs primes the call-argument memory with the interrupted run's calls
// on a resumed run: their announcements were live-only, and the resumed run's
// terminal tool records still carry the full call.
func drainTurn(iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]], record recordFn, usage *sessions.Usage, toolErrors *toolErrorResult, seedArgs map[string]string, rt *runningTurn, planDir string) (drainResult, error) {
	// args remembers each seen call's arguments so the terminal record
	// carries the full call, not just the result.
	args := make(map[string]string, len(seedArgs))
	for callID, callArgs := range seedArgs {
		args[callID] = callArgs
	}
	// started remembers each call's announcement time (design D5) so its
	// terminal record carries the observed duration. Fresh per run — a
	// resumed run cannot know when the interrupted run announced its calls,
	// so those terminal records carry no duration rather than a guess.
	started := make(map[string]time.Time)
	// partial retains the in-flight model call's streamed text across an
	// abrupt end (design D3: a cancelled turn keeps its partial output). The
	// tap watches the live stream without changing what record persists.
	var partial strings.Builder
	tap := func(ev *sessions.Event, durable bool) error {
		switch {
		case ev.Kind == sessions.KindTextDelta:
			partial.WriteString(ev.Text)
			if rt != nil {
				rt.appendPartial(ev.Text)
			}
		case ev.Kind == sessions.KindAssistantMessage && durable:
			// The model call completed durably; only a later in-flight call
			// can leave a partial behind.
			partial.Reset()
			if rt != nil {
				rt.resetPartial()
			}
		case ev.Kind == sessions.KindToolActivity && ev.Tool != nil:
			if rt != nil {
				switch ev.Tool.Status {
				case sessions.ToolStatusRunning:
					rt.addTool(ev.Tool.CallID, ev.Tool.Name, ev.Tool.Arguments)
				case sessions.ToolStatusCompleted, sessions.ToolStatusFailed:
					rt.removeTool(ev.Tool.CallID)
				}
			}
		}
		return record(ev, durable)
	}

	for {
		ev, ok := iter.Next()
		if !ok {
			return drainResult{args: args}, nil
		}

		if ev.Err != nil {
			if errors.Is(ev.Err, adk.ErrExceedMaxIterations) {
				// Bounded runaway loop: drain whatever remains so the run
				// finalizes, then leave the visible notice as the turn's
				// final word (the caller persists turn_completed after).
				drainToEOF(iter)
				notice := sessions.Event{Kind: sessions.KindAssistantMessage, Text: iterationCapNotice()}
				return drainResult{partialText: partial.String(), args: args}, record(&notice, true)
			}
			drainToEOF(iter)
			return drainResult{partialText: partial.String(), args: args}, ev.Err
		}
		// Tool interrupt (design D4): the run pauses for a human decision.
		// The event carries no transcript output — the coordinates park the
		// run and the drain ends without a terminal event.
		if ev.Action != nil && ev.Action.Interrupted != nil {
			if hit := interruptOf(ev.Action.Interrupted); hit != nil {
				return drainResult{interrupt: hit, args: args}, nil
			}
			continue
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mo := ev.Output.MessageOutput

		switch mo.AgenticRole {
		case schema.AgenticRoleTypeAssistant:
			if err := drainAssistantOutput(mo, tap, usage, args, started); err != nil {
				drainToEOF(iter)
				return drainResult{partialText: partial.String(), args: args}, err
			}
		default:
			msg, err := mo.GetMessage()
			if err != nil {
				drainToEOF(iter)
				return drainResult{partialText: partial.String(), args: args}, fmt.Errorf("collect tool result: %w", err)
			}
			if err := recordToolResultMessage(msg, tap, args, started, toolErrors); err != nil {
				drainToEOF(iter)
				return drainResult{partialText: partial.String(), args: args}, err
			}
			recordPlanSnapshot(msg, planDir, tap, toolErrors)
		}
	}
}

// drainAssistantOutput consumes one model output: streaming frames become
// live-only text and reasoning deltas; the completed message is persisted
// (reasoning first, then the assistant message, the transcript's durable
// form); and the message's tool calls are announced live as running
// ToolActivity records — the model has committed to them, execution follows.
// Usage rides the frames of the model call; only the last frame carrying
// usage counts (providers report it once, on the final frame).
func drainAssistantOutput(mo *adk.TypedMessageVariant[*schema.AgenticMessage], record recordFn, usage *sessions.Usage, args map[string]string, started map[string]time.Time) error {
	var text, reasoning strings.Builder
	calls := newToolCallAssembler()

	if mo.IsStreaming && mo.MessageStream != nil {
		var frameUsage *schema.TokenUsage
		for {
			frame, err := mo.MessageStream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if frame == nil {
				continue
			}
			if frame.ResponseMeta != nil && frame.ResponseMeta.TokenUsage != nil {
				frameUsage = frame.ResponseMeta.TokenUsage
			}
			for _, b := range frame.ContentBlocks {
				if b == nil {
					continue
				}
				if b.Type == schema.ContentBlockTypeReasoning && b.Reasoning != nil && b.Reasoning.Text != "" {
					reasoning.WriteString(b.Reasoning.Text)
					// Delta tap: best-effort; a failed broadcast must never fail
					// the turn.
					_ = record(&sessions.Event{Kind: sessions.KindReasoningDelta, Text: b.Reasoning.Text}, false)
				}
				if b.Type == schema.ContentBlockTypeAssistantGenText && b.AssistantGenText != nil && b.AssistantGenText.Text != "" {
					text.WriteString(b.AssistantGenText.Text)
					_ = record(&sessions.Event{Kind: sessions.KindTextDelta, Text: b.AssistantGenText.Text}, false)
				}
				if b.Type == schema.ContentBlockTypeFunctionToolCall && b.FunctionToolCall != nil {
					calls.observeBlock(b.FunctionToolCall)
				}
			}
		}
		accumulateTokenUsage(usage, frameUsage)
	} else if mo.Message != nil {
		msg := mo.Message
		if msg.ResponseMeta != nil && msg.ResponseMeta.TokenUsage != nil {
			accumulateTokenUsage(usage, msg.ResponseMeta.TokenUsage)
		}
		for _, b := range msg.ContentBlocks {
			if b == nil {
				continue
			}
			if b.Type == schema.ContentBlockTypeReasoning && b.Reasoning != nil && b.Reasoning.Text != "" {
				reasoning.WriteString(b.Reasoning.Text)
			}
			if b.Type == schema.ContentBlockTypeAssistantGenText && b.AssistantGenText != nil && b.AssistantGenText.Text != "" {
				text.WriteString(b.AssistantGenText.Text)
			}
			if b.Type == schema.ContentBlockTypeFunctionToolCall && b.FunctionToolCall != nil {
				calls.observeBlock(b.FunctionToolCall)
			}
		}
	}

	// Durable outputs of this model call, in transcript order.
	if reasoning.Len() > 0 {
		if err := record(&sessions.Event{Kind: sessions.KindReasoning, Text: reasoning.String()}, true); err != nil {
			return err
		}
	}
	if text.Len() > 0 {
		if err := record(&sessions.Event{Kind: sessions.KindAssistantMessage, Text: text.String()}, true); err != nil {
			return err
		}
	}

	// Announce the committed tool calls live (running is live-only by
	// design D10 — Append rejects it, the broadcast spins the UI). Each
	// announcement also stamps the call's start time (design D5): the
	// terminal record measures the call's duration from here.
	for _, call := range calls.finish() {
		if call.CallID != "" {
			args[call.CallID] = call.Arguments
			started[call.CallID] = now()
		}
		_ = record(&sessions.Event{Kind: sessions.KindToolActivity, Tool: &sessions.ToolActivity{
			CallID:    call.CallID,
			Name:      call.Name,
			Arguments: call.Arguments,
			Status:    sessions.ToolStatusRunning,
		}}, false)
	}
	return nil
}

// recordToolResult lands one tool call's terminal record: completed with the
// real result, or failed with the error text when the tool-error-result
// middleware recorded a failure for the call. The record carries the call's
// duration when the same run announced it (design D5): measured from the
// announcement to the terminal stamp. A resumed run's pre-interrupt calls
// have no announcement time on record, so their terminal records carry none
// — the runtime never fabricates a latency.
func recordToolResult(mo *adk.TypedMessageVariant[*schema.AgenticMessage], record recordFn, args map[string]string, started map[string]time.Time, toolErrors *toolErrorResult) error {
	msg, err := mo.GetMessage()
	if err != nil {
		return fmt.Errorf("collect tool result: %w", err)
	}
	return recordToolResultMessage(msg, record, args, started, toolErrors)
}

// recordToolResultMessage is recordToolResult over an already collected
// message (a streaming variant can be read only once).
func recordToolResultMessage(msg *schema.AgenticMessage, record recordFn, args map[string]string, started map[string]time.Time, toolErrors *toolErrorResult) error {
	if msg == nil {
		return nil
	}

	for _, b := range msg.ContentBlocks {
		if b == nil {
			continue
		}
		var callID, name, result string
		if b.Type == schema.ContentBlockTypeFunctionToolResult && b.FunctionToolResult != nil {
			callID = b.FunctionToolResult.CallID
			name = b.FunctionToolResult.Name
			var resBuilder strings.Builder
			for _, cb := range b.FunctionToolResult.Content {
				if cb != nil && cb.Type == schema.FunctionToolResultContentBlockTypeText && cb.Text != nil {
					resBuilder.WriteString(cb.Text.Text)
				}
			}
			result = resBuilder.String()
		} else if b.Type == schema.ContentBlockTypeToolSearchResult && b.ToolSearchFunctionToolResult != nil {
			callID = b.ToolSearchFunctionToolResult.CallID
			name = b.ToolSearchFunctionToolResult.Name
			if b.ToolSearchFunctionToolResult.Result != nil {
				resBytes, _ := json.Marshal(b.ToolSearchFunctionToolResult.Result)
				result = string(resBytes)
			}
		} else {
			continue
		}

		status := sessions.ToolStatusCompleted
		if errText, failed := toolErrors.FailureOf(callID); failed {
			status = sessions.ToolStatusFailed
			result = errText
		}
		activity := &sessions.ToolActivity{
			CallID:     callID,
			Name:       name,
			Arguments:  args[callID],
			Status:     status,
			Result:     result,
			IsError:    status == sessions.ToolStatusFailed,
			DurationMs: durationMsSince(started[callID]),
		}
		if err := record(&sessions.Event{Kind: sessions.KindToolActivity, Tool: activity}, true); err != nil {
			return err
		}
	}
	return nil
}

// durationMsSince reports the wall-clock milliseconds elapsed since start
// (design D5). A call with no recorded start (a resumed run's pre-interrupt
// call, or a zero time) yields zero, which the omitempty JSON tag drops —
// a record carries the latency the runtime observed or none at all. A
// measured call that finished inside a millisecond (local file tools do)
// floors to 1 so its record still states a latency instead of reading as
// unmeasured.
func durationMsSince(start time.Time) int {
	if start.IsZero() {
		return 0
	}
	ms := int(now().Sub(start) / time.Millisecond)
	if ms < 1 {
		return 1
	}
	return ms
}

// iterationCapNotice is the final visible notice a turn ends with when it
// hits the iteration cap (spec agent-tools: bounded runaway loops end with a
// notice, not a silent failure or a turn error).
func iterationCapNotice() string {
	return fmt.Sprintf("Turn stopped: the tool-call iteration cap (%d) was reached before a final answer.", DefaultMaxIterations)
}

// drainToEOF consumes and discards the remaining ADK events so the run's
// asynchronous finalization lands before the turn goroutine moves on
// (mirrors the donor's drain pattern).
func drainToEOF(iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) {
	for {
		if _, ok := iter.Next(); !ok {
			return
		}
	}
}

// documents reads the agent's persona documents for one turn. With no
// PromptDocs configured (the default) all three come back empty: turns run
// without persona context (null-object behavior).
func (m *manager) documents(ctx context.Context, agent store.Agent) (identity, soul, bootstrap string, err error) {
	if m.prompts == nil {
		return "", "", "", nil
	}
	return m.prompts.Documents(ctx, agent.Slug)
}

// persona is one turn's persona context: the agent's three workspace
// documents, read fresh from disk (design D12).
type persona struct {
	identity  string
	soul      string
	bootstrap string
}

// text renders the persona in composition order (identity, soul, bootstrap):
// the non-empty documents, each trimmed, joined by a blank line. An agent
// with no documents renders empty — its turns carry no system persona.
func (p persona) text() string {
	parts := make([]string, 0, 3)
	for _, doc := range []string{p.identity, p.soul, p.bootstrap} {
		if doc = strings.TrimSpace(doc); doc != "" {
			parts = append(parts, doc)
		}
	}
	return strings.Join(parts, "\n\n")
}

// knowledgeGuidance is the corpus guidance composed after the operating
// contract on turns exposing the /knowledge mount (add-knowledge-corpus
// design D7): where the scopes live, how to search the corpus with the
// filesystem tools the turn already has (D1 — raw corpus, no retriever), the
// directed skill activation (D10), and the save path (D4). Agents whose turns
// expose no mount never see it — no orphan guidance.
const knowledgeGuidance = `## Knowledge corpus

Saved documents live under /knowledge: /knowledge/global holds the documents every agent shares, /knowledge/agent holds this agent's own. Before answering a question that saved documents may cover, search them with the filesystem tools — list and read each scope's INDEX.md first, grep for anchors, and read around the hits (PDFs by page range). Activate the knowledge-search skill whenever a request may concern saved documents. Save new durable documents with the save_document tool.`

// skillsGuidance is the skills-authoring guidance composed after the
// knowledge guidance on turns whose skill backend resolved
// (add-agent-created-skills design D1 — authoring is taught, not tooled):
// where authored skills live, the directory and frontmatter conventions (the
// DNS-label name, the description later turns see), when saving is worth it,
// the personal-until-promoted boundary, and the directed activation of the
// seeded create-skill skill — authoring rides the jailed write tools the
// turn already has, so it names no new tool. Agents whose turns resolve no
// skill backend never see it — no orphan guidance.
const skillsGuidance = `## Skills you can save

Durable, reusable workflows are saved as skills in this agent's own workspace skills directory: one directory per skill under /workspace/skills/, the directory name is the skill's name and must be a DNS label (lowercase letters, digits, hyphens), and the directory holds a SKILL.md whose frontmatter declares a description — that description is what the skill listings on later turns show. Save a skill when a multi-step workflow has repeated or a correction has produced a procedure that will recur; one-off steps do not need one. Skills you save are personal to this agent until the user promotes them to the global library. Activate the create-skill skill ($create-skill) whenever you save a skill — it carries the full authoring playbook.`

// presetGuidance renders the document-preset appendix for one turn
// (add-create-document design D6; spec document-generation "Preset
// awareness in turns"): when the create_document tool is offered and at
// least one preset exists in any format, the preset names per format —
// each format's default marked (add-document-editing design D6) — so the
// model can reference one without asking. Every other state composes
// nothing: the appendix-free composition stays byte-identical. A store read
// failure degrades to no appendix (logged, not fatal — the appendix is
// informational and the tool resolves presets per call).
func (m *manager) presetGuidance(ctx context.Context, offered bool) string {
	if !offered {
		return ""
	}
	var formats []presetFormatSummary
	for _, f := range []struct{ format, label string }{
		{store.DocumentPresetFormatPDF, "PDF"},
		{store.DocumentPresetFormatDOCX, "DOCX"},
		{store.DocumentPresetFormatPPTX, "PPTX"},
	} {
		presets, err := m.store.DocumentPresets().List(ctx, f.format)
		if err != nil {
			slog.Warn("compose document preset guidance", "error", err)
			return ""
		}
		if len(presets) == 0 {
			continue
		}
		names := make([]string, 0, len(presets))
		for _, p := range presets {
			names = append(names, p.Name)
		}
		// The default marking is best-effort: an unreadable settings row leaves
		// the list unmarked rather than dropping it.
		def, err := readDefaultPresetName(ctx, m.store.ToolSettings(), f.format)
		if err != nil {
			slog.Warn("compose document preset guidance: read default", "error", err)
			def = ""
		}
		formats = append(formats, presetFormatSummary{Label: f.label, Names: names, Default: def})
	}
	if len(formats) == 0 {
		return ""
	}
	return presetAppendix(formats)
}

// composeSystem joins the turn's system context in composition order
// (design D2): the persona documents, then the operating contract, then the
// document-preset appendix when the turn offers create_document and presets
// exist (add-create-document design D6), then the knowledge corpus guidance
// when the turn exposes the corpus (spec add-knowledge-corpus design D7),
// then the skills-authoring guidance when the turn's skill backend resolved
// (add-agent-created-skills design D1), then the memory block when the agent
// has a workspace (add-agent-memory design D3), then the compaction summary.
// The contract composes on every call — an agent whose persona generation
// failed still gets it (spec agent-prompts "Contract present without persona
// documents"). Empty presetText, knowledgeText, skillsText, and memoryText
// keep the output byte-identical to the appendix-, guidance-, and block-free
// composition, so the pinned contract ordering holds.
func composeSystem(personaText, presetText, knowledgeText, skillsText, memoryText, summary string) string {
	var sb strings.Builder
	if personaText != "" {
		sb.WriteString(personaText)
		sb.WriteString("\n\n")
	}
	sb.WriteString(operatingContract)
	if presetText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(presetText)
	}
	if knowledgeText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(knowledgeText)
	}
	if skillsText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(skillsText)
	}
	if memoryText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(memoryText)
	}
	if summary != "" {
		sb.WriteString("\n\n")
		sb.WriteString("# Earlier conversation summary\n\n")
		sb.WriteString(summary)
	}
	return sb.String()
}

// toolCallAssembler merges streamed tool-call chunks into complete tool
// activities, in first-appearance order. Streamed frames split one call
// across chunks: the first frame carries the identity, later frames append
// argument text — chunks for one call merge by stream index, falling back to
// the call id when the provider does not set one. One assembler serves one
// model call; calls of different iterations never share keys.
type toolCallAssembler struct {
	order []string
	calls map[string]*sessions.ToolActivity
}

func newToolCallAssembler() *toolCallAssembler {
	return &toolCallAssembler{calls: make(map[string]*sessions.ToolActivity)}
}

func (a *toolCallAssembler) observe(calls []schema.ToolCall) {
	for _, call := range calls {
		key := call.ID
		if call.Index != nil {
			key = fmt.Sprintf("idx:%d", *call.Index)
		}
		existing, ok := a.calls[key]
		if !ok {
			existing = &sessions.ToolActivity{}
			a.calls[key] = existing
			a.order = append(a.order, key)
		}
		if call.ID != "" {
			existing.CallID = call.ID
		}
		if call.Function.Name != "" {
			existing.Name = call.Function.Name
		}
		existing.Arguments += call.Function.Arguments
	}
}

func (a *toolCallAssembler) observeBlock(call *schema.FunctionToolCall) {
	if call == nil {
		return
	}
	key := call.CallID
	existing, ok := a.calls[key]
	if !ok {
		existing = &sessions.ToolActivity{}
		a.calls[key] = existing
		a.order = append(a.order, key)
	}
	if call.CallID != "" {
		existing.CallID = call.CallID
	}
	if call.Name != "" {
		existing.Name = call.Name
	}
	existing.Arguments += call.Arguments
}

// finish returns the assembled tool activities in first-appearance order.
func (a *toolCallAssembler) finish() []*sessions.ToolActivity {
	if len(a.order) == 0 {
		return nil
	}
	out := make([]*sessions.ToolActivity, 0, len(a.order))
	for _, key := range a.order {
		out = append(out, a.calls[key])
	}
	return out
}

// titleFromMessage derives a session title from its first message: the first
// non-empty line, trimmed and bounded.
func titleFromMessage(message string) string {
	for _, line := range strings.Split(strings.TrimSpace(message), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 80 {
			line = strings.TrimSpace(line[:80])
		}
		return line
	}
	return "New chat"
}

// titleSource is the text the session title derives from: the message text,
// or — on an attachment-only send (spec chat-attachments: a message with
// attachments and no text is sendable) — the first attachment's name.
func titleSource(message string, atts []sessions.Attachment) string {
	if strings.TrimSpace(message) != "" || len(atts) == 0 {
		return message
	}
	return atts[0].Name
}

// effectiveModel resolves the model id a turn runs on (design D1): the
// session's override when set, otherwise the agent's configured model.
func effectiveModel(agent store.Agent, override ModelOverride) string {
	if override.Model != "" {
		return override.Model
	}
	return agent.Model
}
