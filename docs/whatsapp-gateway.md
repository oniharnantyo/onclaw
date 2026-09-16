# WhatsApp Gateway

WhatsApp is a gateway platform alongside Telegram: paired members DM the workspace's WhatsApp number and the routed agent answers in a `wa_dm_<phone-digits>` session. A workspace configures exactly one WhatsApp gateway under **Settings → Gateways → WhatsApp**, on one of two lanes:

- **Official Cloud API** (`cloud_api`) — Meta's hosted API. Production-grade, but ingestion is webhook-only, sent messages cannot be edited, and free-form sends are restricted to the 24-hour customer-service window.
- **Multi-device** (`multi_device`) — a self-hosted device client (whatsmeow) paired like a second phone. No webhook, no messaging window, and streaming edits work — at the cost of ban risk, because the protocol is unofficial.

Both lanes are direct messages only — no WhatsApp groups in v1. Identity is normalized to bare phone digits on both lanes, so switching lanes later preserves sessions, bindings, and paired links.

## Choosing a lane

| | Official Cloud API | Multi-device |
|---|---|---|
| Nature | Official Meta API | Unofficial protocol (whatsmeow) |
| Ingestion | Webhook required (manual registration in the Meta dashboard) | None — messages arrive over the device connection |
| Streaming | No edits: typing indicator while the turn runs, then one final message | Placeholder + debounced edits, like Telegram |
| Approvals | Two quick-reply buttons (Approve / Deny) | Reply `APPROVE` / `DENY` as text |
| Messaging window | 24-hour customer-service window | None |
| Ban risk | None (official) | Meta may ban the account — personal/test numbers only |
| Recommended for | Production | Personal and self-hosted use |

## Lane 1: Official Cloud API

### 1. Create the Meta app and collect the credentials

1. At [developers.facebook.com](https://developers.facebook.com) create an app (type **Business**) and add the **WhatsApp** product.
2. Open **WhatsApp → API Setup** and copy the **Phone number ID** of the number you will use. The panel's test number works for development; add a real number for production.
3. Get a long-lived **access token**: in **Business Settings → Users → System users**, create a system user and generate a token with the `whatsapp_business_messaging` and `whatsapp_business_management` permissions. (The token shown in API Setup expires in 24 hours — fine for a first test, not for a running gateway.)
4. Find the **app secret** under **App Settings → Basic → App Secret**. OnClaw uses it to validate the `X-Hub-Signature-256` signature on every incoming webhook.
5. Generate a **verify token** yourself — any random string, e.g. `openssl rand -hex 16`. The same value goes into OnClaw and into the Meta webhook configuration below.

### 2. Paste the credentials into OnClaw

In **Settings → Gateways → WhatsApp**, pick the **Official Cloud API** lane and fill the four labeled fields:

- **Access token**
- **Phone number ID**
- **App secret**
- **Verify token**

Set the **default agent for DMs**, then **Save & connect**. OnClaw stores the credentials encrypted, validates them against the Meta API, and shows the webhook callback URL and the verify token to paste into Meta — keep this pane open for the next step.

### 3. Register the webhook (manual)

Meta has no API for this; you do it once in the app dashboard:

1. Under **WhatsApp → Configuration → Webhook → Edit**:
   - **Callback URL:** `https://<your-host>/api/v1/webhooks/whatsapp/<workspace>` — the exact URL shown in the OnClaw pane; `<workspace>` is your workspace slug.
   - **Verify token:** the value shown beside the URL in the pane.
   - Click **Validate and save** — Meta sends a `GET` challenge, and OnClaw echoes it when the verify token matches.
2. Under **Webhook fields**, subscribe to **`messages`**. That is the only field OnClaw consumes; message-status events are ignored.

### 4. The 24-hour customer-service window

The Cloud API only allows free-form messages within 24 hours of the customer's last message. OnClaw never sends template messages to re-open a closed window: a reply that lands outside it fails with error 131047, the outbox entry is marked dead (no retries), and the failure surfaces in gateway health and logs. For normal DM use this is a non-issue — anyone who has messaged the workspace number in the last 24 hours is inside the window.

**Approvals:** a pending shell-command approval arrives as a message with two quick-reply buttons, **Approve** and **Deny**. Sent messages cannot be edited, so the resolution is confirmed by a follow-up receipt message rather than by updating the card.

## Lane 2: Multi-device (self-hosted)

> **Ban risk.** This lane speaks an unofficial protocol (whatsmeow). Meta may ban the linked account at any time. Use a personal or test number — never a business-critical one.

1. In **Settings → Gateways → WhatsApp**, select the **Multi-device** lane. The pane shows a QR code and an 8-digit pair code.
2. On the phone: **WhatsApp Settings → Linked devices → Link a device**, then scan the QR — or choose **Link with phone number** and enter the pair code.
3. The status settles to **Connected** and ingestion starts. No webhook URL is involved. If the connection drops, the gateway reconnects with backoff; a disconnected state is surfaced in the pane.

- **Streaming works:** the multi-device protocol supports edits, so replies stream as a placeholder updated in place, like Telegram.
- **Approvals are text:** native buttons are deprecated on consumer accounts, so a pending approval card asks you to reply `APPROVE` or `DENY`. While a card is pending, the next DM text on that session is treated as the decision; anything else gets the pending-approval notice.
- **Logout tears down:** **Log out device** unlinks the phone and deletes the stored device session; re-pairing starts fresh.
- **DMs only:** no group support in v1.

## Member pairing (both lanes)

Access is default-deny: only paired identities can trigger turns. Each member links their own WhatsApp account with the same `/start <token>` flow used for Telegram:

1. In the **Settings → Gateways** pane footer, copy your personal pairing command `/start <token>` (one-time, expires when the countdown shown beside it runs out).
2. Send it as a WhatsApp direct message to the workspace's WhatsApp number.
3. The gateway confirms in-chat and binds your WhatsApp identity (phone digits) to your member account. Your runs execute under your own permissions.

Unpaired senders receive only a pairing hint. Pairing is revocable from the same footer (**Unlink**).

Commands work as plain leading-slash text in the DM: `/new` starts a fresh session, `/compact` compacts context, `/usage` reports the context-meter numbers.
