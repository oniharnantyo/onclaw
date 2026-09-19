# Memory System

Two memory tiers answer two different needs: the **kept documents** are the
manually curated, always-injected tier; the **extracted stores** are derived
from conversation history by a background pipeline and read on demand.
Architecture from Agent Zero Memory (arXiv 2608.29606) on OnClaw's substrate:
`session_events` as raw evidence, Postgres as the store, `RunFinished` as the
trigger, cheap-model side-calls for every pipeline LLM call.

```mermaid
flowchart LR
    classDef kept fill:#eef4ff,stroke:#2f6feb
    classDef ex fill:#f0faf0,stroke:#2f9e44

    subgraph KEPT["Tier 1 · kept documents — rent, always injected"]
        UD["USER.md<br/>one per workspace + user"]:::kept
        WD["WORKSPACE.md<br/>one per workspace"]:::kept
    end
    subgraph EX["Tier 2 · extracted stores — pay on use"]
        NOTES["memory_notes<br/>curated atomic facts"]:::ex
        EVENTS["memory_events<br/>episodic gists"]:::ex
    end

    TURN(["every turn"]) -->|"composed verbatim<br/>zero retrieval"| KEPT
    TURN -.->|"intent gate → prefetch<br/>memory.search tool"| EX
    EX -.->|"conflict = review flag<br/>never overwrites"| KEPT
```

Guarantees baked into the design: ingestion is off the hot path and never
fails a run; nothing is overwritten or erased (supersede + tombstone);
visibility is structural in the query layer, never prompt-policed; documents
outrank extracted notes.

## Component architecture

```mermaid
flowchart TB
    classDef onclaw fill:#eef4ff,stroke:#2f6feb
    classDef store fill:#f0faf0,stroke:#2f9e44
    classDef side fill:#fff7e8,stroke:#d97706
    classDef ui fill:#fdf0f6,stroke:#c2255c

    subgraph RUNNER["Runner · internal/agents"]
        COMPOSE["composeMemoryDocs<br/>intent gate → prefetch injection"]:::onclaw
        ENQUEUE["enqueueTurnIngest<br/>RunFinished · completed + failed"]:::onclaw
        CHIP["AppendMemoryChip<br/>x.memory_ingested event + broadcast"]:::onclaw
        MSEARCH["memory.search tool<br/>read-only, identity-bound"]:::onclaw
    end

    subgraph MEM["Memory pipeline · internal/memory"]
        WORKER["Worker<br/>queue 256 · concurrency 2<br/>per-session serialization"]:::onclaw
        GISTER["Gister<br/>per-run windowed gist"]:::onclaw
        GATE["Curation Gate<br/>ADD / UPDATE / SUPERSEDE / NOOP<br/>dedupe-before-write"]:::onclaw
        INTENT["IntentGate<br/>hard 1.5s · fail-open"]:::onclaw
        SEARCHER["Searcher<br/>hybrid lexical · zero model calls"]:::onclaw
        CONSOL["Consolidator<br/>nightly ~02:00 local<br/>merge · topics · report"]:::onclaw
    end

    subgraph LLM["Side-calls (providers pattern · Langfuse-traced · fail-soft)"]
        SC1["gister model"]:::side
        SC2["gate model"]:::side
        SC3["intent model"]:::side
        SC4["consolidator models"]:::side
    end

    subgraph PG["Postgres"]
        SE[("session_events<br/>raw evidence")]:::store
        ME[("memory_events<br/>episodic gists")]:::store
        MN[("memory_notes<br/>curated facts")]:::store
        MNE[("memory_note_evidence")]:::store
        MR[("memory_reports<br/>last morning report")]:::store
        DOCS[("user_memories + workspaces<br/>kept documents")]:::store
        TS[("workspace_tool_settings<br/>row key 'memory'")]:::store
    end

    subgraph WEB["Memory pane · Settings → Memory"]
        TABS["Facts · Report · Configuration"]:::ui
    end

    COMPOSE --> INTENT
    INTENT --> SC3
    COMPOSE --> SEARCHER
    MSEARCH --> SEARCHER
    SEARCHER --> MN
    SEARCHER --> ME

    ENQUEUE -->|"IngestJob"| WORKER
    WORKER --> GISTER
    WORKER --> GATE
    GISTER -->|"window"| SE
    GISTER --> SC1
    GISTER -->|"one gist row"| ME
    GATE -->|"turn events + top-8 similar"| SC2
    GATE -->|"ops"| MN
    GATE -.->|"doc digest, read-only"| DOCS
    COMPOSE -.->|"always injected"| DOCS
    WORKER -->|"chip payload"| CHIP
    CHIP -->|"x.memory_ingested"| SE

    CONSOL -->|"cluster + fold"| SC4
    CONSOL --> MN
    CONSOL --> MNE
    CONSOL --> MR
    TABS -->|"notes · events · report · settings"| MN
```

## Data model

Migrations `000054_memory_stores`, `000056_memory_evidence_reports`,
`000057_agent_memory_sidecall` (000055 dropped the legacy
`agent_daily_memories` daily-log tier this pipeline replaces).

```mermaid
erDiagram
    memory_events {
        uuid id
        uuid workspace_id "tenant partition - every row"
        uuid agent_id
        text session_id
        text turn_id
        text visibility "shared, user, agent"
        uuid user_id "owner when tier = user"
        text origin "manual, dialogue, infer, doc"
        timestamptz event_time "when it was true"
        timestamptz learned_at "when it was stored"
        text source_event_id "evidence pointer"
        text description
        text outcome
        jsonb participants
        timestamptz tombstoned_at
    }
    memory_notes {
        uuid id
        uuid workspace_id "tenant partition - every row"
        text visibility "shared, user, agent"
        uuid user_id "owner when tier = user"
        uuid agent_id "owner when tier = agent"
        text origin "manual, dialogue, infer, doc"
        timestamptz event_time "when it was true"
        timestamptz learned_at "when it was stored"
        text source_event_id "evidence pointer"
        text content
        int importance "0 to 10"
        bool pinned
        text topic
        text conflict_flag "doc-conflict = review flag"
        uuid supersedes
        uuid superseded_by
        uuid promoted_by "audited human widening"
        timestamptz promoted_at
        timestamptz tombstoned_at
    }
    memory_note_evidence {
        uuid note_id
        text source_event_id "extra evidence from merges"
        timestamptz added_at
    }
    memory_reports {
        uuid workspace_id "one row per workspace"
        jsonb report "consolidator wire contract"
        timestamptz generated_at
    }
    memory_notes ||--o{ memory_note_evidence : "cites"
    memory_notes |o--o| memory_notes : "supersedes"
```

The provenance tuple `(origin, event_time, learned_at, source_event_id)` is
NOT NULL on every row — no backfill path exists. Pipeline writes are always
`dialogue`-provenanced: extraction can never mint `manual`.

```mermaid
stateDiagram-v2
    [*] --> Active : gate ADD, provenance stamped
    Active --> Active : promote — human widens, audited
    Active --> Superseded : UPDATE / SUPERSEDE — new note born
    Active --> Tombstoned : user delete, deletion recorded
    Superseded --> Tombstoned
    note right of Superseded
        hidden from every read path by default
        history stays queryable
    end note
    Tombstoned --> [*]
```

## Ingestion pipeline

```mermaid
sequenceDiagram
    autonumber
    participant R as Runner
    participant W as Worker (background)
    participant G as Gister
    participant C as Curation Gate
    participant DB as Postgres
    participant M as Cheap-model side-calls

    R->>W: Enqueue(IngestJob) at RunFinished<br/>completed and failed runs
    Note over W: queue absorbs bursts · full queue drops<br/>jobs on one session serialize
    W->>G: Window(session)
    G->>DB: read events since last gist cursor
    G->>M: gist the window (one call)
    M-->>G: description · outcome · participants
    G->>DB: INSERT memory_events (provenance + participant-rule visibility)
    W->>C: Curate(turn events, window bounds)
    C->>DB: top-8 similar notes (dedupe context)
    C->>DB: USER.md / WORKSPACE.md digest (read-only)
    C->>M: propose ops per candidate fact (one call)
    M-->>C: ADD · UPDATE · SUPERSEDE · NOOP
    C->>C: clamp visibility to session ceiling<br/>explicit "remember this" → importance ≥ 8 + pin
    C->>DB: apply ops (supersede, never overwrite)
    W->>DB: append x.memory_ingested chip event (counts only)
```

| Op | Meaning |
|---|---|
| `ADD` | New fact (refused when too similar to an existing note) |
| `UPDATE` / `SUPERSEDE` | Correction — new note superseding the targeted one |
| `NOOP` | Trivial, transient, or unusable material |

One malformed op is skipped individually — never rejects the batch. An
explicit user "remember this" marks the note importance ≥ 8 and
pin-eligible; a fact contradicting the documents is stored with the
`doc-conflict` flag instead of touching them. The chip carries counts only,
never content — channels disclose private extractions without revealing
them. Scheduled and heartbeat runs never emit chips.

## Visibility model

```mermaid
flowchart TD
    classDef ceil fill:#f0faf0,stroke:#2f9e44

    JOB(["IngestJob: origin + human participants"]) --> Q1{"origin?"}
    Q1 -->|"scheduler / heartbeat"| A1["ceiling agent"]:::ceil
    Q1 -->|"channel"| Q2{"humans"}
    Q2 -->|"≥ 2"| S1["ceiling shared"]:::ceil
    Q2 -->|"1"| U1["ceiling user"]:::ceil
    Q2 -->|"0"| A2["ceiling agent"]:::ceil
    Q1 -->|"web DM · gateway DM"| U2["ceiling user"]:::ceil
    A1 --> CLAMP
    S1 --> CLAMP
    U1 --> CLAMP
    A2 --> CLAMP
    U2 --> CLAMP
    CLAMP["gate proposals clamp to ceiling<br/>store re-validates as last defense"]
```

```mermaid
flowchart LR
    P(["gist: humans in session"]) -->|"≥ 2"| GS["shared"]
    P -->|"1"| GU["user, owned by that member"]
    P -->|"0"| GA["agent"]
```

The human count resolves at enqueue time from session shape: channel runs
read the roster; Telegram/WhatsApp DMs count one human only when the sender
maps to a member (unmapped or group-origin senders count zero — external
membership never widens a workspace tier). The workspace **posture** switch
(`narrow` default, `org-shared`) raises only the gate's *default* proposal to
`shared`, never past the ceiling. **Only humans widen** (promotion, audited
in `promoted_by`/`promoted_at`); nothing narrows silently. Reads compute the
visible set from the caller: shared + own-user + serving-agent rows — no
query argument can widen it.

## Retrieval

```mermaid
sequenceDiagram
    autonumber
    participant T as Turn composition
    participant IG as Intent Gate
    participant P as Prefetch
    participant A as Agent
    participant MS as memory.search tool

    T->>IG: turn text
    Note over IG: one cheap-model call · hard 1.5s<br/>fail-open = self-contained
    alt self-contained · timeout · model error
        IG-->>T: docs only
    else needs deep memory
        IG->>P: route buckets notes / events
        P-->>T: ≤ 5 candidates · ≤ 500 chars total<br/>each with source event id + visibility
    end
    T->>A: injected documents + retrieved memory + citation rule
    A->>MS: query · from · until · visibility · topic · limit
    MS-->>A: provenance-bearing results<br/>or "Nothing is recorded in memory for this query."
    Note over A: only evidence opened this turn is citable<br/>every claim names its source event id<br/>nothing found → say nothing is recorded
```

`memory.search` is the only memory tool over the extracted stores:
read-only, zero model calls, identity bound at construction from the run's
`ToolContext` (arguments carry no identity fields; `visibility` can only
narrow). Scheduler/heartbeat origins, `/compact`, and empty turns skip the
gate entirely. The tool registers only when the composition root wires the
searcher — unwired deployments surface no tool at all, not a broken one.

## Consolidator and morning report

```mermaid
sequenceDiagram
    autonumber
    participant S as Nightly ~02:00 local · or consolidate-now
    participant C as Consolidator
    participant M as Side-calls
    participant DB as Postgres

    S->>C: run per workspace
    C->>DB: list shared-tier notes (≤ 2000)
    loop near-duplicate clusters · similarity ≥ 0.8 · cap 8 per call
        C->>M: merge call
        M-->>C: canonical note
        C->>DB: supersede members · stamp evidence links
    end
    loop topic folding · batches of 50
        C->>M: topic call
        C->>DB: labels ≤ 64 runes
    end
    C->>DB: persist morning report<br/>conflicts · merges · extraction failures
```

Copy-out only: per-owner (user/agent) notes are never folded — collapsing
them into another owner's view would be the one widening the design forbids.
The consolidator holds no document store at all, so it structurally cannot
write the kept documents. The report's extraction-failure count is the delta
of the worker's `Failed` counter since the last pass — silence is never
mistaken for success.

## Side-call model resolution

```mermaid
flowchart TD
    R(["side-call needs a model"]) --> Q1{"agent defines its own<br/>memory sidecall provider + model?"}
    Q1 -->|"yes — tier 1"| M1["agent override"]
    Q1 -->|"no"| Q2{"Memory settings<br/>side-call model set?"}
    Q2 -->|"yes — tier 2"| M2["workspace settings"]
    Q2 -->|"no — tier 3"| M3["the model the agent itself runs<br/>first configured agent for workspace-level callers"]
    M1 --> B
    M2 --> B
    M3 --> B
    B["resolve provider · decrypt key AAD = workspace id<br/>build model · trace under onclaw.memory.side_call"]
    B -->|"unresolvable"| F["stage fails soft<br/>raw session events intact"]
```

## Workspace settings

Stored as the structured workspace tool-settings row with key `memory`
(absence = defaults). Managed in **Settings → Memory → Configuration**.

| Setting | Values | Effect |
|---|---|---|
| Visibility posture | `narrow` (default) · `org-shared` | The gate's default proposed tier; ceilings still clamp |
| Ingestion toggle | on (default) / off | Turn-end extraction on/off |
| Side-call model | provider + model via the catalog combobox | Tier 2 of the resolution chain above |
| Embedding model | workspace provider + model + dimension | Saved and connection-tested; **vectors themselves are wave 3** — lexical-only today |

```mermaid
flowchart LR
    UI["Configuration tab"] -->|"PUT provider_id · model · dimension"| REC[("settings row 'memory'")]
    TEST(["Test connection"]) -->|"resolve provider_id"| PROV[("workspace provider record")]
    PROV -->|"base URL + key<br/>decrypted AAD = workspace id"| PROBE["embed probe string"]
    PROBE --> CHK{"reported dimension<br/>matches saved?"}
    CHK -->|"yes"| OK["200 ok + dimension"]
    CHK -->|"no"| NO["422 naming both dimensions"]
```

The embedding config is provider-driven: the user picks a workspace provider
from the dropdown and the endpoint and credential resolve from that record at
call time — the settings record stores only provider/model/dimension.
Unknown-provider and missing-model saves are 400s.

## API surface

All under `/api/v1/workspaces/{workspace}` (settings-mutating routes require
`WorkspaceWrite`):

| Route | Purpose |
|---|---|
| `GET / PUT /memory` | Workspace `WORKSPACE.md` document (32k-char cap; 422 over cap) |
| `GET / PUT /me/memory` | Caller's `USER.md` document |
| `GET /memory/notes` | Curated notes — `q`, `visibility`, `topic`, time window, `include_tombstoned` filters + visibility counts |
| `GET /memory/notes/:id` | One note with provenance |
| `POST /memory/notes/:id/promote` | Human visibility widening (audited) |
| `DELETE /memory/notes/:id` | Tombstone delete |
| `GET /memory/events` | Episodic gist timeline |
| `POST /memory/consolidate` | Consolidate now |
| `GET /memory/report` | Last morning report (empty shape: `generated_at: null`, zero counts) |
| `GET / PUT /memory/settings` | The settings record above |
| `POST /memory/settings/test` | Embedding connection test |

The UI (`web/src/screens/settings/MemoryPane.tsx`) presents everything as one
page with three tabs — **Facts** (notes + events browser, promotion, delete),
**Report**, **Configuration** — stating the precedence rule in copy.

## Evaluation

The wave-0 harness (`internal/memory/eval`, CLI `go run . eval-memory`)
seeds a workspace with LongMemEval-protocol scenarios against a running
server and scores recall, citation, and scope behavior — the scoreboard every
capability wave is measured against, and the gate for wave-3's vector
investment (`pgvector` + the dimension-menu columns sketched in the design's
D16; lexical-only is fully functional without them).

## Design references

- Change: `openspec/changes/integrate-agent-zero-memory/` (proposal, design
  D1–D16, specs `agent-memory-pipeline` / `agent-memory-retrieval` /
  `agent-memories`, per-paper diagrams).
- Research: `docs/memory-papers-survey.md`.
- Implementation: `internal/memory/` (worker, gister, gate, intent, searcher,
  consolidator, report, eval); runner seams in
  `internal/agents/runner.go` (`composeMemoryDocs`, `enqueueTurnIngest`,
  `AppendMemoryChip`); tools in `internal/agents/tools/memory.go`; HTTP in
  `internal/server/handlers/memory_notes.go`.
