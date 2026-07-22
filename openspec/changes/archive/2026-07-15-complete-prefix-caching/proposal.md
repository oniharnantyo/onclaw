# Change: Complete Prefix Caching

## Why

The `add-compaction-scrubbing-and-recovery` change shipped per-adapter prefix caching
(Layer D). Review found two gaps that were deferred rather than completed:

1. **Gemini is not actually cached.** `internal/llm/adapter/agentic_gemini.go` sets only the
   cache *expiration policy* (`config.CacheExpiration`). Gemini requires a named cached-content
   resource created from the stable prefix via `CreatePrefixCache`, referenced on each request.
   Without that runtime wiring, Gemini re-bills the prefix every turn — caching is effectively OFF
   and degrades gracefully. Task 1.4 was over-marked as done.
2. **Anthropic cache boundary is coarse.** eino's `agenticclaude` adapter applies
   `config.CacheControl` at the request level (`req.CacheControl`), which the Anthropic SDK
   (`anthropic-sdk-go@v1.42.0`, `MessageNewParams.CacheControl`) serializes as a top-level
   `cache_control` placed on the **last block of the last message**. The stable system + tools +
   history prefix is inside the cached region, but no explicit breakpoint pins it, so the boundary
   is coarser than the spec's "marked cacheable at the prefix" intent.

Both are functionally safe (graceful degradation holds); this change closes the gaps so the
caching benefit the spec promises is actually realized.

## What Changes

- Wire Gemini cached-content creation/reference at request time (`CreatePrefixCache` + cached-content
  reference), with TTL/lifecycle management and a stable-prefix hash key.
- Evaluate and, if needed, switch the Anthropic adapter to per-content-block cache breakpoints
  (system prompt + tool definitions) so the stable prefix is explicitly pinned.
- Add regression tests for both.

## Impact

- `internal/llm/adapter/agentic_gemini.go`, `internal/llm/adapter/agentic_claude.go`, adapter tests.
- No change to the compaction/scrubbing logic, storage, or wire protocol.
