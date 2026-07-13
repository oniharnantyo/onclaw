# Proposal: Revise Shell Tool Policy (permissive-by-default with a catastrophic denylist)

## Intent
Make the shell tool **loose by default** so the agent is no longer blocked on ordinary
tasks, while still refusing the small set of commands that are catastrophically destructive or
that open an external execution channel. Tool-call **confirmation is deferred** — this change
ships auto-deny of the catastrophic floor only; an interactive approval flow is a separate later
change.

The default policy flips from `allowlist` (default-deny, narrow) to a new **`denylist`** mode
(default-allow), gated only by a small, curated set of catastrophic-command patterns matched
against the **full command string**.

## Problem
- The current default is `allowlist` with ten binaries (`ls, cat, git, go, make, npm, node,
  python3, python, docker`). Most commands an agent reaches for are refused — `echo`, `find`,
  `grep`, `mkdir`, `curl`, `sed`, `awk`, `head`, `tail`, `diff`, `jq`, … — so the agent is
  constantly "blocked" and burns `MaxIterations` retrying variants against a wall it cannot see.
- `isAllowedCommand` (`internal/agent/tools/shell.go`) inspects **only the first non-env token**
  of the command. This is wrong in both directions:
  - **Too strict:** any compound command whose leading token is a shell builtin or an unlisted
    binary is blocked even when the real work binary is allowed — e.g. `cd web && npm run build`
    (leading token `cd`, a builtin) is refused.
  - **Too permissive:** the first-token gate cannot see pipes, `&&`, `;`, or `$()`, so
    `git log | grep secret` runs in full because `git` is allowlisted. Composition is invisible
    to the policy.
- `ask` mode blocks on `os.Stdin` (`fmt.Fscanln`). In the web/SSE path there is no stdin, so
    `ask` either hangs the request or immediately refuses — it is unusable in the UI onclaw
    actually ships.

## Proposed Solution
**New policy mode `denylist` (the new default).** Everything runs except commands that match a
curated catastrophic pattern. The existing `deny`, `allowlist`, and `ask` modes are unchanged and
remain available as opt-ins.

**Full-command evaluation.** Patterns are matched against the **entire** command string, not the
first token. This makes composition (pipes, `&&`, redirections) visible to the policy — retiring
both the "compound blocked at token 1" and "pipe bypasses the gate" failures at once.

**A small catastrophic floor** — three buckets, ~8 pattern families:
1. **Mass-destruction / halt** — recursive forced deletion of a broad target (`rm -rf /`, `~`,
   `$HOME`, `*`), `mkfs`, `dd` writing to a block device, fork bombs, and `shutdown`/`reboot`/
   `halt`/`poweroff`.
2. **Remote code-execution pipes** — a download via `curl`/`wget` piped into an interpreter or
   shell (`curl … | sh`, `… | bash`, `… | python`).
3. **Reverse shells / covert channels** — `/dev/tcp/`, `/dev/udp/`, `bash -i` redirected to a
   socket, `nc`/`ncat` with an execute flag, `mkfifo`-based reverse-shell shapes.

**Behaviour on match.** A matched command is **not executed**; the tool returns a blocked result
that **names the matched pattern/category** (so the agent does not blindly retry), and the match
is **logged** (command + matched category) for audit.

**Configurable.** The pattern set defaults to the floor above and is overridable via
`tools.shell.denylist` (and `ONCLAW_TOOLS_SHELL_DENYLIST`) using the normal config layering
(`defaults < config file < ONCLAW_* env < CLI flags`), exactly like the existing allowlist.

## Constraints & Dependencies
- **Threat model is explicit and narrow.** The denylist raises the bar against **accidental**
  and **prompt-injected** harm (e.g. a workspace README instructing `curl … | sh`). It is **not**
  a defence against a determined adversary: variables, `eval`, `base64`, heredocs, and `$(…)`
  dissolve literal patterns. This is acceptable for the stated intent and is recorded in
  `design.md`; sandboxing (seccomp/namespace) or removing shell is the answer for the adversarial
  case and is explicitly out of scope.
- **Existing modes preserved.** `deny`, `allowlist`, and `ask` keep their current semantics; only
  the **default** value changes (`allowlist` → `denylist`) and a new mode is added.
- **Low-resource target.** Matching is a small set of compiled patterns run once per shell call —
  no parser, no subprocess, negligible footprint on a ~2 GB RAM device.
- **Compatibility with `ask`.** `ask` remains CLI/stdin-only in this change. A web-compatible
  (async, SSE-based) confirmation flow is deferred — see below.
- **Existing config parsing.** The denylist reuses the comma-separated array parsing already
  specified for the allowlist (`env-config`).

## Out of Scope (Deferred)
- **Tool-call confirmation / `ask` on the web UI.** The async approval primitive (generalising
  the memory subsystem's staged-write approve/reject flow; Eino interrupt/resume) is its own
  change and is the prerequisite for making `ask` usable beyond the CLI.
- **The remaining denylist categories** from the broader taxonomy (env-injection, persistence,
  package-install, crypto-mining, network-recon, filter-bypass-via-tool-flags, env-dump, etc.).
  These are not catastrophic-floor material and are deferred until confirmation exists, so they
  can be `ask`-tier rather than hard-deny.
- **Per-agent shell policy.** Today policy is global (`store.Agent` has no shell-policy field).
  Per-agent/per-workspace policy is a separate change.
- **Known gaps accepted under the narrow threat model:** `docker run --privileged` (container
  escape — `docker` is no longer gated by an allowlist, but it is not in the catastrophic floor
  either), and `ONCLAW_*`/`GOCLAW_*` environment-variable secret exfiltration. Neither is
  catastrophic-floor nor adversary-defence; both are revisited when confirmation lands.
- **Argument/flag-aware matching** (e.g. distinguishing `git log` from `git push --force`). The
  floor uses full-string pattern matching, not a shell parser.
