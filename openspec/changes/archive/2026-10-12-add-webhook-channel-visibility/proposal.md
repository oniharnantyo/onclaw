# Proposal

## Why

Standalone webhook deliveries targeted at a channel complete end to end — verified ingress, agent run, transcript persisted, delivery-log row with a run id — but nothing in the UI shows them. The channel room renders only agent-chat session transcripts it drove itself; webhook runs execute under deliberately unindexed session ids (the session index stays human-only), and the final-text post rides the raw messages feed, which no component renders. An admin who binds an endpoint to `#ops` sees "submitted" in the delivery log and an empty channel — the spec's own live-pass expectation ("the MR-opened run lands in the bound channel") is not visually true.

## What Changes

- **Channel run discovery:** a ChannelsRead-gated endpoint SHALL enumerate the channel's webhook deliveries — across every standalone endpoint bound to that channel — returning endpoint name, resolved event, outcome, run transcript id, and timestamps, newest first. The existing per-webhook delivery log remains the per-endpoint view; this is the per-channel view. No new tables: the delivery log joined over the channel's endpoints is the source, and the delivery-log row already is the run's channel anchor.
- **No run-path changes.** The runs themselves stay exactly as shipped: service-origin, synthetic transcript sessions, final-text posts to the feed. Anchoring runs into channel work sessions was evaluated and rejected — the runner's documented contract is that unattended origins never carry channel coordinates (channel-docs composition and channel-tool wiring key on `ChannelID` non-empty, and the origin guard exists precisely to keep unattended profiles free of room context); mutating that for visibility would silently change run behavior. The human-only session index is likewise untouched.
- **Room activity surface:** the channel room SHALL surface the channel's webhook runs as an activity list (endpoint, event, outcome badge, relative time), each entry opening the run's transcript via the existing run-transcript hydration used by scheduler run cards. The webhooks pane's delivery-log run link SHALL open the same transcript instead of the unfiltered runs screen.
- **Transcript discoverability:** webhook run transcripts SHALL keep their synthetic, non-indexed session ids (the human-only session index contract stands); discoverability comes from the delivery log and the channel view, not the session sidebar.

Deliberately out of scope: rendering the raw messages feed in the room (a separate UX decision affecting scheduler posts equally); the connection-webhook lane (same gap, shipped and gated on a live pass — follow-up); inline merging of run transcripts into the room conversation flow (v1 is an activity list with openable transcripts); scheduler channel-delivery visibility.

## Capabilities

### New Capabilities

- `webhook-channel-visibility`: channel-anchored webhook runs — service-origin runs bound to the target channel's work session, a per-channel delivery/run listing derived from the delivery log, and the channel-room activity surface that opens run transcripts.

### Modified Capabilities

- `web-app/channels`: the channel room gains the webhook activity surface (the capability delta names the room's new requirement; if no `web-app/channels` capability exists in the main specs, the requirement rides in `webhook-channel-visibility`).

## Impact

- **Backend**: new store query over the delivery log joined to the endpoints targeting one channel (`internal/store/workspace_webhooks.go` + fake + postgres adapters), a `ChannelRuns` service method (`internal/webhooks/standalone_service.go`), and `GET /channels/:id/webhook-runs` behind ChannelsRead (`internal/server/handlers/workspace_webhooks.go`, `internal/server/router.go`). No run-path changes: the ingress, queue, submitter, and runner are untouched (design.md D1).
- **Frontend**: `web/src/lib/workspaceWebhooksApi.ts` (channel runs method + shared run-navigation state), a channel-room activity segment (`web/src/components/chat/ChannelWebhookActivity.tsx` mounted via `ChatRoute`/`ChatView`), and the webhooks pane's delivery-log run link (`WebhooksPane.tsx`).
- **Specs**: new `webhook-channel-visibility` capability; no changes to the human-only session-index or connection-webhook contracts.
