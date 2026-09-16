## Context

`GatewaysPane.tsx` currently renders a horizontal Telegram/WhatsApp `Segmented` strip (the add-whatsapp-gateway D12 tabs) over a stacked `max-w-xl` column. Status exists only inside each platform's section cards; the navigator-level view is a dumb two-way switch. All data the sidebar needs is already in the pane's `load()` fan-in (config, WhatsApp health, links) — this change re-presents it. See proposal.md — Why.

## Goals / Non-Goals

**Goals:**
- Status-at-a-glance: platform health readable from the sidebar without opening a detail.
- Room to grow: the frame hosts N rows per platform without redesign (multi-bot lands later; this change renders one row per platform and no Add affordance).
- Full-width detail: binding and pairing surfaces stop fighting a 576px column.

**Non-Goals:**
- No backend changes; no new endpoints; no transport-switch behavior change (that is `fix-gateway-webhook-switch`'s scope — the two compose but are independent).
- No URL routing for platform selection (Settings sections are local state; staying consistent).
- No new gateway platforms.

## Decisions

**D1 — Sidebar anatomy.** Platform section headers (TELEGRAM, WHATSAPP) over rows: icon + identity second line + status dot. Identity = @bot_username (Telegram) / linked lane identity (WhatsApp). Shared chip vocabulary with the detail status card:

| State | Dot | Chip |
|---|---|---|
| Configured + enabled + healthy | ● accent | Connected (WA linked: Linked) |
| Configured + disabled | ○ muted | Paused |
| Configured + `status_error` | ● danger | Error |
| Unconfigured | ○ muted | Not set up |

The dot derives from the same payload fields the detail card uses — one mapping function, two renderers, no drift.

**D2 — Frame (approved gallery, ≥768px).**

```
┌───────────┬─────────────────┬──────────────────────────────────────────────┐
│ Settings  │ GATEWAYS        │ Telegram                        ● Connected  │
│ nav       │                 │                                              │
│ Workspace │ ┌─────────────┐ │  ● Connected · @onclaw_bot · token ••••a1b2  │
│ Providers │ │ ⬤  Telegram │ │                       [ Send test message ]  │
│ Members   │ │   @onclaw_  │ │  ──────────────────────────────────────────  │
│ Integr.   │ │   bot    ●  │ │  BOT TOKEN   ••••a1b2           [ Replace ]  │
│ ▸Gateways │ └─────────────┘ │  DEFAULT AGENT [ Atlas ▾ ]                   │
│ MCP       │ ┌─────────────┐ │  ──────────────────────────────────────────  │
│ Skills    │ │ ⬛ WhatsApp  │ │  TRANSPORT …      (per fix change when landed)│
│           │ │   Not set ○ │ │  ──────────────────────────────────────────  │
│           │ └─────────────┘ │  Gateway enabled ● … GROUP BINDINGS … YOUR   │
│           │                 │  ACCOUNT …                                   │
└───────────┴─────────────────┴──────────────────────────────────────────────┘
```

**D3 — Mobile collapse (default adopted; reversible).** Below 768px the sidebar becomes a horizontal scrollable chip row (today's Segmented, promoted with status dots); detail stacks beneath. The 360×800 contract holds; the existing mobile behavior is literally preserved plus dots. Alternative (icon rail at all sizes) rejected this round: labels + status words beat icons alone at small sizes.

**D4 — Full-width detail (default adopted; reversible).** Pane grows from `max-w-xl` to the content area's `max-w-5xl`: sidebar ~208px fixed, detail flexes. Members/MCP panes set the breathing-room precedent.

**D5 — Multi-bot-ready shape, single-bot content.** The sidebar renders per-platform sections containing a row list fed by a list-shaped view model. This change feeds the list with exactly one item per platform; the later multi-bot change adds rows and an `＋ Add` affordance without touching the frame. Deliberately no disabled "Add bot" placeholder — real copy only.

**D6 — Local state selection.** `useState` per platform, defaulting to the first platform with a problem (Error > Not set up > first configured) so admins land where attention is needed. No URL sync.

## Risks / Trade-offs

- [Archive-time spec reconciliation with the in-flight `add-whatsapp-gateway` change, which also modifies this requirement] → Its deltas still describe the tab-strip pane; when both archive, the sidebar wording supersedes tabs chronologically (house precedent from the 2026-09-11 bulk archive). This design's spec text already assumes the WhatsApp surface exists.
- [Status dot lies if the detail card and sidebar diverge] → Single shared mapping function + component test asserting dot==chip across states.

## Migration Plan

Pure presentation; ship behind nothing, roll back by revert. No data or API migration.

## Open Questions

None — open items were adopted as recorded defaults (mobile collapse D3, full width D4, local state D6).
