# Tasks

## 1. Operating contract constant + main-turn composition

- [x] 1.1 New `internal/agents/contract.go`: the full operating contract as a Go constant, copied from the design appendix (v2, post fact-check) — virtual document, no file on disk
- [x] 1.2 compose the contract in `composeSystem` (`internal/agents/runner.go`): order persona documents → contract → compaction summary; contract present even when persona text is empty
- [x] 1.3 Subagent child call site (`internal/agents/runner.go`) passes persona-only text as `subagents.Config.Instruction` (the child must not inherit the full contract's mode guidance)
- [x] 1.4 Tests pinning the contract: `# AGENTS.md` heading, section set and order (Harness, Communicating, Working in the workspace, Operating modes, Context), precedence statement, presence without persona documents

## 2. Subagent child operating notes

- [x] 2.1 Reduced notes constant in `internal/agents/subagents` (design appendix "reduced child variant"): findings in the final reply, never report/summary files; workspace-safe absolute paths
- [x] 2.2 Compose the notes into the child's instruction at the child build in `subagents.New`
- [x] 2.3 Tests: child carries the reduced notes; live-chat/autonomous-run mode guidance absent from the child's composed instruction

## 3. Turn environment block

- [x] 3.1 `environmentBlock` composition in `assembleTurnInput` (`internal/agents/turninput.go`), after the compaction summary: effective provider and model, current date, turn origin, user presence; no workspace-mount duplication
- [x] 3.2 Tests per origin: user origin → user present; scheduler/webhook/channel origin → user not present; current date stated (clock stubbed)
- [x] 3.3 Full-order pinning test: persona documents → contract → compaction summary → environment block

## 4. Verification

- [x] 4.1 `gofmt -w` clean, `go vet ./...` clean, `go test ./...` green
- [x] 4.2 Every spec-delta scenario maps to a test or structural check (contract without persona, virtual, persona-change invariant, precedence; origin/presence/date; child notes, child no-modes)
