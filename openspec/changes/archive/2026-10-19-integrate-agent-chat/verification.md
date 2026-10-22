# Verification — integrate-agent-chat

Team run 2026-10-18 (parallel worker waves + adversarial verifier + fix loop).
Final verdict: **PASS** (re-verified after fixes). This file is the tasks.md 10.3
scenario-mapping checklist; evidence paths are `file:line` at implementation time.

## Gates (tasks.md 10.1) — all exit 0, run fresh

| Gate | Result |
|---|---|
| `gofmt -l .` | clean |
| `go vet ./...` | ok |
| `go test ./... -count=1` | all 17 packages ok |
| bindings freshness | regeneration is byte-identical (hash-checked) |
| `task build` | ok |
| frontend `npm run build` | ok |
| frontend `npm test` | 71 files / 512 tests pass |
| frontend `vp check src` | 0 errors (17 non-failing warnings) |

Known pre-existing, not introduced by this change: `go test -race` trips inside
pinned vendor code (`eino-ext/components/model/claude@v0.1.25 claude.go:446/480`
via anthropic-sdk-go ssestream) reached by `TestBuildModel_EffortMapsToNativeKnob`;
all repo frames are the test's own drain helper. A second test-closure race
(`resume_test.go` `streamCalls++`) is likewise test-local. Remedy for both is an
upstream bump (design.md already flags eino bumps as change-sensitive).

## Spec mapping (tasks.md 10.3)

### chat-view
- Sessions island (column, meta, active tint, Settings-hidden) — SessionRail.tsx; App.tsx:367 — VERIFIED
- Delete + confirm; refusal for running session surfaced as toast — SessionRail.tsx:56-61, chat.go:104-112, store.ts:669-718 — VERIFIED (refusal semantics noted for spec sync)
- Running indicator incl. approval-paused; per-session fold — SessionRail.tsx:131-134, store.ts:225-250 — VERIFIED
- Composer (Enter/slash/skills/mentions), queue order, drain after cancel — Composer.tsx + menus; store.test.ts:390-425 — VERIFIED
- Agent picker switches context, session binding untouched — ComposerPickers.tsx + tests — VERIFIED
- Model picker; effort picker limited to model set, hidden when empty — ComposerPickers.tsx:288 — VERIFIED
- Override persists on session, survives reload, seeds new sessions — chat.go:223, store.ts hydrate/persist/seed, migration 000008 — VERIFIED
- Stop control: partial text kept, cancelled terminal, control restores, queue dispatches — Composer.tsx:494-503, runner_cancel_test.go, store.test.ts:390 — VERIFIED
- Right sidebar: avatar, effective model, context limit from capability — RightSidebar.tsx, chat.go:232 — VERIFIED
- Chat header: idle/running/approval-paused + mono effective-model chip — ChatHeader.tsx:25-59 — VERIFIED
- Windowing: newest page, prepend dedupe, height-delta anchor, mid-turn merge — chat.go:90, events.go:100-112, store.ts:622-663, entries.ts:472-495, Transcript.tsx:208-234 — VERIFIED
- Day dividers — DayDivider.tsx:25-61 — VERIFIED
- Minimap thresholds/active/click — Minimap.tsx — VERIFIED
- Regenerate: real turn, variant_of grouping, newest-default live, replay parity, offline switching, failed regen restores — manager_impl.go:219-276, entries.ts:27-32, VariantPicker.tsx — VERIFIED

### chat-approvals
- Dangerous-command gate (pattern families), interrupt instead of execute — shell_approval.go:81-186 — VERIFIED
- Decision consumed once; identical command interrupts again — resume.go:31-45, shell_approval_test.go:143 — VERIFIED
- Durable pending (payload+checkpoint, no terminal while pending, sends queue, survives restart) — resume.go:53-101, manager_impl.go:302-317, resume_test.go:103,364 — VERIFIED
- Resolve side-channel: live reuse + cold rebuild via Runner.Resume; deny feeds refusal; stale conflict; cancel discards+clears — resume.go:110-211,347-485, manager_impl.go:393-439 — VERIFIED
- Concurrent double-resolve conflict (fix-loop addition): guard under m.mu, deterministic channel-gated test — manager_impl.go:68-75, resume.go:127-139, resume_test.go:193-256 — VERIFIED
- Approval card: pending actionable, resolved outcome, replay reconstruction — ApprovalCard.tsx, entries.ts:33-35 — VERIFIED

### chat-attachments
- Caps (4/message, image 5MB, PDF 20MB, text 1MB) with reasons; send blocked while uploading — attachments (Go+TS caps mirrored), manager_impl.go:323-343, Composer.tsx:218-220 — VERIFIED
- Capability-keyed storage, opaque 128-bit key, no raw path exposure, bearer-gated server mode — attachments.go:56-197, chat.go:137-160, main_server.go:6 — VERIFIED
- Multi-part model input: image/PDF per capability flags; text content rides inline (fix-loop addition; lost file degrades truthfully) — turninput.go:98-162, attachments_turn_test.go incl. TestTurn_TextAttachmentRidesInline — VERIFIED
- Degradation: per-model flags, warn before send, pointer reference, send never fails — Composer.tsx:231-259, turninput.go:121-157 — VERIFIED (PDF rides gemini connector only — documented connector limit)
- Replay reconstructs metadata; regenerate re-sends stored content — turninput/assembleTurnInput, attachments_turn_test.go — VERIFIED

### chat-panels
- Panel shell: tabs, docked/overlay, persisted width, per-tab close, reset on session switch, mid-turn non-interrupting — RightPanel.tsx, store.ts:562-564 — VERIFIED
- Jailed preview sources: abs/traversal/symlink-escape refused before open; refused outcome rendered — chat.go:267-296, workspaces.go:209-235, RightPanel.tsx:188-202 — VERIFIED
- Workspace file + dir sources; trajectory via jailed reader (.tasks/<id>.md) — RightPanel.tsx, toolPaths.ts — VERIFIED
- Tool/task-card path links open previews; foreign paths → refused outcome — PathText.tsx, ToolCallCard.tsx:78-81, TaskCallCard.tsx:102-134, AssistantMessage.tsx:29-70 (markdown chips) — VERIFIED
- Run card: tasks lane + plan lane derived from events, live + replay, hidden when empty — RunCard.tsx, entries.ts:170-193 — VERIFIED

### agent-runtime
- Provider-required typed errors as turn events — registry.go:49-88 — VERIFIED
- Session model override wins; listing-rejected override → clear per-turn error — manager_impl.go:194-258, registry.go:72-105 — VERIFIED
- Effort: session > agent default > none; native knobs (openai effort, claude/gemini thinking budgets); unset = omitted; unsupported rejected — connectors.go:78-149, registry.go:111-121 — VERIFIED
- Cancellation: safe-point cancel + 2s escalation; cancelled terminal persists via cancel-proof recorder ctx with partial output; cancel-pending discards; idle cancel no-op — manager_impl.go:354-391, runner.go:469-495, runner_cancel_test.go — VERIFIED

### agent-management
- Default effort validated against model set on create/update; empty = clear — agents.go:149,215-223,359-369 — VERIFIED
- Config UI effort select follows model, clears on unsupported switch — AgentConfigModal.tsx:126-132,272-287 — VERIFIED

### data-store
- Home layout incl. attachments dir — config.go:21-25, ensure.go:16-17 — VERIFIED
- Session rows carry overrides (empty=inherit); replay reconstructs incl. variants; checkpoints `adk_<turnID>` / `checkpoint_<sessionID>` / ledger `approval/<sha256>` — events.go, resume.go — VERIFIED
- Additive migrations 000008/000009 only; forward-compatible (unknown kinds skipped) — migrations/, events.go:117-172 — VERIFIED
- Event log append-only (single INSERT; DELETE only with whole-session delete) — sessionevents.go:19,118 — VERIFIED

## 10.2 (live end-to-end smoke) — status

NOT run as a GUI screenshot session. The runtime behaviors 10.2 exercises are
pinned by automated end-to-end tests instead: event-kind-sequence parity across
the Runner swap (runner_swap_test.go), cancel/approval/resume flows incl. cold
resume (resume_test.go), attachment turn paths (attachments_turn_test.go), and
windowing/store flows (windowing.test.tsx, store.test.ts). The GUI screenshot
pass (composer pickers → stop → attach → regenerate → approve, incl. restart
mid-pending) remains to be recorded before archiving this change.
