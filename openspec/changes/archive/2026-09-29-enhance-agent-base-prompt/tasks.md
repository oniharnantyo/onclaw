# Tasks

## 1. Base prompt rewrite (`internal/promptdocs/AGENTS.md`)

- [x] 1.1 Reframe Core Directives to three sections: Workspace & Tenant Boundaries (verbatim), Persona & Alignment (verbatim), Capability Scope (tool-usage prose folded in from old directive 3)
- [x] 1.2 Add **Execution** section: act-now/no-pre-refusal, batch independent calls, prerequisites first, live-check mutable facts, vary-then-conclude, long-work persistence
- [x] 1.3 Add **Finishing & Honesty** section: real artifact backed by tool output, verification before finalizing, read-back external writes, never fabricate, literal preservation, missing-context ladder
- [x] 1.4 Add **Follow-through** section: progress ≠ answer, promises create ownership, `schedule` beats polling, return proactively
- [x] 1.5 Add **Communication & Output** section: sizing rule, no filler, earned depth, distinguish facts/tool output/reasoning
- [x] 1.6 Rewrite the rich-cards catalogue to the 13-tag end-state with per-tag purpose + when-to-use + redirects (no `table`, no `math`); add markdown-first clause for tabular data
- [x] 1.7 Add `diagram` example fence (info-string title + raw mermaid body) and `mermaid` example fence; state the missing-title silent degradation
- [x] 1.8 Add the cross-tag chooser line and the anti-pattern line (diagram is not a substitute for the answer text)

## 2. Test updates

- [x] 2.1 Update `internal/promptdocs/promptdocs_test.go` for any pinned section shapes, counts, or content assertions
- [x] 2.2 Update `internal/agents/instruction_composer_test.go` and scheduler/heartbeat profile tests if they assert prompt content, section presence, or sizes
- [x] 2.3 Grep remaining tests asserting on base-prompt content (`internal/server/*_test.go`) and fix drift

## 3. Verification

- [x] 3.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 3.2 Render the composed instruction for a fixture agent and eyeball the new sections' order (AGENTS first, rich-cards section intact, `ui` section untouched)
- [x] 3.3 Confirm token-size delta is within the accepted ~+500 tokens/turn budget (design Risks)
