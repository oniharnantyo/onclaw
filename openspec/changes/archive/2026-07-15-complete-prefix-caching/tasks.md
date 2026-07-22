# Tasks

## Gemini runtime caching

- [x] 1.1 Create the cached-content resource from the stable prefix (system + tools + history) via
  the Gemini `CreatePrefixCache` API at first request of a conversation/turn boundary; reference the
  returned `cachedContent` name on subsequent requests.
- [x] 1.2 Manage cache lifecycle: key the cache by a stable-prefix hash so it is reused across turns;
  honor `CacheExpiration` TTL; evict/recreate when the prefix changes (post-compaction).
- [x] 1.3 Degrade gracefully when the provider/key lacks caching support or the call fails (re-bill,
  no error). Keep the `prompt_caching: false` Settings flag to disable entirely.
- [x] 1.4 Tests: a fake/in-memory Gemini transport asserting the cached-content reference is included
  on turn 2+ and that disabling the flag omits it.

## Anthropic cache boundary

- [x] 2.1 Verify against the eino `agenticclaude` adapter whether request-level `CacheControl`
  places the breakpoint at the stable system+tools+history prefix or only the final message block.
- [x] 2.2 If the boundary is only the final block, switch to per-content-block cache breakpoints on
  the system prompt and tool definitions (eino exposes `SetToolInfoCacheControl` /
  `SetContentBlockCacheControl`); confirm the stable prefix is explicitly pinned.
- [x] 2.3 Tests asserting the cache breakpoint location across two turns (prefix portion identical).

## Cross-cutting

- [x] 3.1 On completion, archive this change so the main `conversation-history` spec reflects Gemini
  caching as wired (not degraded).
