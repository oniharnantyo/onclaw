# Design — add-agent-management

## Context

The backend has a complete tenancy/identity/provider pipeline to build on: the `workspace_providers` work (commit `52f36fd`) is the template pipeline (domain entity → store port + fake + postgres adapter → handlers → routes with `RequirePermission` → closed permission catalog + backfill migration). Store invariants (`internal/store/store.go:13-31`) fix timestamps as app-managed and sentinel errors at the boundary; `RequireWorkspace` resolves `:ws` by slug with 404 enumeration defense. The frontend design contract lives in git history — extract with `git show 5fb94d1^:web/Web-Prototype/onclaw-app.html` (+ `DESIGN-MANIFEST.json`, `DESIGN-HANDOFF.md`); do not restore the directory wholesale.

## Goals / Non-Goals

**Goals:**
- Agents as first-class tenant entities: CRUD, three-step wizard creation (Identity → Model → Capabilities), visible prompt states
- The platform's first real LLM call: synchronous identity/soul generation from the stored brief via the agent's own provider config
- Data-driven model/effort dropdowns with a cached models.dev fallback
- DB-level tenant safety via a composite foreign key
- Capabilities (tools/skills/mcp) and autonomy attached at create via wizard Step 3 (skippable)

**Non-Goals:**
- Agent runtime/chat execution (a later change consumes the L1–L5 prompt layers)
- Slash commands, thread retention, channel-posting switch — deferred; schema stays additive later
- Tool/MCP name validation (no registries exist yet); stored free-form
- A durable job queue; in-process dispatch only
- Backend persistence for API keys / MCP servers panes (unchanged from today)

## Decisions

### 1. Providers pipeline as the template
Agent features clone the providers shape end to end. Keeps the change reviewable and idioms consistent. Alternative: bespoke structure — rejected, consistency wins.

### 2. Same-workspace provider FK at the data layer
`UNIQUE (workspace_id, id)` on `workspace_providers` plus composite FK from `agents (workspace_id, provider_id)`. Cross-tenant provider references become DB errors, not code-review-only guarantees (CLAUDE.md requires data-layer tenant isolation). Alternative: app-level check inside the transaction — rejected; it only protects paths that remember to call it.

### 3. Capability methods are a floor, the catalog supersedes
`Provider` interface gains `ListModels` plus static floors `RequiresMaxTokens`, `ValidEfforts`, `SupportsTemperature`, implemented by the six built-ins. Per-model metadata (models.dev) supersedes floors; floors cover unknown gateway models and gemini budget-only models. `RequiresMaxTokens` stays purely static (anthropic API contract). Alternatives: `if type == "anthropic"` special-casing (violates the "fix the interface" rule), catalog-only (no floor when offline/unknown).

### 4. Model catalog: two-tier resolution + cached fallback
Tier 1 live provider `ListModels` (compatible types reuse the family client with base_url). Tier 2 models.dev `api.json` cached at `ONCLAW_CACHE_DIR` (default `.onclaw/cache`), 24h TTL, atomic write, serve-stale-on-refresh-failure, singleflight fetch. Effort values from `reasoning_options[type=effort].values`; per-model `temperature` bool supersedes the static floor. Verified against the live `api.json` (2026-08-31):
- Model object: `id`, `name`, `reasoning_options` (`{type:"effort", values:[...]}`, `{type:"budget_tokens", min,max}`), `temperature` (bool), `limit.context/output`, `cost`
- Provider ids: `openai`, `anthropic`, `google` (catalog type `gemini`), `openrouter`; `-compatible` types have no mapping

### 5. Prompt layers L1–L5
L1 `AGENTS.md` go:embed constant (no DB) → L2 `agents.identity` (IDENTITY.md) → L3 `workspaces.policy` → L4 `agents.soul` (SOUL.md) → L5 `agent_user_memories.content`; `language` rides as a directive. This change stores L2/L4 (and L3); assembly is the runtime change's job. Alternative: single "system prompt" column — rejected; per-layer editability and per-user memory need separate surfaces.

### 6. Synchronous generation in the request path
Create, the atomic birth, and regenerate run generation synchronously (bounded by a timeout) and return the agent with its final prompt state; `generating` describes an in-flight generation only. Generation failure never fails the request — the agent exists with `failed` + short `prompts_error` for retry. The startup sweep still resets stale `generating` rows ("interrupted — retry") for crashes mid-request. Alternative: async dispatch after commit (users couldn't see generation finish and had no natural loading surface), or a durable queue (deferred until cron/schedules exists).

### 7. Generation via Eino ChatModel adapters
Use Eino ChatModel components — OpenAI/Anthropic/Gemini client families cover all six catalog types (compatible types via base_url). Hardcoded generation params: temperature 0.7, generation-time max_tokens 2000 (satisfies anthropic's required max_tokens). Prompts are requested as **structured output**: a shared JSON schema (`identity`, `soul` — required markdown strings) is enforced per adapter family — `response_format: json_schema` on the OpenAI family (construction-time config), `ResponseFormat.Schema` on the Claude family (construction-time config), `WithResponseJSONSchema` on Gemini (call-time option); the system prompt additionally instructs a bare JSON reply. The parser accepts a tool-call payload first, then the message-content JSON (tolerating code fences and surrounding prose), and fails the generation otherwise. Alternatives: hand-rolled HTTP clients (duplicates Eino), free-text delimiter protocol (brittle parsing — replaced), other frameworks (no repo signals).

### 8. Remaining ledger decisions, collapsed
- **Slug routes**: routes address agents by slug; store keeps `ByID` + `BySlug(workspaceID, slug)`; handlers resolve BySlug first, ByID fallback (user-facing @mention identity)
- **Workspace dir**: every agent gets an on-disk directory `<root>/<tenant_slug>/agents/<agent_slug>` under the workspace root — `ONCLAW_WORKSPACE_DIR` (e.g. `/var/lib/onclaw/.onclaw/workspaces`), defaulting to `$HOME/.onclaw/workspaces` — stored in the `workspace_dir` column (migration 000013) and created before the agent row is written (create + birth); assigned at creation and stable across slug renames
- **Memory endpoints**: membership-gated view/reset of own memory; runtime-owned writes; `agents.memory` never written by the management API
- **Avatar**: jsonb props object ≤ 2KB, loosely validated (react-nice-avatar props)
- **Skills validation**: agent.skills ⊆ workspace_skills names (disabled included; runtime skips disabled)
- **Atomic birth**: provider + starter agent inside CreateWorkspace's existing WithTx; generation kicked after commit
- **Permissions**: agents.read/write + skills.read/write join the closed catalog; role arrays updated (Owner 9→13, Admin 8→12, Member 4→6, Superadmin 14→18); backfill 000012 mirrors 000008
- **Migrations**: 000009 workspaces policy/language; 000010 agents; 000011 workspace_skills + agent_user_memories; 000012 permission backfill

## Risks / Trade-offs

- [Workspace keys pay for generation] → gated by `agents.write`; one call per create/regenerate; cost note in spec already accepted for self-hosted
- [Crash mid-generation leaves `generating` rows] → startup sweep flips them to `failed`; acceptable until a durable queue exists
- [models.dev unreachable on cold cache] → endpoints still resolve tier 1 live; catalog features degrade per spec (free-text model entry)
- [Loose tools/mcp data gets grandfathered] → accepted; tighten when tool/MCP registries land
- [models.dev schema drift] → parse defensively; unknown fields ignored; verified paths documented above

## Migration Plan

Deploy backend first (migrations 000009–000012 are additive; `down` migrations provided for rollback), then the frontend. The permission backfill is idempotent (guards with `NOT (perm = ANY(permissions))`). Startup sweep makes redeploys safe with in-flight generations.

## Open Questions

None — all forks were resolved with the user during exploration; runtime-side questions (gemini budget_tokens ↔ effort mapping, openrouter effort values) belong to the runtime change.
