# Proposal

## Why

buddies' chat already runs end-to-end on the ported ADK runtime, but it lags its donor `onclaw-v2` in visible chat behavior — no stop control, no attachments, no approvals, no transcript windowing or chat header — and neither app has ever let the user choose **agent, model, or reasoning effort from the chat itself**: onclaw fixes model/effort as agent config (its effort field is validated but inert), while buddies hard-wires the model per agent row. This change closes the chat parity gap at full scope and adds per-session agent/model/effort pickers with effort actually applied to model calls.

## What Changes

- **Per-session model/effort overrides + composer pickers (new feature, beyond onclaw):** the composer gains an agent picker (switches conversation context), a model picker (models of the session's provider), and an effort picker (options the selected model supports). Choices persist on the session row, stick as defaults for new sessions, and override the agent's configured defaults for every subsequent turn. `Send(sessionID, text)` stays unchanged — overrides ride the session row.
- **Reasoning effort applied for real:** effort is mapped to each providers' native parameter (OpenAI reasoning effort, Anthropic thinking budget, Gemini thinking config) through the eino connectors, validated against a per-model capability catalog — unlike onclaw, where effort is stored but never consumed.
- **Stop control:** cancelling an in-flight turn is a first-class runtime operation with a terminal `cancelled` event; the composer's send button becomes a stop button while busy.
- **Durable approvals (full onclaw parity, chosen durable):** dangerous-shell tool calls interrupt the run, persist an approval decision request with a checkpoint, render an approve/deny card, and resume the same turn from the checkpoint on decision — including across an app restart.
- **Attachments:** composer chip tray with upload progress, drag-drop and paste; lane-classified local storage under `~/.buddies/attachments` served through capability URLs; multi-part user messages rendered in the transcript; per-model degradation when a model can't read images/PDFs.
- **Transcript chrome:** chat header (avatar, status, effective-model chip), day dividers, windowed history loading ("load earlier"), a conversation minimap, regenerating an answer with a local branch picker, session delete and running-state rows, and an override-aware right sidebar.
- **Side panels:** a right-side preview panel (workspace file/document previews, subagent trajectory files, persona docs) and a floating run card (plan/todos lane, background tasks lane).
- **Agent config:** the agent's Model section gains a default reasoning effort, validated against model capability; the create wizard benefits from the same capability data.

**Out of scope:** the browser-mirror panel source and channel members (no backing subsystems in buddies), cross-device/server-authoritative session indexing, and per-message (within-transcript mixed-agent) routing — agent selection switches conversation context, not the bound agent of a session.

## Capabilities

### New Capabilities

- `chat-attachments`: end-to-end attachments — composer chip tray, lane classification and caps, capability-gated local storage/serving, multi-part user messages in the transcript, and per-model degradation.
- `chat-approvals`: durable human-in-the-loop tool approvals — the shell safety gate, persisted interrupt awaiting a decision, resolve side-channel, and checkpoint-based resumption of the interrupted turn.
- `chat-panels`: the right-side preview panel (file/document previews, subagent trajectories, persona docs) and the floating run card (plan/todos lane, background-tasks lane) hosted over the transcript.

### Modified Capabilities

- `chat-view`: composer affordances gain the agent/model/effort pickers with per-session overrides, sticky defaults, and the stop control; transcript rendering gains day dividers, windowed earlier-history loading, and regenerate with a local branch picker; the sessions island gains delete and running state; a new chat header requirement (status + effective-model chip); an override-aware right sidebar; session rows show the active agent switcher context.
- `agent-runtime`: per-turn model/effort resolution honors session overrides over agent defaults with capability validation; reasoning effort is applied as provider-native parameters; an in-flight turn can be cancelled and records a terminal cancelled outcome.
- `agent-management`: the agent configuration's Model section includes a default reasoning effort selectable only from the model's supported options.
- `data-store`: `~/.buddies` layout gains the attachments directory; session rows persist optional model/effort overrides alongside the event sequence; new event kinds (cancelled, approval, attachment metadata) remain part of the replayable log.

## Impact

- **Go runtime:** `internal/agents/` — `runner.go` (Runner swap for interrupt/resume, cancel, override-aware build), `registry.go`/`connectors.go` (model override + effort mapping), `catalog.go` (effort capability data), `compose.go` (shell approval gate), new resume path; `internal/store/` + migrations `000008+` (session/agent columns, attachments dir, checkpoint reuse of the existing `checkpoints` table with a new key prefix).
- **Go services:** `internal/services/chat.go` (cancel, resolve-approval, paged transcript, workspace file reader), `settings_providers.go` (model capability DTO), attachments storage/serving (desktop Wails + server-mode bearer-gated HTTP).
- **Frontend:** `features/chat/` (composer pickers + stop, transcript windowing/day dividers/minimap/regenerate, header, approval card, attachment tray, run card, right panel), `features/agents/` (config effort select), `lib/client.ts` + regenerated Wails bindings after every Go signature change.
- **Both hosts:** desktop and server mode must expose identical behavior per the app-shell service-surface requirement; capability URLs must work in both.
- **Themes:** every new UI surface specifies light and dark behavior per the design contract.