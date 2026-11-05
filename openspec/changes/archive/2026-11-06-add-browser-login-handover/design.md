# Design: add-browser-login-handover

Context: OpenBot's mechanics were researched against source (2026-11-04, memory `buddies-openbot-browser-login-handover-research`); assistant-ui 0.15.30's approval seam was verified in installed types. User-locked: pane simplification decisions (2026-11-04, merged from `simplify-browser-use-pane`); vault posture/pane-placement/v1-scope adopt the recommended defaults below — flip before implementation if wanted.

## D1 — The secret never reaches the model (the invariant)

The secret exists in exactly **three memory locations** — the frontend card's local state (cleared on submit), the parent's in-memory pending map (keyed by interruptID, dropped at resume), the child's CDP fill frame — and nowhere else. Every channel is structurally closed:

| Channel | Closure |
| --- | --- |
| Tool arguments | `submit_secret` schema has no value field; unknown fields rejected — the model cannot express a secret |
| Tool result | Closed contract `{"status","savedAsLogin?"}`; error paths return fixed reason codes, never page exceptions |
| Inspection after fill | Protection freeze: snapshot/screenshot/read/evaluate refused on that session from request to scrub-release |
| Page echo | Post-submit scrub (D4); `urlCarriesSecret` navigation guard; recording + diagnostics suppressed; history cleared on the releasing navigation |
| Resume payload | Frontend→parent over the local bridge; parent→child via parent-initiated `__deliver_secret` (inert without a live pending request); never a model turn |
| Event log | Resolution event stores a receipt only; replay inherits safety |
| Message part | `mapping.ts` is host code — raw text omitted, receipt rendered |
| Vault read-back | No read-secret tool exists; `list_logins`-style surface is out of scope for v1; the sole plaintext consumer is the fill path |
| Logs | MCP layer keeps its no-payload-logging property; vault registers values for redaction at construction |

Honest limits: Go strings cannot be reliably zeroized (mitigate with minimal lifetime — one fill frame); renderer memory holds the typed value briefly (masked input, `autocomplete=off`, immediate clear); providers never receive the value at all because arguments and results are value-free. Proof plan: schema test (no value field); log-content integration test (loopback sign-in form; byte-grep session events, sqlite rows, transcript, tool results, logs for the typed value); receipt-shape test; freeze test (inspection refusals + post-scrub scan finding nothing, shadow-DOM echo fixture).

## D2 — submit_secret contract and preparation

Schema: `{targets: 1-12 field refs, method: password|otp|authenticator, digits?: 0-12 (default 6), submission: click|enter|on_input, submitTarget?, identifierTarget?, loginId?, username?}` — the same semantic ref union `click`/`type` already use. Password = exactly 1 target; code = 1 or `digits` targets; `click` ⇔ `submitTarget`. An `identifierTarget` (2026-11-05 follow-up) names the login form's identifier field — username or email, whichever the site asks for: the card shows a plain identifier box, the typed identifier overrides `username` for the vault save, and the fill frame types it before the secret (fingerprints re-checked like every other field).

`prepareSecret` (child, before consent): HTTPS-only origin; refuses tabs already holding a secret; refuses frames; requires isolation from connected/opener tabs showing the same site; resolves every target and snapshots a **form fingerprint** (`localName/type/id/name/autocomplete/form.action/form.method`), re-checked immediately before entry ("Authentication target changed."). Any prepare failure answers the model "Secure authentication is unavailable. \<reason\> …or use browser takeover." — takeover is the fallback, not a retry.

## D3 — Parking: the `input_required` event and ResolveInput

A fourth event variant beside `approval_required`: `KindInputRequired {InterruptID, TurnID, Prompt, Method, Digits, Origin}` — plain event data, replay-safe like D4 of chat-approvals. `ResolveInput(ctx, sessionID, interruptID, text)` mirrors `ResolveApproval`'s log-derived pending validation and resolving-guard; **the value never enters the event** — the `input_resolved` receipt records outcome + redacted length only. The parent holds the value in a memory map keyed by interruptID until the child confirms delivery. Cold restart: the pending request resolves `expired` (assistant-ui's `resolution: "expired"` renders it); the agent re-requests — correct hygiene, not a bug.

## D4 — Fill and scrub (child, port of OpenBot's proven sequence)

Fill: `DOM.focus` + `select()` + `Input.insertText` (trusted events across shadow roots) after the fingerprint re-check — the identifier field first when the request declared one; submission per `submission` (click waits for button-enabled via MutationObserver; `enter` refocuses the last field). Scrub: wait ≤5s for navigation; if same-document → clear filled fields → in-page scan (`document.title`, `location.href`, text nodes, element values, all attributes, **open shadow roots**): clean → **SPA-keep** (protection releases to a weaker state: `evaluate` + recording blocked across the opener group until a real main-frame navigation, which also clears history); value found anywhere → **GET-reload** to replace the document without replaying a form POST. Failure keeps protection and falls back to takeover. `on_input` only fills. Secrets are not retried.

## D5 — Vault storage and service

`vault_items(id, kind, name, origin, username, added_by, secret_blob, created_at, updated_at)` — migration 000021 (000020 was taken by agent-groups before this change landed), v1 kind `login` only. Password sealed: AES-GCM, key = 32 random bytes in the OS keyring (`github.com/zalando/go-keyring`, service `buddies`), fallback keyfile `~/.buddies/vault.key` 0600 where no keyring exists (GOOS-derived paths — platform-agnostic rule). `internal/vault` owns origin matching (`websiteMatchesOrigin`: HTTPS-only, registrable-domain or exact-host) and upsert semantics (same origin+username ⇒ password update). Service: `VaultLogins` (redacted views — the `CredentialSet`/`CredentialHint` pattern), `SaveVaultLogin`, `RemoveVaultLogin`, `RevealVaultLogin` (settings-only). UI: **Saved logins** section in the Browser Use pane (list/add/remove/reveal) — recommended default; promotes to a standalone pane when cards/apiKeys arrive. The agent-facing surface gains nothing in v1. Existing MCP/OAuth credentials stay plaintext-in-sqlite (recorded follow-up).

## D6 — Attention cards on the assistant-ui approval seam

`mapping.ts` builds the parked tool call as a tool-call part with `approval: {id: interruptID, prompt, display}` — `"text"` for secrets (masked renderer: `input[type=password]` or `digits` OTP boxes + save-to-vault checkbox; submit = `respondToApproval({approved: true, text})`), `"decision"` for takeover. The runtime's `toolApprovalAcceptsText` accepts the text response only because the mapping sets `display` — the host owns that gate. The card clears its local value on submit (`setValue("")` + `onCleanup`, OpenBot's pattern) and renders receipts ("Filled ✓" / "Saved as login `<id>`" / "Filled for you"). Installed 0.15.30 supports all of this; no upgrade.

## D7 — Internal parent↔child channel

Three child tools, advertised but filtered from `resolveMCPTools` (reserved `__` prefix; also inert without a matching live request, so even an explicitly allowlisted id does nothing): `__deliver_secret {requestID, value}`, `__begin_takeover {requestID}`, `__resolve_attention {requestID, outcome}`. They ride the existing supervision stdio channel — the parent already invokes tools on the child (`countedInvokableTool` path); the model never composes or sees these calls. No network listener, no new transport.

## D8 — Takeover (lean axis)

`request_takeover {tabId?, reason: "sign-in-blocked"}` parks on the attention seam; card states: pending (**Approve**/Cancel) → active (**Done**). Approve = `BeginBrowserTakeover` service call → parent `__begin_takeover` → child relaunches that profile's browser **headed** and focused (headless config is overridden for this relaunch; next idle respawn restores config), invalidates refs + snapshot, marks the session held: every tool on it returns the fixed refusal "A person controls this browser. Take a fresh snapshot when they finish." Done/Cancel → `respondToApproval` (`completed`/`cancelled` + "take a fresh snapshot") → parent `__resolve_attention` → child ends held state. Held ends only at resolution or tab close — no timeout (OpenBot-literal). User-grab takeover (park-and-resume) deferred.

## D9 — Pane simplification (merged from `simplify-browser-use-pane`)

Engine knobs leave the UI; the stored config document, defaults, and `SaveBrowserConfig` are untouched (the supervisor decodes the same document at spawn). Default profile: badge on the config's default + **Make default** row action (read config → swap `defaultProfile` → save pass-through; applies at next spawn boundary). Import: `DetectChromeProfiles` (Chrome-only, user-locked) — GOOS-aware user-data dir (darwin Library path / Windows `%LOCALAPPDATA%\Google\Chrome\User Data` / Linux XDG→`~/.config/google-chrome`), `Local State` → `profile.info_cache` display names with dir-name fallback, existing-directory filter, `Default` first, absent Chrome → empty+nil, corrupt `Local State` → wrapped error, sizes informational; per-row Import slugs the display name through the backend name rule (fallbacks: lowercased DirName → `chrome`), backend collision suffix handles repeats; the three import disclosures render first; manual source-path input dropped (user-locked). No Chrome-running detection in v1.

## D10 — Test map

Go: resolver table (GOOS × env), detector fixture (names/order/fallback/missing-dir/absent/corrupt), vault seal round-trip + keyring-fallback + origin-match table + redacted list shape, `submit_secret` schema rejection of value fields, prepare validations (HTTPS/frames/isolation/fingerprint), scrub sequence on loopback fixtures (SPA-keep vs reload vs echo-in-shadow-DOM), `urlCarriesSecret` guard, delivery-channel inertness without a live request, `input_resolved` receipt shape (byte-grep integration: typed value appears in no event row, no sqlite row, no transcript byte, no log line), takeover held-refusal + resolve endings. Frontend: SecretGate masked render + submit/clear + receipts; mapping sets display and omits text; takeover card states; pane rewrite tests (engine controls absent, badge/make-default pass-through, detected list, import slug + suffix, Chrome-absent hint, disclosures); `client.test.ts` mocks widened.

## D11 — Screenshots inside the session workspace (2026-11-05 follow-up)

The model could not read its own screenshots: the child wrote captures to the global `<browserDir>/screenshots/`, outside every jail root, so `read_file` refused the path the tool itself reported. Captures now land inside the calling session's workspace (`sessions/<id>/browser/` under the agent's workspace — a `workspaces.DirBrowser` peer of `artifacts/`/`uploads/`). The routing channel is the call's `_meta` (`buddies.dev/screenshot-dir`), never the arguments — arguments land in the transcript, the metadata does not, and the model can neither see nor influence the path. Parent: `resolveMCPTools` wraps the system server's tools (`sessionDirTool`) so every model-invoked browser-use call carries the directory through `MCPManager.Invoke` → supervisor `invokeWithMeta` (the in-flight gauge counts it like any listed tool's call); a manager without the invoker, an agent without a slug, or a scope without a session degrades to the unwrapped tools and the child falls back to its own `screenshots/`. Child: `screenshotDir` accepts only a clean absolute path. Serving: `BrowserScreenshot(sessionID, name)` resolves the same workspace directory (session → agent → slug), refusing names and resolving strictly inside it; `WithChatBrowserDir` is gone. The webview thumb addresses the read with the viewing session. Transcripts stay unchanged: the tool result's path is a host path today and stays one — but now under the workspace, so the jail admits it.
