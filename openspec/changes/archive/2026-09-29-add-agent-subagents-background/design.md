# Design

## Context

OnClaw composes every agent through `internal/agents.Compose` — a pure function stacking eino middlewares (patchtoolcalls → reduction → summarization → skill → filesystem → attachments → hooks → connection gate → tool-error-result) onto one `adk.TypedChatModelAgent[*schema.AgenticMessage]`, and runs it per turn via `composeAgent` (runner.go) with a per-run identity (workspace, agent, session, hooks chain). Eino v0.10.0-alpha.35 (identical deep/subagent surface to the alpha.28 explored on 2026-09-27) provides: `adk/middlewares/subagent` (delegation tool + instruction + available-types reminder; child runs on a fresh bridge session), `adk/backgroundtask/local` (process-local `Runner` that owns a real `backgroundtask.Manager` with in-memory stores), and `adk/middlewares/backgroundtask` (`task_output`/`task_stop` control tools bound to a Manager). `prebuilt/deep` is the reference assembly of the same parts.

## Goals / Non-Goals

**Goals:**
- The deep-agent recipe (delegation tool + general-purpose clone + background control) adopted into OnClaw's own compose, not `deep.NewTyped` wholesale.
- Per-agent opt-in with zero schema/API change: reserved allowlist name + catalog row.
- v1 background = process-local; honest copy about restart loss; completion pumped into the parent session.

**Non-Goals:**
- Durable background tasks (Postgres `TaskStore` against the conformance suite) — its own change; the Local lane's Manager seam is where it slots in later.
- Declared subagent types in the domain model, API, and UI (agent config picker for "expose as subagent", cross-agent tool/hook semantics) — compose accepts `SubAgents` now; the runner wires only general-purpose; the picker is the follow-up.
- write_todos adoption (rejected 2026-09-27: strictly weaker than OnClaw's Postgres-backed agent-todos) and deep's CC-style base instruction (OnClaw always supplies its own instruction).
- Nested-delegation depth configuration (depth is one, by construction).
- A dedicated background-tasks UI surface (v1 renders through existing tool-call cards + a completion transcript event).

## Decisions

**D1 — Subagent middleware directly, not `deep.NewTyped`.** Hand-rolled assembly following the deep recipe: `subagent.NewTyped[*schema.AgenticMessage]` + `backgroundtaskmw.NewTyped` + our own clone builder (deep's `buildSubAgentsList` is ~30 lines). Why not deep: it routes the instruction through its own `GenModelInput` (instruction-as-system-message semantics we don't want on the top-level agent), hardcodes the `task` tool name, and couples us to its assembly choices (write_todos, fs middleware ordering) we'd permanently opt out of via flags. The recipe is three pieces; owning the assembly keeps Compose's contract intact.

**D2 — Tool names.** Delegation tool keeps eino's default `agent` (the domain noun; deep's `task` collides with Run/Scheduler vocabulary — the same reason agent-todos kept "todos"). Control tools keep eino's `task_output`/`task_stop` (framework convention, no collision). Hook matchers can target `agent` like any tool name.

**D3 — Middleware position.** Subagent then background-control, appended after filesystem and before attachments/hooks/gate/tool-error-result (deep's relative order: fs → subagent → control). Later-attached wrappers stay outermost, so hooks and the gate wrap the `agent` tool call — the parent's policy gates delegation itself; a block returns the canonical block JSON. The tool-error-result middleware stays outermost so delegation failures degrade to tool results like every other tool.

**D4 — General-purpose clone.** Built inside Compose from the same Config (instruction, model, resolved tools, iteration cap, handlers minus the subagent/control steps) via `adk.NewTypedChatModelAgent`, name `general-purpose`, description adapted from deep's ("general-purpose agent for researching complex questions… Tools: \*"). Composing the clone from the pre-subagent handler slice is what makes delegation non-recursive by construction (spec: "Delegation cannot recurse"). `WithoutGeneralSubAgent bool` on Config mirrors the eino field name the user asked for; validation fails fast when suppressed with zero declared subagents.

**D5 — Background lane v1 = Local.** `backgroundlocal.New` constructs a Runner that internally owns a `backgroundtask.Manager` over in-memory reference stores — no Postgres TaskStore, no conformance work, and `Runner.Manager()` is the binding point for the control middleware, so `task_output`/`task_stop` work on the same task-id space. The Runner is constructed per composition (per run), so tasks are naturally scoped to one run's lifetime: run ends or process dies → tasks gone, matching the honest-copy requirement. Composition purity holds: Runner construction is allocation-only.

**D6 — Output files via a jail append-opener.** The subagent background lane writes the child transcript to an output file (`<uuid>.output`) it wants the model to Read. The jailed backend doesn't implement `einofs.AppendOpener`, so add a small `backend` adapter: append-only opens resolved under the agent dir with traversal rejection, rooted at `<agentDir>/.tasks/`. Output paths stay inside the jail so the fs middleware's `read_file` can read them (launch copy points the model at the jail path).

**D7 — Child-event filtering in the stream lane.** The middleware forwards child execution events onto the parent's event stream tagged with the child session id in the event's session variant. eino's turn loop already skips them for parent-session persistence (our `ADKSessionAdapter` never sees them); OnClaw's SSE translator must drop events whose variant session ≠ the parent session id, so the delegation renders as exactly one tool-call card (its own started/finished events ride the normal tool-call lane). Live and hydrated views then agree by construction.

**D8 — Completion notification pump.** The Manager writes session-routed notifications to its outbox; nothing drains it automatically (verified: no turn-loop consumer in alpha.35). Per run, a small pump goroutine leases notifications for the parent session (`ReceiveNotifications` + ack) and appends a completion transcript event (task id, kind: completed/failed/canceled, output path) through the existing session-events machinery, so the next turn's hydrated context carries the outcome. The pump dies with the run — consistent with D5's lifetime honesty.

**D9 — Enablement via reserved allowlist name.** `ReservedSubagentsTool = "subagents"`, mirroring `ReservedShellTool = "execute"`: the runner checks the effective allowlist (agent config ∩ workspace gate, honoring `AllowedTools` overrides), passes the capability into Compose when present, and the reserved name never reaches the model surface. A `workspace-tools` catalog row makes it toggleable in the existing agent-config tool picker. No domain field, no migration, no API change.

**D10 — Version bump rides first.** alpha.28 → alpha.35 verified by full-module diff: only additive/refinement changes touching packages OnClaw uses (`SetToolReturnDirectly`, checkpoint-save + session-persistence sentinels, `reorderParallelToolResults` on reconstruction, summarization skill-preamble reordering + re-execution guard, patchtoolcalls insertion-anchor fix, reduction Extra deep-clone, retry `RewriteError`). eino-ext modules resolve via MVS. Land the bump as its own green build before any capability code.

**D11 — Background shell rides the same Runner through the fs middleware's own background seam.** The filesystem middleware accepts `Background *BackgroundConfig` alongside `Shell`; `Background.Local` puts THE middleware's shell under managed lifecycle (deep's cfg-level Shell/LocalShell mutual exclusion was an artifact of deep building the middleware itself — at the middleware level they coexist by design). Wiring in `buildMiddlewares`: `fsConfig.Background = &fsmw.BackgroundConfig{Local: &fsmw.LocalBackgroundConfig{Runner: same per-run Runner, OutputStore: same tasks opener, OutputDir: ".tasks"}, NotificationSessionID: resolves the parent session id}` — so shell tasks and delegation tasks share one task-id space, one `task_output`/`task_stop` pair (bound to the same Manager, injected exactly once by the control middleware), and one pump lane (shell completions ride `NotificationSessionID` into the same outbox the pump drains). The shell enters the lane only when a second reserved allowlist name (`background_shell`) is selected AND the shell tool is wired; the runner resolves the pair coherently, and Compose fails fast on an incoherent direct call (the summarization-requires-filesystem precedent). Two v1 policy pins: `Config.ForegroundTimeoutMs` set non-positive (timer disabled — foreground commands run to completion exactly as today, nothing is ever auto-backgrounded; the default timer would have been a silent regression on long foreground commands), and explicit `run_in_background` is the only path into the background.

**D12 — L1 prompt copy.** `promptdocs.BasePrompt` (internal/promptdocs/AGENTS.md) ships unconditionally to every composed instruction and already describes catalog-toggled tools plainly (the `memory` section); the new section follows that convention with "if you have" phrasing so agents without the capabilities read honest conditionals. It deliberately does NOT duplicate the delegation-etiquette prompt the subagent middleware injects at BeforeAgent (when/when-not, briefing style) — L1 carries only platform mechanics: delegation as context hygiene, the background-command pattern, and restart honesty. The section lands with the capability (task 6.2), never before — a prompt teaching absent tools is a documentation lie on every turn. Placement: after "## Follow-through", before "## Communication & Output". Verbatim copy:

```markdown
## Delegation & Background Work

- If you have the `agent` tool, delegate self-contained subtasks — research sweeps, multi-step exploration, long builds — to a sub-agent instead of absorbing them into your context. Brief it completely (goal, constraints, where to write results); only its final report returns to you, so the brief is the whole interface. Fire independent briefs in one turn.
- Tools that take `run_in_background` — the shell tool, and the `agent` tool itself — need not block the turn: a test suite, a build, or a delegated research task can run while you keep working. Poll interim progress with `task_output`; cancel with `task_stop`; both address every background task. Never background a step the current work depends on; launch, do other work, collect.
- Background tasks live inside the current run: a server restart loses them. Completion is announced in your session — never end a turn waiting on one, and never promise one survives a restart.
```

## Risks / Trade-offs

- [Child interrupt/approval resume path] Sub-agent shell approvals flow through the middleware's composite interrupt bridge; our approval ledger resumes against the parent checkpoint. → Explicit test for an approval-raising tool inside delegation; fallback if the resume path breaks: children compose with the execute tool disabled in v1 (capability loss, not correctness loss).
- [Approval-raising background commands] A command that would raise a shell-approval interrupt, launched with `run_in_background`, has no interactive approver attached to a detached task. → Spike during implementation: verify the ledger's interrupt path under the managed lane; fallback: background launches route through the same pre-execution approval check and a pending-approval command fails the launch with a readable tool result (the approver approves a foreground rerun).
- [Orphaned processes on restart] A background command is a child of the onclaw server process; a server restart kills or orphans it while the in-memory Manager (and its task record) dies. → Honest launch copy (spec-pinned); same lifetime model as background delegation — the durable TaskStore change is where cross-restart shell recovery (RecoverableShell) slots in later.
- [Token accounting] The clone shares the parent's model instance, so child usage aggregates into the parent run's totals with no per-subagent breakdown. → Verify aggregation lands in run totals (test); breakdown deferred with the domain-model work.
- [Prompt budget] The delegation tool description + instruction + reminder ≈ 700 tokens/turn, only for opted-in agents. → Verify `context_breakdown` accounts the BeforeAgent-injected instruction slice; add a lane if it under-counts (the skill middleware precedent).
- [In-memory task loss] Background delegations die with the run/restart. → Honest launch copy (spec-pinned), pump-scoped to the run; durable lane is the designed follow-up at the Manager seam.
- [Upstream drift] alpha.35's summarization preamble ordering and parallel-result reordering change hydrated-context bytes. → Full Go + web + smoke suites gate the bump; existing compaction tests cover the ordering change.

## Migration Plan

1. Land the version bump alone: `go.mod`/`go.sum`, build, vet, full test suites, smoke — green before proceeding.
2. Add the capability behind the reserved name (opt-in per agent; nothing changes for agents that never select it).
3. No deployment ordering, no data migration; rollback = revert (no schema).

## Open Questions

None blocking. Deferred to follow-up changes: Postgres TaskStore durability; subagent-type picker in agent config; per-subagent usage breakdown.
