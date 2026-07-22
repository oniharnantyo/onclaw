## Context

**Current State:**
onclaw compacts long conversations via Eino's `summarization` middleware (`internal/agent/agent.go`),
configured with input-token anchoring (`inputTokenCounter`, `PromptTokens`-based), a 200-message /
80%-token trigger, `MaxRetries = 2`, and a `Callback` that persists the summary and exports a
transcript file. Bounded replay returns the active summary plus the last 3 turns past the
coverage cursor; the DB retains everything append-only.

The middleware is correct and resilient, but maximalist about content: it replays and summarizes
reasoning blocks, full tool-call arguments, and raw tool results verbatim, then externalizes
overflow into a transcript file written on every compaction. No prefix caching is enabled, so the
growing prefix is paid for in full on every turn.

**Problem:**
- Replaying `Reasoning`/thinking blocks is a provider-correctness risk (Anthropic rejects them in
  input) and a large token cost for reasoning-model-backed agents.
- Full tool results and tool-call args (`write_file` content, `edit_file` strings) dominate token
  count and are redundant with the file itself.
- The summarizer is fed the full verbatim history, so the one-time summary call is needlessly
  expensive and focuses the model on mechanics.
- No prefix caching means the verbatim prefix is re-billed every turn.
- The transcript is written on every compaction regardless of whether anyone reads it.
- There is no measurement of whether summaries actually preserve what matters (paths, decisions,
  the unresolved bug).

**Constraints:**
- Stay provider-agnostic in spirit; accept that caching and reasoning-handling are provider-specific.
- Keep append-only retention and the bounded-replay contract unchanged.
- No client-side tokenizer; keep the chars/4 estimator shape.
- Do not churn the live replay prefix (it must stay cache-stable).

## Goals / Non-Goals

**Goals:**
- Preserve actionable state across compaction; discard transient mechanics.
- Make the live prefix cache-stable so prefix caching pays off.
- Ground post-compaction recovery in fresh workspace files rather than stale conversation copies.
- Measure summarization quality so prompt/policy tuning is evidence-based.

**Non-Goals:**
- Change bounded replay (summary + last 3 turns) or append-only retention.
- Introduce a tokenizer or client-side token caching.
- Replace Eino's summarization with provider server-side compaction.
- Migrate or reclaim existing persisted rows (scrub-at-load handles them).

## Decisions

### Decision 1: Five-layer model, one transform per layer
Each concern is handled where it is cheapest and least disruptive:
- **Layer A — live replay:** reasoning-strip only (load-time, deterministic).
- **Layer B — summarizer input:** tool-result clearing, arg trimming, mechanics strip; the tailored prompt.
- **Layer C — recovery:** files-first re-grounding, `session_search`, lazy transcript, DB audit.
- **Layer D — cost substrate:** per-adapter prefix caching.
- **Layer E — measurement:** recall→precision probes.

### Decision 2: Tool-result clearing lives at Layer B (summarizer input), not Layer A
Clearing old results out of the *live* replay as they age would change earlier message content
each turn and defeat prefix caching. Instead, clearing is anchored to the compaction event: only
the summarizer sees cleared content, and only for the range being compacted. The live prefix stays
verbatim and cache-stable. This is the key refinement forced by the cache-stability requirement.

### Decision 3: Reasoning strip at Layer A (load-time), deterministic
Reasoning blocks are filtered out of replayed history in `HistoryMiddleware.BeforeAgent`. This is
provider-correctness (Anthropic rejects replayed thinking) plus a token win, and because it is a
deterministic removal applied once at load, it does not churn the prefix. Reasoning is also not
persisted into new turn rows; observability (Langfuse) captures it via the event/callback path.

### Decision 4: Stub taxonomy with durable-pointer preservation
When the summarizer's input is scrubbed, an old tool result is replaced by a stub, classified by
tool name + args already present in the `FunctionToolCall`:
- **File-producing** (`write_file`/`edit_file`, or a result saved to a file): `[write_file -> <path>]` — preserve the path verbatim.
- **Skill invocation**: `[skill used: <name> -> <outcome>]` — preserve skill identity + one-line outcome.
- **Other** (reads/search): `[cleared <tool>]` — call identity only.

Tool-call *args* are also trimmed for file-write tools (drop `content`/`new_string`, keep the path),
because `write_file` args are themselves the bulky body. The principle: keep the durable pointer,
drop the bulky copy.

### Decision 5: Tailored, recall-first `UserInstruction`
A custom `UserInstruction` replaces Eino's generic default (it overrides wholesale). It keeps the
proven elements (no-tools warning, `<analysis>`/`<summary>` structure, verbatim user-message block)
and adds onclaw specifics: how to read stubs, redaction handling (`<redacted>` preserved as-is),
and explicit verbatim preservation of decisions, file paths, skills, unresolved bugs, and
identifiers. Tuned via probes (Decision 8): recall maximized first, precision iterated after.

### Decision 6: Compaction-anchored horizon (cache-stable)
Clearing is tied to the summarization event and to `summary_until_seq`: the summarized range is
scrubbed for the summarizer; the post-summary tail and the pre-compaction prefix stay verbatim.
The verbatim prefix is what prefix caching amortizes. This was selected over a rolling turn-count
horizon (which churns the cache once per turn) and a token-budget horizon (which needs the
estimator in the hot path).

### Decision 7: Prefix caching per adapter (non-uniform, verified)
There is no generic Eino `model.Option`. Caching is wired per adapter: Anthropic `CacheControl`
(already referenced in `agentic_claude.go`); OpenAI automatic prefix caching (no opt-in, needs a
stable prefix — which Decision 6 guarantees); Gemini context-cache API. Where a provider offers no
caching, the verbatim prefix is still correct, just re-billed.

### Decision 8: Recently-accessed-files re-grounding (Layer C, list + on-demand)
After compaction, inject the freshest N workspace file paths alongside the summary; the agent
re-reads on demand via `read_file` rather than eagerly ingesting contents (suits the 2 GB-RAM
target). Recency is tracked by observing filesystem-tool calls into a per-conversation side-channel
(in-memory, spilled to KV). This replaces the transcript's role as the primary recovery path; the
transcript becomes a lazy fallback.

### Decision 9: Transcript written lazily
The transcript file is produced only when the agent actually requests deep recall (a read of the
transcript path), not on every compaction. This stops the per-compaction write/storage cost while
keeping the verbatim-recovery fallback available on demand.

### Decision 10: Probe-based evaluation harness
A harness runs the summarizer over captured real agent traces and scores recall (did the summary
keep the path / decision / unresolved bug / skill?) then precision (did it drop redundant tool
bodies?). Probes cover the six dimensions; the set is rotated to avoid blind spots. This is the
only way to know Decisions 4–6 are calibrated and to detect drift across re-compaction cycles.

**Alternatives Considered (cross-cutting):**
- *Clear at the replay boundary (Layer A).* Rejected: churns the prefix, defeats caching (Decision 2).
- *Dedicated cheaper summarization model.* Deferred: reuse the agent's `chatModel` for v1 (simple, provider-agnostic); revisit if compaction cost dominates.
- *Drop the transcript entirely.* Rejected for v1: keep it lazy as a fallback; dropping loses the direct verbatim path.
- *Eager file-contents re-grounding (Claude Code's 5 files).* Rejected for low-resource: list + on-demand read instead.

## Risks / Trade-offs

**Risk:** Clearing at Layer B only means the live replay still carries full tool results until caching lands.
- **Mitigation:** Phase ordering ships reasoning-strip + caching early; the verbatim prefix is what caching amortizes, so cost is bounded once caching is on.

**Risk:** Custom `UserInstruction` drops Eino's i18n/example scaffolding.
- **Mitigation:** The custom prompt preserves the load-bearing elements (no-tools, structure, user-message block); probe harness (Decision 10) validates quality; Eino's finalizer is content-agnostic (verified: it does not parse markers from the default prompt).

**Risk:** File-write arg trimming could drop a path-bearing field if a tool's schema differs.
- **Mitigation:** Trimming is keyed on known tool names/fields; unknown shapes keep the call verbatim (safe default).

**Risk:** Caching is provider-specific and may be absent for some adapters (e.g., Ollama local).
- **Mitigation:** Caching is additive; absence only means re-billing, not incorrectness.

**Risk:** Probe sets have blind spots (they pass while critical info is lost).
- **Mitigation:** Rotate probe sets; cover all six dimensions; track re-fetching frequency as a field signal.

**Trade-off:** The transcript becomes lazy, so first deep-recall request pays a one-time build cost.
- **Mitigation:** Build is from the append-only DB (`Transcript(upToSeq)`), already cheap; acceptable for a fallback path.

## Open Questions

- N for recently-accessed-files re-grounding (default 5, tunable).
- Whether to also trim `read_file`/search result content at Layer B beyond simple stubbing (probe-driven).
- Whether to add a dedicated summarization model once compaction cost is measured (Decision 8 follow-up).
