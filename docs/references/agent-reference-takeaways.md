# Agent Reference Takeaways — What onclaw Should Adopt

- **Status:** Exploration synthesis (thinking artifact, not an implementation plan)
- **Date:** 2026-06-24
- **Owner:** oniharnantyo
- **Sources:**
  - `docs/references/zeroclaw.md` — **PicoClaw** (multi-agent chat-bot framework)
  - `docs/references/nullclaw.md` — **NullClaw** (Zig autonomous runtime, 678 KB binary)
  - `docs/references/goclaw.md` — **GoClaw** (multi-tenant Go agent platform)
- **Grounded against:** `docs/superpowers/specs/2026-06-23-onclaw-design.md` (approved design)
- **Integration surface verified:** `internal/agent/agent.go` (3-line stub), `internal/llm/service.go` (`Build()` → `model.ChatModel`)

---

## 1. Framing

The three references describe agents under very different constraints. Mapping each to
onclaw's own invariants (single static binary, ~2 GB RAM, inference-only over the wire,
single-user/single-process) yields one clear winner and two that are useful only for
*patterns*, not *topology*.

| File | Really about | Topology | Alignment to onclaw |
|---|---|---|---|
| `zeroclaw.md` | **PicoClaw** — multi-agent chat-bot framework | Multi-agent registry, channel routing (telegram/slack/discord), multi-tenant workspaces | ❌ Wrong shape (server, multi-tenant) |
| `goclaw.md` | **GoClaw** — multi-tenant Go agent platform | 8-stage pipeline, JSONB configs, dual identity, tenancy/sharing | ◐ Patterns yes, topology no |
| `nullclaw.md` | **NullClaw** — Zig autonomous runtime, **678 KB binary** | Single binary, tight RAM, per-turn allocation discipline, security tiers | ✅ Architectural twin |

> **Meta-takeaway:** NullClaw is built under almost the exact constraints onclaw targets
> — one static binary, starved RAM, inference-only over the wire, everything else local.
> Its playbook transfers directly. PicoClaw/GoClaw are *server platforms*; treating all
> three equally would pollute a 2 GB-budget design with registry/routing machinery it must
> never carry.

## 2. Fit vs. effort map

```
  HIGH VALUE
      ▲
      │   ● C compaction-trigger      ● B clean /stop (ctx cancel)
      │      (eino summarization?)       (M1, idiomatic Go)
      │
      │   ● D no-placeholder-rehydrate   ● A shell exec tiers
      │      (threat-model hardening)       (deny/allowlist/ask, M2)
      │
      │            ● E sys-prompt fingerprint cache
      │
      │                          ● F explicit loop stages
      │                              (Prune + Checkpoint, testability)
      ├─────────────────────────────────────────────────► EFFORT
      │
      │   ● G persona/context files      ● H delegate-to-self
      │      (DB-memory file complement)     (single-proc "subagent", M4+)
      │
      │                 ● I provider/model fallback (post-retry)
  LOW VALUE
```

## 3. Key takeaways suitable to implement

### Top tier — concrete, fills a real spec gap, M1–M2 horizon

#### A. Shell/exec tool with explicit security tiers — NullClaw §Exec
The design spec lists `shell` as merely "confirmable." NullClaw gives a concrete,
implementable policy model: a `security` enum `{allow, deny, allowlist}` × an `ask` mode
`{always, on_miss}`, plus a command allowlist.

- Default = `ask` (confirm), with a `deny` kill-switch and a trusted-commands allowlist.
- Turns a vague "confirmable" into testable behavior.
- **Maps to:** design `§7/M2` builtin tools. **Source:** `docs/references/nullclaw.md`.

#### B. Cancellation → a clean transcript line, not a torn process — NullClaw §Interrupt
NullClaw's atomic interrupt flag, checked inside the tool loop, yields a structured
`Interrupted by /stop` result. In Go this is `context.Context` cancellation — but the
valuable part is the **contract**: when `chat` is interrupted (Ctrl-C / `/stop`), the loop
writes a `tool_result: interrupted` / `error` event to the session JSONL and exits the turn
cleanly rather than leaving a half-finished entry.

- Build into `loop.go` from **M1** so `chat --resume` never loads a corrupt tail.
- **Maps to:** design `§3` per-turn flow, `§8` session transcripts. **Source:** `docs/references/nullclaw.md`.

#### C. History compaction with a cheap, dual-trigger heuristic — NullClaw §Compaction
Compact when `message_count > N` **OR** `token_estimate > 75% of limit`; then keep N recent
messages and inject a `[Conversation Summary]` system message. Token estimate is literally
`(chars+3)/4` — zero-cost, no tokenizer, ideal for a Pi.

- **Caveat to verify:** onclaw already mandates eino's `summarization` middleware always-on
  (design `§5`), so eino may provide this for free. Actionable = *confirm* eino's middleware
  does keep-recent + summary-message with a ~75% trigger; if dumber, port NullClaw's
  heuristic into the agent middleware config.
- Either way, enforce `MaxContextTokens: 8192`.
- **Maps to:** design `§5` agent middleware, `§10` memory budget. **Source:** `docs/references/nullclaw.md`.

#### D. Redaction invariant: never rehydrate placeholders into tool args — NullClaw §Redaction
The design's threat model (`§12`) gestures at this but doesn't pin it down. If onclaw
redacts the user's message (or known-secret patterns) *before* sending to the provider, the
model may echo the redaction *placeholder* back inside a tool-call argument. NullClaw passes
placeholders through **opaque** and never swaps the secret back in at tool-execution time —
preventing a provider→tool PII/secret leak.

- Extends onclaw's existing logging-boundary redaction (`internal/logging`) to the
  **tool-dispatch boundary**. Cheap to specify, expensive to discover by incident.
- **Maps to:** design `§12` threat model, `§11` error handling. **Source:** `docs/references/nullclaw.md`.

### Second tier — cheap efficiency wins that fit the discipline

#### E. System-prompt fingerprint cache — NullClaw §Fingerprint
onclaw reassembles its system prompt every turn (base + injected skills + recalled memory +
active rules). NullClaw content-hashes that blob and skips reassembly when unchanged.

- *Catch:* because onclaw recalls memory per turn, the fingerprint must include the
  recalled-facts digest, so most turns still miss — the win is mainly on rule/skill-only turns.
- Low-risk, opt-in later. **Source:** `docs/references/nullclaw.md`.

#### F. Name the loop's stages explicitly (esp. Prune + Checkpoint) — GoClaw pipeline
GoClaw's 8 stages (Context→Think→Prune→Tool→Observe→Checkpoint→MemoryFlush→Finalize) mostly
collapse into eino's ReAct internals for onclaw — but making **Prune** (the compaction
decision point) and **Checkpoint** (flush to session JSONL, one `fsync` per turn) *named,
separable stages* in `loop.go` directly serves the project's 80%-coverage / unit-testable-
per-stage rule. Conceptual/structural, not new code.

- **Maps to:** design `§13` testing. **Source:** `docs/references/goclaw.md`.

### Third tier — note for later phases, don't build now

#### G. User-editable persona/context files — GoClaw bootstrap (USER.md / CAPABILITIES.md / IDENTITY.md)
A file-based complement to onclaw's DB-backed memory — a user-facing escape hatch to
hand-edit "who I am / what the agent knows about me" as markdown, assembled into the system
prompt. Bridges the `memory/` and `plugin/skill` systems. Defer; the DB `facts`/`rules` +
BM25 recall already cover the *learned* version, but a hand-edited layer is a nice ergonomic
for **M3+**.

- **Source:** `docs/references/goclaw.md`.

#### H. "Delegate-to-self" nested turn as the single-process subagent primitive — GoClaw/PicoClaw SubTurn
The *one* multi-agent idea that survives the constraint filter: not separate agent instances
(RAM-killer), but a nested turn within the same process — a `delegate(task)` tool that runs a
focused sub-turn with `prompt-mode: minimal` and a fresh, reduced context. This is how onclaw
could get subagent-like behavior *cheaply* in **M4+** without a registry. Explicitly a future
lever.

- **Source:** `docs/references/goclaw.md`, `docs/references/zeroclaw.md`.

#### I. Provider/model fallback chain (after retry exhausts) — NullClaw + PicoClaw
The design's error handling does backoff-retry on 429/5xx; a primary→fallback profile chain
is the layer after retries give up. Touches secret resolution (a fallback profile needs its
own key) and the `default_provider` model, so it's non-trivial. Optional, later.

- **Maps to:** design `§11` error handling. **Source:** `docs/references/nullclaw.md`, `docs/references/zeroclaw.md`.

## 4. What to explicitly NOT take (and why it matters)

These look impressive in the references but would **violate onclaw's 2 GB / single-process /
single-user invariants**:

- **Multi-agent `AgentRegistry` + channel routing + multi-tenant workspaces** (PicoClaw,
  GoClaw): each agent instance carries its own context, session store, and tool registry
  (~50–100 MB+ each, per the design's own `§10` budget table). Two agents and the OS is
  gasping. And onclaw has no telegram/slack channels — it's a terminal.
- **Dual identity (human key + canonical UUID), `AgentAccessStore`, sharing/owner fields**
  (GoClaw): pure multi-tenant machinery. onclaw's `name` PK + ULID sessions are sufficient.
- **Per-agent separate SQLite session DBs / separate tool registries** (PicoClaw lifecycle):
  budget killer; onclaw correctly uses one shared DB handle.
- **Prompt-modes-for-subagents, agent discovery descriptors, `allow_agents` hierarchies**:
  no subagents/teams in v1 — and the *intent* (trust scoping) is already captured by onclaw's
  `trust: user/agent/core` plugin column.

## 5. What onclaw already absorbed implicitly

The design spec already encodes the right lessons from these references — the genuinely
additive items are A–D:

| Reference pattern | Already in onclaw design |
|---|---|
| GoClaw JSONB-with-defaults | `llm_providers.settings TEXT '{}'` (design `§9`) |
| PicoClaw permission hierarchy | plugin `trust: user/agent/core` (design `§7`) |
| NullClaw "full session never in memory" | append-only JSONL + `fsync` per turn (design `§8`) |
| GoClaw 8-stage pipeline | per-turn data flow (design `§3`) + eino ReAct |

## 6. Where this should land (decision log)

No active OpenSpec changes; design doc is "approved, pending implementation plan." The
top-tier takeaways are **refinements to harden M1–M2**, not new scope. Candidate homes:

| Item | Home | Action type |
|---|---|---|
| A — shell exec tiers | design `§7` / M2 | spec edit |
| D — no-placeholder-rehydrate | design `§12` threat model | spec edit |
| B — clean-cancel contract | M1 `loop.go` implementation note | implementation note |
| C — compaction trigger | M1 — verify eino summarization, port if needed | implementation note |
| E–I | design `§15` Deferred/open | named future levers |

### Open questions for next session
1. Does eino's `summarization` middleware already do keep-recent + summary-message at a ~75%
   trigger? (Resolves whether C needs custom code.)
2. Confirm `loop.go` will own the clean-cancel contract (B) vs. delegating fully to
   `context.Context` plumbing.
3. Decide whether to capture A+D as an OpenSpec change proposal now, or edit the design spec
   in place.
