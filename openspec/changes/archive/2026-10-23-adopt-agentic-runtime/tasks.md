# Tasks

## 1. Version spike (gates everything)

- [x] 1.1 Add `eino-ext/components/model/agentic{claude,gemini,openai}` and `eino-ext/adk/backend/local` to go.mod; `go build ./...` against the pinned eino tag; if compilation fails on API drift, bump eino to the newest tag the components support and re-run `go test ./internal/agents/...` to re-verify the adopted middlewares. Verification: `go build ./...` and `go vet ./...` clean.
- [x] 1.2 Fake-model smoke through the agentic type: compose one turn, run it, drain it, with `*schema.AgenticMessage` end to end (text + reasoning + one tool call). Record the bump decision and any API notes in this change's design.md. Verification: the spike test passes and its outcome is written down.

## 2. Provider catalog on agentic components

- [x] 2.1 Bind each catalog provider family to its agentic model component (claude, gemini, openai; openai-compatible rides agenticopenai with the configured base URL), honoring stored credentials and model listings. Verification: catalog unit tests build every provider type and assert the component/basewire-up per family.
- [x] 2.2 Capability predicate for the required API surface: endpoints without it resolve as unavailable with a named reason (next to the existing key gate), and the turn fails before any model request. Verification: table test over endpoint flavors (native openai, compatible-with-responses, compatible-without) asserting gate outcomes and error text.
- [x] 2.3 Carry model capability metadata (effort set, vision, context limit) through the agentic components into the stored-authoritative model rows. Verification: refresh upsert test shows capabilities populated from the agentic components' listings.
- [x] 2.4 Probe a real Ollama local endpoint and decide the gate's granularity (family-level vs per-model feature bits); record the outcome in design.md. Verification: documented decision + test covering the chosen granularity.

## 3. Runtime message-type migration

- [x] 3.1 Streaming parity harness: pin current drain behavior (assistant text deltas, reasoning deltas, tool activity ordering, usage) against a fake model BEFORE the swap. Verification: harness passes on the pre-swap tree and becomes the regression baseline.
- [x] 3.2 Move compose, run, and drain to `*schema.AgenticMessage` (instruction as system agentic message, tool calls/results as function blocks, effort through the agentic request path). Verification: parity harness green on the agentic path.
- [x] 3.3 Checkpoint version marker with dual-read: unmarked payloads convert legacy messages to content blocks on rehydrate (text → gen-text, reasoning → reasoning block, tool pairs → function blocks; unknown parts pass through as marked text, never dropped). Verification: round-trip tests over recorded legacy session fixtures.
- [x] 3.4 Measure the summarization estimator against real block payloads; adjust `TokenCounter` wiring if block overhead shifts the ~0.9 trigger materially. Verification: measurement note in design.md; layered-threshold test (in-run net never triggers checkpoint compaction) still green.
- [x] 3.5 Transcript `TranscriptFormat` over agentic blocks producing today's `transcriptEntry` JSONL shape. Verification: transcript fixture test shows entry shapes byte-comparable to current output.
- [x] 3.6 Delete the legacy `*schema.Message` turn path; full gate: `gofmt -w .`, `go vet ./...`, `go test ./...`. Verification: gates green with no dead legacy path remaining.

## 4. Subagent consolidation (deep-shaped, on agentic types)

- [x] 4.1 Build `internal/agents/subagents/` as the single stack home: `Config` (persona/instruction, model, child surface, manager deps as fields, dirs, registry), `validateConfig` with explicit errors, `New` composing the child clone internally (no delegation tools in the child's surface, background never propagated to children). Verification: constructor unit tests cover validation errors and the child surface's tool set; import direction `subagents → compose` holds (no reverse import).
- [x] 4.2 Move the task card's birth to the bridging store's `Create` (uniform for foreground and background) and delete `setPending`, `latestAllocatedID`, and the `"with ID: "` parse. Verification: registry test shows one record per run minted at Create with the IDGen identifier; no string-parse code remains (`grep`).
- [x] 4.3 Configure the middleware natively (default `agent` tool name, eino arg schema, eino result shapes) and delete both wrapper middlewares; keep the `transcriptEntry` materialization through `TranscriptFormat`. Verification: hooks-by-name regression test (`agent`/`task_output`/`task_stop` still fire hooks) and foreground/background delegation tests against a fake model.
- [x] 4.4 Delete root `subagent.go` and the orphaned duplicate implementation; wire runner and resume through the package; adapt the W2 spike tests to the new seam. Verification: `go test ./internal/agents/...` green; `grep resolveDelegation` empty.

## 5. Backend composition + frontend contract

- [x] 5.1 Compose `eino-ext/adk/backend/local` beneath the jail via a path-translation shim for file ops (read/write/edit/glob/grep/ls); upstream `Execute` not adopted. Verification: existing jail tests (in-workspace success, escape rejection) pass unchanged against the composed backend.
- [x] 5.2 `read_file` multimodal rendering: images as image parts, PDFs whole or by page range, upstream size caps with clear error results, text files unchanged. Verification: multimodal read tests (image, paged PDF, oversize, text passthrough).
- [x] 5.3 Confirm the jailed shell remains the exec path (approvals, timeout, output caps) with the backend swap in place. Verification: shell regression tests (approval gate, timeout kill, truncation report) green.
- [x] 5.4 Frontend agent tool schema: update mapping/`TaskTool`/trace rows to the native args (`prompt`/`description`/`subagent_type`/`run_in_background`) and native results; task_activity cards unchanged. Verification: updated frontend tests (`taskRenderers`, `mapping`, `runSurfaces`) green.
- [x] 5.5 Regenerate bindings at wave end (`task common:generate:bindings --force`), then `task build`; frontend gates `vp check src`. Verification: bindings diff reviewed, build and vp check clean.

## 6. Verify + close

- [ ] 6.1 Live smoke against a real provider: a normal turn, a foreground delegation, a background delegation with task_output/task_stop, app restart showing the transcript and a legacy session loading through dual-read. Verification: recorded session outputs in the change notes (server-mode headless or desktop).
- [x] 6.2 Full gate + spec check: `gofmt -w .`, `go vet ./...`, `go test ./...`, `openspec validate adopt-agentic-runtime --strict`. Verification: all green; tasks ticked.
