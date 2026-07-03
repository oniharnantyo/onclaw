## Context

onclaw's agent has workspace file tools (`read_file`/`write_file`/`list_dir`), a policy-gated
`shell`, and a swappable `browser` category, but no targeted file editing and no web access.
GoClaw (`sausheong/goclaw`) ships Go implementations of `edit_file`, `web_fetch`, and
`web_search` under the same single-binary/low-RAM ethos. This change ports them, making the two
web tools **extensible and swappable** across multiple backends.

The decisive prior art is **onclaw's own `browser` category** (`internal/browser/` +
`internal/agent/tools/browser/`): a capability package with a `Factory` registry, config-driven
engine selection, and a default fallback. The web tools are designed as its structural twin so the
codebase has one proven pattern for "swappable category," not two.

Constraints: pure-Go single binary (`CGO_ENABLED=0`), ~2 GB RAM SBC target, security-first
(pathguard, redaction, DEK/KEK secrets), tools-management spec already governs category
config/toggle/seeding generically.

## Goals / Non-Goals

**Goals:**
- Add `edit_file` (targeted, unique-match, workspace-confined).
- Add `web_search` + `web_fetch` with multiple swappable backends each, selectable via the Web
  category config.
- Always-available defaults (`duckduckgo` / `http`) so the agent always gets a result.
- SSRF egress protection for fetch.
- Reuse the existing secrets mechanism for provider API keys; never store keys as plaintext.
- Match the `browser` architecture so the swappable-category pattern stays uniform.

**Non-Goals:**
- A `cron`, messaging, or `ask_agent` tool (separate future changes).
- A shell-style network egress *policy* (`allow`/`deny`/`ask`) — SSRF guard is the v1 security
  measure.
- JS-rendering fetch or `html-to-markdown` as a dependency (kept stdlib-only; pluggable later).
- A full provider *chain* (ordered fallback list) — v1 is single configured provider + one
  hardcoded default fallback.
- Changing the tools-management, browser-tool, or providers specs.

## Decisions

**D1 — Mirror the browser capability/tool split.** New `internal/web/` holds interfaces, factory
registry, Config DTO, and `ssrf.go`; provider impls live in `internal/web/{ddg,http,tavily,exa,
google,lightpanda}/`; thin tool wrappers live in `internal/agent/tools/web/`. `edit_file` stays
flat in `internal/agent/tools/` (no backend ⇒ no capability package), like `read_file`.
*Why over flat files in `internal/agent/tools/`:* the coding-style rule mandates separating
contract/types/impl, and the browser code already follows this split. Flat files would co-locate
interfaces with implementations and reinvent the factory idiom.
*Alternative rejected:* everything flat in `internal/agent/tools/` — violates the rule and diverges
from the established pattern.

**D2 — No `Manager` (web is stateless).** Browser needs a `Manager` because its engine is a
long-lived process with pages/cookies/contexts. Web fetch/search are one-shot calls with no
process or session to own, so the lifecycle layer is dropped entirely. The tool resolves config +
factory inline per call. *Alternative rejected:* a `web.Manager` mirroring browser's — needless
state/lifecycle for stateless operations.

**D3 — Two interfaces, not one.** `Searcher` (query→results) and `Fetcher` (url→content) have
different shapes; forcing a single `Backend` interface would be awkward. They share one package,
one Web config, and one SSRF guard, but separate factory maps (`RegisterSearcher` /
`RegisterFetcher`) and independent selection (`search_provider`, `fetch_provider`).

**D4 — Reuse the factory-registry idiom, lighter signature.** Like `browser.Register(name,
Factory)`, but web factories return a configured provider with no cleanup func:
`SearcherFactory func(cfg Config) (Searcher, error)`. An error from the factory (e.g. missing API
key, missing binary) is the signal that triggers fallback. *Alternative rejected:* registering
stateless instances directly — but providers need config (keys, binPath, timeouts) at
construction, so a factory taking `cfg` is the right seam.

**D5 — Read config at runtime via `scope.ToolGroupCfg`.** Each tool's execute closure calls
`scope.ToolGroupCfg.GetConfig(ctx, "Web")` per invocation, like `browser.Manager.Start` reads
`GetConfig(ctx, "Browser")`. This honors hot-reload without restart (a tools-management
requirement). *Alternative rejected:* stashing config in a package var via `RegisterConfig`'s load
callback (the `browser/register.go` `lastCfg` approach) — that pattern looks vestigial; the mature
path reads `ToolGroupCfg` directly. (`RegisterConfig` is still used to register the schema +
persist/echo config, just not for provider selection.)

**D6 — Fallback lives in the tool layer, not the capability.** Browser errors hard on a bad
engine (`unsupported browser engine`). To keep `internal/web/` behaviorally consistent with
`internal/browser/`, the factory returns an honest error when unavailable, and the "always-succeed"
fallback policy (try configured provider → on error try default → prepend notice) is implemented
in `internal/agent/tools/web/`. This isolates the policy and keeps the capability pure.
*Alternative rejected:* fallback inside the capability — would make `internal/web/` behave
differently from `internal/browser/` and hide misconfiguration.

**D7 — Shared, parameterized `SecretResolver`, threaded via `Scope`.** Provider API keys
(Tavily/Exa/Google) resolve env > SecretStore+KeyManager — the same env>DB>error *order* as
`llm.Service.ResolveSecret` (`service.go:210-228`), but a **different namespace**: web wants env
`ONCLAW_WEB_<PROVIDER>_API_KEY` and SecretStore key `web.<provider>`, whereas `ResolveSecret`
hardcodes the `ONCLAW_PROVIDER_<NAME>` prefix (`service.go:214`) and reads provider-profile
secrets. So the resolver cannot be a direct lift of `ResolveSecret` — it takes the env-var name
and secret key explicitly: `SecretResolver { Resolve(ctx, envVar, secretKey) (string, error) }`,
living in `internal/secrets/`. Both the LLM layer (passing `ONCLAW_PROVIDER_<NAME>` + its profile
key) and web (passing `ONCLAW_WEB_<PROVIDER>_API_KEY` + `web.<provider>`) depend on it, and it is
threaded through `tools.Scope` (which already carries `ToolGroupCfg`/`KVStore`). Key naming:
SecretStore key `web.<provider>` (e.g. `web.tavily`), env `ONCLAW_WEB_<PROVIDER>_API_KEY`.
*Why over env-only:* the LLM layer already persists keys encrypted; env-only would diverge and
force operators to manage keys twice. *Why over coupling web→llm:* a shared interface avoids a
web→llm dependency and keeps the secret-resolution contract in one place.
*Crucial:* non-secret knobs (timeouts, `google_cx`, `lightpanda_bin_path`, user agent) stay in
`tool_group_config`; only secrets go through the resolver. Plaintext config never holds a key.

**D8 — Shared SSRF guard in `internal/web/ssrf.go`.** `ValidateURLNotInternal` (ported from
GoClaw) blocks private/link-local IPs, cloud-metadata hosts, and re-validates redirect targets.
Used by the `http` and `lightpanda` fetch providers. Search providers call fixed external hosts,
so they need no per-call SSRF check. This is the web analogue of `pathguard`.

**D9 — Lightpanda fetch is a one-shot CLI exec, distinct from browser CDP.** The browser category
drives a *running* lightpanda over CDP (:9222); the fetch provider shells out
`lightpanda fetch --dump markdown <url>` via `exec.CommandContext` with the URL as a discrete argv
element (**no `sh -c`** ⇒ no injection), SSRF-validating first. It reuses the same binary
(`lightpanda_bin_path`, default `lightpanda`) but is an independent invocation mode.

**D10 — Stdlib-only defaults; no new dependencies.** GoClaw's `web_fetch` uses
`html-to-markdown`; we instead reuse GoClaw's `web_search` tag-stripping approach (stdlib
`strings`) for the `http` provider's HTML→text step. All defaults and the SSRF guard are
`net/http`/`net/url`/`strings`/`os`/`os.exec` only, preserving `CGO_ENABLED=0` and binary size.

**D11 — Web category needs a structured UI form (the "no UI changes" assumption no longer
holds).** When this change was drafted, a new configurable category was assumed to render for free
via generic UI wiring. Since then (commit `9650f74`) the Browser category config is hand-rendered
as **structured fields** in `web/src/components/Tools.tsx`, and **every other configurable category
falls into a generic raw-JSON `<textarea>`** (`Tools.tsx:453-483`). The repo's `CLAUDE.md` now
codifies "structured fields only, never a JSON textarea" as a hard rule and names the Browser config
+ MCP `env` as offenders being fixed (Browser done in `9650f74`, MCP `env` in `03a2c56`). Therefore
the Web category **must** ship its own structured form — `search_provider`/`fetch_provider` selects
plus the non-secret tunables (timeout, max bytes, user agent, `google_cx`, `lightpanda_bin_path`) —
mirroring the Browser branch, or it would render as the forbidden JSON blob. The API needs no
change: it already serves any category's schema generically via `tools.GetConfigEntry`
(`internal/api/service/tools.go`); only `Tools.tsx` gains a Web branch.
*Alternative rejected:* rely on the generic JSON textarea — violates the `CLAUDE.md` rule and
regresses the structured-config cleanup the web layer just completed.

## Risks / Trade-offs

- **[DDG HTML parsing is fragile]** DDG can change its markup → search breaks. *Mitigation:*
  fixture-based parser tests, and the default provider is only one of several; users can switch
  to Tavily/Exa/Google via config.
- **[Web tools are network-bound on an SBC that may be offline]** If there is no connectivity,
  *all* providers fail; fallback only rescues a failed *provider*, not no network. *Mitigation:*
  tools return clear errors and degrade gracefully; not a regression (onclaw had no web tools
  before).
- **[SecretResolver extraction touches the LLM layer]** Promoting `ResolveSecret` into an
  interface is a small refactor of working code. *Mitigation:* behavior-preserving; covered by
  existing provider tests.
- **[Lightpanda binary is an external dependency]** Missing/old binary breaks that provider.
  *Mitigation:* fallback to `http`; binPath configurable.
- **[Provider misconfiguration could be masked by fallback]** Silent fallback hides a broken
  Tavily key. *Mitigation:* fallback prepends a visible notice line to every result.

## Migration Plan

Purely additive — no data migration. New tools auto-seed into `tool_registry` (default enabled)
per the existing tools-management spec; the Web category appears as a new configurable category.
The `SecretResolver` extraction is internal and behavior-preserving. Rollback = revert the change;
no schema changes to undo.

## Open Questions

- **Single provider vs ordered chain:** v1 ships single configured provider + one hardcoded
  default fallback. A configurable ordered chain (`search_chain: [tavily, duckduckgo]`) is
  deferred — revisit if operators need multi-tier resilience.
- **SecretResolver extraction scope:** resolved by D7 — because `ResolveSecret` hardcodes the
  `ONCLAW_PROVIDER_` prefix (`service.go:214`), the interface is parameterized
  (`Resolve(ctx, envVar, secretKey)`) and `ResolveSecret` becomes a thin caller of it
  (behavior-preserving for the LLM layer; web passes its own `ONCLAW_WEB_*` / `web.*` keys).
- **Shared lightpanda `binPath`:** Web's `lightpanda_bin_path` and the browser category's
  lightpanda config are independent for now; consider unifying if operators report drift.