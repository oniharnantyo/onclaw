## 0. Scaffold + spec

- [x] 0.1 Create this OpenSpec change (`openspec/changes/add-agent-skills/`)
- [x] 0.2 Promote `gopkg.in/yaml.v3` to a direct dep; `go mod tidy`
- [x] 0.3 `internal/skill/` package skeleton (paths, backend, middleware stubs) compiling

## 1. SkillStore (ledger)

- [x] 1.1 `Skill` DTO in `internal/store/types.go` (Name, Scope, SourceType, Source, SkillPath, Version, Hash, Description, Enabled, InstalledAt, UpdatedAt)
- [x] 1.2 `SkillStore` interface in `internal/store/store.go` (Add/Get/List/ListByScope/Update/Remove)
- [x] 1.3 `skills` table migration in `internal/store/sqlite/db.go` (PK `name`)
- [x] 1.4 SQLite impl `internal/store/sqlite/skill.go` mirroring `profile.go`
- [x] 1.5 Tests `internal/store/sqlite/skill_test.go` via `setupTestDB`

## 2. Skill domain (`internal/skill/`)

- [x] 2.1 `internal/skill/paths.go`: `TargetDir(home, scope)`; runtime `ResolveDirs(home, agent)` (3-tier) lives in `internal/agent/middlewares`
- [x] 2.2 `manifest.go`: frontmatter parse + normalize (force `SKILL.md`, complete name/description, strip fork, preserve extras)
- [x] 2.3 `discover.go`: recursive `Discover(root, restrict)` → candidates
- [x] 2.4 `internal/agent/middlewares/skill_backend.go`: `multiDirBackend` implementing eino `skill.Backend` (List/Get, dedupe first-wins) — agent runtime
- [x] 2.5 `internal/agent/middlewares/skill_middleware.go`: `BuildMiddleware(ctx, home, agent)` → `skill.NewTyped[*schema.AgenticMessage]`; no-op when no dirs
- [x] 2.6 Unit tests: precedence, dedupe, normalization, discover nesting

## 3. Install (`internal/skill/install.go` + adapters)

- [x] 3.1 `Fetcher` interface + `Installer` with `Discover`/`Install(source, selected, scope)`/`Remove`/`Update`/`List`
- [x] 3.2 `source_github.go`: codeload tarball via `net/http` + `archive/tar` + `compress/gzip` (covers skills.sh)
- [x] 3.3 `source_http.go`: download + extract tar.gz/tgz/zip (`archive/zip` for zip)
- [x] 3.4 `source_local.go`: `filepath.Walk` copy
- [x] 3.5 `source_plugin.go`: detect `.claude-plugin/plugin.json`, restrict `Discover` to `skills/`
- [x] 3.6 Naming: source has >1 skill → `<package>:<skill>`; exactly 1 → bare
- [x] 3.7 Idempotency: compare hash+source vs ledger → install / no-op / update / collision-error
- [x] 3.8 Tests (httptest for github/http; tmpdir for local/plugin)

## 4. Agent wiring

- [x] 4.1 `internal/agent/agent.go`: append `middlewares.BuildMiddleware(...)` to Handlers; zero signature change
- [x] 4.2 Existing agent tests still pass (no skill dirs in temp homes)

## 5. CLI

- [x] 5.1 `internal/cli/skill_cmd.go`: install/list/show/remove/update modeled on `provider_cmd.go`
- [x] 5.2 Source classification (local/http/github/plugin) + flags (`--scope`, `--branch`, `--path`, `--all`, `--dry-run`, `--as`, `--plugin`)
- [x] 5.3 Interactive multi-select picker (TTY); non-TTY requires `--path`/`--all`
- [x] 5.4 `getSkillInstaller` helper in `context.go`; register `skillCommand` in `app.go`

## 6. API

- [x] 6.1 `SkillView`/inputs in `service/types.go`
- [x] 6.2 `service/skill.go`: `DiscoverSkills` / `InstallSkills` / `List` / `Get` / `Remove` / `Update`
- [x] 6.3 `handler/skill.go` modeled on `handler/provider.go`
- [x] 6.4 Routes in `routes.go` (`GET /api/skills`, `POST /api/skills/discover`, `POST /api/skills`, `GET/DELETE /api/skills/{name}`, `POST /api/skills/{name}/update`) behind `requireAuth`
- [x] 6.5 Wire `*skill.Installer` into `service.New` + server + serve command

## 7. Web UI

- [x] 7.1 `web/src/components/Skills.tsx` (TanStack table + two-step install modal: discover → checkbox select → install), modeled on `Providers.tsx`
- [x] 7.2 `App.tsx`: add `skills` tab + nav item; load state
- [x] 7.3 `make ui && make build`

## 8. Verify

- [x] 8.1 `make vet`, `make test` (≥80% on new Go code)
- [x] 8.2 `go build ./...`; `make build-all` (`CGO_ENABLED=0`; amd64/arm64/armv7)
- [x] 8.3 E2E: install from owner/repo, local dir, HTTP tar.gz, Claude plugin (skills/ only); per-agent vs global precedence; re-install upsert; chat invokes the `skill` tool
- [x] 8.4 API/UI: `serve` → Skills tab install/list/remove; `curl /api/skills`