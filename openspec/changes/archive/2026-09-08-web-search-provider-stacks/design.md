## Context

`web.search` resolves a single provider from flat workspace config (`{provider, api_key, base_url}`): the catalog schema (`tool_catalog.go webSearchConfigSchema`), the per-field secret machinery in `toolsettings.go` (encrypt with workspace AAD, `field_hint` last-4, write-only merge), dynamic credential validation, and `searchProviderFor` in `tool_registry.go`. Providers register in `websearch_registry.go`; `duckduckgo` is the credential-free default, scraped from `html.duckduckgo.com` — blocked outright in the operator's environment, so the default path is dead weight. Search providers currently run on `http.DefaultClient` with no timeout. Decisions below were locked during exploration with the user (see conversation: failover trigger, window composition, timeout, migration, DDG removal).

## Goals / Non-Goals

**Goals:**
- Multiple named provider entries per workspace, same provider repeatable with different keys; order = priority.
- Bounded-latency failover: a 3-deep positional window, any-error advance, per-attempt timeout.
- Reorder-safe secret preservation via stable entry ids.
- Remove DuckDuckGo entirely; unconfigured = explicit, visible error.
- Migration of existing flat rows without re-encrypting secrets.

**Non-Goals:**
- Stateful failure memory (sticky demotion of failing head entries) — deferred.
- Distinct-provider dedupe inside the window — deferred; positional is the locked behavior.
- Round-robin / load-spreading across keys.
- Generalizing list-shaped config to other tools (Browser stays flat).
- Making the window size or stack cap user-configurable.

## Decisions

**D1 — Config shape: entries list with server-assigned stable ids.**
`config = { entries: [{id, name, provider, api_key?, base_url?}], request_timeout_seconds? }`. `id` is a short random id assigned by the server when an entry is first persisted. Rationale: the client cannot echo secrets back, and "empty key = keep stored" keyed by list index breaks on reorder; an id makes secret preservation identity-based. Alternative considered: match by `(provider, hint)` — fragile when two entries share a provider.

**D2 — Failover: positional 3-window, any-error advance, stateless.**
Window = `entries[0:min(3, len)]`, tried in order. Any attempt error (non-200, transport error, decode failure) advances; a 200 with zero results is a valid answer (no advance); all window entries failing returns the last error prefixed with the entry name ("web.search: Exa 1 returned status 429"). Rationale: strict priority matches the user's "first configured first"; stateless keeps the resolver pure and deterministic. Accepted trade-off: once the head key is rate-limited, each request pays one wasted 429 until the user reorders or the quota resets — sticky failure memory is a deliberate later addition.

**D3 — Chain provider behind the existing seam.**
A `chainProvider` implements `SearchProvider` and wraps the constructed entries; the per-provider clients and `websearch_registry.go` table are unchanged apart from the DDG removal. `searchProviderFor` builds the chain from resolved tool config. Construction-time validation errors (unknown provider, missing credential) fail the build as today.

**D4 — DuckDuckGo removed, unconfigured fails fast.**
Delete `duckduckgoProvider`, its HTML extraction helpers, `SearchCredentialNone`, `WithUserAgent`, and the legacy `NewSearchProvider` constructor; drop the registry entry. The `x/net/html` dependency goes if nothing else imports it (verify `webfetch` at implementation time). Empty entries + no env provider → construction error "web.search is not configured — add a provider in Settings → Tools", no network attempt. Rationale: the scraping default was blocked in practice; a silent dead default is worse than an actionable error. AGENTS.md's "built-ins ship zero external credentials" wording is updated as part of the change.

**D5 — Per-entry secrets.**
Each entry's credential field encrypts with the same workspace-AAD derivation as today; the last-4 hint lives inside the entry (`api_key_hint` nested in the entry object, stripped from API reads exactly as now). Upsert semantics per entry: known id + empty credential = keep stored; new entry (no id) with a key-requiring provider and empty credential = 422. Hint deletion/masking helpers become entry-aware.

**D6 — Env fallback: empty-list only, never a silent default.**
`ONCLAW_SEARCH_PROVIDER` / `ONCLAW_TAVILY_API_KEY` seed a single fallback entry (labeled "Instance default (env)") only when `entries` is absent or empty. Env naming a key-requiring provider with no key available is a construction error. `ONCLAW_SEARCH_PROVIDER=duckduckgo` is treated as unset.

**D7 — Per-attempt timeout.**
Flat tool-level field `request_timeout_seconds` (positive integer, default 10, max 60), applied to each chain attempt via a dedicated `http.Client` per attempt — this also closes the existing no-HTTP-timeout gap. Worst-case request latency ≈ 3 × timeout.

**D8 — Migration 000023.**
One up/down pair. Up rewrites each workspace tool settings row for `web.search`: flat `{provider, api_key, base_url, api_key_hint}` → `{entries: [entry], request_timeout_seconds absent}` where `entry = {id: <generated>, name: "<ProviderLabel> 1", provider, api_key, base_url, api_key_hint}`; envelopes are opaque strings (AAD is the workspace id, unchanged), so no re-encryption — pure JSONB re-nesting. Rows with `provider=duckduckgo` (or no provider) become `{entries: []}` — explicitly unconfigured. Down restores entry 0 to the flat shape best-effort; ddg-era rows stay empty (documented as not fully reversible). The migration runs before the new code reads the shape anywhere permanent; deploy order is migrate-then-restart as usual.

**D9 — Validation and gating.**
Enable requires ≥1 fully valid entry; every entry requires non-empty name (unique, case-insensitive), known provider, credential per kind. Sanity cap: 20 entries (422 above). Enabled toggle stays default-on as workspace intent; `Configured` = ≥1 valid entry; a divergent enabled-but-unconfigured state errors clearly at runtime per D4.

**D10 — Config dialog: stack list editor.**
Replaces the flat three-field form for `web.search` only. Per the settings spec delta: rows of [↑][↓] (bounds-disabled) · name input · provider select · per-row credential (write-only, hint placeholder) · ✕ remove; Add control; flat timeout field. First three rows labeled "in rotation", rest dimmed "standby", labels re-evaluated live on reorder. Remove is undo-toast, nothing persists until Save; Save submits the whole ordered list with ids and renders per-row validation inline. Mockups:

```
╔══════════════════════════════════════════════════════════════════════╗
║  Web Search                                                       ✕  ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                      ║
║  Enabled  [✓]                                                        ║
║                                                                      ║
║  Provider stack                                             [+ Add]  ║
║  Requests try the first three in order — first success wins.         ║
║  Lower entries stand by until promoted into the top three.           ║
║                                                                      ║
║  ┌────────────────────────────────────────────────────────────────┐  ║
║  │ [↑][↓] ① [ Tavily 1         ] [ Tavily      ▾ ] in rotation ✕  │  ║
║  │          └─ API key  [ •••••••• ab12                       ]   │  ║
║  └────────────────────────────────────────────────────────────────┘  ║
║  ┌────────────────────────────────────────────────────────────────┐  ║
║  │ [↑][↓] ② [ Tavily 2         ] [ Tavily      ▾ ] in rotation ✕  │  ║
║  │          └─ API key  [ •••••••• 9f04                       ]   │  ║
║  └────────────────────────────────────────────────────────────────┘  ║
║  ┌────────────────────────────────────────────────────────────────┐  ║
║  │ [↑][↓] ③ [ Exa 1            ] [ Exa         ▾ ] in rotation ✕  │  ║
║  │          └─ API key  [ •••••••• c7e1                       ]   │  ║
║  └────────────────────────────────────────────────────────────────┘  ║
║  ┌────────────────────────────────────────────────────────────────┐  ║
║  │ [↑][↓] ④ [ Brave 1          ] [ Brave       ▾ ]  (standby) ✕  │  ║
║  │          └─ API key  [ •••••••• 41d0                       ]   │  ║
║  └────────────────────────────────────────────────────────────────┘  ║
║                                                                      ║
║  Request timeout (per attempt)  [ 10 ] s                             ║
║  Bounds each attempt — worst case ≈ 3 × timeout.                     ║
║                                                                      ║
║                                                [ Cancel ]  [ Save ]  ║
╚══════════════════════════════════════════════════════════════════════╝
```

SearXNG row variant (per-row `show_if`): `└─ Base URL [ http://searxng:8080 ]`. New-row validation variant: inline `⚠ API key is required for exa` under the empty key field. Empty state: "No providers configured — web.search will error until you add one."

## Risks / Trade-offs

- [Same-provider pileup: window full of one provider burns all attempts on a provider-wide outage] → positional is locked; user controls the mix by ordering; provider-dedupe deferred.
- [Wasted call per request while the head key is quota-exhausted] → accepted for stateless first cut; sticky failure memory is the designated follow-up.
- [Breaking config shape for API consumers] → migration handles stored rows; the settings API payload change is called out in the proposal; no other in-repo consumer writes flat web.search config.
- [Migration corrupting envelopes] → re-nesting is string moves only (no crypto); 000023 down restores entry 0; smoke + integration tests cover a migrated row end-to-end.
- [Dead dependency after DDG removal] → `golang.org/x/net/html` dropped if `webfetch` doesn't use it; verified in tasks.
- [Duplicate-name check is cosmetic (names never resolve anything)] → kept anyway for error-message sanity; cheap.

## Migration Plan

1. Ship 000023 up + new code in one wave (migrate-then-restart).
2. Post-deploy, every migrated workspace has ≥0 entries; ddg-era workspaces show web.search unconfigured and enabling it requires adding an entry — intended.
3. Rollback: `migrate down` restores flat shape for non-ddg rows; ddg rows remain empty (acceptable — the old default was the broken behavior being removed).
