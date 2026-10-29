// Package telegram implements the Telegram bot channel (add-telegram-channel):
// a gateway-attached service that long-polls the Bot API, authorizes senders
// against the store's channel aggregates (default deny), maps each chat to a
// persistent agent session, and delivers turns back to Telegram with
// MarkdownV2 streaming edits, per-chat pacing, flood fail-closed cooldowns,
// update receipts, and a liveness heartbeat.
//
// The service consumes its collaborators through narrow interfaces defined
// here (Runtime, BotAPI, AttachmentRegistry, Artifacts, DefaultAgents) so
// tests can fake them; *botapi.Client, *attachments.Store and the store's
// gateway-settings port satisfy them structurally. Lifecycle follows the
// scheduler precedent (internal/scheduler/ticker.go): Attach subscribes first,
// the poll loop starts on the gateway's first Running transition, holds on
// Paused, stops on Stopped, and Stop cancels and joins with a bounded wait.
package telegram
