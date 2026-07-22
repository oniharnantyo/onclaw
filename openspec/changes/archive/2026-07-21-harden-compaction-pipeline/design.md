# Design — harden compaction pipeline

## Systemic framing

Not four independent bugs — a subsystem that drifted from its spec, with holes
where the spec is silent.

| Bug | Spec status |
|-----|-------------|
| 1 (crash) | **Gap** — `conversation-history` describes *what* to replay, never the summary message's role/block-type validity |
| 2 (extraction when disabled) | **Violation** — `agent-memory` explicitly forbids archival when extraction is disabled |
| 3 (no meter/indicator) | **Gap** — only post-hoc `compaction_count` is specified |
| 4 (answer wrong) | **Gap** — `answer` defined as "last assistant block text"; undefined for summary rows |

**Layering root cause.** `handleSummarization` is a cross-layer orchestrator
pretending to be a callback: it injects recency (recency layer's job), runs
extraction (memory layer's job — bypassing `ExtractionEnabled`), and persists
(store layer's job — undefined shape). Bugs 1, 2, 4 are the seams it crossed.

## The summary message lifecycle

```
 Eino summarization → role:user + user_input_text(summary)
   → handleSummarization appends recency as assistant_gen_text   ← crash source
   → SaveSummary reads assistant_gen_text only                   ← answer wrong
   → replay injects → eino converter rejects assistant_gen_text  ← NodeRunError
```

## Altitude: patch now, redesign next

Two cross-layer side-effects have a higher-altitude fix than the patch. Both are
**deferred** (the patches ship in this change); both follow the same principle —
move the side-effect back to its owning layer.

**Recency.** `RecentFilesNote` has one producer (`agent.go:612`) and zero
code-readers; only the model at replay consumes it. Persisting it into the
durable summary is pure overhead-plus-harm (crashes the converter, pollutes
`answer`, freezes a transient "recently accessed" snapshot). Pressure-tested
safe to move to a fresh per-turn injection — the `RecencyTracker` already spills
to KV and reloads at agent start, and curated memory is already re-injected as
fresh system context each turn (an established pattern). Dissolves Cluster 4 and
fixes staleness. Pinned decisions: inject only when a summary exists (pre-
compaction the full context is in-window); use a role-valid block; accept losing
the frozen audit record (the note becomes always-current).

**Extraction.** Route compaction-time extraction through
`MemoryMiddleware.ExtractCompacted` (owns `ExtractionEnabled`) instead of the
callback calling `ExtractAndFlush` directly. The flag check moves to its owner
and `handleSummarizationParams` drops three fields (`ChatModel`/`MemoryStore`/
`Embedder`, used only for extraction). Constraint: extraction must stay at
compaction time (`agent-memory` requires it before the summary is persisted), so
this is a new compaction-time method, not a deferral to turn-end `FlushMessages`.

## Capstone: the SummaryMessage contract

The layering redesigns fix the *causes*; the contract fixes the *symptom class*
— the data structure drifting invalid — so the next contributor cannot
reintroduce a broken summary regardless of which layer touches it.

**Invariant:** a summary message is a single `role: user` message containing only
`user_input_text` blocks.

**Enforcement:** a `validateSummaryMessage` at two boundaries — **persist**
(reject) and **replay** (sanitize, stripping role-invalid blocks). The replay
sanitizer retroactively neutralizes legacy bad rows (e.g. the live DB's row 27)
on every load, so the "no backfill" decision costs nothing in safety. Defer the
heavy `SummaryMessage` wrapper type unless a second mutation point reappears;
after the recency patch the summary is single-block, so there is little to wrap.

## Cluster 3: EventSink (progressive meter + live indicator)

Middleware callbacks run **synchronously** on the handler goroutine inside
`it.Next()`, so an `EventSink` writes SSE inline — no iterator-protocol change,
no concurrent channel draining. `handleSummarization` emits `compaction`
start/end; `HistoryMiddleware.AfterModelRewriteState` emits per-step `usage`.
The handler wires the sink to the `SSEWriter` after assembly, before `Run`.

**Risk to verify:** confirm `ResponseMeta.TokenUsage` is populated at
`AfterModelRewriteState`. Fallback: the model `OnEnd` callback or extraction
from the streamed final chunk.