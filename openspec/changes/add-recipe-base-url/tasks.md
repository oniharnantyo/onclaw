## 1. Recipe domain: origin parameter

- [x] 1.1 Add `RecipeOriginParam{Name, Default, Help}` to `domain/recipes.go` plus `OriginParam *RecipeOriginParam` on Recipe; validation: PAT-kind recipes only (reject on OAuth-kind), default must parse as an origin
- [x] 1.2 Implement `ParseOrigin(raw string) (string, error)` (scheme http/https, host required, no path/query/fragment/userinfo, trailing slash normalized) with table tests
- [x] 1.3 Recipe validation: recipes with HTTP verbs or MCP endpoint paths resolve against origin + existing path constants; add unit tests for resolution helper

## 2. Connection lifecycle plumbing

- [x] 2.1 Find the per-service uniqueness check (connect-begin path); narrow to (service, origin) for recipes declaring an origin param; keep plain per-service for others; update conflict error detail to name the origin
- [x] 2.2 Persist resolved origin on the connection row (nullable column, migration 000067 up/down in postgres store + fake store); include in ConnectionView
- [x] 2.3 Materialization: derive the materialized server URL / declared-call base from the resolved origin; reauthorize and refresh paths must not alter it (immutability test)
- [x] 2.4 Connect request/response plumbing in `handlers/connections.go`: accept origin field, validate via 1.2, default when empty; PAT connect stores it

## 3. GitLab recipe re-scope

- [x] 3.1 Rewrite the gitlab recipe: AuthKind PAT, HTTP-kind, origin param (default `https://gitlab.com`), path prefix `/api/v4`; read-only verbs (projects list/get, issues list/get, MRs list/get, branches list, pipelines list, current user) and read-write verbs (issue create/update/comment, MR create/update/merge, pipeline retry/cancel)
- [x] 3.2 Probe = current-user declared call; verify probeHTTPConnection path resolves the origin
- [x] 3.3 Keep the webhook block byte-compatible (secret token header, events, templates); update guidance text (PAT now; OAuth MCP tracked separately; GHES unsupported note is GitHub-side only)
- [x] 3.4 Remove the stale MCP-kind tool tier lists (`gitlabReadTools`/`gitlabWriteTools` curation moves to the REST verb tiers)

## 4. GitHub recipe origin param

- [x] 4.1 Declare the origin param on the github recipe (default `https://api.githubcopilot.com`); guidance text documents ghe.com pattern and GHES unsupported-remote status
- [x] 4.2 Verify materialized server URL derivation for a ghe.com-style origin (unit test)

## 5. Web: connect dialog origin field

- [x] 5.1 Gallery/connect dialog renders a labeled origin input preset to the recipe default when the recipe declares an origin param (design-contract styling, monospace origin chip on the connection card)
- [x] 5.2 Validation errors from 1.2 surface on the field; immutability reflected (no origin edit on existing connections)

## 6. Verification

- [x] 6.1 `go build ./... && go vet ./... && go test ./...` green; web typecheck/tests green
- [x] 6.2 Smoke: connect GitLab against a fixture origin (mock GitLab REST), probe connected, one verb per tier, same-origin-second-connect rejected, different-origin-second-connect allowed (equivalents covered by TestConnect_OriginUniqueness/Immutability service tests; live fixture smoke deferred)
- [ ] 6.3 Live pass: connect gitlab.com with a real PAT (probe + read verb); optionally a self-managed origin if reachable
- [x] 6.4 `openspec validate` clean; update the explore memory pointer after apply
