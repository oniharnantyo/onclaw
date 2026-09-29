# Tasks

## 1. Skill content

- [x] 1.1 Author `internal/agents/systemskills/document-read/SKILL.md` on the `web-research` template: name/description header; Available Tools (`document.search`, `document.read` with pages/section and hit read hints "when present"); Workflow (manifest first → search → scoped read → cite name + locator linked to `references/<name>`); a Never-via-shell section (no Glob, no `find`, no manual unzip); multi-document delegation via the `agent` tool. Verify: content review against the `web-research` template structure.
- [x] 1.2 Add the directory to the `go:embed` directive in `internal/agents/systemskills/sync.go`. Verify: `go build ./...`.

## 2. Tests

- [x] 2.1 Sync test covers the new skill: boot mirror produces `document-read/SKILL.md` beside `web-research`, and content-diff/removal semantics hold for it. Verify: `go test ./internal/agents/systemskills/...` (or the sync test's home package).
- [x] 2.2 Content contract test: the SKILL.md mentions `document.search`, `document.read`, `references/`, and the never-shell rule, and carries a valid name/description header. Verify: unit test asserting on the embedded file.

## 3. Gates & live pass

- [x] 3.1 Full gates: `go build ./... && go vet ./... && go test ./...`. Verify: all green.
- [x] 3.2 Live pass (user-gated): boot, confirm the skill appears in the skills surfaces as system-tier with a working fork affordance, and a fresh chat turn "give me PostLogin payload" consults the manifest/search/read path without shell calls. Verify: manual checklist recorded.
  - 2026-09-28 21:0x WIB live pass (dev, fresh boot on the wave tree): Settings → Skills → System skills lists `document-read` v0.1.0 (system, always on) beside `web-research` with the "Fork to workspace" affordance rendered; agent config Capabilities step shows it attached as "System skill — always attached". Fresh chat turn "give me PostLogin payload" (same run as fix-reference-document-retrieval 5.2) used document.read + ls only, no shell. Fork button not clicked (would create a workspace skill copy in the dev DB) — execution of the fork left to the user.
