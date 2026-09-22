## Context

Change add-workspace-connections made "connection materializes a workspace MCP server" universal. That holds only for MCP-transport services. Figma's official MCP server is desktop-local; its REST API is PAT-friendly. The gap is a credential-safe API lane: the model must be able to issue service calls without ever seeing the credential, and multi-tenancy forbids user-declared URLs (the SSRF property of change 1's D7 must survive).

## Goals / Non-Goals

**Goals:**
- One tool per HTTP connection, safe by construction: pinned base URL, server-side auth, no credential in context.
- Same connection lifecycle as every other kind (probe-gated, hint-only secrets, cascade disconnect).
- A tool-source seam general enough that future connection kinds plug in beside MCP.

**Non-Goals:**
- Generic user-configurable HTTP endpoints (the pin is always recipe data).
- Rich per-verb tool surfaces or declarative verb-table cards (follow-on polish).
- OAuth for HTTP-kind connections (rides add-connection-oauth's lifecycle later).
- Response caching, rate-limit handling beyond surfacing provider errors.

## Decisions

- **D1 — A connection tool source beside MCP, yielding verb facades over one engine.** The runner gains a `ConnectionToolSource` consulted in the same resolution pass as `mcp.ToolSource` (after built-ins): it yields one tool per recipe-declared verb, each a thin facade — pre-bound method, path template, generated parameter schema — over the shared request engine (pin validation, credential injection, response capping). **Verbs only: no free-form request tool exists** (user decision, 2026-09-23). The exposed surface is exactly what the recipe declares; covering a new operation means shipping a recipe update. This is fail-closed by construction — the model cannot invent an endpoint — and it makes per-verb authority tiering (the authority-gate change) meaningful. Alternative rejected: a generic `service.request` pipe (maximal reach, but every call depends on the model composing the right URL, the surface is unbounded, and tiering degenerates to per-connection).
- **D2 — The pin is structural at bind time.** Parameter values are validated and encoded before joining (traversal and absolute-URL values rejected), the joined URL is re-validated against scheme + host + prefix before any dial, and redirects are resolved but refused on host change. With paths pre-bound, the model never constructs a URL; the remaining injection surface is parameter *values*, and this is where it is closed.
- **D3 — Credential hygiene.** The token stays in the connection's encrypted secret row (same machinery as change 1); the request tool receives a resolver function, not a string; tool schema, description, and errors contain no credential material. Errors quote status codes and provider messages, never headers.
- **D4 — Response discipline reuses web-fetch rules.** Size cap, content-type gating (JSON/text), and truncation markers match the existing web fetch tool so turn budgets behave identically across lanes.
- **D5 — Figma as reference recipe.** Base URL `https://api.figma.com`, auth header, probe call (authenticated `/v1/me`), guided scopes, and the **curated verb list** — drafted from Figma's OpenAPI spec and pruned to the workflows that matter (file/file-tree reads, comments, project listings) — live in the recipe; values are pinned by live verification at apply time (task 5.3 pattern).
- **D6 — Kind is a recipe-level declaration, not a per-connection choice.** A service's kind is fixed by its recipe (GitHub is mcp; Figma is http); users never pick transports. This keeps the gallery honest and the connect flows purpose-built.

## Risks / Trade-offs

- [Curated verb lists lag real user needs — a missing operation blocks the agent] → fail-closed by design; recipes are release-shippable data, and usage traces show which operations are demanded so the verb list grows deliberately. The starter skill documents what the connection can and cannot do.
- [Provider response shapes vary (envelopes, pagination)] → the engine returns the provider's JSON verbatim (capped); summarization and the model handle shape, matching how MCP tool outputs already behave.
- [A degraded connection's missing tools confuse agents mid-task] → the skip-and-mark degradation matches MCP behavior and is visible in run diagnostics.
- [Tool budget: N verbs per connection] → bounded by curation — the recipe declares the workflows that matter, not the provider's full API; the budget policy follow-on can cap per connection.

## Migration Plan

1. No schema change in this change — the connection kind is derivable from the recipe; the connections table's service uniqueness already prevents duplicates.
2. Rollback: remove the tool source; HTTP-kind connections become inert rows (gallery hides them behind a kind check shipped in the same release).

## Open Questions

None blocking. The Figma recipe's probe call and scope guidance are pinned by live verification during apply.
