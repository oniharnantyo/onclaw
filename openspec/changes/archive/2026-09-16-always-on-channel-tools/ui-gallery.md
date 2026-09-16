# UI Gallery — always-on-channel-tools (user-approved 2026-09-15)

Approved full-surface mockups. Task 4.1/4.2 implement these; deviations need user approval.

## SURFACE 1 — Settings → Tools pane

```
┌──────────────────────────────────────────────────────────────────────┐
│ Tools                                                                │
│ The workspace-wide tool status. Disabled tools are removed           │
│ from every agent in this workspace.                                  │
│                                                                      │
│ CHANNEL TOOL                                                        │
│ ══════════════════════════════════════════════════════════════════ │
│ ┌────┐ Channel Post              ● Always on · channel runs          │
│ │ M  │ Post a message into the channel the agent is running in.     │
│ └────┘                                                              │
│ ┌────┐ Channel History          ● Always on · channel runs          │
│ │ H  │ Page back through the channel's earlier messages.            │
│ └────┘                                                              │
│ ┌────┐ Close Work Session      ● Always on · facilitator only       │
│ │ ✓  │ Close the channel's active work session with a stored        │
│ └────┘ summary.                                                     │
│                                                                     │
│ ────────────────────────────────────────────────────────────────── │
│ (flat list below — exactly today's pane, minus the three)           │
│ ══════════════════════════════════════════════════════════════════ │
│ List Files / Read File / Write File / Edit File / Glob / Grep /     │
│ Delete File / Read Document / Create Document / Shell / Memory  ⊙   │
│ Web Search   3 configured · 3 stacked                    [⚙] ⊙      │
│ Web Fetch                                                       ⊙   │
│ Browser                                                  [⚙] ⊙      │
│ Schedule                                                        ⊙   │
└──────────────────────────────────────────────────────────────────────┘

  ● badge (accent-tinted pill, no toggle, no cog)   ⊙ = Toggle (unchanged)
  Badge rows ignore stored enabled state (payload reports them enabled).
  Loading skeleton / error state unchanged.
```

## SURFACE 2 — Agent config → Capabilities (wizard step 3 / edit tab)

```
┌ Agent Configuration ─────────────────────────────────────────────────┐
│  Create:  ① Identity / ② Model / ③ Capabilities                      │
│  Edit:    [Identity & Soul] [Model] [Capabilities] [Hooks] [Prompts] │
│                                                                      │
│  CAPABILITIES & INTEGRATIONS                                         │
│  Built-in Tools                                                      │
│  [■ List Files] [■ Read File] [□ Write File] [□ Edit File] …         │
│  [□ Delete File] [■ Read Document] [□ Create Document] [■ Shell]     │
│  [□ Memory] [■ Web Search] [□ Web Fetch]                             │
│  [□ Browser] [□ Schedule]                    — 15 chips, no more     │
│  Skills / MCP Servers  (unchanged)                                   │
└──────────────────────────────────────────────────────────────────────┘

  ■ = selected (accent)   □ = unselected
  REMOVED vs today: [Channel Post] [Channel History] [Close Work Session]
  Stored allowlists carrying those keys keep them invisibly (no chip,
  saved back harmlessly).
```

## SURFACE 3 — Transcript tool cards

```
  BEFORE: blank glyph box + raw id flash pre-catalog-fetch
  ┌─────────────────────────────────────────────┐
  │ □  channel.history                    1.2 s │
  └─────────────────────────────────────────────┘

  AFTER: static mirror + new glyphs resolve instantly
  ┌─────────────────────────────────────────────┐
  │ ⏱  Channel History                    1.2 s │
  └─────────────────────────────────────────────┘
  channel.post → ▨ Channel Post   session.close → ✓ Close Work Session
```

## Glyphs (added to Icon.tsx, keys unchanged)

- `message` — speech bubble (Channel Post)
- `history` — clock with counterclockwise arrow (Channel History)
- `check-circle` — circled check (Close Work Session)

Stroke 1.8, round caps/joins, 24×24 viewBox — matching the existing icon set.
