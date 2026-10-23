# Proposal

## Why

The runtime is pinned to eino's legacy `*schema.Message` while eino and eino-ext have made the provider-neutral content-block schema (`*schema.AgenticMessage`) the primary surface for new middleware and per-provider model components. At the same time the subagent implementation exists twice on the tree (the live `resolveDelegation` in the root package and an orphaned `internal/agents/subagents/` rewrite) and survives on a fragile task-ID handoff (`latestAllocatedID` plus a `"with ID: "` string parse). Migrating both together puts the runtime on upstream's forward path once, instead of refactoring the delegation stack on the legacy type and redoing it on the agentic type later.

## What Changes

- **Agentic schema migration**: the runtime message type becomes `*schema.AgenticMessage`. Providers wire to eino-ext's per-family agentic model components (`agenticclaude`, `agenticgemini`, `agenticopenai`, and siblings as catalog families grow); compose, run, drain, checkpoint, transcript, and the subagent lane all move to the agentic type. The frontend stays insulated behind the drain's `sessions.Event` vocabulary (review, not rewrite).
- **BREAKING** (provider matrix): openai-compatible endpoints ride `agenticopenai` with a custom base URL, which requires the server to implement `/v1/responses`. Endpoints without it are surfaced as unavailable in the catalog (LM Studio and Poolside today; Ollama local partially — no vision input, some item types rejected).
- **Subagent consolidation** (eino `adk/prebuilt/deep` shape): one `internal/agents/subagents/` package — `Config` + `New` + `validateConfig` + nil-safe helpers, the package builds the child clone (manager deps as config fields, builtin middlewares without delegation tools, background never propagated to children), native middleware configuration (`ToolName: "agent"` default, `ToolDescriptionGenerator`) instead of `BeforeAgent` tool-swapping wrappers. The task card's birth point moves to the bridging store's `Create` (uniform for foreground and background); `setPending`, `latestAllocatedID`, and the `"with ID: "` parse are deleted. The root `subagent.go` live path and the orphaned duplicate are both removed.
- **BREAKING** (tool contract): the `agent` tool's arguments become eino-native (`subagent_type`, `prompt`, `description`, `run_in_background`), replacing `task`; foreground and background tool results become eino's native shapes. Frontend tool mapping and its tests follow. Hooks matching `agent`/`task_output`/`task_stop` by name keep working (names unchanged).
- **`eino-ext/adk/backend/local` under the jail**: the workspace jail keeps its rooting and approval-gated shell and composes the upstream `Local` backend beneath it for file operations, gaining multimodal reads — images and paged PDFs rendered as structured parts (in-process WASM pdfium, no CGO). Upstream caveats accepted and recorded: Unix/macOS only, `/bin/sh` execute stays replaced by buddies' jailed shell.
- **Checkpoint versioning**: stored checkpoints are message-shaped, so the agentic payload gets a version marker with dual-read on rehydrate; the session event log vocabulary is unchanged.
- **Sequencing**: `adopt-eino-middlewares` (18/18 complete, uncommitted) is committed first as the rollback base, and its agent-tools delta (todo pair → Task\* tools) lands before this change's deltas are applied.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the provider registry requirement gains agentic execution — turns execute on provider-native agentic model components, and a provider whose endpoint lacks the required API surface is reported unavailable for the turn instead of attempted.
- `agent-subagents`: the delegation requirement pins the `agent` tool's native argument schema and the foreground/background result contract.
- `agent-tools`: adds multimodal file reads — `read_file` renders images and PDF pages as structured parts via the composed upstream backend, inside the jail.

## Impact

- **Code**: `internal/agents` (compose, run, catalog provider wiring, `subagent.go` deletion, `subagents/` consolidation, `backend/` composition), `internal/sessions` (checkpoint payload versioning), frontend chat tool mapping and tests, regenerated bindings.
- **Dependencies**: go.mod gains eino-ext `components/model/agentic{claude,gemini,openai}` and `adk/backend/local`; the agentic components target eino v0.9.x while the repo pins `v0.10.0-alpha.35` — a compile spike against the pinned tag is a first-week task (Go MVS keeps alpha.35; API drift is unverified).
- **Data**: checkpoint payloads get a version marker with dual-read migration of historical sessions; the event log and UI transcript vocabulary do not change.
- **Sequencing**: requires `adopt-eino-middlewares` committed first; its `agent-tools` RENAMED requirement (Todo tools → Task plan tools) must land before this change's `agent-tools` delta is applied.
