# Manual pass — task 7.3 (live, 2026-09-20, workspace `memory-eval-harden-w3`)

## Entity-filtered search — PASS

A live chat turn as Budi asked the agent to search memory by entity:

> Search workspace memory by ENTITY "Project Nilam" (use the entity filter, not just free text)…

The `/v1` transcript shows `memory.search` invoked with the entity argument —
`{"entity":"Project Nilam","query":"internal beta"}` — and the answer cites
linked source event ids across sessions (`21fa14fd…`, `9c06cb37…`, `66f1db4b…`,
`8cde14b1…`), correctly distinguishing the 24 March beta from the slipped
21 April launch. No identity fields on the wire.

## Chip unchanged (counts only) — PASS

A fact-bearing turn ("the war room channel is renamed to #nilam-launch-room…")
produced the `x.memory_ingested` chip with exactly the contracted payload:
`counts {user: 2, shared: 0, agent: 0}` + `note_ids` + `event_ids` — no
content, no visibility-bearing text.

## Scheduled-run non-ingestion (chip suppression) — PASS

A scheduler was created for the fixture agent and fired via run-now. The
`sched_…` session recorded 7 events and **zero** `x.memory_ingested` chips,
while ingestion still landed exactly one agent-visibility gist for the turn
(the agent-tier ceiling for zero-human origins). The test scheduler was deleted
afterward.

## Raw-evidence citation — VERIFIED BY TESTS + INDEX, not forceable live

The raw index is live at scale (99 `raw` rows embedded at dimension 2048 in the
fixture workspace), and the raw-surfacing rule (raw turns surface only when
they lead the vector ranking, citing the source event id — D9) is pinned by the
deterministic fusion tests (`internal/memory/search_fusion_test.go`, including
the raw citation + suppression cases). Three live probes with noise-session /
thin-gist phrasings were each answered correctly from extracted stores with
valid citations — this workspace's gister/gist extraction is thorough enough
that raw never led on the probes, which is the designed behavior (raw is the
index-before-extract recoverability backstop, not a preferred channel).

## Smoke — PASS (with a pre-existing, unrelated environmental caveat)

`scripts/smoke.sh`: **756 assertions, 0 failures** with the `ONCLAW_LANGFUSE_*`
variables neutralized. With the default `.env` loaded, exactly one assert fails
("langfuse_url null without tracing") because the smoke server auto-loads the
repo `.env`'s Langfuse keys — **reproduced identically on clean HEAD** with all
wave-3 work stashed, so it is a pre-existing environmental leak, not a wave-3
regression (schedulers/run-history/tracing are untouched by this change).

`go build ./... && go vet ./... && go test ./...` — all green on the final tree.
