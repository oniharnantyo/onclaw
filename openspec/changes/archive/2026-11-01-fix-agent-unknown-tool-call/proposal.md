# Proposal

## Why

A live smoke (2026-10-29, session ef8a8b0c) twice died mid-turn with `[NodeRunError] tool list not found in toolsNode indexes [node_1, ToolNode]` (then again for `read`): the model hallucinated tool names that do not exist in its surface (`list`, `read` — the real tools are `ls` and `read_file`). eino's ToolsNode dispatch fails before any tool endpoint runs, so the `ToolErrorResult` middleware — whose contract is that a failing tool must not kill the turn — never sees the failure, and the node error aborts the whole graph. One hallucinated name erases every prior step of the turn.

## What Changes

- The per-turn tool-node composition gains eino's `UnknownToolsHandler`: a tool call naming a tool outside the offered surface is routed to a handler that fails the call with "unknown tool" — inside the normal tool-execution machinery, so the existing `ToolErrorResult` middleware converts it to the same JSON error payload a real tool failure produces and the model reads it as turn data and retries with a real tool name.
- The turn survives an unknown tool name: the known calls in the same response execute normally, the unknown ones feed back as errors, and the loop continues to a final answer.
- The synthetic result rides the same failure registry and transcript surface as a real tool failure (a failed tool record on the session transcript), with no duplicate records.
- `agent-tools` "Tool execution loop" updated: the failure-feeds-back rule covers dispatch failures (unknown tool names), not only endpoint failures.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-tools`: "Tool execution loop" — an unknown tool name SHALL be answered with an error tool result instead of ending the turn.

## Impact

- `internal/agents/compose/` (new middleware file + registration in `compose.go`'s handler list; reuses `toolerror.go`'s error payload shape), `internal/agents/compose/compose_test.go` or a sibling test file, `internal/agents/` turn-level test.
- No frontend, binding, or storage changes; no eino upgrade.
