# Design

## Context

Three layers already teach the search→read pattern (base prompt, tool descriptions, manifest) yet a live run defected to shell forensics. The failures are in tool *outputs*, where fresh results override static instructions: the manifest TOC hid the section the user asked about; search hits named no next call; `document.read` rejected a valid firmlink spelling with an unteachable error. `Service.TableOfContents` (service.go:409) and `referenceManifestTOCEntries` (references_manifest.go:21, = 6) control manifest depth. `document_read.go` `isWithinRoot` is `strings.HasPrefix` over `EvalSymlinks`-resolved spellings; on macOS `EvalSymlinks` does not collapse `/System/Volumes/Data/...` firmlinks, so one file carries two non-prefix-equal spellings.

## Goals / Non-Goals

Goals: make the correct next action the path of least resistance in tool output; make the jail check spelling-proof; make every rejection self-correcting; one prompt guardrail line.

Non-Goals: no shell sandbox/jail rework (the defection trigger disappears with the above); no embeddings or reranking; no skill (per-agent opt-in fragments a universal contract); no changes to visibility or tenancy.

## Decisions

- **D1 — Manifest TOC = level-1 headings.** `TableOfContents` gains a level filter (level ≤ 1, ordered by ordinal); the manifest passes the full budget-derived count instead of the fixed 6. Budget math is unchanged — long TOCs still collapse per entry. *Alternative:* keep 6 entries but sort by search relevance — rejected: no query at compose time; chapter structure is the honest projection.
- **D2 — Read hint as a string field per hit.** Each hit gains `"read"`: `"references/<name>` scoped to `<locator>"` for located hits, or the plain path for locator-less ones. A string keeps the tool result shape stable and is directly imitable by the model. Locator-less txt/csv hits hint the unscoped read.
- **D3 — Canonicalize firmlink spellings, not SameFile.** A `canonicalizePath` helper strips a leading `/System/Volumes/Data/` (darwin firmlink to `/`) applied uniformly to stored roots (construction) and the resolved candidate (resolve). `EvalSymlinks` stays. *Alternative:* `os.SameFile` inode comparison per root — rejected: prefix semantics needed (path *within* root), not equality; canonicalization is ~6 lines and platform-guarded.
- **D4 — Teaching error copy.** The outside-roots error becomes: `path is outside allowed directories: %q — use references/<document name>, a workspace-relative path, or /workspace/...`. When canonicalization *would* have matched, the error additionally says so ("same file as `references/...`") — turning the 0-ms dead end into a one-retry recovery.
- **D5 — One prompt line, not a skill.** AGENTS.md reference-document paragraph gains: reference documents are already mounted at `references/` and served by `document.search`/`document.read` — never hunt them through the shell. Skills are per-agent opt-in and would fragment a universal contract; promptdocs sweep/tests cover the file.
- **D6 — Make `references/` real for the file-tools lane (session-events finding, 2026-09-28).** The raw run log shows the agent obeyed the base prompt and probed the mount with the file tools first: `glob **/FDS_….docx` → "No files found"; `ls /workspace/references` → `…/workspaces/master/agents/personal-assistant/references: no such file or directory`. The references mount lives under `<onclawDir>/tmp/references-mount/<ws>/<agent>/<session>/references` and is wired as a read-only root into `document.read` only — the glob/ls lane resolves `/workspace` to the agent workspace dir, where no `references/` exists. The prompt points at a path half the toolset cannot see. Fix: wire the run's mount directory into the file tools' read-only roots alongside `document.read` (same registration point — one mount, every lane), so `ls /workspace/references` and `glob **/…` behave as the manifest promises. *Alternative:* prompt-only ("file tools cannot see references/") — rejected: it enshrines the inconsistency the mount design (`referencesRunDir` — "the jail sees the mount exactly at its documented path") intended to avoid.

## Risks / Trade-offs

- [Larger manifest entries (all L1 headings)] → budget cap unchanged; chapters are short strings; worst case collapses per the existing rule.
- [`/System/Volumes/Data` strip on non-macOS paths] → guarded to that exact prefix, which only exists on darwin; unit tests cover both spellings.
- [Hint strings drift from tool contract] → built from the same constants as the tool description (`references/` mount name); single helper, unit-tested.

## Migration Plan

Pure behavior fix; ship with unit tests. No data, API, or schema change.

## Open Questions

- None.
