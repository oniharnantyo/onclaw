## Context

`Scope.Workspace` is the resolved **agent workspace directory** — for the `master` agent it is
`~/.onclaw/workspace/master` (set in `internal/cli/context.go`). Every consumer of `Scope.Workspace`
(fsbackend, memory, embed, pathguard) roots files directly beneath it. The spill subsystem
(`internal/agent/tools/spill.go`, `SpillArtifactPaths`) is the lone outlier: it treats `Workspace` as
a *project root* and re-appends `.onclaw/workspace/<agent>/…`, producing a doubled, semantically wrong
path:

```
~/.onclaw/workspace/master/.onclaw/workspace/master/sessions/<id>/tool_results/
└── Workspace (agent dir) ──┘└── re-appended as if Workspace were a project root ──┘
```

The bug is invisible at runtime because the absolute path written to disk and the relative path handed
to the model are *symmetrically* wrong: the model's later `read_file` does `filepath.Join(workspace, rel)`
(`pathguard.go`), reconstructing the same doubled location. The path contract is encoded across two
specs (`agent-tools` path layout, `conversation-history` compaction detection), so the contract changes
with the code.

## Goals / Non-Goals

**Goals:**
- Spilled artifacts land at the correct, non-doubled location rooted directly under the resolved agent workspace.
- The relative envelope path resolves exactly where the file was written (abs/rel round-trip consistency).
- Spill-path detection across compaction keeps working, so durable pointers survive summarization.

**Non-Goals:**
- Migrating existing doubled-path spill files off disk.
- Changing spill-threshold semantics, the per-tool-group config schema, or the spill-decorator architecture.
- Introducing a project-root vs agent-workspace distinction into the data model.

## Decisions

### D1: Treat `Scope.Workspace` as the agent workspace root in spill construction
Drop the `.onclaw/workspace/<agent>` prefix from both the absolute directory and the relative path. The
agent identity is already encoded by the resolved workspace dir, so the prefix is redundant; aligning
spill with every other `Scope.Workspace` consumer removes the conflation.

Alternatives considered:
- **Make `Workspace` the project root / `~/.onclaw`** — rejected: architectural churn across all tools and breaks the per-agent workspace model the app depends on.
- **Detect-and-strip** when `Workspace` already ends with `.onclaw/workspace/<agent>` — rejected: special-casing masks the conflation instead of fixing it and is fragile across custom workspace paths.
- **Keep the prefix in `rel` only** — rejected: breaks the abs/rel round-trip; `read_file` could not locate the file.

### D2: New path shapes (naming and sanitization unchanged)
- Absolute dir: `<resolved workspace>/sessions/<session_id>/tool_results/`
- Relative envelope path: `sessions/<session_id>/tool_results/<name>`
- Filename `<tool>_<timestamp>_<title>`, component sanitization, and 0700/0600 perms are unchanged.

### D3: Update the scrub detection regex
`internal/agent/summarization_scrub.go` `spillPathRe` changes from
`\.onclaw/workspace/[^ \n]*?/tool_results/[^ \n]+` to a shape that matches the new layout while keeping
the distinctive `tool_results/` anchor (the stable marker the existing design relies on). Candidate:
`sessions/[^/ \n]+/tool_results/[^ \n]+`. Dropping `.onclaw/workspace/` removes the now-absent prefix;
restricting the session segment to `[^/ \n]+` avoids greedy prose matching.

## Risks / Trade-offs

- **Orphaned old spill files** → Not migrated. Old relative paths persisted in compacted stubs still resolve (pathguard joins the old rel to the workspace, hitting the old doubled abs path) until those files are cleaned or sessions age out. Acceptable: spill files are session-lifetime artifacts. Documented in the proposal.
- **Scrub regex precision after change** → If too loose, a non-spill `tool_results` mention could be preserved as a durable pointer; if too tight, real spills could be cleared. Mitigation: pin to `sessions/[^/ \n]+/tool_results/…` and add a test fixture mirroring `summarization_scrub_test.go` cases.
- **Pre-change stored stubs** → Recall of spills created before the change keeps working (old path still on disk and still resolvable). Only new spills use the new path.

## Migration Plan

No data migration. Deploy the code change; new spills use the new path. Rollback = revert code; new spills revert to the doubled path with no data loss. An optional one-time sweep of `~/.onclaw/workspace/*/.onclaw/` orphan directories is a follow-up, out of scope here.
