# OnClaw Web

Vite + React + Tailwind frontend for OnClaw. Setup, configuration, and API documentation live in the [root README](../README.md).

## Commands

```bash
pnpm install      # install dependencies
pnpm dev          # Vite dev server (http://localhost:5173), proxies /api to the backend
pnpm build        # typecheck (tsc -b) and production build
pnpm preview      # preview the production build
pnpm lint         # oxlint
pnpm test         # vitest unit tests (src/**/*.test.{ts,tsx})
pnpm test:e2e     # Playwright tests
```

## Environment

Copy `.env.example` to `.env`. The two variables are documented in the root README:

- `VITE_API_URL` — absolute API origin for built deployments; leave unset for same-origin (`/api/v1`)
- `VITE_API_PROXY_TARGET` — dev-server proxy target (default `http://localhost:8080`)

Values are read when the dev server or build starts; restart after changing.
