## Why

Oversized tool results are spilled to files under a path that is double-nested. For the `master` agent, whose resolved workspace is already `~/.onclaw/workspace/master`, the spill subsystem re-appends `.onclaw/workspace/master/…`, producing `~/.onclaw/workspace/master/.onclaw/workspace/master/sessions/<id>/tool_results/`. The on-disk layout is therefore wrong and confusing. It happens to function because the absolute path written and the relative path handed back to the model are *symmetrically* wrong (the model's later `read_file` reconstructs the same doubled path), so the bug is invisible at runtime and only surfaces when a human inspects disk. The path-doubling is encoded in the spec itself, so the contract must change alongside the code.

## What Changes

- Spilled tool-result artifacts are written directly under the resolved agent workspace at `sessions/<session_id>/tool_results/` instead of `.onclaw/workspace/<agent>/sessions/<session_id>/tool_results/`. The redundant `.onclaw/workspace/<agent>` prefix is removed because the resolved workspace already encodes the agent.
- The workspace-relative path returned in the spill envelope (and consumed by `read_file`) becomes `sessions/<session_id>/tool_results/<name>`, staying self-consistent with the new absolute location.
- The summarizer-input scrub's spill-path detection rule is updated to match the new shape (`sessions/<session_id>/tool_results/…`) so spilled-result paths continue to survive compaction as durable pointers.
- Existing spill files already on disk at the old doubled path are orphaned by this change (still readable via their stored relative path until cleaned); no automatic migration is performed.

## Capabilities

### New Capabilities

<!-- None. This change fixes an existing capability's path contract. -->

### Modified Capabilities

- `agent-tools`: The spilled-artifact location requirement changes from `.onclaw/workspace/<agent>/sessions/<session_id>/tool_results/` to `sessions/<session_id>/tool_results/` within the resolved agent workspace. The image-producing-tool scenario that names the same path is updated correspondingly.
- `conversation-history`: The summarizer-input scrub's spill-path detection rule changes to match the new `sessions/<session_id>/tool_results/…` shape, preserving durable-pointer behavior across compaction.

## Impact

- **Code**: `internal/agent/tools/spill.go` (`SpillArtifactPaths` — both absolute and relative path construction), `internal/agent/summarization_scrub.go` (the `spillPathRe` detection regex).
- **Tests**: `internal/agent/tools/spill_test.go`, `internal/agent/tools/browser/browser_test.go`, and `internal/agent/summarization_scrub_test.go` (path-shape assertions and fixtures).
- **Storage**: On-disk layout of spilled artifacts changes; previously spilled files at the doubled path become orphans.
- **No API, dependency, or config-schema changes** — `spill_threshold_bytes` semantics are unaffected.
