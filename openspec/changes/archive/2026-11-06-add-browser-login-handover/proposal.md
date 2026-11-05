# Proposal: add-browser-login-handover

## Why

The Browser Use engine (rebrand-browser-use-system-mcp, un-archived) automates pages but cannot cross a login wall: the agent cannot ask for a credential, and the user has no way to hand one over or take the browser and sign in by hand. Buddies' only answer today is importing a Chrome profile up front. OpenBot ships two proven handover mechanisms — a credential **vault** (agent never sees values) and **takeover** (user drives the browser) — researched against its source on 2026-11-04; the same conversation produced a pane-simplification capture (`simplify-browser-use-pane`, now merged into this change) because the pane fronts too many engine knobs and a raw path-input import. One change covers the whole surface: handover mechanisms + the profiles-first pane they live in.

## What Changes

### Secure handover (the spine)

- **`mcp__browser-use__submit_secret`** — a new tool whose schema carries *field targets only* (`targets`, `method: password|otp|authenticator`, `submission: click|enter|on_input`, optional `submitTarget`, `loginId`, `username`) — never a value. The child validates before consent (HTTPS-only origin, no frames, not already protected, isolated from connected same-site tabs, form fingerprint), parks the call, and the turn parks on a new `input_required` session event.
- **SecretGate** — a masked card (password input or N OTP boxes + ☑ save-to-vault) rendered via assistant-ui's approval seam (`display: "text"`; the installed 0.15.30 types already model `prompt`/`allowFreeform`/`{approved, text}` responses). The user's value rides `respondToApproval` → a new `ResolveInput` (sibling of `ResolveApproval`, same log-derived pending validation) — **held in parent memory only**.
- **Parent-only delivery channel** — the parent invokes the child's internal `__deliver_secret {requestID, value}` over the existing supervision stdio channel; the tool is filtered from `resolveMCPTools` and is inert without a live pending request. The child fills via CDP `DOM.focus` + `Input.insertText` (re-checking the form fingerprint), submits, then scrubs: clear fields → document scan (title/URL/text/values/attributes/open shadow roots) → SPA-keep with evaluate/recording blocked until a real navigation, or GET-reload; URL-carries-secret navigation guard; recording + diagnostics suppressed; inspection tools refused during protection; **secrets are not retried**.
- **Value hygiene** — the model's world is targets-in, receipt-out: tool result `{"status":"completed","savedAsLogin?"}`; the resolution event stores a receipt only; `mapping.ts` omits the raw text from the message part. A pending input that survives a restart resolves as `expired` (value never persisted).
- **Save to vault** — when the card's box is checked, the parent saves the login (origin, username from args, password, `addedBy` agent name) before resuming; a failed save does not stop the sign-in.

### Vault

- `vault_items` table (migration 000020) + `internal/vault`: v1 **logins only**; metadata plaintext, password **sealed** — AES-GCM (`golang.org/x/crypto`) under a 32-byte key held in the OS keyring (`go-keyring`), GOOS-appropriate keyfile fallback (`~/.buddies/vault.key`, 0600). Origin matching ported from OpenBot (`websiteMatchesOrigin` semantics). Settings service: redacted `VaultLogins` (id/name/username/origin/addedBy — never the password), `SaveVaultLogin` (same-origin+username upsert), `RemoveVaultLogin`, `RevealVaultLogin` (settings-only). A **Saved logins** section joins the Browser Use pane. The agent has **no** read-secret path.

### Takeover (lean axis)

- **`mcp__browser-use__request_takeover`** (reason `sign-in-blocked`) parks on the same attention seam; the card offers Approve / Done / Cancel. Approve is a control-plane service call (`BeginBrowserTakeover`): the profile's browser relaunches **headed** and focused with refs invalidated and inspection refused (held state — every tool on that session returns a fixed refusal). Done/Cancel resolve the interrupt (`completed`/`cancelled` + "take a fresh snapshot"), delivered to the child over the internal channel. Held state ends only at resolution or tab close. User-grab takeover (dock button, park-and-resume) is deferred.

### Pane simplification (merged from `simplify-browser-use-pane`, user-locked 2026-11-04)

- Engine knobs (headless, allow-private, two timeouts) leave the UI entirely; the stored config document and `SaveBrowserConfig` remain (defaults govern; make-default = pass-through save).
- Default profile becomes a row badge + **Make default** action.
- Import becomes a detected-Chrome list: new detector lists Google Chrome's profiles (GOOS-aware user-data dir, `Local State` → `profile.info_cache` names, `Default` first, absent Chrome = empty not error), one-click **Import** per row through the unchanged `ImportBrowserProfile` copy engine; the manual source-path input is dropped; import disclosures stay.

## Impact

- **Specs:** `browser-use` (ADDED: secure secret submission; ADDED: takeover), `settings` (MODIFIED: Browser Use pane — profiles-first + Saved logins + detected-Chrome import), `chat-approvals` (ADDED: browser attention requests — secret input + takeover riding the approval seam).
- **Code:** `internal/vault/**`, `internal/store/sqlite/vaultitems.go` + migration, `internal/browseruse/secret.go` + takeover held-state, `internal/cli/browser_mcp.go` internal tools, `internal/agents/mcp.go` filter, `internal/agents/resume.go` input variant, `internal/services/settings_vault.go` + `browserdetect.go`, `SecretGate.tsx` + `mapping.ts`, `BrowserUsePane.tsx` rewrite, bindings regen.
- **Deferred (future changes):** `list_logins`/auto-fill-from-vault, `request_vault_item`, 1Password/Bitwarden adapters, user-grab takeover, Chromium-family detection, sealing MCP credentials (existing OAuth tokens stay plaintext-in-sqlite — recorded as a follow-up, not this change).
- **User-note:** assistant-ui 0.15.30 needs no upgrade — the approval seam already carries everything (verified in installed types).
