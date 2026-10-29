# Proposal

## Why

When an agent produces a file for the user — a research report in markdown, a PDF from `create_document`, a diagram — nothing in the chat says "here is your deliverable." The file lands flat at the workspace root beside the persona documents, cloned repositories, and scratch, and the only trace in the transcript is a tool card that heavy-turn folding hides behind "9 steps · 1 file changed" the moment a research turn runs four or more calls. Surfacing today depends entirely on the model remembering to write a markdown link in its reply. `create_document` landing made this concrete: its first live-smoke PDFs sit at the root of the `scout` workspace with no way to find them from the conversation.

## What Changes

- **A deliverables home.** Every agent workspace gains an `artifacts/` directory at its root — not dot-prefixed, because unlike `.tasks/` it is user-facing. Created with the workspace and on first run for existing ones.
- **Turn-boundary detection, no new tool.** The runtime snapshots `artifacts/` when a turn starts and again when it ends; files that appeared or changed in between are the turn's artifacts. Because this reads the filesystem rather than tool arguments, every producer counts — `write_file`, shell commands, scripts, `create_document`, delegated subagents. The model is never asked to call anything to declare a deliverable.
- **Artifacts persist on the turn's terminal event.** The delta (workspace-relative path, size, created-or-modified) rides the `turn_completed` event's payload the way token usage already does, so a reloaded transcript shows the same artifacts the live view did. No new event kind and no schema migration: event payloads are opaque JSON blobs.
- **An artifacts row at the end of the assistant message.** Chips — icon, name, size — rendered after the answer text and outside the tool-call grouping, so folding can never hide them. Activating a chip opens the file in the right panel. The grammar mirrors the attachment chips on user messages: attachments are files going in, artifacts are files coming out.
- **PDF artifacts preview as documents.** The right panel's workspace-file preview renders PDF content as a document (the renderer the knowledge panel already uses) instead of as text, so a PDF chip opens something readable.
- **`create_document` writes under `artifacts/`.** Its output root moves from the workspace root to `artifacts/`; the tool's input stays a plain file name. The bound read that serves the PDF to its card resolves plain names inside `artifacts/`, with a read-only fallback to the workspace root so transcripts generated before this change keep rendering.
- **The operating contract teaches the home.** One rule in the contract's workspace section: deliverables the user should find are written under `artifacts/`. This complements, not contradicts, the existing "don't proactively create report files" rule — one governs *whether*, the other *where*.

Out of scope for this change: per-session nesting under `artifacts/`; a cross-session artifact library in the right bar's identity view; surfacing deletions; recording which tool produced a file; artifact titles or descriptions beyond the file name; an explicit `save_artifact` tool; diffing the whole workspace; reveal-in-Finder or external-open actions.

## Capabilities

### New Capabilities

- `chat-artifacts`: Detect the files a turn produces for the user under the workspace's `artifacts/` directory, persist them with the turn, render them as a chips row at the end of the assistant message outside the tool-card fold, and open them in the right panel. The outbound counterpart of `chat-attachments`.

### Modified Capabilities

- `data-store`: "Home directory layout" — an agent's workspace additionally holds user-facing deliverables under `artifacts/`, created with the workspace.
- `agent-tools`: "Create document tool" — the plain file name is written under the workspace's `artifacts/` directory rather than at the workspace root; containment and overwrite rules are unchanged.
- `chat-tool-cards`: "Generated document access" — the bound read resolves the reported plain name inside `artifacts/`, falling back to the workspace root for documents generated before this change; the plain-name and containment refusals are unchanged.
- `chat-panels`: "Preview sources" — a workspace file whose content is a PDF renders as a document in its preview tab, as knowledge PDFs already do.
- `agent-prompts`: "Virtual operating contract" — the workspace rules the contract covers include where user-facing deliverables are written.

## Impact

- **Backend (Go):** `internal/agents/runner.go` (snapshot at turn start and end around the existing `turn_completed` emission; approval-resume path re-snapshots), `internal/sessions/types.go` (an `Artifacts` payload on `Event` for `turn_completed`), `internal/agents/docgen_tool.go` (output root), the `generatedDocument` bound read (base directory plus legacy fallback), `internal/agents/contract.go` (one rule inside the existing "Working in the workspace" section — the section set `contract_test.go` pins is unchanged), and workspace creation (`artifacts/` ensured alongside `.tasks/`).
- **Frontend (React):** `features/chat/thread/ChatAssistantMessage.tsx` (the chips row slot between the parts switch and the footer), a new artifacts chips component beside `AttachmentView`, `features/chat/components/panelRenderers.tsx` (PDF path for workspace-file tabs), and the runtime mapping that carries `turn_completed` metadata onto the assistant message. Both themes.
- **Bindings:** regenerated after the Go surface changes (finish with `task build`, since `task build:server` strips them).
- **Specs:** one new spec, five delta specs.
- **No** new tool, event kind, database migration, or dependency. Existing transcripts are unaffected except that `create_document` cards from before the change now read through the fallback.
