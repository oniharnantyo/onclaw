# Tasks

## 1. Manifest chapter coverage

- [x] 1.1 `Service.TableOfContents` gains a level≤1 filter; the manifest projection requests chapter headings within budget instead of the fixed 6-entry mixed prefix. Verify: unit test — a document with PostLogin at chapter 5 has that heading in its manifest TOC; budget-collapse test still passes.

## 2. Search read hints

- [x] 2.1 `document_search` hits gain a `read` string naming `references/<name>` + locator (or the plain path when no locator). Verify: unit test — hit for a heading-located section carries the scoped hint; txt hit carries the unscoped hint.

## 3. document.read path handling

- [x] 3.1 Canonicalize `/System/Volumes/Data/`-prefixed spellings on stored roots and resolved candidates before the root check. Verify: unit test — same file under both spellings resolves and reads; non-firmlink paths unaffected.
- [x] 3.2 Outside-roots error names the accepted forms (`references/<name>`, workspace-relative, `/workspace/...`). Verify: unit test — error copy contains the `references/` form; model can self-correct in one retry.
- [x] 3.3 Wire the run's references mount directory into the glob/ls file tools' read-only roots (same registration point that configures `document.read`), so `ls /workspace/references` and `glob` see the mounted documents instead of resolving to the agent workspace dir. Verify: component test — with a materialized mount, `ls` on the references path lists the mounted file and `glob **/<name>` finds it; session-events regression documented (2026-09-28 run: glob "No files found", ls "no such file or directory").

## 4. Prompt guardrail

- [x] 4.1 AGENTS.md reference-document paragraph gains the never-shell-hunt line. Verify: promptdocs tests pass; composed base prompt contains the line.

## 5. Gates & live pass

- [x] 5.1 Full gates: `go build ./... && go vet ./... && go test ./...`. Verify: all green.
- [x] 5.2 Live pass (user-gated): fresh chat turn "give me PostLogin payload" answers via document search→read in ≤4 tool steps with no shell calls. Verify: manual checklist recorded.
  - 2026-09-28 21:06–21:08 WIB live pass (Personal Assistant, dev): fresh turn answered in 4 steps — session_events turn d89f7374 shows `document.read` → `ls` (references mount listing SUCCEEDED; the 2026-09-28-morning "no such file or directory" failure is fixed by the workspace link) → 2× scoped `document.read` (§5.1 + content sections, zero path errors) → final answer. No shell/find/grep calls; no outside-roots rejections.
