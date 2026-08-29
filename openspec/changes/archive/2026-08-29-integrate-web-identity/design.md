# Design: integrate-web-identity

## Context

The web app is a seed-driven prototype; the backend (`add-identity-tenancy`) provides auth, tenancy, members/roles, avatars, and the master-tenant admin API. This change replaces the identity/tenancy slice of the data layer with real endpoints — and adds the superadmin's tenant-management menus. Chat/agents/cron/runs stay seeded.

## Key decisions

1. **Token transport: bearer in localStorage (v1).** Simplest client, zero backend delta, works with Vite proxy (dev) and same-origin serving (prod). XSS exposure is accepted for v1; HttpOnly-cookie upgrade is a documented future hardening (would need a small backend delta: set-cookie on login + middleware reads cookie).
2. **API client** (`web/src/lib/api.ts`): fetch wrapper for `/api/v1`, parses the `{error:{code,message,details}}` envelope, maps 401 → session-clear + `/login` redirect, exposes busy-state convention for controls; typed per-resource functions mirroring the backend surface.
3. **Session layer**: a session store (Zustand slice) holds `{user, memberships, status}`; an auth gate blocks shell rendering until `/auth/me` resolves; expired/invalid token → clear + `/login`.
4. **Admin area** (`/admin`, tabbed like SettingsModal): gated client-side by "active workspace is master AND member's role has `admin.*` permissions"; the server stays authoritative. Entry appears in rail/sidebar only in that context. Screens:
   - **Tenants**: table (name, slug, members, status; suspend/restore; create modal with owner email).
   - **Users**: table (name, email, avatar, created date, status, actions; omits membership-count column in favor of a "Created" date; create user modal; disable/enable toggle with active user self-disable guard).
   - **Superadmins**: lists all master workspace members (allowing promotion to superadmin or demotion of existing superadmins with last-admin protection; grant by email).
5. **Members pane & Invited heuristic (v1)**: The SettingsModal Members pane displays member details including `joined_at` dates. The "Invited" status badge relies on a v1 client heuristic (detecting accounts where display name matches the email prefix, no avatar is uploaded, and the member is not the current user).
6. **Seam discipline**: the API-backed source implements the existing data-access seam (`data-layer` "Replaceable data seam"), so screens don't change shape; integrated-domain mutations go through the client, everything else stays on seed.
7. **Error copy map**: `last_owner_protected`, `conflict` (slug/email), `forbidden` (peer/guard), `unauthenticated`, network — each with specific human copy; unknown codes fall back to server message.
8. **New visual surface** (login, admin screens) MUST follow the frozen token contract (Inter, `#2f6feb`, radii, motion); a Hallmark pass is recommended before implementation since the prototype has no mock for these screens.

## Open decision (resolved default, easy to flip)

Token transport defaults to **bearer + localStorage**; flipping to HttpOnly cookie later is a contained change (login sets cookie, middleware accepts it, drop Authorization header). Flagged so the user can override before implementation.

## Non-goals

Profile page UI (PATCH /users/me has no screen yet) · avatar upload UI (backend ready) · cookie/CSRF hardening · SSO login UI (provider buttons) · chat/agents/cron/runs integration (later changes) · admin pagination/search (tables render all; workspaces are small).
