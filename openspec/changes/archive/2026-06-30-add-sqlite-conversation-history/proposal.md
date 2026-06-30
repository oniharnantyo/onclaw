## Why

onclaw records conversation history as a write-only append-only `.jsonl` transcript (`internal/agent/transcript.go`, written from `runner.go`) and submits only the new user message to the agent each turn (`runner.go:37-41`). The transcript is never read back, so the agent has no multi-turn memory, and the file store blocks two roadmap items: durable (non-destructive) summarization and future vector search. We need history that is persisted to the SQLite DB onclaw already opens, replayed into the agent each turn, and kept bounded as conversations grow.

## What Changes

- Move conversation history from per-session `.jsonl` files into SQLite (`conversations` + `conversation_messages` tables), storing the **full `*schema.Message`** per turn as immutable rows. **BREAKING**: the `conversations/<agent>_transcript.jsonl` files are superseded; existing transcripts are not auto-imported.
- Add a **`HistoryMiddleware`** (eino ADK `ChatModelAgentMiddleware`) that (a) reinjects persisted history before each run and (b) saves each new message eagerly as it is produced — replacing the scattered `AppendToTranscript` call sites in `runner.go`.
- Make summarization compaction **durable**: persist the summary and a coverage cursor so compacted messages are represented by the summary on replay, keeping prompts bounded across turns instead of re-injecting the full raw history every turn.
- **Remove** `internal/agent/transcript.go` and all `AppendToTranscript` calls; the runner becomes streaming-only (stdout + `EinoAgent.Run`).
- Drop the `interrupted`/`error` transcript event markers (control-flow signals, not messages); real messages (user/assistant/tool) remain fully captured. Termination conditions surface via existing `slog` logging + returned errors.
- Preserve secret redaction (redact message content/args/results before persisting).

## Capabilities

### New Capabilities

- `conversation-history`: SQLite-backed persistence of full conversation messages, reloadable for replay, with durable summarization compaction (persisted summary + coverage cursor) that keeps replay bounded. Covers the `ConversationStore` contract, schema, message fidelity, redaction, and the per-turn reinject/save behavior.

### Modified Capabilities

- `agent-core`: the transcript requirement moves from per-session `.jsonl` to the SQLite `conversation-history` store; history is reinjected before each run so the agent has multi-turn memory; summarization compaction becomes durable across runs (summary persisted + cursor advanced). The runner no longer writes the transcript — a middleware does. The existing "full session SHALL NOT be held in memory" requirement is honored by replaying only the bounded working set (latest summary + tail), never the full raw session.

## Impact

- **Code:** `internal/store/{types.go, store.go}`, `internal/store/sqlite/{db.go, conversation.go (NEW)}`, `internal/agent/{history.go (NEW), agent.go, runner.go}`, `internal/agent/transcript.go` (DELETED), `internal/cli/{context.go, run.go, chat.go}`.
- **Schema migration:** two new tables + guarded columns on `conversations`; added to the existing idempotent `Migrate()`.
- **Dependencies:** none new — `modernc.org/sqlite` is already linked and opened; eino ADK middleware hooks (`BeforeAgent`/`AfterModelRewriteState`/`AfterAgent`) are present in the pinned `eino@v0.9.9`.
- **Behavior:** `onclaw chat` gains multi-turn memory; `.jsonl` transcript files are no longer written; `AssembleAgent`/`RunAgent` signatures gain a `ConversationStore` + conversation ID.