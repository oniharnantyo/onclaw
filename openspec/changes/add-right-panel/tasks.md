# Tasks: add-right-panel

## 1. Panel shell and registries (slice 1)

- [ ] 1.1 Add the panel store slice (`open`, `tabs[]`, `activeId`, `badge`) with per-chat reset, dedup-by-kind+payload focus, close-last-tab-closes-panel, and unit tests for each rule
- [ ] 1.2 Build the RightPanel shell component: tab strip (title, per-tab close, active state), source dispatch through the panel-source registry, docked ≥xl / overlay sheet <xl chrome reusing the ContextRoute pattern
- [ ] 1.3 Add `registerPanelSource(kind, renderer)` and `registerPanelCandidate(matcher)` registries with candidate extraction from transcript tool cards (`agent.tools[]`), plus tests
- [ ] 1.4 Wire the header panel toggle (open/close, dot badge on panel-able tool finish while closed, badge clears on open) with the never-self-open invariant tests; in direct agent chats the toggle replaces the configure button — remove `btn-configure-agent`/`onConfigure` from ChatHeader
- [ ] 1.5 Refactor ContextPanel into the `members` source: register renderer + channel-only tab offering; remove the standalone `pos.showContext` aside from ChatRoute in the same slice; existing members behavior tests pass unchanged
- [ ] 1.6 Add tool-card open affordances for registered candidates (live and hydrated parity tests: same affordance on a rehydrated transcript)

## 2. Workspace-files API (slice 2, backend)

- [ ] 2.1 Add the files handler (read + `mode=list`) over the agent jail root with granular injected deps (workspace root, auth context) wired through the composition root; no aggregate config struct
- [ ] 2.2 Implement path confinement: Clean + root-prefix check + symlink rejection; traversal, absolute, and symlink-escape requests all return not found — table-driven tests for each rejection scenario
- [ ] 2.3 Implement serving guards: `nosniff` on every response; attachment disposition for `text/html` and `image/svg+xml`; display-safe types inline — tests per content-type scenario
- [ ] 2.4 Enforce workspace scoping from the auth context (cross-workspace agent access → not found) with tests; register routes under the authenticated group
- [ ] 2.5 Implement list mode (one directory level: name, kind, size, modified; non-directory → not found) with tests
- [ ] 2.6 Add files API cases to `scripts/smoke.sh` (read authored file, traversal rejected, html served as attachment)

## 3. File source (slice 2, frontend)

- [ ] 3.1 Add the file source renderer: fetch through the API, loading and not-found states, type dispatch (markdown / code / pdf / image / csv / degrade)
- [ ] 3.2 Implement the markdown pre-render pass: frontmatter → meta chip, relative asset rewrite against the file's jail directory, render through the transcript markdown/shiki/fence pipeline — tests for assets, frontmatter, and fence parity
- [ ] 3.3 Implement the captioned degrade card for xlsx and unknown binary types with download affordance, and the size-threshold degrade for very large text files
- [ ] 3.4 Register file candidates (document.create published URLs, files.write paths) and verify open affordances end-to-end in the running app

## 4. Browser mirror source (slice 3)

- [ ] 4.1 Register the browser candidate matcher (`browser.*` → `{agentSlug, sessionId}` tab payload) and per-agent-session tab dedup
- [ ] 4.2 Build the mirror renderer: latest screenshot, address bar, session call feed from transcript cards; four states (idle, mirroring, frozen, closed) with tests
- [ ] 4.3 Implement the staleness caption (capture time + actions-since count) and frozen-state distinction tests
- [ ] 4.4 Live pass: agent drives a real page, mirror labels staleness honestly across navigate/click/screenshot sequences, multi-agent channel yields independent tabs

## 5. Verification and close-out

- [ ] 5.1 Full `go build/vet/test` plus `web` unit suite green; smoke suite green
- [ ] 5.2 Visual pass against the design contract: docked and overlay chrome at 360×800 and 1920×1080, no horizontal overflow, all four states per source rendered
- [ ] 5.3 Live end-to-end pass: markdown file with assets and frontmatter, browser mirror, members tab, badge flow
- [ ] 5.4 Specs synced and change archived after passes
