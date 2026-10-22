# Tasks

## 1. Store, migration, workspaces

- [x] 1.1 Add migration `000004_agent_prompt_docs.sql` (slug + UNIQUE index, description, brief, prompts_status, prompts_error; keep `system_prompt`), extend the store `Agent` type and SQLite CRUD for the new fields, and verify with new cases in `internal/store/sqlite/crud_test.go` plus `go test ./internal/store/...`
- [x] 1.2 Implement the agent workspaces manager (`~/.buddies/workspaces/<slug>/`: path resolution, create, remove, atomic write with `<name>.bak` backup) under `internal/` and verify with unit tests covering atomic write, backup, backup-failure abort, and remove
- [x] 1.3 Add the one-time Go backfill hook (slugify names with dedupe → set slug, write legacy `system_prompt` into `IDENTITY.md`, set `prompts_status='ready'`) and migration `000005_drop_system_prompt.sql`, then verify with a fixture database containing a pre-change agent: after startup the agent has a slug, `IDENTITY.md` holds the old prompt, status is `ready`, the column is gone, and a second startup is a no-op

## 2. Prompt generation

- [x] 2.1 Create `internal/promptgen` ported from onclaw-v2 (hardcoded generation prompts for IDENTITY/SOUL/BOOTSTRAP, hardcoded generation parameters, 3-minute timeout, model factory over the provider registry + eino runtime) with enhance mode, instruction-as-requested-changes, `.bak`-before-commit, and backup-failure abort; verify with fake-model service tests mirroring onclaw-v2's `service_create_test.go` cases
- [x] 2.2 Wire the prompt status machine into the services layer: create persists `generating` then commits `ready | failed`, regenerate is a conflict while `generating`, and the startup sweep moves stale `generating` rows to `failed` ("prompt generation interrupted — retry"); verify with service tests for each transition including the sweep

## 3. Runtime composition

- [x] 3.1 Change the runner to compose each turn's persona context from the workspace documents (identity, soul, bootstrap, in that order, read fresh from disk each turn, missing documents tolerated) and remove the `SystemPrompt` input; verify by updating the existing turn tests and adding a missing-documents case in `internal/agents`
- [x] 3.2 Apply the agent's optional context-window override in the runtime's context accounting (catalog value when unset) and verify with a context-accounting test covering set, unset, and override cases

## 4. Service surface (Wails)

- [x] 4.1 Extend `internal/services/agents.go`: create DTO gains slug/description/brief (provider/model/temperature/max-tokens/context-window/allowlists already flow), create blocks on generation, add a regenerate call, and agent delete removes the workspace directory after the row delete; regenerate bindings with `task build` and verify `go build ./...` plus the generated bindings expose the new DTO and regenerate call
- [x] 4.2 Verify service-level behavior with tests using a fake promptgen: create returns only after generation settles, slug duplicates are rejected, delete keeps sessions while removing the directory

## 5. Frontend wizard

- [x] 5.1 Replace `CreateAgentModal` with the three-step wizard (Identity: avatar preview from name, name, slug auto-suggest with DNS-label + duplicate validation, multi-line description, brief; Model: provider/model, collapsible advanced temperature/max-tokens/context-window with catalog auto-fill; Capabilities: skills/tools/MCP CheckOptionLists; step pills + per-step validation) in the ModalShell grammar; verify with component tests per step including slug and brief validation
- [x] 5.2 Add `OnboardingLoader` (label "Onboarding \<name\>…", 5s rotation of the informal pool, elapsed timer after 30s, reduced-motion collapse) and show it while create is in flight; verify with a component test asserting rotation and the busy footer
- [x] 5.3 Wire the agents menu to the new flow: card gains the clamped description line and the generating/failed indicator with retry affordance; verify with `frontend/src/features/agents/__test__` cases for card rendering and retry dispatch

## 6. Frontend configuration

- [x] 6.1 Add the Identity tab to `AgentConfigModal`: file rail (IDENTITY/SOUL/BOOTSTRAP), multi-line mono editors reflecting the workspace files, "What should change?" regenerate form with "Enhance prompts" confirm, failed banner with reason, editors disabled while generating; verify with component tests for load, save, regenerate dispatch, and the failed banner
- [x] 6.2 Verify delete-confirmation copy still promises preserved sessions and now-triggered workspace cleanup is invisible to the user, by updating the `DeleteAgentModal` test if copy changed and re-running the agents feature tests

## 7. Theme parity and full gates

- [x] 7.1 Exercise wizard, loader, and Identity tab in both daisyUI themes (dev run screenshots or IAB pass) and fix any legibility gaps before proceeding — wizard steps 1–3 both themes screenshotted via IAB (2026-10-17, 1280×800; legibility fixes from that pass: wide modal + per-field errors), picker both themes re-shot after the avatar pass, loader rotation + Identity tab covered by component tests and the 6.1 hallmark both-theme audit
- [x] 7.2 Run the full gates and a manual smoke: `gofmt -w .`, `go vet ./...`, `go test ./...`, `vp check` scoped to `frontend/src`, `npm run build` then `task build`, and a GUI smoke of create → generate → edit document → regenerate → delete with sessions preserved — gates all green (final pass 2026-10-17, incl. build:server + bindings regen); GUI smoke run isolated via `HOME=/tmp/… bin/buddies-server` against a local mock OpenAI-compatible provider: create with customized avatar → generation ready (docs v1) → chat turn streamed → hand edit saved → Enhance prompts rewrote docs (`.bak` = previous version) → typed-confirm delete removed the row + workspace while the session and its events were preserved; `agents.avatar` JSON verified in SQLite

## 8. Avatar customization (2026-10-17 parity pass, design D14)

- [x] 8.1 Migration `000006_agent_avatar.sql` adds the `avatar` JSON column; store plumbing (Create/Update/scan) persists the config verbatim and reads an empty column back as nil; verify with CRUD tests including the nil round-trip
- [x] 8.2 Port `AvatarPicker` (Randomize + Customize trait grid with onclaw-v2's exact option sets) and wire it into the create wizard's Identity step and the config modal; create and edit saves submit the full config; verify with component tests (randomize, grid toggle, trait edit, stored-config hydration, payload carry-through)
- [x] 8.3 `AgentAvatar` renders the stored config over the id derivation; agent cards and the chat sidebar pass the row's config through; verify with precedence tests (stored wins, empty/absent falls back)
