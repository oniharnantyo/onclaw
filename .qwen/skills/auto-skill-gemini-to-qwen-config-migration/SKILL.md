---
name: gemini-to-qwen-config-migration
description: Move a project's Gemini-CLI config (.gemini/ commands, skills, settings) into Qwen Code's .qwen/ directory, handling git tracking, duplicate skill discovery, and bundled extension settings.
source: auto-skill
extracted_at: '2026-07-11T06:22:49.405Z'
---

# Gemini-CLI → Qwen Code project-config migration

Use when a project accumulated config under `.gemini/` (commands, skills, `settings.json`) while running Gemini CLI, and you now run it under **Qwen Code** (which reads `.qwen/`) — e.g. "apply all oma settings to `.qwen` rather than `.gemini`".

## Why this needs care
The naive `mv .gemini .qwen` produces silent breakage:
- Files under `.gemini/` are usually **git-tracked**; a plain `mv` loses history and shows as delete+untracked.
- Qwen Code **auto-discovers skills/commands from multiple dirs** (`.gemini/skills`, `.claude/skills`, `.agents/skills`, `.qwen/skills`). Copying creates **duplicate skill names** in the registry.
- Gemini's `settings.json` schema differs from Qwen's (`previewFeatures` etc. are Gemini-CLI keys; Qwen uses `permissions.allow` + `$version`). Don't blindly copy keys.
- The OmA extension bundles its **own** `.gemini/settings.json` inside the install dir (`~/.qwen/extensions/oh-my-antigravity/.gemini/settings.json`) — that's a shipped asset; editing it is overwritten on update.

## Procedure
1. **Confirm the runtime.** Check for `.qwen/` (Qwen Code) vs `.gemini/` (Gemini CLI). Look for `qwen-extension.json`/`gemini-extension.json` in the extension and project root.
2. **Inventory the project `.gemini/`.**
   - `find .gemini -maxdepth 3`
   - Classify: `commands/` (workflow commands, e.g. `opsx/*`), `skills/` (project skills), `settings.json` (runtime settings).
   - Separate the **bundled extension** `.gemini/settings.json` (inside `~/.qwen/extensions/...`) from the **project** one — never hand-edit the bundled file.
3. **Inspect existing `.qwen/`.** Typically holds `settings.json` (permissions schema). Note its current contents before merging anything.
4. **Check git tracking.** `git ls-files .gemini` — if tracked, a move needs `git mv` to preserve history; a copy keeps the source tracked.
5. **Decide move vs copy — confirm with the user.**
   - **Move (git-history-preserving):** `git mv .gemini/commands .qwen/commands`, `git mv .gemini/skills .qwen/skills`, then `rmdir .gemini` once empty.
   - **Copy (source kept):** `cp -R .gemini/commands .qwen/commands`, `cp -R .gemini/skills .qwen/skills`. Source `.gemini/` stays; nothing staged.
6. **Handle settings separately.** Don't cargo-cult Gemini keys into `.qwen/settings.json`. Only fold in values that are meaningful to Qwen Code (e.g. permissions). Treat `.gemini/settings.json` as legacy.
7. **Verify.**
   - Reload the session so Qwen Code re-discovers `.qwen/skills`.
   - Watch for **duplicate-skill warnings** in the registry (expected after a copy; eventual fix = drop `.gemini/` once `.qwen/` is confirmed working).
   - `git status` to review what got staged/untracked.

## Gotchas recap
- Copying → duplicates; plan to retire `.gemini/` later.
- `git mv` stages changes but does **not** commit — let the user commit.
- Bundled extension `.gemini/settings.json` is off-limits; override at project level.
- Qwen Code and Gemini CLI `settings.json` schemas are not interchangeable.
