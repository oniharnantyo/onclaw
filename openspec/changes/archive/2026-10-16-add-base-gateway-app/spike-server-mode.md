# Spike: server mode (`-tags server`) — design D1/D7 validation

## What was run

- `go build -tags server -o /tmp/spike-server-bin ./spike-server/` — throwaway `package main` in `spike-server/` (own dir, no root `!server` conflict), `http.HandlerFunc` asset handler (embed not needed), one bound service via `Options.Services: []application.Service{application.NewService(svc)}`, one stream via `app.HandleStream("chat", ...)`, broadcasts via `app.Event.Emit(...)` (note: it is `app.Event`, not `app.Events`; `Options.Server{Host:"127.0.0.1",Port:18742}`; env `WAILS_SERVER_HOST/PORT` override).
- Clients: Go tests in `spike-server/wsclient_test.go` using `github.com/coder/websocket` (raw dial, plain Origin header, no cookies) against `ws://127.0.0.1:18742/wails/events?clientId=X` and `/wails/stream/ws?name=chat`. Raw connects work — no browser-like handshake needed.

## Findings

1. **Server mode works.** Built and served on loopback; `/health` returns ok; asset handler and WS endpoints up; no GUI libs needed. Observed: `1:14AM INF Server mode starting address=127.0.0.1:18742`, `{"status":"ok"}`.
2. **Two simultaneous clients both verified programmatically (PASS).** Both received broadcast events (`{"name":"spike:tick","data":{"n":5}}` on each) and both received all three stream deltas (`delta-1-for-hello` … `delta-3-for-hello`) over independent `/wails/stream/ws?name=chat` connections. Stream protocol is raw binary frames both ways; events are JSON via `/wails/events`.
3. **Reconnect works with no re-subscribe.** Killed the server under a client, restarted: fresh dial with the same `clientId` query param re-registers a brand-new `BrowserWindow` (`websocket_server.go: ServeHTTP` creates one per connection) and the client immediately receives events and streams again. There is no session state to recover — every connect is fresh. The wails-injected `custom.js` already auto-reconnects every 1s in browsers.
4. **Auth / token hook (answers design.md Open Question).** There is **no auth today**: an unauthenticated, origin-less ws dial to `/wails/events` succeeds. Default origin policy is same-origin only (cross-origin `Origin: http://evil.example` → 403; `ServerOptions.WebSocketOriginPatterns` / `WebSocketAllowAllOrigins` relax it). `AssetOptions.Middleware` covers HTTP traffic (`/`, `/wails/runtime` binding calls) but **not** the two WS endpoints (`/wails/events`, `/wails/stream/ws` are mounted on the internal mux with no hook). Browsers cannot set `Authorization` on `new WebSocket`, so for D7's bearer gate: enforce via header for non-browser clients and for browsers either (a) put an authenticating reverse-proxy/host in front (wails bound to loopback-internal port, our mux validates token then proxies — simplest, covers HTTP + both WS endpoints uniformly), or (b) cookie set during page load through `Middleware` + token in query param for WS. Recommend (a); header-or-first-message question is moot — hook at the connection boundary before upgrade.

## Verdict: GO

Server mode supports D1/D7 as designed (same bindings surface, WS events, Stream transport, multi-client). Proceed with task 7.x; no REST fallback needed. Note the auth gap: task 7.2 must implement its own authenticating front layer (recommend reverse-proxy in front of a loopback-bound wails server), since beta.29 exposes no token hook on the WS endpoints.
