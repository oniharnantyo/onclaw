## 1. Registry: migration, domain, store

- [x] 1.1 Migration `workspace_skills`: table (workspace FK, name unique per workspace, description, version, source, enabled, dependencies JSONB, timestamps) + down migration; drop `disabled_skills` from agents in the same set (down recreates)
- [x] 1.2 Domain: workspace-skill entity + validation (DNS-label slug with reserved list shared with agents/workspaces, source enum, version shape, dependencies struct with tools/binaries/python)
- [x] 1.3 `store.WorkspaceSkillStore` sub-interface + fake implementation (CRUD, list by workspace, set enabled) wired per the granular-DI rules; Postgres adapter using pgxpool with workspace-scoped queries only
- [x] 1.4 Integration tests for the Postgres adapter (TEST_DATABASE_URL): tenant-scoped CRUD, unique-name constraint, enabled toggle round-trip

## 2. Runtime: skillBackend and jail

- [x] 2.1 Rework `skillBackend`: drop `disabledSkills`, take an enabled-workspace-skills reader; keep precedence agent > workspace > system; unit tests for tier gating incl. master-switch off → workspace tier absent
- [x] 2.2 Extend frontmatter parsing in `skillBackend` to extract `dependencies.{tools,binaries,python}` alongside description (tests: declared, undeclared, malformed frontmatter)
- [x] 2.3 `fsJailedBackend` read-only extra roots: `NewFilesystemJailWithRoots`, absolute-path resolution under primary or extra roots, per-root symlink checks, Write/Edit still primary-only; port all existing escape tests unchanged + new cases (skills-root read OK, skills-root write rejected, symlink escape rejected)
- [x] 2.4 `NewJailedShell` option prepending the workspace skills venv bin dir to the scrubbed PATH; test that `python3` resolves to the venv interpreter when present
- [x] 2.5 Runner wiring: pass `domain.WorkspaceSkillsDir` as the extra jail root and the enabled-skills reader into `SkillsConfig` (drop `DisabledSkills` from the config struct)

## 3. Install pipeline

- [x] 3.1 Install service skeleton: validate → materialize → write files → insert row → dependency step; orphan-tree cleanup on row failure; slug collision = overwrite-with-confirm (tree replace, version bump, row update)
- [x] 3.2 Author source: name/description/body → SKILL.md synthesis (version 0.1.0)
- [x] 3.3 Upload source: zip/folder unpack with zip-slip rejection, symlink rejection, size and file-count caps, SKILL.md-at-root requirement; table-driven tests for hostile archives
- [x] 3.4 Git/URL source: shallow single-branch clone + archive fetch, ref and one-time token support, private-range SSRF refusal, skill-directory discovery (multi-select), tree scan for SKILL.md dirs
- [x] 3.5 Fork source: copy embedded system skill to workspace tier, registry row with source `fork`
- [x] 3.6 Dependency inference at import: requirements.txt + import scan + body tool-name patterns; inferred set stored on the row without modifying imported files

## 4. Dependency resolution and provisioning

- [x] 4.1 `DependencyProvisioner` interface + registry; status model per dependency kind (met / missing / unprovisioned) stored on the row and re-checkable
- [x] 4.2 Python provisioner: ensure shared workspace venv, pip install union of enabled skills' requirements (timeout, size cap, no custom index), import verification; conflict naming across skills fails the step without touching installed skills
- [x] 4.3 Binary checks: LookPath probe, per-platform install-command hints, re-check updates the row
- [x] 4.4 Tool dependency resolution: "enable everywhere" writes the workspace tool gate + bulk agent allowlist update in one service call (transaction seam); agent-tier scope = owning agent only; `scripts/` presence implies `execute`

## 5. Invocation: $name at the runner

- [x] 5.1 Input preprocessing on ExecRequest: `$name` token match against available tiers (system / enabled workspace / owning agent), blocking-instruction injection ahead of the user message, non-matching tokens untouched
- [x] 5.2 Tests: direct-chat invocation forces skill tool call; cron-prompt path honors it; disabled/unknown `$name` stays plain text

## 6. HTTP API and agent schema

- [x] 6.1 Handlers + routes: `GET/POST/PUT/PATCH/DELETE /workspaces/:slug/skills` (list incl. locked system tier; create per source; update; enable/disable; delete) with permission guards (skills.read / skills.write, Member 403)
- [x] 6.2 Agent-scoped skill install/remove endpoints (same guards) writing the agent-tier directory
- [x] 6.3 BREAKING agent cleanup: remove `DisabledSkills` from `domain.Agent`, API payloads, and workspace-creation `StarterAgent`; `disabled_skills` in any payload is ignored; update all affected tests

## 7. Web: API layer and settings Skills pane

- [x] 7.1 `api.ts`: rewire skills CRUD + dependency/re-check endpoints to the live contract; drop the orphaned mock shapes; seed data updated
- [x] 7.2 SkillsPane rebuild: library rows (name, version chip, source badge, dependency chip, master toggle, edit, uninstall confirm), toasts for enable/disable-everywhere, read-only rendering for non-writers
- [x] 7.3 Install wizard: source step (Author/Upload/Git-URL/Fork entry), content step (body editor / archive drop with tree preview / URL+ref+discovered-skill selection), dependency review step (enable-everywhere checkbox, binary hints with copy + re-check, python provision checkbox)
- [x] 7.4 System skills section: locked always-on entries with Fork-to-workspace action
- [x] 7.5 Vitest coverage for the pane + wizard (touched suites only, per repo convention)

## 8. Web: agent wizard and composer

- [x] 8.1 Step 3 skills inventory: locked chips (system + enabled workspace) with Settings hint, Agent-skills add/remove for writers, inline dependency warning pointing at tool chips
- [x] 8.2 Composer `$` menu: regex trigger, grouped listing (System/Workspace/This agent) from the skills API, arrow-key nav, pick inserts `$name ` token; disabled skills absent
- [x] 8.3 Vitest coverage for Step 3 inventory and the `$` menu

## 9. End-to-end verification

- [x] 9.1 Smoke test: rewrite smoke.sh section 13.1 against the new skills contract (install author skill, enable/disable, list incl. system tier, Member 403); full suite green
- [x] 9.2 `go build ./...`, `go vet ./...`, `go test ./...`, integration suite, `pnpm build` + touched vitest suites
- [ ] 9.3 Manual journey pass: author install → dependency report → live on agents → `$skill` in chat renders tool-call card → disable → gone everywhere → re-enable
