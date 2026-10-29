# Design

## Context

See proposal.md — Why. The pieces this design composes already exist:

- **Turn lifecycle.** `runner.go` emits `turn_completed` carrying `Usage`; `runningTurn` (`manager_impl.go`) holds per-turn state that survives a live approval resume (`usage`, `args`) and is rebuilt from the store on a cold resume after process loss. Hook-blocked submissions complete through `blockedPromptTurn` without calling the model.
- **Event persistence.** `session_events.payload` is an opaque JSON blob; `sessions.Event` carries typed per-kind fields (`Usage`, `Tool`, `Task`, `Plan`, `Attachments`). Adding a field is a struct change, not a migration. Go's decoder ignores unknown fields, so old and new binaries read each other's payloads.
- **Workspace layout.** `workspaces.go` reserves `.tasks/` via `DirTasks` and creates it on demand through `TasksDir(slug)`; the data-store spec pins the layout.
- **`create_document`.** Writes a plain name at the workspace root (`docgen_tool.go`); its card fetches bytes through `ChatService.GeneratedDocument(agentID, name)`, which accepts plain names only.
- **Transcript.** `entries.ts finalize()` closes streaming entries on `turn_completed`; the assistant-ui adapter mints `data` parts (`error`, `compaction`) that `ChatAssistantMessage` renders in its parts switch; tool grouping (`groupPartByType`) groups only `tool-call` and `reasoning` parts. `AttachmentView` renders inbound file chips and exports `formatBytes`.
- **Right panel.** `ChatService.WorkspaceFile(agentID, relPath) (string, error)` is a jailed text read; PDF rendering exists only on knowledge tabs (mime-gated `<object>` embed in `panelRenderers.tsx`).

Constraints: no new tool, no new event kind, no migration, both themes, the fs jail is never loosened.

## Goals / Non-Goals

**Goals:**

- Every file a turn produces under `artifacts/` surfaces with the reply, regardless of which tool or process wrote it.
- The surfacing is replay-exact: a reloaded transcript shows the same chips the live view did.
- A missed convention degrades to today's behavior (file exists, no chip) — never to noise.
- `create_document` output is an artifact without any special casing in detection.

**Non-Goals:**

- Attributing an artifact to the tool that produced it.
- Surfacing artifacts on cancelled or errored turns (follow-up; see Risks).
- Detecting deletions or renames.
- Any change to the fs jail's read/write rules.

## Decisions

### D1 — `artifacts/` is a reserved, user-facing workspace directory

`workspaces.DirArtifacts = "artifacts"` beside `DirTasks`, with `ArtifactsDir(slug)` creating on demand exactly like `TasksDir`. Flat for v1: sessions are per-agent, so artifacts are per-agent; name collisions across sessions are the model's to manage, consistent with `create_document`'s documented overwrite-on-regenerate.

*Alternatives:* dot-prefixed (`.artifacts/`) — rejected; the dot convention marks agent-internal state and these are for the user. Per-session subdirectories — rejected for v1; adds a path the model must know and a UI grouping nobody asked for; can be layered later without changing detection.

### D2 — The directory is the only boundary; no tool, no heuristics

Detection watches `artifacts/` and nothing else. The model learns one sentence (D9).

*Alternatives:* (a) An explicit `save_artifact` tool — new vocabulary for the model, and `create_document` would need a second step. (b) Diffing the whole workspace minus structural exclusions (dot-dirs, `skills/`, persona documents) — scratch directories have arbitrary names (`re_repo/`, `clear/` in the live `scout` workspace), so a clone-a-repo turn floods the result and no fixed exclusion list separates scratch from deliverables. (c) Flat-root-files-only — would have caught every real deliverable in `scout`'s history by inertia, but turns root-level scratch into chips and hardcodes the persona-document names. Precision is the product here: a chip must mean "this is for you."

### D3 — Start snapshot on `runningTurn`, diff at the terminal; mtime predicate on cold resume

At turn start the runner lists `artifacts/` into `runningTurn.artifactsBefore map[relpath]{size, mtime}` — the same lifetime as `usage`, so a live approval resume (same process) keeps it. At the completion site (`runner.go` step 10) the runner lists again and emits the delta: present-now-absent-before → `created`; present in both with a different size or mtime → `modified`.

On a cold resume (process lost; `runningTurn` rebuilt from the store) there is no snapshot. The predicate becomes `mtime >= turn start`, where turn start is the persisted `user_message` event's `OccurredAt` — already read by the resume path. The change kind is omitted on that path because created-vs-modified cannot be known.

*Alternatives:* Client-side derivation from `write_file`/`edit_file` arguments and `create_document` results (no Go at all) — misses shell, scripts, and subagent writes, which is the whole reason for B. Pure mtime always — stateless and simpler, but blind to files extracted with preserved timestamps (`tar -x`, `cp -p`, `git checkout`) and unable to distinguish created from modified; kept only as the fallback. Persisting the snapshot in the approval checkpoint — more durable than the mtime fallback but couples the checkpoint schema to this feature for a rare path.

### D4 — The delta rides `turn_completed`, bounded and ordered

`sessions.Event` gains `Artifacts []Artifact` (`Path` workspace-relative, `Size`, `Change` created|modified|empty) plus `ArtifactsTruncated bool`, set on `turn_completed` only. Entries are sorted by path and capped at 50; the flag reports a cap hit. `blockedPromptTurn` sets none (the model never ran). `turn_cancelled` and `turn_error` carry none in v1.

*Alternatives:* A new `KindArtifact` event per file — duplicates what the terminal event can carry, and `Usage` already establishes the terminal event as the turn-summary carrier. Attaching to every terminal kind — the cancelled path (`recordCancelledTerminal`) persists through a context that already died; adding filesystem work there is a separate change with its own failure modes.

### D5 — Walk rules

Recursive under `artifacts/`; regular files only; skip dot-prefixed entries; do not follow symlinks; paths are workspace-relative (`artifacts/report.md`) so a chip feeds `workspaceFileTab` directly with no mapping. The listing runs on the turn's goroutine at two points; it is one `WalkDir` over a directory the user owns, not a hot path.

### D6 — `create_document` writes under `artifacts/`; its card read gains a legacy fallback

`docgen_tool.go` roots output at `ArtifactsDir(slug)` instead of the workspace root. The tool's input stays a plain name, its validation (`validateDocFilename`) is unchanged, and the result still reports the plain name — so `CreateDocumentTool.tsx`'s `PLAIN_DOC_NAME` parser and body need no change. `ChatService.GeneratedDocument` resolves the plain name inside `artifacts/` first and, when absent there, inside the workspace root — both jailed, both plain-name, read-only — so cards from transcripts recorded before this change keep rendering.

*Alternatives:* Result reports `artifacts/<name>` — breaks the card's plain-name contract and the chat-tool-cards requirement for no gain. No fallback — regresses every existing `create_document` card to "Document unavailable."

### D7 — Chips render as a trailing `data` part, in message order, outside the fold

`entries.ts finalize()` stamps the `turn_completed` event's `Artifacts` onto the turn's assistant entry; the adapter mints a `data` part named `artifacts` after every other part. `ChatAssistantMessage`'s `case "data"` renders it as a chips row. Grouping only folds consecutive `tool-call`/`reasoning` parts, so a trailing `data` part can never be swallowed by heavy-turn folding — the placement guarantee comes from the part type, not from a layout trick. Each chip mirrors `AttachmentView`'s grammar (icon by extension, name, `formatBytes` size), with a `created`/`modified` hint when known; up to a small fixed number render inline with a "+N more" disclosure for the rest. Activating a chip calls `openPanelTab(workspaceFileTab(activeAgentID, path))` — the same call `ChatMarkdownLink` makes. Regenerated variants each carry their own row.

*Alternatives:* `message.metadata.custom.artifacts` rendered after `GroupedParts` — works, but the `error`/`compaction` data-part precedent already exists in the switch and keeps the row inside the message's reading column.

### D8 — PDF preview in the panel via a jailed binary read

A sibling `ChatService.WorkspaceFileBinary(agentID, relPath) → {mime, data base64}` under the same jail as `WorkspaceFile`. `panelRenderers.tsx`'s workspace-file tab picks the read by extension — `.pdf` → binary read → the knowledge tab's `<object>` renderer; everything else → the existing text path with its markdown toggle. `WorkspaceFile` keeps its `string` signature.

*Alternatives:* Reuse `GeneratedDocument` for the panel — serves only plain names under `artifacts/`, so a PDF anywhere else in the workspace still previews as garbage text. Loosen `GeneratedDocument` to relative paths — weakens the card read's contract to serve a different consumer.

### D9 — One contract rule, section set unchanged

In the operating contract's "Working in the workspace" section, after the "don't proactively create report files" rule: deliverables the user should find — reports, generated documents, exported data — go under `artifacts/` at the workspace root, and files that appear or change there during the turn are shown to the user with the reply. The `create_document` tool description says it writes under `artifacts/`. `contract_test.go` pins section order and set; neither changes.

## Risks / Trade-offs

- [Two turns of the same agent overlap — scheduler fire during a live chat — and both write `artifacts/`] → Each attributes the other's files; accepted for v1, the fuzziness is bounded to the overlap window and the chips still open the right files.
- [`artifacts` exists as a regular file in an older workspace] → `ArtifactsDir` fails; the runner logs it and runs the turn with detection disabled, `create_document` returns its readable error. Not silent, not fatal.
- [Cold-resume mtime predicate misses files extracted with preserved timestamps] → Degrades to no chip for those files on that rare path; the live-resume path keeps the snapshot.
- [Model writes a deliverable at the root instead of `artifacts/`] → Today's behavior exactly: the file exists, the model may link it, no chip. False negative, never noise.
- [A turn dumps hundreds of files into `artifacts/`] → Cap at 50 with `ArtifactsTruncated`; the chips row shows the count. The payload stays bounded.
- [Cancelled turn wrote a report before the cancel] → Not surfaced in v1; the file is in `artifacts/` and the next turn or a markdown link reaches it. Follow-up: extend D4 to `turn_cancelled` once the detached-context recording path is reasoned about.
- [Chips row and PDF embed in both themes] → Chips reuse the attachment chip tokens; the PDF `<object>` is the knowledge tab's already-themed element.

## Migration Plan

1. Ship the Go changes: `DirArtifacts`/`ArtifactsDir`, `Event.Artifacts`, snapshot/diff in `runTurn` and the cold-resume predicate, `create_document` root, `GeneratedDocument` fallback, `WorkspaceFileBinary`, contract rule.
2. Regenerate bindings and finish with `task build` (`task build:server` strips them).
3. Ship the frontend: entry stamping, adapter data part, chips row, panel PDF path.
4. No startup sweep: `ArtifactsDir` creates on demand at the first turn and the first `create_document` call; existing workspaces need nothing.
5. Rollback: revert; payloads with `artifacts` decode cleanly under the old struct (unknown fields ignored), and PDFs already written under `artifacts/` stay readable through the jail.

## Open Questions

- The inline chip count before "+N more" (a visual tuning, settled in specs/tasks against the attachment row's behavior).
- Whether the `created`/`modified` hint earns a visible treatment or stays a tooltip.
