# OmA Doctor — Diagnostics + Fix Log

- **Scope:** default (extension health, status)
- **Runtime:** Qwen Code (not Gemini CLI) — protocol adapted from `.gemini`/`.omg` to `.qwen`
- **Extension root:** `/Users/oniharnantyo/.qwen/extensions/oh-my-antigravity` (v0.9.2)
- **Date:** 2026-07-11

## Doctor Result

**HEALTHY — integration fixed.**

OmA commands and deep-work skills are now invocable in Qwen Code.

## Root Cause (pre-fix)

| # | Area | Finding |
|---|------|---------|
| 1 | Command readiness | `oma:*` commands/skills were not surfaced as callable tools. Qwen Code loads the extension's always-on context (`GEMINI.md` → `oma-core.md`) but does not auto-register the extension's `commands/oma/` or `skills/`. |
| 2 | Skill frontmatter | OmA skill `SKILL.md` files use **TOML** frontmatter (`name = "..."`), which Qwen Code's YAML skill parser rejects — skills were silently skipped. (Commands used YAML `description:`, so they parsed once discovered.) |
| 3 | Discovery paths | Qwen Code scans **project-level** `.qwen/skills/` and **user-level** `.qwen/commands/`, but **not** user-level `.qwen/skills/`. |

## Fix Applied

1. **Commands** — bridged into user-level `~/.qwen/commands/oma/` as file symlinks to the
   extension's `commands/oma/*.md`. They now register as `/oma:<name>` (short names the user
   expects). The extension also auto-registers them natively as `oh-my-antigravity.oma:<name>`
   (both paths resolve to the same files; harmless alias, no divergence since both are symlinks
   into the extension).
2. **Skills** — bridged into project-level `.qwen/skills/<name>/` as real directories with the
   `SKILL.md` frontmatter **converted TOML → YAML** (body kept verbatim; skills are single-file).
   Source of truth remains the extension; only the frontmatter wrapper was normalized for Qwen Code.
3. **Stale wiring** — removed `Bash(gemini *)` (Gemini-CLI leftover) from
   `projects/onclaw/.qwen/settings.json`. `Skill(oma:doctor)` kept (now resolves).

## Verification

- `Skill(oma-plan)` → loads successfully. ✅
- `oma:doctor` / `oh-my-antigravity.oma:doctor` → registered. ✅
- All 11 deep-work skills (`plan`, `oma-plan`, `execute`, `prd`, `ralplan`, `research`,
  `deep-dive`, `context-optimize`, `ultragoal`, `blueprint`, `learn`) bridged. ✅

## Notes / Tradeoffs

- User-level `.qwen/skills/` is NOT scanned by Qwen Code; skills live in project `.qwen/skills/`.
  If OmA is needed in other projects, repeat the skill bridge there (or request native skill
  registration from the extension).
- Symlinks keep the extension as the single source of truth; an extension update flows through
  automatically. The only hand-managed artifact is the YAML frontmatter in the bridged `SKILL.md`.
- Nested `.gemini/settings.json` inside the extension remains (harmless Gemini-CLI import leftover).
- Home `.qwen/settings.json` `env` block still holds plaintext API keys — rotate/move to keychain
  for hygiene (values withheld).

## Recommended Next Command

- Run an OmA flow: `/oma:doctor`, `/oma:plan`, `/oma:team`, etc.
- For deep-work skills, invoke by name: `oma-plan`, `prd`, `execute`, ...
