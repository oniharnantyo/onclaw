# Design: Refactor conversation_messages to turn-per-row

## Decision: Reading A (turn delta), not B (full snapshot)
Each turn row stores only the messages new in that turn. Replay concatenates turn arrays in `sequence_num` order. Storage is O(n); the O(n²) full-snapshot reading was rejected for the ~8 GB SBC target (~10 MB for one 100-turn conversation before embeddings/FTS/KG). Replay is byte-for-byte identical to today's `ORDER BY seq` reconstruction.

## Decision: buffered turn commit, not eager per-message
Today the middleware saves each message eagerly as it appears. Turn-per-row requires buffering the turn and committing one row at `AfterAgent` (the existing turn-end hook). Tradeoff accepted: a turn that crashes before `AfterAgent` leaves no row — consistent with "turn = a complete exchange ending in a response". The `_onclaw_persisted` `Extra` flag still dedups within a run.

## Decision: live send = reconstruct + send, bounded by compaction (Path A)
History reaches the model by concatenating turn arrays and prepending to `AgentInput.Messages`. The send policy is **all turns until the context threshold, then summary + last 3 turns** — reusing the existing `summarization` middleware (trigger at 80% of context window), `handleSummarization`, and `SaveSummary`. The only new logic is turn granularity and the `tailTurnWindow = 3` tail. The 3-turn window is a *loading* policy, decoupled from the *trigger* (token threshold), so they tune independently.

`response_id`/`previous_response_id` are stored and surfaced but **not** used in this live path — they exist for audit and for a future stateful-provider optimization (Path B: send `previous_response_id` + delta instead of reconstructed history).

## Decision: clean rebuild migration
Pre-release; the migration detects the old shape (`role` column present) and drops + recreates the table and FTS. No row-level backfill (old rows have no `response_id` or token counts). Guarded so it runs once.

## Decision: token usage + response_id populate lazily
Both come from the final assistant message's response metadata. The only registered adapter is the stub, which doesn't populate them — so columns are stored empty/zero until a real adapter is wired. Schema and plumbing are forward-compatible.