## Why

onclaw's agent can read, write, and list files and run shell commands, but it cannot make
**targeted edits** to existing files (only full rewrites via `write_file`) and it has **no web
access** — no search, no URL fetch. That confines the on-device agent to coarse full-file
mutations and cuts it off from documentation and current information. GoClaw (`sausheong/goclaw`)
already ships battle-tested Go implementations of exactly these tools (`edit_file`, `web_fetch`,
`web_search`) under the same single-binary, low-RAM ethos. Porting them — with the web tools made
extensible/swappable — fills both gaps while staying consistent with onclaw's tiny-binary,
`CGO_ENABLED=0`, security-first constraints.

## What Changes

- **Add `edit_file` builtin tool** (Filesystem category): exact-string replace that requires a
  unique match, workspace-confined via the existing `pathguard` (`ValidatePath`).
- **Add `web_search` builtin tool** (new Web category): swappable search backend — defaults to
  **DuckDuckGo HTML** (no API key); **Tavily**, **Exa**, and **Google Custom Search** as opt-in
  providers chosen via the Web category config.
- **Add `web_fetch` builtin tool** (Web category): swappable fetch backend — defaults to
  **stdlib HTTP** (with an SSRF egress guard and HTML→text strip); the **Lightpanda** CLI
  (`lightpanda fetch --dump markdown <url>`) as an opt-in provider.
- **Add a `web` capability package** (`internal/web/`) using a factory-registry provider model
  that mirrors the existing `internal/browser/` engine pattern, plus a shared SSRF guard.
- **Provider selection via the Web category config** (`tool_group_config`): the configured
  provider is *preferred*; on absence/misconfiguration/failure each tool **falls back** to its
  always-available default (`duckduckgo` / `http`) and prepends a notice.
- **Web provider API keys resolved via the existing env > SecretStore + KeyManager mechanism**,
  exposed through a new shared `SecretResolver` interface; keys are **never** stored as plaintext
  in category config.
- **No new `go.mod` dependencies** for the defaults (stdlib only); **no changes** to the tool
  registry, store seeding, or REST API (all are generic over new categories/tools — the API serves
  any category's schema via `tools.GetConfigEntry`).
- **One Web UI addition:** added a **structured Web-category config form** in `web/src/components/Tools.tsx`
  (provider selects + non-secret tunables, mirroring the Browser form). Required because
  `CLAUDE.md` mandates "structured fields only, never a raw-JSON `<textarea>`," and non-Browser
  categories fell into exactly that textarea fallback. See design D11.

## Capabilities

### New Capabilities

- `web-tools`: the `web_search` and `web_fetch` builtin tools, their swappable provider
  backends and factory registry, the configurable Web category, the SSRF egress guard, the
  default-provider fallback policy, and resolution of web-provider API keys.

### Modified Capabilities

- `agent-tools`: add `edit_file` to the builtin file-tools requirement (targeted exact-string
  edit requiring a unique match, workspace-confined). The existing redaction-decorator
  requirement already covers new tools, so `edit_file` inherits masking with no per-tool code.

## Impact

- **New code:** `internal/web/` (Searcher/Fetcher interfaces, factory registries, Config DTO,
  `ssrf.go`); provider impl packages `internal/web/{ddg,http,tavily,exa,google,lightpanda}/`;
  `internal/agent/tools/web/` (`web_search`, `web_fetch` tools + Web `RegisterConfig`);
  `internal/agent/tools/editfile.go`.
- **Shared abstraction:** promote the LLM layer's secret resolution (`llm.Service.ResolveSecret`)
  into a `SecretResolver` interface reused by both the LLM layer and web tools; thread it
  through `tools.Scope`.
- **No external dependencies** added; `CGO_ENABLED=0` and the tiny binary preserved.
- **No data migration:** purely additive — the new tools auto-seed into `tool_registry` (default
  enabled) and the Web category appears as a new configurable category, per the existing
  tools-management spec. The REST API is unchanged (generic over categories); the only UI work is
  the structured Web-category form (see design D11).