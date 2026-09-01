# Add Agent Management

## Why

OnClaw's core entity does not exist yet. Workspaces already have members, roles, and provider configs, but there is nothing for users to actually *create and run*: no agents, no agent-facing screens beyond an empty placeholder, and no code path that calls a model. Agents are the product; this change lands the agent data layer, the management UI, and the platform's first real LLM invocation (AI-generated agent prompts), which together unblock chat, channels, cron, and runs.

## What Changes

- **Migrations 000009–000012**: `workspaces` gains `policy`/`language`; new `agents`, `workspace_skills`, `agent_user_memories` tables; permission backfill into built-in roles (mirrors 000008).
- **Agents table**: brief/identity/soul prompt fields, short free-form role, per-workspace-unique slug, required provider + model, autonomy, `prompts_status` state machine (`generating → ready | failed`), react-nice-avatar props in `avatar` jsonb, runtime-owned `memory` column (read-only via the management API).
- **Provider interface extension**: `ListModels` plus a static capability floor (`RequiresMaxTokens`, `ValidEfforts`, `SupportsTemperature`); six built-ins implemented, plugin providers bring their own rules.
- **Model catalog** (platform's first data-driven dropdowns): tier 1 live provider models API → tier 2 models.dev `api.json` fallback (cached 24h in `ONCLAW_CACHE_DIR`, atomic write, stale-serve on fetch failure, singleflight) → free-text override. Efforts resolve from models.dev `reasoning_options`, floored by static per-type values.
- **Prompt generation service** — the platform's first real LLM call. A user-supplied brief plus the agent's own provider/model produce IDENTITY.md (`agents.identity`) and SOUL.md (`agents.soul`) synchronously within the create/regenerate request, behind an interactive loading experience; startup sweep resets interrupted jobs; regenerate endpoint re-runs from the stored brief.
- **REST API**: slug-routed agents CRUD under `/workspaces/:ws/agents`, `regenerate`, workspace skills CRUD, per-user memory view/reset, provider models endpoints (stored credential + credential-preview for onboarding).
- **Atomic workspace birth**: `POST /workspaces` accepts optional `provider` + `starter_agent`; workspace, built-in roles, Owner membership, provider config, and starter agent commit in one transaction; starter agent prompts generate in the background.
- **Permission catalog**: `agents.read/write`, `skills.read/write` join the closed catalog; Owner/Admin/Superadmin get full access, Member gets reads; backfill migration for existing roles.
- **Frontend**: two-step create wizard (Step 1 identity & model incl. surfaced autonomy and collapsed sampling params; Step 2 skippable capabilities), agent screens built from the design contract, onboarding provider step, nice-avatar picker, model/effort dropdowns wired to the catalog endpoints, setup checklist states (`generating… → ready`, failed → retry).
- **Deliberate deferrals**: slash commands, thread retention, tools/MCP name validation, and any job queue stay out of scope; CLAUDE.md vocabulary updates `system prompt` → `soul`/`identity`/`brief` when this lands.

## Capabilities

### New Capabilities
- `agents`: agent CRUD, two-step creation contract, slug identity and tenant scoping, provider/model requirements, avatar props object, autonomy, read-only prompt fields on the management API.
- `agent-prompts`: synchronous IDENTITY.md/SOUL.md generation from the stored brief using the agent's own provider/model, `prompts_status` machine, regeneration, failure reporting, startup sweep.
- `model-catalog`: model/effort dropdown data — live provider models API, models.dev fallback with 24h cache, effort resolution rules, credential-preview endpoint for onboarding.
- `workspace-skills`: workspace-scoped SKILL.md storage (name/description/body/enabled), uniqueness rules, agent skill-array validation source.
- `agent-memories`: per-user, per-agent memory — membership-gated view and reset; writes are runtime-owned and never exposed on the management API.

### Modified Capabilities
- `providers`: deleting a provider config that agents reference is refused with a conflict (409) instead of succeeding.
- `members-roles`: the permission catalog and the four built-in role permission sets gain `agents.read/write` and `skills.read/write`.
- `tenancy`: workspace creation becomes an atomic birth that can include a provider config and a starter agent in the same transaction.
- `web-app/agents`: the single structured configuration modal is replaced by the two-step creation wizard; prompt-generation status and catalog-driven model/effort selection join the agent screens.
- `web-app/workspaces`: the onboarding screen's deploy action opens the two-step wizard, and the workspace-creation flow gains provider and starter-agent steps feeding the atomic birth API.

## Impact

- **Migrations**: `000009_workspaces_prompt_policy`, `000010_agents`, `000011_workspace_skills_memories`, `000012_backfill_agent_permissions`.
- **Backend**: `internal/domain` (Agent, WorkspaceSkill, AgentUserMemory, permission catalog), `internal/store` (+fake, +postgres), `internal/server/handlers`, `internal/server/router.go`, `internal/providers` (interface + per-type `ListModels`/capability floor), new `internal/modelcatalog` (models.dev cache service), new `internal/agents` (generation service), `internal/cli` (cache dir wiring), `internal/config`.
- **API surface**: new endpoints under `/api/v1/workspaces/:ws/agents`, `/api/v1/workspaces/:ws/skills`, `/api/v1/providers/models-preview`, and `models` subresource on provider routes; `POST /workspaces` payload extension.
- **Frontend**: `web/src` screens/modals/store; new dependency `react-nice-avatar`; design contract extracted via `git show 5fb94d1^:web/Web-Prototype/…`.
- **Dependencies**: `github.com/cloudwego/eino` + `eino-ext` model components (OpenAI/Anthropic/Gemini clients — the three families cover all six catalog types).
- **Environment**: new `ONCLAW_CACHE_DIR` (default `.onclaw/cache`).
- **Docs**: CLAUDE.md domain vocabulary (system prompt → soul/identity/brief) updated at landing.
