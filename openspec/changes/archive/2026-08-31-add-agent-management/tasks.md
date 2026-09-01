# Tasks — add-agent-management

## 1. Domain & migrations

- [x] 1.1 Migration `000009_workspaces_prompt_policy`: add `policy text NOT NULL DEFAULT ''` and `language text` to workspaces
- [x] 1.2 Migration `000010_agents`: agents table — slug/name/role/description/brief/identity/soul, required provider_id + model, temperature numeric(3,2) CHECK 0–2 DEFAULT 1.00, max_tokens CHECK > 0, effort, autonomy CHECK (approval|suggest|full) DEFAULT 'approval', tools/skills/mcp text[] NOT NULL DEFAULT '{}', avatar jsonb DEFAULT '{}', prompts_status CHECK (generating|ready|failed) DEFAULT 'generating', prompts_error, created_by/updated_by FK users, `UNIQUE (workspace_id, slug)`, composite FK (workspace_id, provider_id) → workspace_providers(workspace_id, id) plus the backing UNIQUE on workspace_providers, index on workspace_id
- [x] 1.3 Migration `000011_workspace_skills_memories`: workspace_skills (UNIQUE (workspace_id, name), enabled default true) and agent_user_memories (PK (agent_id, user_id), workspace_id FK CASCADE, content)
- [x] 1.4 Migration `000012_backfill_agent_permissions`: mirror 000008 with idempotent guards — agents.read/write + skills.read/write to Owner/Admin/Superadmin, reads to Member
- [x] 1.5 `internal/domain/agent.go`: Agent, WorkspaceSkill, AgentUserMemory entities; autonomy/temperature/avatar (props object ≤ 2KB) validation; agent slug validation sharing workspace slug rules
- [x] 1.6 `internal/domain/permissions.go`: AgentsRead/Write + SkillsRead/Write catalog entries; four role arrays + count comments (Owner 9→13, Admin 8→12, Member 4→6, Superadmin 14→18); IsValidPermission
- [x] 1.7 `go build ./... && go vet ./...` passes; `go test ./...` green
- [x] 1.8 Migration `000013_agent_workspace_dir`: agents.workspace_dir text NOT NULL DEFAULT ''; `ONCLAW_WORKSPACE_DIR` configures the workspace root (flag `--workspace-dir`, default `$HOME/.onclaw/workspaces` via `domain.DefaultWorkspaceDir()`); `domain.AgentWorkspaceDir(root, tenantSlug, agentSlug)` builds `<root>/<tenant_slug>/agents/<agent_slug>` (absolute); create + birth mkdir the directory before any DB write; path stable across slug renames

## 2. Provider interface & model catalog

- [x] 2.1 Extend the Provider interface: ListModels + floors (RequiresMaxTokens, ValidEfforts, SupportsTemperature), implemented on all six built-ins; update providers tests
- [x] 2.2 New `internal/modelcatalog`: models.dev service — fetch/cache at ONCLAW_CACHE_DIR (default .onclaw/cache), 24h TTL, atomic write, stale-serve-on-error, singleflight; type→id mapping (gemini→google, compatible→none); effort resolution (reasoning_options → static floor); defensive parsing
- [x] 2.3 Model resolution: tier 1 ListModels(cred) → tier 2 catalog → source live|catalog|none; enrichment (efforts, supports_temperature) when catalog knows the model
- [x] 2.4 internal/config + internal/cli: ONCLAW_CACHE_DIR wiring

## 3. Stores

- [x] 3.1 Store ports: AgentStore (Create, ByID, BySlug, ListForWorkspace, Update, Delete, CountByProvider, SetPromptState, SweepGenerating), WorkspaceSkillStore (CRUD), AgentUserMemoryStore (Get/Upsert/Delete); root Store accessors + fake implementations
- [x] 3.2 Postgres adapters for agents, skills, memories (workspace-scoped, BySlug/ByID, in-use count, prompt-state writes, sweep)
- [x] 3.3 Store tests: fake contract tests + postgres integration tests (TEST_DATABASE_URL)

## 4. Generation service

- [x] 4.1 `internal/agents` generation service: prompt built from stored brief + role/description/name; Eino ChatModel adapter factory (openai/anthropic/gemini families cover all six types); hardcoded params (temp 0.7, gen max_tokens 2000); structured output (shared `identity`/`soul` JSON schema enforced per adapter family) → prompts_status ready|failed + short prompts_error
- [x] 4.2 Synchronous generation in the request path: create/birth/regenerate call Generate inline (bounded timeout) and return the final prompt state; startup sweep (generating → failed "interrupted") retained for crashes mid-request; delete-during-flight is a no-op status write
- [x] 4.3 AGENTS.md (L1) embedded base prompt: `internal/agents/prompts/AGENTS.md` + go:embed
- [x] 4.4 Unit tests: prompt building, output parsing, status transitions, sweep

## 5. HTTP layer

- [x] 5.1 Agents handlers: CRUD slug-addressed (BySlug-first, ByID fallback), managed-field ignoring, capability validation (anthropic max_tokens; effort vs resolved values), skills-array validation against the store
- [x] 5.2 Regenerate endpoint (synchronous, 200 with final state; 409 while generating); memory view/reset endpoints (membership-gated, own only); models endpoints — GET /workspaces/:ws/providers/:id/models (providers.read) and POST /api/v1/providers/models-preview (authed; body credential never stored; unknown type 400)
- [x] 5.3 Skills handlers: CRUD, 409 duplicate name, list omits body / get includes body
- [x] 5.4 router.go: register routes + permission guards; DeleteProvider gains in-use check → domain.ErrConflict 409
- [x] 5.5 Handler tests: CRUD + 404/403 matrix, catalog endpoints (httptest + fake store + stub resolver), skills, memory, delete-in-use 409, regenerate while generating

## 6. Atomic workspace birth

- [x] 6.1 CreateWorkspace payload gains optional provider + starter_agent; validation through the same paths as standalone endpoints; WithTx: workspace + roles + owner + provider + agent (provider_id → new config); failures abort the whole birth; generation kicked after commit
- [x] 6.2 Response includes the starter agent (prompts_status generating); key material never in any response
- [x] 6.3 Handler tests: birth success, validation failure aborts (no workspace row), key secrecy

## 7. Frontend — foundation

- [x] 7.1 Extract the design contract from git history (`git show 5fb94d1^:web/Web-Prototype/...`): tokens, agent screens, onboarding, states
- [x] 7.2 Add react-nice-avatar dependency; Avatar component with controlled props + randomize
- [x] 7.3 API client: agents/skills/memories/models endpoints, models-preview, birth payload, prompts_status polling helper
- [x] 7.4 Store slices + types for Agent, WorkspaceSkill, prompts_status states

## 8. Frontend — agent surfaces

- [x] 8.1 Three-step wizard modal: Step 1 Identity (name, slug auto-suggest, role with kebab suggestions, description + brief textareas, avatar picker); Step 2 Model (provider select, model combobox with free-text fallback + effort dropdown, collapsed Advanced holding temperature/max_tokens, auto-expanding on submit error); Step 3 Capabilities (tools/skills/mcp toggles from GET /skills, autonomy segmented control with a short description per option; skippable → empty arrays)
- [x] 8.2 Model/effort dropdowns on the catalog endpoints: source badge (live|catalog|none), free-text when none, effort hidden when empty, model reset on provider switch
- [x] 8.3 Agents roster: avatar cards, model chip, autonomy, prompts_status states (generating indicator, failed + Retry → regenerate), empty state, 1/2/3-column grid at 640/1280
- [x] 8.4 Agent detail: identity/soul editors (editable once ready), Capabilities tab (PATCH arrays), own-memory view/reset, regenerate + status line
- [x] 8.5 Onboarding deploy-first-agent action opens the wizard
- [x] 8.6 Workspace creation flow: provider step (type/name/base_url/key, models-preview-powered model dropdown), optional starter-agent step (slim fields), single birth call, failure keeps entered data

## 9. Verification & docs

- [x] 9.1 `go build ./... && go vet ./...`; `go test ./...`; integration suite with TEST_DATABASE_URL
- [x] 9.2 Extend ./scripts/smoke.sh: agents CRUD, slug conflicts, delete-in-use 409, skills CRUD, memory view/reset, models endpoints, birth flow, regenerate 409
- [x] 9.3 Frontend: pnpm build, pnpm test, Playwright parity run for agent screens
- [x] 9.4 CLAUDE.md vocabulary: system prompt → soul/identity/brief; slash commands/thread retention marked deferred; design-contract deviations recorded

## 10. Cleanup

- [x] 10.1 Verify no placeholder/stub tasks remain; final validate + status
