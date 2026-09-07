# workspace-skills-ui — Design

## Context

Skills execute end-to-end today: three-tier disk discovery (`internal/agents/backend/skill_backend.go`) feeds the eino skill middleware (progressive disclosure via the `skill` tool), system skills are embedded and synced at startup, and the filesystem jail (`internal/agents/backend/fs_jailed_backend.go`) plus the jailed shell (`backend/shell.go`, scrubbed env, fixed PATH) bound execution to the agent directory. What does not exist: any persistence for workspace skills (no registry, no API — the previously removed `/workspaces/:slug/skills` routes left `api.ts` orphaned), any install path beyond hand-placing directories, any dependency story, and any explicit invocation. The agent carries a `DisabledSkills` denylist that the settings UI misrepresents as an allowlist. Motivation: see proposal.md; behavior contract: the delta specs.

## Goals / Non-Goals

**Goals:**
- One registry (`workspace_skills` table) + file bodies on disk; install via author / upload / git-URL / fork behind one pipeline
- Tier-governed activation with zero per-agent skill state; `disabled_skills` deleted everywhere
- Dependency declare → infer → resolve → provision (python venv) → verify, with re-check
- `$name` explicit invocation at the runner seam, all execution surfaces
- Read-only jail reachability for the workspace skills tree; venv PATH injection for the shell
- Rebuild the three UI touchpoints (settings pane, agent Step 3, composer) on the real API

**Non-Goals:**
- Node/Go dependency provisioners (interface seam reserved; python ships first)
- MCP-server dependencies (`dependencies.mcps`), marketplace browsing/refresh UX
- Skill usage analytics (the old pane's run counts die with the mock)
- Per-agent skill assignment of any kind (deliberately rejected product decision)

## Decisions

**D1 — Registry rows in Postgres, bodies on disk.** `workspace_skills` rows carry name (unique per workspace, DNS-label + reserved list shared with agents/workspaces), description, version, source, `enabled`, `dependencies` (JSONB), timestamps; bodies stay multi-file under `<ONCLAW_DIR>/workspaces/<slug>/skills/<name>/`. Alternative (all-DB bodies) loses bundled `scripts/`/`references/` that the middleware already serves by absolute path; all-disk loses the master switch and queryability. System tier stays unregistered (embedded); agent tier stays unregistered (disk presence *is* its state) — no migration for either.

**D2 — Tier activation replaces the denylist (BREAKING).** The middleware attaches: system always; workspace by registry `enabled`; agent by presence. `domain.Agent.DisabledSkills`, `skillBackend` filtering, and the workspace-creation `StarterAgent.DisabledSkills` param are deleted; agent payloads ignore `disabled_skills` like managed fields. The default-on alternative (denylist) was analyzed and rejected: it pollutes every agent's progressive-disclosure context with every skill's metadata and contradicts the tools mental model; the fleet-admin desire is served by install + enable being one action. Volume control is the workspace master switch — accepted trade-off, recorded in the proposal.

**D3 — skillBackend becomes registry-aware.** `NewSkillBackend` loses `disabledSkills` and gains a read-only dependency on a workspace-skill-state reader (list of enabled workspace skill names). Name collision precedence (agent > workspace > system) and the three-directory walk stay as-is.

**D4 — One install pipeline, four sources.** Validate → unpack/clone → sanitize → write files → insert row → dependency step. Order matters: files land before the row so a crashed install leaves at most an orphan directory, never a phantom row; row-insert failure removes the written tree. Sanitization: zip-slip rejection (resolve-then-prefix-check against the target dir, symlinks included), per-archive size/file-count caps, `SKILL.md`-at-root required. Git: `clone --depth 1 --single-branch`, no submodule recursion, `file://` refused, token used for the fetch only. URL/git targets are resolved against private/internal ranges (SSRF guard) — real risk on LAN self-hosts. Name slugification reuses the domain slug rules. Same-name import = overwrite-with-confirm: replace tree, bump version, update row; never two skills with one name.

**D5 — Dependencies: declare, infer, provision.** Frontmatter extension `dependencies.{tools,binaries,python}` parsed by our own skillBackend parser (eino's `FrontMatter` untouched); `requirements.txt` and body/imports feed inference at import; the resolved set is stored on the row (JSONB) so reports re-render without re-parsing and imported files stay byte-faithful. Tool deps resolve against the workspace tool gate + agent allowlists with the pre-checked "enable everywhere" option (gate write + bulk agent allowlist update in one confirm); `scripts/` non-empty implies the reserved `execute` name. Binaries: `exec.LookPath` probes, never auto-installed (server process lacks root; report names per-platform commands). Python: one shared venv per workspace at `workspaces/<slug>/skills/.venv`, pip-installing the **union** of all enabled skills' requirements — per-skill venvs can't share one honest PATH; conflicts are named and fail the step. Provisioning is one `DependencyProvisioner` interface (python first registration), per the plugins-first-class rule. Re-check re-probes LookPath + import and updates the row.

**D6 — Jail gains read-only extra roots (also fixes a live bug).** `fsJailedBackend` gains optional read-only roots; `resolvePath` accepts absolute paths resolving under the primary or an extra root (EvalSymlinks + prefix check per root, existing symlink policy unchanged); `Write`/`Edit` remain primary-root-only — an agent writing into the skills root could plant a skill that auto-flows workspace-wide (privilege escalation), so read/execute yes, write never. This also repairs an existing defect: the middleware instructs models to open bundled files by *absolute path*, which the jail rejects unconditionally today — for every tier. Shell needs no jail change: `NewJailedShell` gains an option prepending `skills/.venv/bin` to the scrubbed PATH.

**D7 — `$name` parsing lives in the runner.** One seam (`ExecRequest` input preprocessing) so chats, channels, and cron all honor it. Matching tokens (system / enabled-workspace / owning-agent tier) inject a blocking instruction — mirroring the middleware's own "invoke before responding" tool language — ahead of the user message; the middleware stays the single execution path so fork/`fork_with_context`/model-override frontmatter and tool-call cards keep working. Alternative rejected: injecting skill bodies directly (duplicates middleware state, breaks fork semantics). Non-matching tokens pass through untouched.

**D8 — API and permissions.** Routes return under `/workspaces/{slug}/skills` (list incl. locked system tier; create/update/enable/delete) and agent-scoped skill install/remove under the agent endpoints; permission guard middleware reuses the catalog as-is (`skills.read` list / `skills.write` mutate; Member 403). Store access follows the DI rules: granular sub-interface (`store.WorkspaceSkillStore`) as a positional parameter; the install service is the transaction seam (row + files + tool-gate writes).

**D9 — UI reuses established patterns.** Settings pane: real-API library + three-step install wizard + locked System section (source of truth: the delta scenarios; visual language per `web/Web-Prototype` tokens). Agent Step 3: tool chips unchanged; skills become locked chips + Agent-skills section. Composer: `$` menu clones the regex-trigger/arrow-nav/pick-replaces mechanics of `SlashMenu`/`MentionMenu`; disabled/absent skills simply don't list. Seed data updates to the new shapes; `api.ts` skills CRUD rewires to the live contract.

## Risks / Trade-offs

- [Every enabled skill's metadata lands in every agent's context — token cost scales with library size] → accepted (D2); master switch is the volume control; wizard review step shows the metadata the agent will carry
- [`pip install` executes package setup code on the server] → same trust boundary as running the skill itself; admin-only via `skills.write`; declared requirements only, no custom index URLs, hard timeout, download size cap
- [SSRF via git/URL import on LAN deployments] → resolve-and-refuse private ranges; `file://` refused; re-evaluate with an allowlist if a real need emerges
- [Workspace skill bundled files were already unreachable (jail rejects absolute paths) — this change fixes it, but agents gain read on the whole skills tree] → read-only roots, symlink policy per root, no write; tenant isolation holds (skills root is inside the tenant's own subtree)
- [Jail/shell behavioral change could break existing agents' file expectations] → extra roots are additive; all existing escape tests must pass unchanged; new tests cover absolute-path read, skills-root write rejection, venv PATH
- [BREAKING column/field removal vs old deployed UI] → server ignores `disabled_skills` on input, so old UI keeps working against the new server; the reverse pairing (new UI, old server) 404s on skills routes — deploy web + server together (they already ship as one artifact)
- [python3 absent on the host] → guidance + re-check path is first-class; scripted skills install with a persistent warning chip instead of failing

## Migration Plan

1. New migration: `workspace_skills` table (up/down pair). No backfill — the library starts empty; system skills need no rows.
2. Same migration set drops `disabled_skills` from agents (down recreates it). Data loss accepted: the denylist is inert in the product today (no UI wrote it; runtime honored it but nothing populated it in practice).
3. Deploy order: server + web together (single release artifact). Rollback: down-migration restores the column; rollback restores the old binary — old server ignores nothing (field is unknown → rejected by strict decoders? no: the old server rejects unknown `skills` routes, which the rolled-back UI no longer calls).

## Open Questions

None blocking. Deferred tunables (archive size caps, venv pip timeout defaults) land as constants in the install service and can be adjusted without touching specs or structure.
