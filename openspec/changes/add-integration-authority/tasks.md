## 1. Domain & Recipes

- [x] 1.1 Add tier values (`read`, `write`) to recipe tool declarations; fail-safe default `write` for undeclared tools
- [x] 1.2 Tier the GitHub and GitLab recipe tool lists (read: fetch/list/search verbs; write: create/update/merge/delete verbs)

## 2. The Gate

- [x] 2.1 Wrap connection-sourced tools (MCP-kind and HTTP-kind) with connection identity at their source seams so the gate can key off the originating connection
- [x] 2.2 Resolution-time pruning: non-`integrations.write` users resolve only read-tier connection tools; write-tier tools are pruned from the run's tool surface
- [x] 2.3 Invocation-time re-check: verify the requesting user's current permission set at call time; deny out-of-tier calls with the canonical block result naming the required permission — run continues
- [x] 2.4 Service-authority path: webhook-triggered runs gate as read-tier; first write-tier call interrupts via the existing approval machinery and resumes or ends on Owner/Admin decision
- [x] 2.5 Effective-tier projection: per-attached-connection read/write tool counts for the agent config surface

## 3. Web

- [x] 3.1 Tier counts in the agent config integrations section (read/write split alongside access level)
- [x] 3.2 Approval card rendering for service-run write escalation in the bound thread/channel, reusing the shell-approval transcript pattern

## 4. Verification

- [x] 4.1 Unit tests: tier default (undeclared = write), pruning by role, invocation re-check after role change, canonical block shape, service-run escalation interrupt/resume/deny
- [x] 4.2 HTTP/run-level tests: Member read allowed + write blocked; Admin both allowed; webhook run escalation end-to-end with a stub
- [x] 4.3 `go build ./...`, `go vet ./...`, `go test ./...` green; web typecheck + tests green
- [x] 4.4 Smoke coverage: member-requested write denial visible in transcript without run failure
- [ ] 4.5 Manual pass: Member asks for a write action (blocked, run continues), Admin approves a webhook-run write from the transcript
