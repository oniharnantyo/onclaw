# Proposal

## Why

Tool-call records persisted by turns on the GLM/Z.AI connector lose the call's arguments: 21 of the 27 `call_*`-prefixed tool records in the live database carry no `arguments` field, while all 342 `chatcmpl-tool-*` records carry them. Because the chat's tool cards render the command/arguments from the persisted record, a reloaded transcript shows shell cards with a bare "Result" and no command — the shaped terminal body (which throws on a missing command) silently degrades to the fallback body. Execution itself is unaffected; only the persisted record is incomplete.

## What Changes

- Fix the tool-call assembler so streamed tool-call argument chunks merge into the call they belong to even when the provider's delta frames identify the call incompletely (empty or missing call id on argument frames), instead of accumulating into an orphan entry that never reaches the terminal record.
- Terminal tool records then persist the call's arguments for every provider family, making reloaded tool cards show the request/command again.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the Streamed turns requirement gains arguments persistence — a tool activity's terminal record SHALL carry the call's arguments as delivered to the tool, per family and regardless of streaming shape, so a reloaded transcript shows the same request the live view did.

## Impact

- `internal/agents/runner.go` (`toolCallAssembler.observeBlock` merging, `finish`), plus its unit tests. No frontend change: the card bodies and fallback already render arguments whenever the record carries them; no stored-data migration (records persisted without arguments stay as they are and keep rendering result-only).
