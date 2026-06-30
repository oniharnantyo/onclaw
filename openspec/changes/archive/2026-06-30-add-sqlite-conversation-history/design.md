## Context

Today onclaw records history as a write-only append-only `.jsonl` transcript (`internal/agent/transcript.go`, written from seven scattered call sites in `internal/agent/runner.go`) and submits only the new user message to the agent each turn (`runner.go:37-41`). The transcript is never read back, so `onclaw chat` is effectively per-turn stateless. Summarization is wired (`agent.go`) but compacts only the agent's in-memory `state.Messages` — it is not durable, so on the next turn the full raw history would have to be re-sent and re-compacted.

Constraints: low-resource SBC target (~2 GB RAM, 8 GB storage); pure-Go SQLite (`modernc.org/sqlite`, already linked and opened for profiles/secrets/kv); `CGO_ENABLED=0`; eino pinned at `v0.9.9`, whose ADK middleware hooks (`BeforeAgent`, `BeforeModelRewriteState`, `AfterModelRewriteState`, `AfterAgent` in `adk/handler.go`) have been verified present.

## Goals / Non-Goals

**Goals:**
- Persist full conversation messages (the entire `*schema.Message`) to SQLite as immutable rows.
- Replay history into the agent before each run (multi-turn memory) via a history middleware.
- Make summarization compaction durable (persisted summary + coverage cursor) so replay stays bounded.
- Save each new message eagerly as produced; remove the scattered `AppendToTranscript` writes.
- Add no new dependencies; follow the existing store contract/types/impl split.

**Non-Goals:**
- Vector search / embeddings (future; schema accommodates a later `*_embeddings` table keyed to message rows).
- Auto-import of existing `.jsonl` transcripts.
- A user-facing `onclaw history` command (future).
- Encryption-at-rest for history (redaction only, matching today's transcript behavior).
- Concurrent agent runs (onclaw is `concurrency: 1`; the middleware carries per-run state via context).

## Decisions

**1. Store the full `*schema.Message` as a JSON column with a `role` sidecar.**
The store holds `message TEXT` (JSON) + `role TEXT` (indexed sidecar); the agent layer marshals/unmarshals `*schema.Message` and redacts. This keeps `internal/store` eino-free (matching the convention where `Profile.Settings`/`Agent.Tools` are opaque JSON strings), preserves the full message (content, tool calls, response metadata, and the `Extra` map that carries eino's summarization/`_onclaw_persisted` tags), and keeps `content` directly queryable for future embeddings.
*Alternative considered:* flattened columns mirroring today's `TranscriptEntry` — rejected because it loses fidelity and cannot be replayed into the LLM.

**2. Saving and replay are a history middleware, not runner logic.**
A new `HistoryMiddleware` (`internal/agent/history.go`, embedding `adk.TypedBaseChatModelAgentMiddleware`) handles reinject in `BeforeAgent` and eager save in `AfterModelRewriteState` (+ final flush in `AfterAgent`). This consolidates the seven scattered `AppendToTranscript` sites into one cohesive component and is the idiomatic eino extension point (same pattern as `automemory`).
*Alternative considered:* keep saving in the runner's event loop — rejected per the explicit direction to de-scatter and use a middleware.

**3. Persistence tracking uses a per-message marker, not a positional cursor.**
Each saved message is tagged in its `Extra` map (`_onclaw_persisted=true`); the save hooks scan for unmarked messages and persist them. This is robust to summarization rewriting/shrinking `state.Messages` mid-run, which would break a positional index.
*Alternative considered:* a positional save cursor — rejected because compaction invalidates indices.

**4. Durable compaction via the summarization `Config.Callback`.**
The existing summarization middleware gains a `Callback` that fires after each compaction with before/after state; it persists the summary row and advances `conversations.summary_until_seq` to the highest `seq` covered. Replay (`LoadHistory`) then returns `[summary] + messages with seq > cursor`, excluding the compacted originals.
*Alternative considered:* persist no summary and replay all history every turn — rejected as unbounded (re-compaction every turn, prompt grows without limit).

**5. Per-message eager saving (most durable).**
The user message is saved in `BeforeAgent` before any compaction; each assistant/tool message is saved at the `AfterModelRewriteState` that follows its production. A crash loses at most the message currently being produced.
*Alternative considered:* batch the whole turn at end — rejected because it loses the entire turn (including the prompt) on a mid-turn crash.

**6. SQLite over files for raw history.**
SQLite is already linked and open; adding a `messages` table is ~zero marginal cost (binary weight, RAM, dependencies) and gives transactions, indexed queries, and — critically — a `message_id` join for future embeddings, which files cannot provide cheaply.

## Risks / Trade-offs

- **Compaction ↔ save interaction** → the marker-per-message approach is chosen for robustness. Residual risk: if summarization rebuilds preserved messages as new objects without our `Extra` marker, an already-saved message could be re-saved (duplicate row). *Mitigation:* verify summarization preserves message object identity/`Extra` during implementation; if not, dedup at `LoadHistory` by content hash. (Tracked as an open question.)
- **Replay memory cost** → each turn loads the working set into memory. Bounded to `[summary + tail]`, never the full raw session, honoring the existing "full session SHALL NOT be held in memory" requirement.
- **Behavioral changes** → `interrupted`/`error` transcript markers are dropped (control flow, not messages); `.jsonl` files are no longer written. Documented in the `agent-core` delta.
- **First turn after a large resume** may trip compaction immediately on the first model call. Acceptable: it compacts once, persists the summary, advances the cursor.

## Migration Plan

- Schema migration is idempotent and additive: `CREATE TABLE IF NOT EXISTS` for `conversations` and `conversation_messages`, plus guarded `ALTER` columns on `conversations` via the existing `columnExists` helper. No data transformation of existing tables.
- Existing `conversations/<agent>_transcript.jsonl` files are left on disk but no longer read or written; they are not auto-imported.
- Rollback: revert the code; the new tables are harmless if unused, and old `.jsonl` files remain intact on disk.

## Open Questions

- Does eino's summarization preserve message object identity/`Extra` when it retains "preserved" messages during compaction? Determines whether the marker approach can produce duplicate rows (decision 3 risk). Resolve during implementation via a focused unit test before wiring the callback.
- Should interruption/error events be persisted for audit in a later iteration? Deferred; not in scope.