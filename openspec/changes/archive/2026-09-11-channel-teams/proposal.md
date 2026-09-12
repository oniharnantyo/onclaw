# Proposal: channel-teams

## Why

The `integrate-agent-channels` substrate makes rooms where agents observe and respond. The product vision goes further: a channel that acts like a **team** — an architect, PM, scrum master, frontend, backend, and tester that *actively collaborate* on a job, divide labor by specialization, produce real artifacts, and pause for human sign-off. The research base (MetaGPT/ChatDev role teams, MultiAgentBench milestone scoring, TheAgentCompany's ~30% autonomous ceiling) says this works best exactly as gated collaboration: agents do the legwork, humans own the gates. This change adds that layer on top of the v1 chokepoint without reworking it.

## What Changes

- **Work sessions**: a human kickoff (explicit affordance on a message) opens a bounded collaboration scope — turn budget (~12 agent hops), its own root, lifecycle (`open → paused → closed`). Inside a session, agent-authored mentions summon freely; the v1 chain caps apply only outside sessions. Budget exhaustion and agent-tagged-human both **pause** the session; a human reply resumes it; the facilitator closes it with a stored summary.
- **Facilitator role**: a structural-lite membership role (one per channel — the scrum master). Powers: receives kickoff summons to plan, receives stall-watchdog summons, and closes sessions via a `session.close` tool. Everything else about coordination stays emergent.
- **Shared project space**: each channel gets `projects/<slug>/` mounted read-write at `/project` into member agents' filesystem jail (the jail's extra-roots precedent, now RW). Artifacts live in files (`spec.md`, `PLAN.md` as file-as-tracker), discussion lives in the feed.
- **Team templates**: built-in role-slot templates ("Software Team": PM, architect, scrum master, frontend, backend, tester) that materialize a channel — memberships with specializations, prefilled conventions, facilitator assignment — with each slot bound to a spawned agent (role-prompted via promptgen) or an existing one.
- **Human gates made real**: tagging a human member inside a session pauses it and surfaces the awaiting state (in-app mention state on the channel).

## Capabilities

### New Capabilities

- `channel-teams`: work sessions (kickoff, budget, pause/resume, close, stall watchdog), the facilitator role and its powers, the shared `/project` mount, and built-in team templates.

### Modified Capabilities

- `agent-channels`: the loop-caps requirement gains the work-session exception (session budget replaces chain depth inside a session); membership gains the facilitator role; session context rides the channel document.
- `agent-runtime`: the filesystem jail gains a per-channel shared project root mounted read-write for the channel's member agents.
- `web-app/chat`: kickoff affordance, work-session status surface (running / awaiting-human / closed-with-summary), and the pause/resume behavior.

## Impact

- **Backend**: migration for `channel_members.role`, `channel_work_sessions`, `channel_messages.work_session_id` + kickoff flag; chokepoint gains the session branch (budget accounting, pause/resume, watchdog); new facilitator tool (`session.close`); jail extension for the shared root; template materialization (agents + channel + memberships).
- **Frontend**: composer kickoff affordance, session status banner, awaiting-human state, session summary rendering.
- **Unchanged**: the v1 silence policy tiers, per-agent sessions, context assembly, feed, and REST/SSE contracts (session fields extend them additively); eino stays per-run execution — inner delegation via `NewAgentTool` remains a documented future escape hatch, not scope here.
