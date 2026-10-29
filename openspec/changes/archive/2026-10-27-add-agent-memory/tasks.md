# Tasks

## 1. Memory layout and notes (Phase 1)

- [x] 1.1 `internal/memory` package: `types.go` (Note, Frontmatter with name/description/type/concepts/relations/supersedes, Layout paths), `layout.go` (`EnsureLayout` creates `memory/` and `memory/archive/` on demand, idempotent), `notes.go` (frontmatter parse + validation; `type` restricted to user/feedback/project/reference; wiki-link extraction; malformed notes skipped with a logged reason), `daily.go` (today's `YYYY-MM-DD.md` path) — verify with a testify suite covering valid parse, each invalid-frontmatter case skipped without error, and `EnsureLayout` run twice leaving the tree unchanged
- [x] 1.2 Home layout: workspace `memory/` and `memory/archive/` subpaths in `internal/config` (types, resolve, ensure-on-demand) and the README home-layout table row — verify config tests pass and the README row names the index, notes, daily notes, and archive

## 2. Memory block and curation guidance (Phase 1)

- [x] 2.1 `internal/agents/memory_prompt.go`: `memoryGuidance` constant (note types, frontmatter fields, daily-note purpose, supersede convention, activate `$memory` for preference/history requests) and a block builder taking the index text, ranked notes, and sweep state — index truncated to 3,200 characters with a truncation notice, notes section of at most five notes within 6,000 characters or absent, "memory is empty" and "memory unavailable" notices, stale-maintenance notice — verify with table-driven tests that unchanged inputs produce byte-identical output and each notice appears only in its case
- [x] 2.2 `composeSystem` in `internal/agents/runner.go` gains the memory block after the knowledge guidance and before the compaction summary, wired at fresh turns (`runner.go`) and resumed turns (`resume.go`), and withheld from subagent child turns (`subagents.go`) — verify pinned composition tests show the block's position on fresh and resumed turns and its absence on child turns
- [x] 2.3 Turn start calls `EnsureLayout` for the session agent before composition; a layout or read failure yields the unavailable notice and the turn proceeds — verify with a test that makes the memory directory unreadable and asserts the turn completes with the notice in the block

## 3. Memory skill (Phase 1)

- [x] 3.1 `internal/memory/skill.go` `SeedSkill` mirroring `internal/knowledge/skill.go` (seed into the global tier when missing, repair if deleted, log-not-fatal) with the `memory` skill body (read-first: index → `memory_search` → read note; write conventions; query refinement) and a description naming preferences, past decisions, and earlier work; called from `internal/cli/app.go` beside the knowledge seed — verify tests show seeding when absent, no overwrite when present, and that an agent-tier `memory` skill shadows it through the existing two-tier discovery

## 4. Derived memory state in the store (Phase 2)

- [x] 4.1 Migration `internal/store/sqlite/migrations/000016_memory.sql`: `memory_traces` (agent FK `ON DELETE CASCADE`, session, turn, origin, query, hits JSON, created_at), `memory_note_state` (agent FK, note name, retention, recall count, distinct-query count, last recall, first seen; unique per agent+name), `memory_staging` (agent FK, source ref, signals JSON, first staged, promoted flag; unique per agent+source), `memory_sweeps` (agent FK, watermark, last start, last finish, outcome, counts JSON) plus the version-pin bump in `internal/store/sqlite/db_test.go` — verify migrations apply on a fresh database and on one at the previous version
- [x] 4.2 `internal/store/memory.go` interface (traces append/list-since, note state upsert/list, staging upsert/list/expire, sweep state get/put) with `internal/store/sqlite/memory.go` implementation and the fake-registry entry — verify tests cover round-trips, cascade removal on agent delete leaving other agents' rows intact, no note-content column, and a sweep transaction committing while a concurrent trace insert succeeds under WAL

## 5. Memory search tool and recall traces (Phase 2)

- [x] 5.1 `internal/memory/index.go` inverted index: tokenizer (lowercase; identifiers and backticked spans kept whole), inverse-document-frequency weighting, boosts for `name`/`description`/`concepts`, matched excerpt, rebuild when any file's mtime changed since the last build — verify tests rank a two-term match above a one-term match, return empty on no hit, skip malformed notes, and rebuild only after an mtime change
- [x] 5.2 `memory_search` catalog tool: `ToolMemorySearch` in `internal/agents/builtin_tools.go` (display "Memory search", gated like every catalog tool, offered to child turns) and the handler in `internal/agents/memory_tool.go` scoped to the session agent's workspace, recording a trace with the turn's origin (`user`/`scheduler`/`subagent`/`other`), returning ranked results or a readable error on read failure — verify tests show the result shape, another agent's matching note absent, a trace row per call, and the correct origin for a user turn, a scheduled run, and a child turn

## 6. Consolidation sweep (Phase 2)

- [x] 6.1 `internal/memory/signals.go`: the six normalized signals (relevance, frequency, diversity, recency with a 14-day half-life, consolidation, concept density) with weights 0.30/0.24/0.15/0.15/0.10/0.06 computed from user-origin traces only, and the three gates (score ≥ 0.8, recalls ≥ 3, distinct queries ≥ 3) — verify tests reproduce the spec scenarios: 0.85 with four recalls over three queries passes; 0.9 with five recalls over two queries fails; scheduler-origin traces leave signals unchanged
- [x] 6.2 `internal/memory/retention.go`: `Φ ← max(0, Φ + I·Γ·Ψ − λ·Δt)` with `I = 0.35·novelty + 0.45·relevance + 0.20·repetition`, `Γ` capped at 5, `Ψ` = 1 − max similarity to a higher-retention note, `λ` = 0.02/day, initial `Φ = I`, archive floor 0.1, injection threshold 0.3, 30-day creation grace — verify tests show an unrecalled note crossing the floor only after the grace, recalls raising retention versus a control, a fresh note never archived, and a near-duplicate dampened
- [x] 6.3 `internal/memory/dedupe.go`: token-set Jaccard ≥ 0.9 merges candidates into one — verify tests merge two paraphrase-free duplicates and keep two distinct lines apart
- [x] 6.4 `internal/memory/indexmd.go` and `promote.go`: managed section between marker comments regenerated with stable ordering, free section preserved byte-for-byte, promoted subsection append-only with `Source: memory/YYYY-MM-DD.md#Lnn` and date, never promoted twice, at most ten per sweep, staging expiry at 30 days, archived and superseded notes excluded — verify golden-file tests show a byte-identical file on a no-op run and exact-once promotion on repeated recalls
- [x] 6.5 `internal/memory/archive.go`: move below-floor and superseded notes to `memory/archive/`; a note moved back is listed again on the next sweep — verify tests cover both directions and that nothing is ever deleted
- [x] 6.6 `internal/memory/sweep.go`: per-agent sweep = ingest files changed since the watermark (skipping files modified within the last 10 seconds) → dedupe → signals → gates → retention → archive → promote → index → record outcome; idempotent; a failure records its reason and does not affect other agents; the sweep's constructor takes no model or provider dependency — verify tests show a second run with no new input changing no file and no row, one agent's failure leaving another's sweep successful, and the provider fake receiving zero calls
- [x] 6.7 `internal/memory/ticker.go`: hourly ticker modeled on `internal/scheduler/ticker.go`, attached in `internal/cli/app.go` with the gateway, skipping while Paused, with an on-demand entry point, writing heartbeat state and exposing staleness (no success within two intervals while Running) to the block builder — verify tests show no sweep while Paused, the stale notice after two missed intervals, and no notice after a successful sweep

## 7. Compaction flush instruction (Phase 2)

- [x] 7.1 If the summarization capability in `internal/agents/compaction/summarization.go` exposes a prompt or instruction input, append the memory-flush instruction ("record durable facts to memory before summarizing") to it; if it does not, record the limitation under design.md Open Questions instead — verify either a test asserting the instruction is present in the compaction prompt or the design note, and in both cases that the turn's model-call count is unchanged

## 8. Documentation

- [x] 8.1 README: a Memory section describing the workspace files, the frontmatter schema, the `memory` skill, the `memory_search` tool, and the hourly sweep with its archive-never-delete rule, plus the `internal/memory` package in the package list — verify the described paths and names match the shipped code

## 9. Verification

- [x] 9.1 `gofmt -w .`, `go vet ./...`, and `go test -race ./...` are green with no rule weakened
- [x] 9.2 Headless smoke via the `cli.Assemble` recipe: create an agent, run a turn that writes a note and calls `memory_search`, confirm the trace row, force a sweep and confirm the managed index section and note-state rows, run a second sweep and confirm `MEMORY.md` is byte-identical, then confirm a new session's composed system context carries the note — record the evidence under `.team/smoke/add-agent-memory/`
- [ ] 9.3 Desktop smoke via `task dev`: NOT RUN — screen locked (loginwindow frontmost) at smoke time, all desktop captures die in that state; the identical scenario (preference told in one session, recalled unprompted in a fresh session from the memory block) was verified headless under 9.2 with a real model (poolside/laguna-s-2.1): in one session tell the agent a preference, start a new session, and confirm the agent recalls it from the memory block without being told again
