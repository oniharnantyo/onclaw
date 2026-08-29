## 1. Foundation

- [x] 1.1 Vite dev proxy: `/api` → `http://localhost:8080`
- [x] 1.2 `lib/api.ts` — fetch wrapper for `/api/v1`: bearer header from token store, `{error:{code,message,details}}` envelope parsing, typed functions per resource (auth, workspaces, members, roles, admin), 401 → session clear + `/login`
- [x] 1.3 Session store + provider: `{user, memberships, status}`, boot gate calling `/auth/me` before shell render, login/logout actions

## 2. Auth

- [x] 2.1 `/login` route + LoginView per design contract (email/password, submit busy state, generic error, redirect on success)
- [x] 2.2 Route guard: unauthenticated → `/login`; logout control in the shell (rail user chip menu)

## 3. Workspaces

- [x] 3.1 WorkspaceSwitcher → real memberships + role badges, suspended marked/blocked, create entry preserved
- [x] 3.2 CreateWorkspaceModal → `POST /workspaces`; server 409/400 as per-field errors; starter-agent toggle stays (seeded agent)
- [x] 3.3 Danger zone: leave workspace (two-click confirm, last-owner toast) replaces delete; suspended-state screen

## 4. Settings

- [x] 4.1 Members pane → real members + roles; role select from `GET roles`; guard rejections as toasts; "Invited" hint for passwordless members; avatar img with initials fallback

## 5. Admin area

- [x] 5.1 Admin gating + entry (rail/sidebar) in master context; `/admin` route group with not-authorized state
- [x] 5.2 Tenants screen: table (name, slug, members, status), suspend/restore, create-tenant modal (owner email assignment)
- [x] 5.3 Users screen: table, create user, disable/enable with confirm
- [x] 5.4 Superadmins screen: master member list, promote/demote, last-admin guard toast

## 6. Verification

- [x] 6.1 Toast copy map implemented (`last_owner_protected`, conflict, forbidden, network)
- [x] 6.2 Web tests green: api client (envelope/401), auth gate, switcher hydration, members pane, admin tables
- [x] 6.3 Manual e2e vs running backend: login → switcher → create tenant → members → guards → suspend/restore → superadmin promote/demote → logout
