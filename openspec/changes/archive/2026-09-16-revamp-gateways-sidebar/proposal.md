## Why

The Gateways settings pane stacks a horizontal Telegram/WhatsApp tab strip over a narrow `max-w-xl` column. It cannot show gateway status at a glance, and the single-column layout wastes the settings content area while crowding the group-binding and pairing surfaces. A second sidebar — platform sections with status-bearing rows beside a detail pane — gives gateways room to grow and puts health in the navigator.

## What Changes

- Replace the horizontal `Segmented` platform tabs with a vertical second sidebar (~208px) inside the pane: TELEGRAM and WHATSAPP section headers, one status-bearing row per gateway account beneath each.
- Row anatomy: platform icon + name/identity second line (@bot_username, lane/phone) + status dot with chip vocabulary — ● Connected, ⏸ Paused, ● Error, ○ Not set up (WhatsApp: ● Linked).
- Detail pane to the right renders the selected platform's existing sections, reflowed wider; the pane widens from `max-w-xl` to the full settings content area (sidebar + flexing detail).
- Below 768px the sidebar collapses into the horizontal chip row (today's Segmented, promoted with status dots); detail stacks beneath — 360px contract preserved.
- Selection is local component state, consistent with the settings page's other panes; deep linking stays out of scope.
- Member-level "Your account" pairing blocks stay inside each platform's detail (per-user, not per-gateway status).
- The sidebar is drawn multi-bot-ready in shape (platform sections → rows): this change renders exactly one row per platform with no Add affordance; a later multi-bot change adds rows without rebuilding the frame.
- This change is independent of and composable with `fix-gateway-webhook-switch`; if that change has not landed, the pane carries the current transport flow as-is (no behavior change to transport switching here).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app`: Gateways settings pane requirements — platform selection SHALL present as a second sidebar with per-platform status rows (collapsing to a chip row below 768px); the detail pane SHALL render the selected platform's configuration with live status vocabulary shared between sidebar row and detail.

## Impact

- `web/src/screens/settings/GatewaysPane.tsx`: layout restructure (sidebar + detail split), no API changes, no data-layer changes.
- No backend work: this is a presentation restructure over existing `GET` payloads (config, health, links already carry everything the sidebar needs).
- Tests: GatewaysPane component tests rewritten for the two-column layout; responsive tests for the <768px collapse.
