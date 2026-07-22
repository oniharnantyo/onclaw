---
name: oma-extension-in-qwen
description: How to run OmA (oh-my-antigravity) `oma:*` commands in Qwen Code when they aren't invocable as skills — read the command file and execute manually, adapting .gemini/.omg paths to .qwen.
source: auto-skill
extracted_at: '2026-07-11T06:32:09.108Z'
---

# Running OmA (`oh-my-antigravity`) commands inside Qwen Code

## When to use
- A user invokes `/oma:<command>` or `Skill(oma:<command>)` (e.g. `/oma:doctor`, `/oma:status`, `/oma:team`) in a **Qwen Code** session.
- The OmA extension is installed at `~/.qwen/extensions/oh-my-antigravity/` (or `<project>/.qwen/extensions/...`), built originally for **Gemini CLI**.
- Qwen Code loads the extension's always-on *context* (its `GEMINI.md` → `context/oma-core.md`, imported into the system prompt) but does **NOT** register its `commands/` or `skills/` as callable `Skill(...)` tools.

## Symptom (how you know)
- Invoking the skill returns `Skill "oma:<command>" not found` even though `commands/oma/<command>.md` exists on disk.
- `settings.json` sometimes contains stale Gemini-CLI permissions: a `Skill(oma:doctor)` allow entry or `Bash(gemini *)` — both non-resolving under Qwen Code.

## Procedure
1. **Confirm the runtime gap.** Look for `qwen-extension.json` + `GEMINI.md` at the extension root and confirm `oma:<command>` is absent from the active skill registry. That proves Qwen Code surfaces the context but not the commands.
2. **Do NOT retry `Skill(oma:<command>)`** — it will keep failing identically.
3. **Read the command's protocol file directly** and follow its numbered steps manually:
   `<extension-root>/commands/oma/<command>.md` (e.g. `.../oh-my-antigravity/commands/oma/doctor.md`).
4. **Adapt Gemini-CLI paths to Qwen Code** while executing (the protocol is written for Gemini CLI):
   - `.gemini/` → `.qwen/`
   - `.omg/state/` → `.qwen/state/`
   - `gemini extensions list` / `gemini extensions ...` recovery commands → there is no `gemini` CLI here; operate on the filesystem extension path or use Qwen's extension mechanism instead.
5. **Run the steps by inspecting the filesystem** (list_directory / read_file / glob), not by calling a skill.
6. **Write any state output** (e.g. `doctor.md`) to `.qwen/state/` instead of `.omg/state/`.

## What the OmA doctor protocol actually checks (useful template)
- Extension surface integrity: `agents/`, `commands/oma/`, `skills/`, `context/oma-core.md`, `qwen-extension.json`, `GEMINI.md` import chain (`GEMINI.md` → `@./context/oma-core.md`).
- No duplicate OmA roots; no project-level `.gemini/` shadowing the extension.
- Retained deep-work skills present with valid `SKILL.md` frontmatter (`name:` + `description:`).
- State dir hygiene (`hud.json`, etc.) and absence of stale `.omg/` artifacts.
- Secrets hygiene: **never print** API keys found in `env` blocks of `settings.json`.

## Remediation after a run
- Remove stale Gemini-CLI permissions (`Skill(oma:doctor)`, `Bash(gemini *)`) from the project `.qwen/settings.json`.
- Treat OmA as **context-only + manual command-file execution** until/unless Qwen Code surfaces extension `commands/` as slash commands.

## Why this is the right path
The OmA extension's value in Qwen Code is its always-on role/workflow context and its command *recipes* (the `.md` files). Calling them by name is unavailable, but the recipes are plain markdown you can read and execute — so the capability is preserved, just invoked manually.
