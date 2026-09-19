## Context

Settings is a routed page (`/settings/:section`, 13 sections) that already hides the workspace sidebar but keeps the icon rail and offers no explicit exit. The workspace "Default model" select is client-only decoration: `saveWorkspace` PATCHes only name/timezone, the value comes from a static `MODELS` list, and `web-app/settings` spec line "Workspace pane" explicitly deferred it ("remain local workspace fields until their domains integrate"). Agents require a pinned `provider_id`+`model` (create/patch reject empty; composite FK `(workspace_id, provider_id)` guards tenancy), and the only default-model-like behavior in the backend is `spawnedAgentDefaultModel = "gpt-4o"` for channel template spawns. Provider verify probes only *stored* keys (400 without one), so it cannot serve the create/edit dialog as-is — but `models-preview` already established the pattern of accepting unsaved draft credentials. The Memory pane opens on the Facts tab and takes the dimension as a free number input; the models.dev catalog carries no dimension metadata (verified against the cache), so "auto-detect from the model" needs a local known-model map, and the connection test already returns the discovered dimension with a server-side mismatch guard.

User-locked decisions from the explore session: true takeover with a `← Back` header; dimension set D16 (768/1024/1536/3072) plus 2048; dialog verify is a test, never a save gate; inherit blocked until a default exists.

## Goals / Non-Goals

**Goals:**
- Settings owns the viewport with an explicit, history-aware way back.
- One workspace default model pair that agents can inherit, end to end (schema → API → runner → UI).
- Draft-credential verification usable from the provider dialog without persisting anything.
- Memory pane opens where users configure; dimension is a constrained choice with auto-detection.

**Non-Goals:**
- Thread retention stays a local-only field (deferred domain).
- No wave-3 vector storage: the dimension remains settings metadata; nothing indexes embeddings yet.
- No per-agent "default temperature" or other inheritable knobs — only the model pair inherits.
- No changes to the catalog-mapping semantics (D3 hint, auto-detect-from-host, explicit-wins).
- No changes to the memory side-call inheritance chain (it already exists and works).

## Decisions

**D1 — Takeover mechanics: hide the rail via route check, back via remembered route.** `App.tsx` already derives `view` from the pathname and hides the sidebar for `settings`; extend that check to also not render `<Rail>` on `/settings`. `SettingsPage` renders a new header row (`← Back` + surface title) above the existing section nav. Back target: a module-level/sessionStorage "last non-settings path" captured in a `Layout` effect on every pathname change; the button navigates there, falling back to `/c`. `navigate(-1)` was rejected: a deep link into `/settings/x` would back out of the app entirely. The header exists at every width; below 768px the section nav stays a horizontal scroll strip.

**D2 — Default model storage: two columns, pair semantics, side-call shape on the wire.** Migration 000058 adds `default_provider_id` (nullable, FK `(workspace_id, default_provider_id) → workspace_providers(workspace_id, id)`) and `default_model` (nullable text) to `workspaces`. API shape follows the memory `side_call_model` precedent: `default_model: {provider_id, model} | null`. Both-or-neither is enforced in the handler (mirror `ValidateAgentMemorySidecall`). Payloads that carry workspace data to the web boot/switcher add the field. Alternatives: a jsonb blob (no FK possible), or a single `provider/model` composite string (loses the FK and the pair validation).

**D3 — Inherit = nullable agent pair, one resolver.** Migration 000058 also makes `agents.provider_id`/`agents.model` nullable (existing rows all pinned, so no backfill). The composite FK stays — NULL columns fall outside it, so inheritance is schema-expressible without weakening tenancy. Save-time rules in the agents handler: empty pair accepted only when the workspace default exists (else 422); half-set pair 400; `RequiresMaxTokens()` and effort catalog validation run only for pinned agents (deferred to run time for inherit). Clearing the workspace default is refused with 422 while `agents WHERE provider_id IS NULL` is non-empty (count in the message). Run-time resolution lives in one function next to the runner's compose path (`effectiveAgentModel(agent, wsDefault)`), and every `agent.ProviderID`/`agent.Model` consumer is routed through it: `composeAgent` (runner.go:709), the promptgen/catalog preview handlers (agents.go:162), heartbeat, scheduler, and the channel template spawn (which uses the workspace default when set, else keeps `gpt-4o`). Alternatives considered: a materialized "resolve at save" (copy the default onto the agent — breaks the moment the default changes, defeats the point of inheritance) and runtime-fail-through (rejected: silent breakage).

**D4 — Run-time max-tokens default for inherit agents.** When the effective provider type requires max tokens and the agent pins none, the runner applies a constant default (4096) instead of failing. Pinned agents keep the strict save-time 400. This trades symmetry (pinned = forced declaration) for ergonomics (inherit on a requiring type just works); the constant lives next to the existing runner defaults and is documented in the requirement.

**D5 — Draft verify: one new endpoint, reusing provider `Verify`.** `POST /workspaces/:ws/providers/verify-draft` with `{type, base_url?, key?, catalog_provider?, provider_id?}`; `providers.write` required; resolves the credential (typed key, else stored key of `provider_id` when key blank), builds a throwaway `providers.Credential` + `CatalogHint`, calls the same per-type `Verify` the row action uses, returns `{ok, error}` synchronously, writes nothing. 400 when no credential resolves. Mirrors `models-preview`'s draft-creds contract, so the dialog's verify and its future catalog preview stay symmetric.

**D6 — Dialog verify UX: present, optional, never gating.** `ProviderFormDialog` gains a secondary "Verify connection" button (disabled until a credential is expressible: typed key, or edit-with-stored-key) with an inline busy/success/error strip. Save keeps today's enablement rules (`isValid` + `saving`); verification state is display-only. The per-row Verify action stays for re-checking stored configs. This is the user's explicit revision of the original "save only after verify success" idea.

**D7 — Dimension: client dropdown + known-model map; server stays permissive.** `SUPPORTED_EMBEDDING_DIMS = [768, 1024, 1536, 2048, 3072]` and a small `KNOWN_MODEL_DIMS` map (text-embedding-3-small → 1536, text-embedding-3-large → 3072, Zhipu embedding-3 → 2048, plus the few catalog models we can pin confidently) live in `lib/embedding.ts`. The dropdown offers Auto + the five values; choosing a model found in the map preselects its dimension; Auto means "unset until the test fills it" (existing behavior). The server keeps validating positive-int only — the dropdown is a product constraint, not a data integrity one, and the test-connection mismatch guard (memory_notes.go) remains the authority. An allowlist on the server was rejected: it would 400 legitimate future models before wave 3 lands.

**D8 — Memory pane default tab is a one-line flip** (`useState("config")`), folded into the same pane requirement that now exists in the spec (the pane was previously unspec'd — this change adds its requirement rather than modifying a phantom one).

## Risks / Trade-offs

- [Nullable agent provider/model widens what the schema accepts] → Save-time both-or-neither validation, inherit-requires-default check, and the FK still pinning non-null pairs keep every stored row either fully pinned or fully empty; a regression test asserts no half-pairs can persist.
- [Clearing the default is a cross-entity guard (workspaces reads agents)] → The handler already holds both stores via the transaction seam; the count query is a single indexed scan on `provider_id IS NULL`.
- [Takeover changes a shell contract users know (rail always visible)] → Confined to `/settings` routes; rail behavior elsewhere is untouched and covered by existing shell tests plus a new takeover test.
- [Known-model map can be wrong or stale] → The map only *preselects*; Test connection remains authoritative and the mismatch guard rejects wrong values. Catalog has no dims, so a map is the only auto-detect source until wave 3.
- [Draft verify could be abused as a free SSRF probe] → Same exposure class as the existing verify and models-preview endpoints (owner-only `providers.write`, provider-keyed egress); no new mitigation needed beyond keeping permissions identical.
- [Inherit agents bypass save-time effort/max-tokens validation] → Deferred checks run at run start; failure mode is a fast, well-named run error, not a corrupt run.

## Migration Plan

1. Ship migration 000058 (workspace default columns; agent pair nullable). Backward compatible: all existing rows keep their meaning, `default_model` reads as `null`.
2. Deploy backend (new fields, endpoints, run-time resolution), then the web build. Older web builds keep working: they never send `default_model`, and pinned agents behave identically.
3. Rollback: `migrate down` drops the new columns; the backend build prior to this change ignores the fields. No data migration is needed in either direction.

## Open Questions

None — the explore session locked the four product decisions (takeover, dimension set incl. 2048, verify-never-gates, block-until-set) and this design resolves the rest.
