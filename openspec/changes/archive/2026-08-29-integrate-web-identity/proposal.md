# Proposal: integrate-web-identity

## Why

The web app is a complete, seed-driven prototype: every screen renders `web/src/data/seed.ts`, there is no login, no API calls, and workspace switching toggles JS objects. The backend slice (`add-identity-tenancy`) is captured but not implemented. Wiring identity and tenancy to real endpoints turns the prototype into a usable multi-user product and exercises the new API the way real users will.

## What Changes

- **Auth domain (new)** — a login screen and session layer replace "always authenticated":
  - `/login` route + screen (email/password, uniform error, per design contract)
  - API client (`/api` fetch wrapper, error-envelope parsing, 401 → login redirect)
  - Session boot: token → `GET /auth/me` → hydrate user + memberships → auth gate; logout
  - Token: bearer in `localStorage` (v1; cookie hardening documented as future)
- **Tenancy goes live** — switcher, creation, and state from real data:
  - WorkspaceSwitcher lists real memberships (`/auth/me`) with role badges; suspended tenants marked and blocked
  - CreateWorkspaceModal → `POST /workspaces` (creator becomes Owner; slug conflict/validation from server)
  - Danger zone: workspace deletion replaced by **leave workspace** (backend has no tenant delete; suspension is admin-side)
  - Suspended-workspace state screen
- **Members pane goes live** — Settings → Members & roles uses `GET/POST/PATCH/DELETE members` + `GET roles`; role select populated from real roles; guard rejections (peers, last-owner) surfaced as toasts; avatars render `avatar_url` with initials fallback
- **Instance admin menus (new)** — when the active workspace is the `master` tenant and the member holds `admin.*` permissions, an **Admin** area appears (`/admin`) with:
  - **Tenants**: all workspaces incl. suspended + member counts, create-tenant-with-owner, suspend/restore (master protected, errors toasted)
  - **Users**: list, create, disable/enable globally
- **Unchanged (stay seeded)**: chat, agents, cron, runs, integrations, MCP, skills, keys, notifications panes stay on the seed layer until their domains integrate

## Capabilities

### New Capabilities
- (none — this change modifies the existing `web-app` capability)

### Modified Capabilities
- `web-app`: new `auth` and `admin` domains; `shell` routing gains `/login` + `/admin`; `workspaces` switching/creation from real API, deletion → leave; `settings` members pane from real API + avatars; `data-layer` seam replaced for the identity/tenancy domain

## Impact

- **Code**: `web/src` — new `lib/api.ts` client + auth session module; App.tsx boot gate + routes; WorkspaceSwitcher, CreateWorkspaceModal, SettingsModal (members/danger zones); new `screens/LoginView.tsx`, `screens/admin/*`; vite.config proxy
- **Dependencies on backend**: requires `add-identity-tenancy` implemented (endpoints, JWT, master tenant + admin routes)
- **Specs**: deltas against `web-app` domains (auth, admin, shell, workspaces, settings, data-layer)
- **Design**: login + admin screens are net-new visual surface — must follow the frozen token contract; Hallmark pass recommended before implementation
- **Testing**: web unit tests (api client, auth gate, switcher hydration) per existing web test setup
