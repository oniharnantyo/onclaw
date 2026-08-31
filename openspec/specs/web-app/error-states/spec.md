# web-app/error-states Specification

## Purpose

A shared system for every failure surface in the app — full-page error states, in-shell error states, and the connection banner — so all failures render with one visual grammar, one component, and consistent recovery actions.

## Requirements

### Requirement: Shared state-page grammar
All state pages (404, server error, crash, boot failure, suspended workspace, not-authorized, onboarding) SHALL share one visual grammar rendered by a single shared component with two variants: **full** (bundled illustration hero, tinted to the accent token, entered with the standard fade/pop motion) and **compact** (icon medallion for inline contexts). Both variants present title, body copy, and actions with the design-contract grammar (24px semibold title, 14px muted body, primary accent button and secondary bordered button at 13px). State pages that render ad-hoc markup with drifted sizes (18px titles, inconsistent medallion shapes) SHALL be migrated onto the shared grammar.

#### Scenario: Consistent grammar across state pages
- **WHEN** any state page renders (404, 5xx, crash, boot error, suspended, not-authorized, onboarding)
- **THEN** it uses the shared component: one title size (24px semibold), one body style (14px muted), one medallion shape, and one illustration slot — no per-page ad-hoc sizing

#### Scenario: Illustrations work offline
- **WHEN** a state page renders while the network is down
- **THEN** its illustration renders from the bundled asset — no remote request is made

### Requirement: Route not-found (404)
Opening a URL that matches no route SHALL render a 404 error state inside the app shell (the rail and sidebar remain) — never a silent redirect to an arbitrary screen. The state SHALL offer navigation back into the workspace (e.g. back to chats and to agents).

#### Scenario: Unknown URL
- **WHEN** the user opens a URL matching no route
- **THEN** a 404 state renders in place; the app does not redirect to a random chat, and the URL stays so the back button returns to where the user came from

#### Scenario: No history pollution
- **WHEN** the user activates the state's back action
- **THEN** no placeholder route is left in history — navigation continues from where the user came from

### Requirement: Unknown chat identifier
A well-formed chat identifier that does not exist in the active workspace SHALL render a not-found error state in place — never a silent redirect to another chat. The state SHALL offer "View agents" and "Back to chats" actions.

#### Scenario: Deleted agent link
- **WHEN** a user follows a link to a deleted agent's chat
- **THEN** "This agent no longer exists" renders with View agents / Back to chats actions — the app does not silently substitute another chat

### Requirement: Server error page (5xx)
A 5xx response while a view loads its data SHALL render a full-page server error state (the app chrome is replaced) with Retry as the primary action. Retry SHALL re-run the failed data load in place — no page reload, no logout. The state SHALL show a mono detail chip with the status, server error code, and request id when present. 5xx responses to in-place mutations SHALL continue to surface as toasts; only data loads get the full-page treatment.

#### Scenario: Data load fails with 500
- **WHEN** the user opens a view whose data load returns 500
- **THEN** a full-page server error state renders; activating Retry re-runs the load in place and the view returns without reload or re-login

#### Scenario: Mutation failure stays a toast
- **WHEN** an in-place action (e.g. sending a message, renaming an agent) fails with 500
- **THEN** a toast surfaces the failure; the view and its data remain rendered

### Requirement: Crash containment
An uncaught error in the React tree SHALL render a full-page error state — never a blank white screen. The primary action SHALL be Reload (a full reload is acceptable here since the tree is torn down), and the error message SHALL appear in the detail chip. Boundaries SHALL exist at the root and inside the app shell so a crashing view keeps the chrome alive where feasible.

#### Scenario: Component throws
- **WHEN** a view component throws during render
- **THEN** a full-page error state renders instead of a blank screen, with the error message in the mono chip

#### Scenario: Reload restores the app
- **WHEN** the user activates Reload after a crash
- **THEN** the app re-boots normally without re-login (token preserved)

### Requirement: Connection banner
While the server is unreachable mid-session, the app SHALL render a single sticky banner with a Retry action; the banner SHALL disappear when connectivity recovers. While the banner is visible, repeated network failures SHALL NOT render repeated toasts. Boot-time unreachability renders the boot error page instead (never the banner alone), and the banner never blocks the UI (no modal takeover).

#### Scenario: Network drops mid-session
- **WHEN** requests start failing with network errors while the app is open
- **THEN** one sticky banner renders; subsequent network failures do not stack toasts

#### Scenario: Connectivity recovers
- **WHEN** the user activates Retry (or any request succeeds) while the banner is visible
- **THEN** the banner disappears and normal operation resumes
