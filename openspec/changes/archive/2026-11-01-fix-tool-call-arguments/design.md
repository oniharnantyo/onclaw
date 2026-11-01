# Design

## D1: Evidence and root-cause shape

Live database: 369 terminal tool records, 348 with `arguments`. Split by call-id prefix: `chatcmpl-tool-*` 342/342 carry arguments; `call_*` (GLM/Z.AI connector) 6/27 — the 6 with arguments are the non-streamed path, the 21 without are streamed runs. The persisted shape (name + result + duration present, arguments absent) means the running announcement and terminal record ran, but the assembler's argument text for that call was empty: `drainAssistantOutput` feeds `toolCallAssembler.observeBlock` (`internal/agents/runner.go:893,914`), and `recordToolResultMessage` copies `args[callID]` (`runner.go:1006`), which `calls.finish()` stamped from the same assembled calls (`runner.go:937`). Empty at the assembler ⇒ empty everywhere.

`observeBlock` keys strictly by `call.CallID` (`runner.go:1241-1258`). A provider that streams argument deltas on frames whose CallID is empty (or id-carrying only on the identity frame) makes those delta frames open a second entry keyed `""`; the real call keeps its empty Arguments and the orphan (CallID `""`) is never stamped into `args` (`runner.go:938` guards `call.CallID != ""`) — exactly the observed record. The sibling `observe` already solves this class of problem for classic tool calls with an index-keyed fallback; `observeBlock` never got the equivalent.

## D2: Fix — merge unidentifiable frames into the call in progress

`observeBlock` gains streaming-merge semantics: a block with an empty CallID appends its Name/Arguments to the most recently observed in-progress call instead of opening an `""` entry; `finish` drops any residual `""` entry defensively. Call order across a single model call is sequential, so "most recent" is the same call the frames belong to — the same assumption `observe`'s index fallback makes. Non-streaming frames (id + args in one block) are unchanged. Keep both paths' existing first-frame behavior (id and name stamp only when non-empty).

## D3: Reproduction first

The bug is provider-chunking-shaped, so the regression test must reproduce the chunking, not just the merge: a fake streamed `AgenticMessage` whose tool call arrives as an identity frame (CallID + name, no args) followed by argument-only frames (empty CallID) must assemble one call with the full argument string and no orphan. Add it before the fix (red), then green.

## D4: Non-goals

- No frontend change: `mapping.ts` passes `call.arguments` through, `ExecuteTool` renders the command once it exists, and the fallback body already shows an Arguments pane when `argsText` is non-empty — the observed card was the spec's degrade path doing its job on an incomplete record.
- No data migration: records persisted without arguments keep rendering result-only (spec: no fabrication).
- No change to the live running-announcement path beyond what the assembler fix naturally improves (live rows gain their chip/command text too).
