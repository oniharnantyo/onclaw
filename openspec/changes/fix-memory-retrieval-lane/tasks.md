# Tasks: fix-memory-retrieval-lane

## 1. Gate budget setting

- [x] 1.1 Add optional `gate_budget_ms` to the memory tool-settings record: struct field, JSON round-trip, absence-is-defaults resolution to 4000ms, validation bounds [500, 20000] with a 422 naming the range on save
- [x] 1.2 Expose `gate_budget_ms` on the memory settings GET/PUT handlers and the settings UI record shape (field flows through; no new pane)
- [x] 1.3 Thread the resolved budget through `composeMemoryDocs` into the intent-gate call as the per-turn deadline, replacing the hard-coded 1.5s (fail-open semantics untouched)
- [x] 1.4 Tests: default resolution when absent, bounds rejection (below 500 / above 20000 / non-integer), and a gate call honoring a configured budget (fake slow model times out at 1.5s but succeeds under a 4s budget)

## 2. Harness ingestion wait

- [x] 2.1 Change `waitForIngestion` to poll the notes API with the fixture owner's token (Sari) instead of admin, ending early once the expected minimum note count is visible
- [x] 2.2 Correct the `_seed` note wording so a full-budget wait reports "no notes visible to the owner" (drop the misleading pre-change-baseline phrasing)
- [x] 2.3 Regression: scope-audit style check that the wait actually observes seeded notes on a live-shaped fake (admin token must still see ~0 — assert the identity asymmetry is the reason the old path was blind)

## 3. Langfuse tracing repair (diagnose first)

- [x] 3.1 Spike: verify the `.env` Langfuse keys against the configured cloud project via the public API (401/404 names the layer), check `ONCLAW_LANGFUSE_SAMPLE_RATE` value, and confirm the eino Langfuse callback handler is constructed and registered in the composition root of the running build
  - Finding (2026-09-19): keys valid — `GET /api/public/projects` → HTTP 200, project `onclaw` (`cmu71atny0005ad0div6wjasx`, org `oni`); sample rate 1.0; wiring live (cli/server.go:192–208 constructs, :294–296 registers, :537–543 flushes at shutdown). Export has worked since 2026-09-18 21:20:31 WIB — 1 minute after `.env` (mtime 21:19 WIB) gained the credentials and a fresh process started: 535 observations since, across both server generations. The prior 24h silence was the credential-less process, not code.
  - Verification trap that produced the "zero traces" claim: `GET /api/public/traces` is unavailable to this org (`LEGACY_API_UNAVAILABLE_FOR_NEW_ORGANIZATION`, org created ≥ 2026-09-16) — read counts via `GET /api/public/v2/observations`; the ingest path (`POST /api/public/ingestion`) is unaffected.
- [x] 3.2 Fix the identified break (keys/rotation → operator config task recorded here; sample rate → config default; wiring → code fix) so any live turn exports a trace
  - Outcome: failing layer was operator config — Langfuse credentials absent from the then-running process's environment until 2026-09-18 21:19 WIB; already remediated operator-side (keys added + restart). No code change in `internal/cli` / `internal/observability` / `internal/config` — all verified conformant (host/keys/rate forwarded into the eino config at observability/langfuse.go:139–153; flush interval 500ms + shutdown flush cover short-lived processes).
  - Fidelity notes (reported, out of this fix's scope): model calls export as generic `SPAN` named `ChatModel` with latency (not `GENERATION` — no model id/token usage; the eino-ext handler's `ComponentOfChatModel` branch is not hit via the ADK callback chain); memory side-calls export as root traces with empty user/session attribution (their ctx carries no `langfuse.SetTrace` options). Neither blocks 3.3.
- [x] 3.3 Acceptance: one live chat turn produces a visible trace (name + model request observation) in the Langfuse project within ~1 minute
  - Verified live 2026-09-19 during the scoreboard rerun (run eval-20260919-194224, server restarted 19:42 WIB on the new build): the run's turns exported `memory.intent_gate`, `memory.gister`, `memory.curation_gate`, and `ChatModel` spans within seconds of each turn — 30 intent-gate observations in the run window, read via `GET /api/public/v2/observations`.

## 4. Scoreboard rerun and records

- [x] 4.1 Rerun `go run . eval-memory --email … --password … --model glm-5.3-flash` on the reused `memory-eval` fixture after 1–3 land, with a fresh run id
  - Run `eval-20260919-194224` on the reused `memory-eval` fixture (reused=true, 9 sessions / 18 turns re-scripted; scope audit passed: third-party sees 0 private rows, owner sees 2).
- [x] 4.2 Record the post-fix scoreboard JSON in the change folder and summarize the delta against eval-20260919-174856 (recall/abstention/citation movement; whether prefetch now injects)
  - Scoreboard: `eval-scoreboard-postfix.json` (this folder). Headline: recall 28.6%, citation_valid 50%, scope_safe 100%, abstention 66.7%, overall 61.3%.
  - Per-question movement vs eval-20260919-174856: temporal recall False→True on BOTH temporal questions (evidence opened: 1→2); abstention corrected on q-abstain-competitor and q-scope-contact (now correctly refusing/scope-safe); regression on q-abstain-offsite (answered where it should abstain). recall-db/invoice, update-payments, and both multihop questions unchanged (still no evidence opened).
  - Whether prefetch now injects: partially. On the 4s default the gate completed on 18/30 turns and timed out on 12 (Z.AI round trips straddle 4s — gister side-calls measured 3.7–4.5s). Langfuse shows gate latency 1.7–3.9s on completions. The temporal gains are consistent with gate-succeeded turns; the remaining misses split between gate timeouts and the matcher finding below.
  - Finding 1 (drives the knob): 4s default is still too tight for Z.AI glm-5.3-flash. `gate_budget_ms=8000` was set on the `memory-eval` workspace via the new PUT (live-verified: GET before resolved 4000-by-absence, PUT persisted, GET after 8000); after the raise every gate call completes (1.7–3.9s, zero timeouts).
  - Finding 2 (wave-3 signal, out of scope here): `Searcher.Prefetch` passes the RAW turn text into the lexical matcher, which under-matches multi-word queries (as budi: q=billing → 7 notes, q=Midtrans → 2, but q="payment provider billing" → 0 despite the note containing "billing" verbatim). Injection starves on phrasing luck even when the gate succeeds. Fix direction: keyword extraction or OR-with-ranking in the prefetch query.
  - Fidelity observation: the fixture agent's memory tool call fails with `skill not found: memory` — the model cannot self-search on this agent; all evidence flows through prefetch injection. Agent tool exposure (or the tool/skill naming path) is the next suspect for the self-search leg.
- [x] 4.3 Update `docs/memory-system.md` where the 1.5s gate budget and the eval-wait behavior are described (P2 pins, retrieval section)

## 5. Validation

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green; settings endpoints covered by fake-based tests
- [x] 5.2 Smoke pass: a workspace with `gate_budget_ms` set + a live turn shows prefetch injection (chip/trace evidence), and the scoreboard recorded in 4.2 is attached to the change before archive
  - Live 2026-09-19 20:03 WIB, workspace `memory-eval` with `gate_budget_ms=8000`, turn "Where does the staging database run?" as the corpus owner: gate span `memory.intent_gate` exported at 13:03:44Z completing in 3.17s (DEFAULT), prefetch injected, and the answer cited the stored facts verbatim with two source-event ids ("note dca1f0a4 (source event b645561d…); corroborated by note dc5cbd65 (source event 68198d46…)") — the citation-lock format of the injected prefetch document. Scoreboard attached above (4.2).
