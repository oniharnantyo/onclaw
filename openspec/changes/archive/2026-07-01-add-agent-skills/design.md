# Design — agent-skills

## Why reuse eino's skill middleware

eino's `adk/middlewares/skill` (present in the pinned `v0.10.0-alpha.9`) already implements the
loader we need. As a `TypedChatModelAgentMiddleware`, its `BeforeAgent` appends a "Skills System"
instruction and a `skill` tool; the tool's `Info()` renders the `<available_skills>` catalog from
`Backend.List()`, and `InvokableRun` returns the full `SKILL.md` body on demand. That is exactly
progressive disclosure, and it composes with onclaw's existing summarization + history
middlewares. Building a custom tool would duplicate this and risk drifting from eino's contract.

onclaw uses `*schema.AgenticMessage`, so we instantiate with
`skill.NewTyped[*schema.AgenticMessage]`. eino explicitly errors on fork / fork_with_context for
`AgenticMessage`, so installed skills are normalized to inline mode.

## The one gap: multi-directory resolution

eino's `NewBackendFromFilesystem` takes a single `BaseDir`. onclaw resolves skills from three
directories in precedence order (agent-scoped twice, then global). We implement `skill.Backend`
directly (`List` / `Get`) over `os` / `filepath`, scanning immediate subdirs for `SKILL.md`,
parsing frontmatter, and deduping by name with first-directory-wins. This avoids depending on
eino's heavier `filesystem.Backend` sandbox abstraction (~100 LOC, fully decoupled).

## Runtime is disk-only; the DB is a ledger

The agent reads skills from disk via the backend; it never touches SQLite. The `SkillStore` is
purely the install ledger (provenance + hash + scope + cached description) used by management
(CLI/API/UI) and by idempotency/change detection. This keeps the agent path dependency-light and
means "update in place" is just overwriting a directory — the next turn re-reads from disk, no
invalidation. 

Skill-dir resolution reuses `userConfigDir` (already `~/.onclaw`) + `agentConf.Name`. Installation for agent-specific scopes targets the Tier-2 directory (`~/.onclaw/workspace/<agent>/.agents/skills`) to keep all installed agent-specific artifacts encapsulated under `.agents/` inside the workspace folder, while Tier-1 (`workspace/<agent>/skills`) remains available for manual user-managed overrides or developer-checked-in code. Both are fully resolved at runtime, ensuring no signature changes are required for `AssembleAgent` and existing tests are unaffected.

## Compatibility = normalize-on-install

All target ecosystems publish the same shape (`<dir>/SKILL.md` with YAML frontmatter), so
compatibility is a normalization pipeline, not per-source code:

- filename → `SKILL.md` (case-sensitive glob on Linux);
- complete `name` / `description` if missing (synthesize from dir / first line, with a warning);
- drop `context: fork*`;
- preserve unknown frontmatter keys (eino ignores them);
- copy bundled files verbatim so relative paths resolve via the base directory eino returns.

skills.sh resolves to GitHub `owner/repo` (verified: `npx skills add <owner/repo>`; the skills.sh
leaderboard maps each skill to a repo such as `frontend-design` → `anthropics/skills`), so the
GitHub-tarball adapter covers it without a separate provider.

## Two-phase discover → select → install

A source may contain one skill (`vercel-labs/skills` → `find-skills`) or many (`anthropics/skills`
→ 17). Discovery is always automatic; selection is interactive: the CLI (on a TTY) shows a
multi-select, the API splits into `discover` + `install(selected)`, the web modal shows checkboxes
+ "Select all". Naming: a source with >1 skill namespaces every skill `<package>:<skill>`
(collision-free); a single-skill source uses the bare name. `<package>` is the plugin manifest
`name`, else the `owner-repo` slug, else the archive/dir basename.

## Idempotent upsert

Re-install compares each candidate's hash + source against the `SkillStore` row: same source +
same hash → no-op; same source + changed hash → update in place; different source (collision,
within a scope) → error with `--as` / `--force` remediation. Different scopes are independent
(coexist; agent-scope shadows global at runtime). `install <same-source>` is equivalent to
`update <name>`.

## Stdlib-only fetch; CGO-free preserved

Fetch uses `net/http`, `archive/tar`, `archive/zip`, `compress/gzip`, `os`, `filepath` — no
`go-git`, no `go-getter`. This keeps the binary small for the low-resource ARM targets and
preserves `CGO_ENABLED=0` cross-compilation. Full `git clone` of arbitrary non-GitHub URLs is a
fast-follow (would require `go-git`).

## Alternatives considered

- **Custom `load_skill` tool + prompt catalog.** Rejected — eino already provides this exact
  progressive-disclosure behavior; rebuilding it would duplicate the contract and the maintenance.
- **One tool per skill.** Rejected — pollutes the tool list and sends every skill description to
  the model each turn, bad for the 8192-token default budget.
- **Always inject full skill bodies into the system prompt.** Rejected — overflows the small
  default context window when many skills are installed.
- **`skills-lock.json` lockfile for install records.** Rejected — diverges from how onclaw stores
  every other entity (SQLite) and is harder for the web UI/SQL to query; a SQLite `SkillStore`
  matches `ProfileStore` / `AgentStore`.
- **eino `filesystem.Backend`-based backend.** Rejected — requires implementing eino's filesystem
  sandbox abstraction; a direct `List`/`Get` over `os` is less code and decouples onclaw.

## Out of scope (fast-follows)

- Per-skill enable/disable enforced at runtime (v1 runtime is disk-only; `remove` is the off
  switch).
- Native skills.sh registry-API resolver and `skills.sh/…` URL handling (v1: use `owner/repo`).
- Full `git clone` of arbitrary git URLs.
- Config keys for a default scope / disabled list.