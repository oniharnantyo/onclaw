## 1. Sidebar frame

- [x] 1.1 Extract a `gatewayStatusView`-feeding shared status mapping (state → {dot, chip}) used by both sidebar rows and detail status card; unit tests across the four states for both platforms
- [x] 1.2 Build the second sidebar: TELEGRAM / WHATSAPP section headers, one row per platform (icon + identity second line + status dot), selection state with problem-first default (Error > Not set up > first configured)
- [x] 1.3 Split the pane into sidebar + detail layout; detail renders the selected platform's existing sections unchanged in content, reflowed to the flexed width; widen the pane from `max-w-xl` to the full content area

## 2. Responsive collapse

- [x] 2.1 Below 768px render the rows as a horizontal scrollable chip row (keep per-row testids `tab-telegram` / `tab-whatsapp`) with detail stacked beneath; verify 360×800 with zero horizontal overflow
- [x] 2.2 Component tests: chip row carries the same dots; selection works from both layouts; hydration preserves the active platform

## 3. Spec alignment and cleanup

- [x] 3.1 Retire the platform `Segmented` strip in wide layout (it survives only as the <768px chip row); remove dead tab-strip tests; keep `pane-gateways` testid stable
- [x] 3.2 Member (read-only) pass: sidebar visible with dots, admin cards hidden/disabled, pairing flow reachable inside each platform's detail
