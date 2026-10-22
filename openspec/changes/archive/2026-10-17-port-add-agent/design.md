# Design

## Context

buddies is a single-user local Wails3 app: Go backend, SQLite at `~/.buddies/buddies.db`, eino agent runtime, React frontend with the locked Hallmark design system (`design.md`). The agents menu ships with a minimal name-only create modal; `Agent.SystemPrompt` is a hand-edited DB column consumed by the runner. onclaw-v2 (the donor project) provides the proven implementation to port: `web/src/modals/AgentConfigModal.tsx` (wizard), `internal/promptgen` (generation), and the `agents`/`agent-prompts` specs. See proposal.md for motivation and the delta specs for behavior.

## Goals / Non-Goals

**Goals:**

- Faithful port of the wizard + file-backed persona documents, adapted to a single-user local app.
- Documents are real files users can open and edit; the runtime reads them fresh each turn.
- Create-time generation with a visible, characterful in-flight state; crash-safe status recovery.
- Regeneration with enhance mode and `.bak` safety, surfaced from the config modal.

**Non-Goals:**

- No autonomy/approval gating (runtime has no such concept; porting it is runtime work, not wizard work).
- No reasoning-effort field (the runtime has no effort plumbing; it would be inert config).
- No workspace-default model inheritance, no memory side-call (no workspace concept, no memory pipeline).
- No tools denylist flip — buddies keeps its allowlist semantics.

## Decisions

- **D1 — File-backed documents over DB columns.** IDENTITY/SOUL/BOOTSTRAP live at `~/.buddies/workspaces/<slug>/`; the DB row holds slug/description/brief/status only. Rationale: the user explicitly chose the full onclaw-v2 model; files are directly user-editable and keep the DB free of large persona text. Alternative rejected: TEXT columns (simpler migration, but loses the file-first property that is the point of the port).

- **D2 — SystemPrompt removed, not composed.** `Agent.SystemPrompt` and the `system_prompt` column are dropped; turns compose identity + soul + bootstrap. Existing agents' `system_prompt` content migrates into their `IDENTITY.md` (see Migration Plan). Alternative rejected: keeping SystemPrompt as a compose-extra (two overlapping persona surfaces — the muddle onclaw-v2 avoided).

- **D3 — Slug rules.** DNS-label shape (`^[a-z0-9](-?[a-z0-9])*$`, ≤63 chars), unique among agents, auto-suggested from the name client-side, editable only before create, immutable after. The slug names the workspace directory, which is why it cannot change. Enforced by a UNIQUE index; duplicates surface as a step-level validation error.

- **D4 — Synchronous generation at create.** The create service persists the row with `prompts_status=generating`, runs generation, then commits `ready | failed` before returning; the wizard shows the onboarding loader meanwhile. Rationale: in a desktop app the "background + poll" machinery buys nothing and creates orphan handling; persisting `generating` first means a crash mid-generation sweeps to `failed` at next startup instead of sticking. Alternative rejected: async job queue.

- **D5 — Promptgen mirrors onclaw-v2's service.** New `internal/promptgen`: hardcoded generation prompts for the three documents, hardcoded generation parameters (generation temperature; generation-time max_tokens large enough for the Anthropic API contract), 3-minute timeout, model calls through a model factory built on buddies' existing provider registry + eino runtime. No encryption layer — buddies stores keys in clear (single-user assumption already on record).

- **D6 — Regeneration safety.** Enhance mode always sends current document contents (voice/structure preserved, current brief incorporated, optional instruction as a requested-changes section). Each overwritten document is backed up to `<name>.bak` before the enhanced content commits; a backup failure aborts the whole commit and fails the generation. Single-flight is enforced by the status: a regenerate call while `generating` is a conflict.

- **D7 — Capabilities step reuses buddies' allowlists.** The step presents skills/tools/MCP as the existing CheckOptionList opt-ins; onclaw-v2's denylist inversion is not ported (its denylist existed to make new catalog tools default-on in a multi-tenant product — wrong default for a single-user tool, and buddies' opt-in is already specced).

- **D8 — Context window override, catalog auto-fill.** The advanced section carries an optional context-window value, auto-filled from the static model catalog when the model is known; the runtime's existing context accounting uses it as an override when set. No effort field (see Non-Goals).

- **D9 — Onboarding loader in buddies' voice.** Same mechanics as onclaw-v2 (`OnboardingLoader`: 5s line rotation, elapsed timer after 30s, label "Onboarding \<name\>…") with an informal starter pool: "Reading the brief twice…", "Sketching a personality…", "Warming up its voice…", "Deciding what it cares about…", "Packing its toolbox…", "Learning to say 'I don't know' nicely…", "Stretching its context window…", "Practicing its first hello…", "Making a good first impression…", "Almost done — it's on the last page…". No office/HR jokes.

- **D10 — Two-phase migration with Go backfill.** `000004_agent_prompt_docs.sql` adds slug (with UNIQUE index), description, brief, prompts_status, prompts_error and keeps `system_prompt`; a one-time idempotent Go backfill (runs after migrations, guarded by the presence of populated `system_prompt` values) slugifies names with dedupe, writes each agent's `system_prompt` into its `IDENTITY.md` ("## Carried over from previous version" seed), and sets `prompts_status='ready'` for pre-existing agents; `000005_drop_system_prompt.sql` then drops the column. SQLite can't run Go between SQL migrations, so the backfill is a migration-runner hook, not a migration file. Agents created before this change therefore keep a working persona and are never stuck in `generating`.

- **D11 — Workspace lifecycle.** `workspaces/<slug>/` is created at create time (even before generation finishes — partial dirs from failed generations are legitimate). Delete removes the directory after the row delete, best-effort; sessions are untouched. Documents are read from disk per turn (tiny files, no cache to invalidate — external edits apply next turn for free).

- **D12 — BOOTSTRAP composes every turn.** onclaw-v2 used BOOTSTRAP.md as a session-start artifact of its memory pipeline; buddies has none, so all three documents compose into the turn context on every turn. Composition order: identity, soul, bootstrap, then the session context.

- **D13 — Frontend follows the Hallmark grammar.** Wizard and Identity tab live in the existing ModalShell (island modal over a scrim, glass-panel inner sections, daisyUI light+dark validated). New components: wizard modal (3 steps + step pills), file-rail document editors, regenerate form ("What should change?" → "Enhance prompts"), OnboardingLoader, card status indicator. Bindings are regenerated by rebuilding (`task build`) before frontend gates; the server binary embeds dist, so CSS changes require a rebuild.

- **D14 — Stored procedural avatar with derived fallback (2026-10-17 parity pass).** The Identity step's avatar becomes onclaw-v2's `AvatarPicker` (react-nice-avatar preview, Randomize, Customize trait grid with the donor's exact option sets); the chosen full config persists verbatim on the agent row (`agents.avatar`, a JSON object — the Hook.Config opaque-JSON pattern) via migration `000006_agent_avatar.sql`. Every avatar surface (cards, chat sidebar, wizard/config previews) renders the stored config when present and falls back to the deterministic id-derived face otherwise, so pre-picker agents keep their established appearance. This supersedes the earlier "avatars deterministic from identity" scope cut — the user directed full onclaw-v2 avatar parity. The config modal hydrates the stored config (or rolls a fresh random one when the row has none) and saves it atomically with the rest of the row.

## Risks / Trade-offs

- [Create blocks up to 3 minutes on generation] → loader with elapsed timer keeps it legible; timeout is bounded; closing the app mid-flight is safe (startup sweep marks the row failed, retry from card or config).
- [User edits prompt files externally in odd encodings] → files are read as UTF-8 text per turn with no caching; a read failure degrades to omitting that document and surfaces in the turn error path, never corrupts the transcript.
- [Slug/dir name collisions with OS-invalid names] → DNS-label slug doubles as a safe directory name; the UNIQUE index catches duplicates before any directory is created.
- [Generation cost on every create] → uses the agent's own configured provider/model, visible in the wizard step; failed generations cost only what the provider consumed.
- [Backfill writes files during startup] → idempotent and guarded (only agents with populated legacy `system_prompt` and no existing `IDENTITY.md`); a second startup is a no-op.

## Migration Plan

1. Ship migration 000004 (new columns + index, `system_prompt` retained), the Go backfill hook, and migration 000005 (drop `system_prompt`) in one release; the runner applies them before services start, per the data-store spec.
2. Existing agents emerge with slug (derived, deduped), `prompts_status=ready`, and their old system prompt as `IDENTITY.md`; SOUL/BOOTSTRAP stay absent until regenerated — turn composition tolerates missing documents.
3. Rollback is restore-from-backup of `~/.buddies` (pre-release dev data only; the app has no production fleet).

## Open Questions

None blocking. The exact generation prompt wording for the three documents will be lifted from onclaw-v2's `internal/promptgen/prompts.go` during implementation and adjusted only where it references workspaces/tenancy.
