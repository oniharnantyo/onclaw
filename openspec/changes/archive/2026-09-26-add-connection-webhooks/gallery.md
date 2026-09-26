# 4.1 — Connection webhooks section gallery

Status: shown 2026-09-26; user pivoted to live review ("show me the ui change") — build proceeds, visual acceptance happens on the rendered UI.

Entry point — writer-gated **Webhooks** button on connection cards whose recipe
declares webhooks (GitHub/GitLab), placed before Edit:

```
│ [⌥] GitHub   MCP  Read & write                           │
│     ● Connected  🔒····a1b2  atlas, beacon               │
│          [Webhooks] [Edit] [Check connection] [Disc.]    │
```

Dialog — DISABLED (initial): recipe defaults pre-checked, target unset;
enabling requires agent + channel/thread + ≥1 event.

```
┌─ GitHub webhooks ────────────────────────────────────────┐
│ STATUS                                                   │
│ ○ Disabled — GitHub events are not ingested.             │
│                                                          │
│ TARGET                                                   │
│ Agent     [ Atlas                       ▾ ]              │
│ Deliver   (•) Channel   ( ) Thread                       │
│ Channel   [ #incidents                  ▾ ]              │
│                                                          │
│ EVENTS  (recipe catalog order; recipe defaults checked)  │
│ ☑ push                   ☑ pull_request.opened           │
│ ☑ pull_request.closed    ☐ pull_request.review_requested │
│ ☑ issues.opened          ☑ issues.assigned               │
│ ☑ issue_comment.created  ☑ release.published             │
│                                                          │
│ PROVIDER SETUP  (exact steps from the recipe)            │
│ In GitHub: repo Settings → Webhooks → Add webhook.       │
│ Payload URL  https://…/api/ingest/webhooks/acme/9c8f…  ⧉ │
│ Secret       generated when you enable — shown once      │
│ GitHub sends X-Hub-Signature-256 · X-GitHub-Event ·       │
│ X-GitHub-Delivery                                        │
│                                                          │
│                      [ Cancel ]  [ Enable webhooks ]     │
└──────────────────────────────────────────────────────────┘
```

Dialog — JUST ENABLED / ROTATED: reveal-once banner.

```
│ ✓ Webhooks enabled                                       │
│ ┌──────────────────────────────────────────────────────┐ │
│ │ SECRET — COPY NOW, SHOWN ONLY ONCE                   │ │
│ │ wsec_Kd9f2xQ7mBvL3nR8pT5wY2xC4zJ6hA9dF1g…   [ Copy ] │ │
│ │ Paste it into the GitHub webhook's Secret field.     │ │
│ └──────────────────────────────────────────────────────┘ │
```

Dialog — ENABLED (manage): live state + same pickers; Save applies
target and events; Disable is a two-step danger action.

```
│ ☑ Enabled — GitHub events become turns for Atlas in      │
│   #incidents.                                            │
│ Ingest URL  https://…/api/ingest/webhooks/acme/9c8f… ⧉   │
│ Secret      ····xQ7m            [ Rotate secret ]        │
│ Last error  none                                         │
│          — or, on a fail-closed render drop —            │
│ Last error  issue_comment.created · payload missed field │
│             "comment.body" · 2 min ago                   │
│                                                          │
│ [ Disable webhooks ]                  [ Save changes ]   │
```

Design contract: Modal + SectionLabel pattern from ConnectionEditDialog;
mono chips for URLs/ids; accent primary actions; danger only on Disable;
13px/12px/11px type ladder; states — loading, load-error, empty-agent
hint, per-action busy, role="alert" error block.
