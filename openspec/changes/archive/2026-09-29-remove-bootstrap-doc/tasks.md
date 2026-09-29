# Tasks

## 1. Promptdocs and seeding (backend)

- [x] 1.1 Delete `internal/promptdocs/BOOTSTRAP.md`; remove the `BootstrapTemplate` embed, `SeedBootstrapDocument`, and `bootstrapFileName` seed paths in `promptdocs.go` (keep the `SeedWorkspace` clear-list entries for `BOOTSTRAP.md`/`.bak`)
- [x] 1.2 `promptgen/service.go`: drop the `SeedBootstrapDocument` call in `GenerateForCreate`; update `readCurrentPrompts` comment and the `ReadPromptDocuments` signature (two documents)
- [x] 1.3 Extend the startup sweep in `promptdocs`/`internal/cli/server.go` wiring to also remove stray `BOOTSTRAP.md`/`.bak` from every agent workspace dir, logged per removal

## 2. Composition and API projection (backend)

- [x] 2.1 `internal/agents/runner.go`: remove the composer's BOOTSTRAP.md document slot; update the attended/scheduler/heartbeat profile comments
- [x] 2.2 `internal/domain/agent.go`: remove the `Bootstrap` field; `internal/server/handlers/agents.go`: drop the bootstrap projection in `composePromptDocuments`
- [x] 2.3 `internal/server/handlers/workspaces.go`: drop the starter-agent `SeedBootstrapDocument` call

## 3. Web

- [x] 3.1 `web/src/lib/api.ts`: remove `bootstrap` from the Agent type
- [x] 3.2 `web/src/modals/AgentConfigModal.tsx`: remove the BOOTSTRAP.md entry from `PROMPT_FILES`, the tab state, the read-only preview block, and the `agent-bootstrap-doc` testid
- [x] 3.3 Prune `bootstrap: ''` fixtures from web tests (CreateWorkspaceModal, GatewaysPane, AttachAgentsList, gatewaysApi, AgentConfigModal tests)

## 4. Tests and smoke

- [x] 4.1 Update backend tests referencing bootstrap: promptdocs, promptgen (service/service_create), agents (composer, channels, scheduler/heartbeat profiles, delete_file), server (agents, birth, models, router, v1, handlers agents_create/agents), store fakes/postgres tests
- [x] 4.2 Update `scripts/smoke.sh` agent-read assertions at lines ~814 and ~2847 (drop the `bootstrap` key)
- [x] 4.3 Add a sweep test: a workspace dir pre-seeded with `BOOTSTRAP.md`/`.bak` is cleaned at startup while IDENTITY/SOUL are untouched

## 5. Verification

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green; integration tests with `DATABASE_URL` green (verified by verifier run: unit 41 pkgs ok; integration green except documented pre-existing pg_trgm/cli-config env issues)
- [x] 5.2 `pnpm test` green; `tsc` clean (verified by verifier run: 1460/1460, tsc exit 0)
- [x] 5.3 Manual: create a new agent — no BOOTSTRAP.md on disk, Prompts tab shows two files; restart server with a pre-seeded workspace — sweep removes the stray file; a turn runs with no bootstrap section in the composed instruction (smoke run live-verified the create/turn parts incl. the "No BOOTSTRAP.md" assertion; sweep pinned by `TestSweepStrayPromptFilesRemovesBootstrapLeftovers`; composition by the composer negative tests)
