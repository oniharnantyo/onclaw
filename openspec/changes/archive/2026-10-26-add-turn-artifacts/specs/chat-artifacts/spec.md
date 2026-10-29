# Spec Delta

## Purpose

Surface the files an agent produces for the user: detect what a turn created or changed under the workspace's `artifacts/` directory, persist that list with the turn, and render it as a chips row at the end of the assistant message that opens each file in the right panel — the outbound counterpart of chat-attachments.

## ADDED Requirements

### Requirement: Artifacts directory as the deliverables boundary

An agent's workspace SHALL hold user-facing deliverables under an `artifacts/` directory at its root, not dot-prefixed. The directory SHALL be created on demand — at the first turn and at the first generated document — so existing workspaces need no migration. Artifact detection SHALL consider only this directory: files written anywhere else in the workspace are never reported as artifacts, so a missed convention degrades to today's behavior (the file exists, no chip) rather than to noise. When `artifacts` exists as a regular file instead of a directory, the turn SHALL still run to completion with detection disabled and the failure SHALL be logged.

#### Scenario: Directory created on demand

- **WHEN** an agent whose workspace has no `artifacts/` directory runs a turn
- **THEN** the directory exists afterwards and the turn completed normally

#### Scenario: Root-level file is not an artifact

- **WHEN** a turn writes a report at the workspace root instead of under `artifacts/`
- **THEN** the turn's terminal event reports no artifacts and the file remains readable through the workspace preview

#### Scenario: Artifacts path blocked by a regular file

- **WHEN** a workspace holds a regular file named `artifacts`
- **THEN** the turn runs to completion with no artifacts reported and the failure is logged

### Requirement: Turn-boundary detection

The runtime SHALL record the state of `artifacts/` when a turn starts and compare it when the turn completes. A regular file present at completion and absent at start SHALL be reported as `created`; a file present at both points whose size or modification time differs SHALL be reported as `modified`; unchanged files SHALL NOT be reported. Detection SHALL be recursive, consider regular files only, skip dot-prefixed entries, and MUST NOT follow symlinks. Every producer counts — file tools, shell commands, scripts, generated documents, and delegated subagents — because detection reads the filesystem rather than tool arguments. Deletions and renames SHALL NOT be reported.

#### Scenario: File written by a shell command is detected

- **WHEN** a turn's shell command writes `artifacts/report.md` and the turn completes
- **THEN** the turn reports `artifacts/report.md` as `created` with its size

#### Scenario: Rewritten file is reported as modified

- **WHEN** a file already under `artifacts/` before the turn is rewritten during the turn
- **THEN** the turn reports it as `modified`

#### Scenario: Untouched file is not reported

- **WHEN** `artifacts/` holds a file from an earlier turn that the current turn does not touch
- **THEN** the turn's report omits it

#### Scenario: Hidden entries and symlinks are ignored

- **WHEN** the turn writes a dot-prefixed file under `artifacts/` and creates a symlink there pointing outside it
- **THEN** neither appears in the report

### Requirement: Detection across approval resumes

When a turn pauses for an approval and resumes within the same process, the start-of-turn record SHALL be kept so the resumed turn's report still distinguishes created from modified. When a turn resumes after a process loss, no record exists: the turn SHALL report every regular file under `artifacts/` whose modification time is at or after the turn's persisted user-message time, with the change kind omitted because created-versus-modified cannot be known.

#### Scenario: Live approval resume keeps the record

- **WHEN** a turn writes `artifacts/a.md`, pauses for approval, is approved in the same process, and completes
- **THEN** the report lists `artifacts/a.md` as `created`

#### Scenario: Cold resume falls back to modification time

- **WHEN** the process is lost mid-turn and the turn resumes after restart having written `artifacts/a.md`
- **THEN** the report lists `artifacts/a.md` with no change kind, and files older than the turn's user message are omitted

### Requirement: Artifacts persist on the turn's terminal event

The detected list SHALL be carried on the `turn_completed` event's payload: each entry's workspace-relative path (for example `artifacts/report.md`), size in bytes, and change kind (`created`, `modified`, or absent). Entries SHALL be sorted by path and capped at 50, with a truncation flag set when the cap was hit. The list SHALL be persisted with the event so a reloaded transcript shows the same artifacts the live view did. A hook-blocked submission (the model never ran) SHALL report none; cancelled and errored turns SHALL report none. No new event kind and no schema migration SHALL be introduced, and payloads carrying the list MUST remain readable by a binary that does not know the field.

#### Scenario: Replay shows the same artifacts

- **WHEN** a turn that produced two artifacts is reloaded after an app restart
- **THEN** the assistant message shows the same two chips the live view showed

#### Scenario: Cap reported

- **WHEN** a turn produces more than 50 files under `artifacts/`
- **THEN** the event carries 50 path-sorted entries and the truncation flag is set

#### Scenario: Blocked submission carries no artifacts

- **WHEN** a hook blocks a submission before the model runs
- **THEN** its `turn_completed` event carries no artifacts

### Requirement: Artifacts chips row

The assistant message SHALL render the turn's artifacts as a chips row after the reply text and outside the tool-call grouping, so heavy-turn folding can never hide it. Each chip SHALL show an icon chosen by file extension, the file name, and the human-readable size, matching the attachment chip grammar; the change kind, when known, SHALL be available as a tooltip. Up to six chips SHALL render inline; further entries SHALL sit behind a "+N more" disclosure that expands the full list, and a truncated list SHALL state that the cap was reached. Activating a chip SHALL open the file's preview tab in the right panel. Regenerated variants of a turn SHALL each carry their own row. A turn with no artifacts SHALL show no row. The row SHALL render correctly in both the light and dark themes.

#### Scenario: Chips survive heavy-turn folding

- **WHEN** a turn with five tool calls writes `artifacts/report.md` and its tool cards fold
- **THEN** the chip for `artifacts/report.md` is visible below the reply, outside the fold

#### Scenario: Chip opens the preview

- **WHEN** the user activates an artifact chip
- **THEN** the right panel opens a preview tab for that workspace-relative path

#### Scenario: Many artifacts collapse

- **WHEN** a turn reports nine artifacts
- **THEN** six chips render inline with a "+3 more" disclosure that reveals the rest

#### Scenario: No artifacts, no row

- **WHEN** a turn produces no files under `artifacts/`
- **THEN** the assistant message shows no artifacts row
