## Why

onclaw's agent (eino `adk.TypedChatModelAgent[*schema.AgenticMessage]`) has no notion of
**skills** — reusable, on-demand procedure/knowledge packs. We want agents to (1) resolve
skills from per-agent and global directories at runtime, (2) install skills from external
ecosystems (skills.sh, GitHub, Claude plugins, HTTP archives, local dirs) and normalize them
into one compatible on-disk format, and (3) manage them via the CLI and the web console.

The enabling fact: eino (pinned `v0.10.0-alpha.9`) already ships `adk/middlewares/skill`,
which **is** the skill loader — it injects a progressive-disclosure "Skills System"
instruction and a `skill` tool whose description renders the available-skills catalog from a
`Backend`, returning the full `SKILL.md` body on invocation. We reuse it rather than building a
custom tool. The one gap — eino's filesystem backend takes a single base directory while onclaw
needs three in precedence order — is closed by a thin custom `skill.Backend`.

Every named ecosystem (skills.sh, Claude Code, `anthropics/skills`) publishes the same shape —
a directory with `SKILL.md` (YAML frontmatter `name` + `description`) — so compatibility is a
normalize-on-install step, not a per-source reimplementation.

## What Changes

- **`internal/skill/` (new package):** a multi-directory `skill.Backend` (3-tier precedence:
  `<home>/workspace/<agent>/skills`, `<home>/workspace/<agent>/.agents/skills`, `<home>/skills`),
  a `BuildMiddleware` helper that no-ops when no skill dirs exist, frontmatter parse/normalize,
  recursive `Discover`, and an `Installer` with stdlib-only source adapters (GitHub/skills.sh
  tarball, HTTP archive, local copy, Claude-plugin extract).
- **Agent wiring:** `AssembleAgent` appends the skill middleware to its handler chain. **Zero
  signature change** — it reuses `userConfigDir` (the onclaw home) + `agentConf.Name`. Inline
  mode only (eino errors on fork under `*schema.AgenticMessage`).
- **Store:** a SQLite `SkillStore` ledger (source, sourceType, version, hash, scope, cached
  description, timestamps) following the existing 3-file pattern. Runtime reads disk only; the
  DB is the management ledger.
- **Compatibility:** normalize-on-install to the `SKILL.md` + frontmatter contract — force the
  `SKILL.md` filename, complete missing `name`/`description`, strip `context: fork*`, preserve
  unknown frontmatter, copy bundled files. Multi-skill sources namespace `<package>:<skill>`.
- **Install UX:** two-phase **discover → select → install**. One skill → bare name; many →
  interactive multi-select (CLI TTY) / discover+install API / two-step web modal. Re-install is
  an idempotent upsert (no-op / update / collision-error).
- **CLI:** `onclaw skill install/list/show/remove/update`.
- **API + web:** `/api/skills` (+ `/discover`) behind existing auth, and a Skills tab modeled on
  Providers.

## Capabilities

### New Capabilities

- `agent-skills`: resolve, load (progressive disclosure), install (with source normalization +
  idempotent upsert), and manage skills for agents — via CLI, JSON API, and web console.

### Modified Capabilities

- _(none.)_ Skills API routes inherit the existing `web-ui` auth/JSON requirements; the
  agent-assembly wiring is an implementation detail of `agent-skills` and does not alter
  `agent-core`'s run-loop / context-budget / cancellation behavior.

## Impact

- **Code:** new `internal/skill/` package; `SkillStore` in `internal/store/{types,store}.go` +
  `internal/store/sqlite/{db,skill,skill_test}.go`; `internal/agent/agent.go` (append middleware);
  `internal/cli/skill_cmd.go` + `context.go` + `app.go`; `internal/api/{service,handler,routes,server}.go`
  + serve wiring; `web/src/components/Skills.tsx` + `App.tsx`.
- **API:** new `GET /api/skills`, `POST /api/skills/discover`, `POST /api/skills`,
  `GET/DELETE /api/skills/{name}`, `POST /api/skills/{name}/update`.
- **CLI:** new `onclaw skill ...` command group.
- **Database:** new `skills` table (idempotent migration; PK `name`).
- **Build:** promote `gopkg.in/yaml.v3` from indirect to direct; **no other deps**;
  `CGO_ENABLED=0` and ARM cross-compile unchanged; `make ui` adds the Skills tab.
- **Compatibility:** additive. Agents with no skill dirs behave exactly as before. The gitignored
  root `skills-lock.json` prototype is retired.