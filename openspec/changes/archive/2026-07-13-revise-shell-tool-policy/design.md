# Design: Revise Shell Tool Policy

## Decision: permissive default + catastrophic denylist, confirmation deferred
Flip the default policy from `allowlist` (default-deny) to a new `denylist` mode
(default-allow), gated only by a small catastrophic pattern set matched against the **full
command string**. No confirmation in this change.

### Why flip the default, not widen the allowlist
Widening the allowlist keeps the evaluation model that causes both symptoms: first-token
inspection of a free-form string. Compound commands (`cd web && npm run build`) and shell
builtins can never be made to work cleanly under a first-token binary gate, and pipes
(`git log | grep secret`) are structurally invisible to it. A denylist evaluated against the
**whole** command string retires both failures at once:

```
  TODAY:  isAllowedCommand(cmd)   →  looks at token #1 only
                                    ├─ too strict:  "cd web && npm …" → "cd" not listed → BLOCK
                                    └─ too leaky:   "git log | grep …" → "git" listed → RUN all

  PROPOSED: matchesCatastrophic(cmd)  →  scans the full string
                                    ├─ "cd web && npm run build"   → no catastrophic match → RUN
                                    └─ "curl … | sh"               → RCE-pipe match        → DENY
```

### Threat model — and the honesty that follows
The denylist defends against **accidental** and **prompt-injected** harm — a cooperative agent
steered by a malicious workspace file — **not** against a determined adversary. An adversary who
reads the floor defeats it trivially: `$r -rf`, `b=sh; curl … | $b`, `eval $(base64 -d …)`, and
variable-wrapped rc-file writes all dissolve literal patterns. This is acceptable **because**:

1. The floor only needs to catch what is so bad we will not even *ask* — the high-value payloads
   an injection actually reaches for (`rm -rf /`, `curl|sh`, `/dev/tcp` reverse shells). Even
   substring matching catches the lazy/accidental case, which is the only case in scope.
2. Obfuscation that slips past the floor is the intended subject of the **deferred confirmation
   primitive** (direction D). When confirmation exists, "uncertain" commands are shown to a human
   who sees the *resolved* command; the denylist then only has to cover "deny without asking".

Until confirmation exists, the gap is explicit and documented, not hidden.

### Why three buckets, not the full taxonomy
A broader denylist (env-injection, persistence, package-install, crypto, recon,
filter-bypass-via-flags, env-dump, …) is (a) unbounded — every line has bypasses — and (b)
mostly **not catastrophic**: it is suspicious-but-recoverable, which is exactly what `ask`-tier
confirmation is for. Hard-denying `pip install` or `kill -9` would recreate the "agent blocked"
complaint under a new trigger. So this change ships only the auto-deny floor; the rest waits for
confirmation so it can be `ask`-tier.

### The catastrophic floor
- **Mass-destruction / halt:** `rm` with recursive+force intent against a broad target (`/`,
  `$HOME`, `~`, `*`); `mkfs.*`; `dd … of=/dev/(sd|nvme|disk|mmcblk)…`; fork bombs
  (`:(){ … }` / `: () {`); `shutdown`/`reboot`/`halt`/`poweroff`/`init 0`/`init 6`.
- **Remote code-execution pipes:** `curl`/`wget`/`fetch` output piped into an interpreter or
  shell (`sh`, `bash`, `zsh`, `dash`, `python`, `python3`, `perl`, `ruby`, `node`).
- **Reverse shells / covert channels:** `/dev/tcp/`, `/dev/udp/`; `bash -i` with socket
  redirection (`>&`, `0>&1`); `nc`/`ncat`/`netcat` with execute (`-e`/`-c`); `mkfifo`-based
  reverse-shell shapes.

### Matching engine
Compiled substring/regex patterns run once per call over the raw `input.Command`. Deliberately
**not** a shell parser: the floor is small enough that a handful of patterns suffice, and the
target is a ~2 GB RAM single-board computer where a hand-rolled parser is unjustified. The one
non-negotiable property is **whole-string** evaluation — that is what makes composition visible.

### Known false-positive risk and how it is bounded
- `shutdown`/`reboot` are occasionally legitimate on the device itself. Accepted: on an agent
  that primarily edits/builds code, halting the host is almost never intended, and the block
  message names the rule so the operator can override via config.
- `rm -rf` matching must target a **broad path** to avoid blocking routine `rm -rf build/`. The
  floor matches destructive intent against `/`, `$HOME`, `~`, `*`, or no specific target — not
  against ordinary workspace subpaths.
- `mkfifo` is matched as a whole-token reverse-shell shape, so legitimate named-pipe scripting
  (`mkfifo` for IPC) is also refused. Accepted: it is catastrophic-floor material; operators who
  script with named pipes can narrow `tools.shell.denylist` via `ONCLAW_TOOLS_SHELL_DENYLIST`.

### Compatibility with existing specs
- `agent-tools` "The shell tool enforces an execution policy" is MODIFIED to add `denylist` and
  full-command evaluation; the existing `deny`/`allowlist`/`ask` scenarios are preserved.
- `env-config` gains a requirement for `tools.shell.denylist` and records that the default policy
  is now `denylist`. The existing comma-separated-array parsing requirement already covers the
  new field.

### Re-evaluation triggers (separate future changes)
1. **Confirmation primitive** lands → move the broader taxonomy (and docker/env gaps) to
   `ask`-tier rather than hard-deny.
2. **Adversarial threat** becomes real (untrusted workspace, multi-user, internet-exposed) → the
   denylist is insufficient; add a sandbox or remove shell.
3. **Per-agent policy** is wanted → move `ShellPolicy`/`ShellAllowlist`/`ShellDenylist` onto the
   `store.Agent` DTO, mirroring the existing per-agent tools/MCP/persona model.
