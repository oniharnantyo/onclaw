# Design: channel-teams

Decisions from the 2026-09-09 exploration of the team layer. The v1 capture (`integrate-agent-channels`) is the substrate and stays untouched except where noted. The user's vision: a channel that behaves like a real software team (architect, PM, scrum master, frontend, backend, tester) that actively collaborates.

## D1 — Work sessions are objects, not conventions

A work session is a DB row with a lifecycle: `open → paused(awaiting-human | budget-exhausted) → closed(summary)`. Session state machine:

```
kickoff ⚡ ──► OPEN ──hops──► OPEN ──agent tags human──► PAUSED(awaiting-human)
                │                        │
                │ watchdog(idle)         └──budget==0──► PAUSED(budget-exhausted)
                ▼                              │                │
        facilitator summoned     human posts ◄──┴── human posts ─┘
                │                       │                │
                └──► route/ask/close    └──► OPEN        └─► OPEN
any state ──facilitator session.close(summary)──► CLOSED (terminal)
```

- **Kickoff is explicit and human-gated**: a flag on a posted message (API field + composer affordance), never parsed from text. The chokepoint mints the session and summons the facilitator to plan.
- **Budget**: default 12 agent hops (code constant). Every agent run minted for the session consumes one hop, including the facilitator's. Feed rows in a session carry `work_session_id`.
- **Termination has an owner** (the failure mode the MAD literature and MAST both flag): budget exhaustion and agent-tagged-human both pause; only a human post resumes; only the facilitator closes, via `session.close` with a stored summary. No new summons after close.

## D2 — Facilitator: structural-lite

A membership role (`channel_members.role`: `member` | `facilitator`), at most one per channel (partial unique index). Exactly three powers, all exercised through existing machinery:

1. Kickoff summons — the facilitator plans and tags the first specialist.
2. Stall-watchdog summons — one per idle period (10 min default, code constant); a synthetic event through the chokepoint, no LLM magic.
3. `session.close(summary)` tool — registry tool available only to the facilitator agent.

No other authority: routing, ordering, and who-speaks remain emergent (deciders + mentions + conventions). A human facilitator is allowed (their powers reduce to closing).

## D3 — Chokepoint session branch

Inside an open/paused-resuming session, the chokepoint's bounds swap: the v1 chain-depth/no-re-summon caps are suspended, and mentions summon deterministically against the hop budget. Outside sessions, v1 caps stand unchanged. Silence tiers still apply to *untagged* in-session messages (a session does not make every agent speak). This is a single branch at the existing chokepoint — no second pipeline.

## D4 — Human gates and notifications

Agent-tagging-a-human inside a session pauses it (`awaiting-human`) and surfaces an in-app mention state on the channel (badge/unread-style). A human message in the channel resumes the session and joins the session feed. (The research rationale: TheAgentCompany's ~24–30% autonomous completion — human gates are the design, not a fallback.) Richer notification channels stay out of scope.

## D5 — Shared project space (`/project`)

`projects/<channel-slug>/` under the workspace data root, mounted **read-write** at `/project` into the jail of member agents only — the jail's extra-roots precedent (skills use a read-only root), now RW. Conventions govern write conflicts ("claim your area in PLAN.md"); no merge machinery, no worktrees in this change. `PLAN.md` as the tracker (file-as-tracker — no task schema). Git is *available* via shell tools, not enforced. Symlink/escape rules apply identically to `/project`.

## D6 — Team templates

Built-in, code-defined (no template table in v1): named role slots (specialization note + role prompt hint), conventions prefill, designated facilitator slot. "Software Team" ships first: PM, architect, scrum master (facilitator), frontend, backend, tester. Materialization = create channel + memberships; each slot binds to a **spawned** agent (promptgen generates identity docs informed by the role) or an **existing** workspace agent. Templates are product surface over v1 primitives.

## D7 — Schema (migration `000040_channel_teams`, numbering after v1's 000031 wave)

```sql
ALTER TABLE channel_members
    ADD COLUMN role text NOT NULL DEFAULT 'member'
    CHECK (role IN ('member', 'facilitator'));
CREATE UNIQUE INDEX uq_channel_members_channel_facilitator
    ON channel_members(channel_id) WHERE role = 'facilitator';

CREATE TABLE channel_work_sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id      uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    root_message_id uuid NOT NULL REFERENCES channel_messages(id) ON DELETE CASCADE,
    goal            text NOT NULL,
    status          text NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open', 'paused', 'closed')),
    pause_reason    text CHECK (pause_reason IN ('awaiting-human', 'budget-exhausted')),
    budget          int  NOT NULL DEFAULT 12,
    hops_used       int  NOT NULL DEFAULT 0,
    summary         text,
    closed_at       timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_channel_work_sessions_channel ON channel_work_sessions(channel_id, status);

ALTER TABLE channel_messages
    ADD COLUMN work_session_id uuid REFERENCES channel_work_sessions(id) ON DELETE SET NULL,
    ADD COLUMN is_kickoff      boolean NOT NULL DEFAULT false;
```

Project directories are filesystem, not DB. Template definitions are code.

## D8 — Session context in the channel document

For runs in a channel with an active session, CHANNEL.md's channel branch gains a `## Work session` block (goal, status, hops remaining) — session awareness without a new prompt doc or composition-order change (the v1 agent-runtime spec already delegates CHANNEL.md content to the agent-channels capability).

## D9 — Metrics by construction

The facilitator's phase closes, per-hop accounting, decider decision logs, and run links mean the room natively emits MultiAgentBench-style evaluation data (milestone completion, interaction quality). An eval harness is a cheap future change; nothing to retrofit.

## D10 — Eino boundary (confirmed)

Room-of-runs stands: eino composes agents *within* a run; the OnClaw chokepoint composes runs *within* the room. Synchronous inner delegation via eino's `NewAgentTool` (the maintainer-recommended pattern; supervisor/transfer carry explicit "NOT RECOMMENDED" warnings in v0.10.0-alpha.28 source) is the documented escape hatch for deep sub-work inside a single role's turn — future, not this change.

## D11 — Constants

| Constant | Value | Where |
|---|---|---|
| Session hop budget | 12 | code |
| Stall idle period | 10 min, 1 watchdog per idle period | code |
| Facilitators per channel | 1 | DB constraint |
| Built-in templates | Software Team | code |

## D12 — Non-goals

Parallel-write merge machinery / git worktrees; inter-channel or cross-workspace sessions; trained turn-taking models (decider logs banked for fine-tuning instead); notification channels beyond the in-app awaiting state; template authoring UI (built-ins only); a task/kanban entity (PLAN.md is the tracker); changes to the v1 silence tiers or context assembly.

## D13 — UI contract (ASCII gallery is a hard gate)

As in v1: no UI implementation before the full-surface ASCII gallery is drawn and approved — kickoff affordance, session banner states (open/paused×2/closed + summary), awaiting-human presentation, and template materialization flow. The approved gallery binds as a design appendix.
