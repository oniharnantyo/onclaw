## Why

onclaw's current compaction is maximalist: it replays and summarizes the full conversation
verbatim — reasoning blocks, raw tool outputs, tool-call mechanics, and multimodal bytes — and
externalizes the overflow into a per-conversation transcript file written on every compaction.
Three independent references (Claude Code, NullClaw, GoClaw) converge on the opposite posture:
preserve actionable state (decisions, file paths, identifiers, skill use, unresolved bugs) and
discard transient mechanics (stale tool results, reasoning, raw media bytes). onclaw also sends
this growing prefix on every turn with no prefix caching enabled, paying the full input cost
repeatedly. The cost is avoidable; worse, replaying reasoning blocks is a latent provider
correctness risk (Anthropic rejects thinking blocks in input), and per-compaction transcript
writes churn storage on a 2 GB-RAM / 8 GB-storage target.

## What Changes

- **Replay scrubbing (Layer A):** strip `Reasoning`/thinking blocks from replayed history.
  Provider-correctness win (no replayed thinking) plus a token win; deterministic so the prefix
  stays cache-stable.
- **Summarizer-input scrubbing (Layer B):** via Eino's `GenModelInput`, clear old tool-result
  content (path-preserving stubs for file writes, skill-preserving stubs for skill calls), trim
  bulky file-write tool-call args, and strip reasoning/mechanics from what the summary model sees.
  Anchored to the compaction event so the live prefix never churns.
- **Tailored summary prompt (Layer B):** a recall-first `UserInstruction` that preserves
  decisions, file paths, skills, unresolved bugs, and identifiers verbatim, replacing Eino's
  generic default.
- **Prefix caching (Layer D):** enable per adapter where supported (Anthropic `CacheControl`,
  OpenAI automatic prefix caching, Gemini context caching).
- **Recently-accessed-files re-grounding (Layer C):** after compaction, surface the freshest N
  workspace files (list + on-demand read) alongside the summary; demote the transcript to a
  lazy-written fallback.
- **Probe-based evaluation (Layer E):** a recall-then-precision probe harness over real agent
  traces to tune the prompt and clearing policy, and to detect drift across re-compaction cycles.

## Impact

- **Specs:** `conversation-history` gains new requirements. The bounded-replay contract
  (summary + last 3 turns) and append-only retention contract are unchanged.
- **Code:** `internal/agent/middlewares/history_middleware.go` (reasoning-strip at load);
  `internal/agent/agent.go` and a new summarization-config builder (`GenModelInput`,
  `UserInstruction`); `internal/llm/adapter/*` (per-provider caching); a new file-recency
  side-channel; transcript lazy-write; a probe harness under `internal/agent/`.
- **Compatibility:** existing persisted rows are scrubbed at load — no migration required. Old
  reasoning blocks simply stop being replayed; old tool results stop being fed to summarizers.
  Storage reclaim of legacy rows is optional and out of scope.
- **Verified assumptions:** summarization operates via `BeforeModelRewriteState` on accumulated
  `state.Messages`, so load-time transforms reach the summarizer without `GenModelInput`
  contortions for Layer A. Prefix caching is reachable per-adapter (non-uniform); there is no
  generic Eino `model.Option` toggle.
