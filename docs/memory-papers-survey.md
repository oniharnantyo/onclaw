# Agent Memory on arXiv — Best Benchmark Results & Fit to OnClaw

Research date: 2026-09-16. Scope: identify the arXiv paper on agent memory with the
strongest benchmark result that is actually adoptable by this codebase.

**Bottom line: `ByteRover: Agent-Native Memory Through LLM-Curated Hierarchical Context`
(arXiv:2604.01599, 2 Apr 2026) is the recommendation.** It holds the best published
LoCoMo score (96.1% LLM-judge, #1 on the leaderboard) and a top-tier LongMemEval-S
result (92.8%), while requiring **zero external infrastructure** — no vector database,
no graph database, no embedding service, all knowledge as human-readable markdown files
on the local filesystem. That constraint set is exactly OnClaw's.

> **Sept 2026 update — see §7.** A second sweep of arXiv (Aug–Sept 2026) found ~40 new
> memory papers, including a new LongMemEval leader (`Agent Zero Memory`, 95.60%) and,
> more importantly, a paper that challenges ByteRover's retrieval layer outright:
> `ReFind` (arXiv:2608.12888) shows **agent-controlled lexical search over the raw,
> unmodified record rivals structured memory** — with no index construction at all.
> §7 revises the recommendation accordingly.

---

## 1. What OnClaw's memory looks like today

Grounded in the actual code, not the aspiration:

| Aspect | Current state | Source |
|---|---|---|
| Scopes | 3 flat documents: `USER.md` (per workspace+user), `WORKSPACE.md` (per workspace), `MEMORY-DD-MM-YYYY.md` (per workspace+agent+date) | `openspec/specs/agent-memories/spec.md` |
| Storage | Postgres tables `user_memories`, `agent_daily_memories`, + `workspaces.memory` column | `migrations/000025_agent_memory.up.sql` |
| Format | Markdown text, server-side (not files in the agent jail) | same |
| Operations | `read` + `append` only. **No update, no delete, no search.** | `internal/agents/tools/memory.go` |
| Write path | Agent-side via the `memory` tool, or two human HTTP edit endpoints. Runtime-owned. | spec.md |
| Size limit | 32,000 chars (~8k tokens) per document, enforced on all write paths | `internal/domain/memory.go` |
| Retrieval | Whole-document read; `USER.md`/`WORKSPACE.md` are composed into instructions every turn | `docs/agent-runtime-architecture.md` |
| Indexing | **None** — no embeddings, no pgvector, no graph store anywhere in the repo | verified by grep |
| Agent substrate | Jailed agent dir with `ls read write edit glob grep` (no shell); MCP tools; denylist config | architecture doc |
| Concurrency | Atomic append at the store layer | memory.go |

So OnClaw is already: **agent-native (the agent writes its own memory via a tool),
markdown-based, and infrastructure-free.** It is missing hierarchy, mutation
(update/delete/merge), lifecycle/forgetting, and any retrieval index.

---

## 2. The leaderboards (as of Sept 2026)

### LoCoMo — LLM-as-judge accuracy (higher is better)

| System | Overall | Source | arXiv |
|---|---|---|---|
| **ByteRover 2.1.5** | **96.1** | ByteRover paper | **2604.01599** |
| Honcho | 89.9 | Honcho benchmarks | — |
| Hindsight (Gemini-3) | 89.6 | Hindsight paper | 2512.12818 |
| MemMachine v0.2 | ~91.7 | MemMachine paper | 2604.04853 |
| Hindsight (OSS-20B) | 83.2 | Hindsight paper | 2512.12818 |
| MemOS | 75.8 | MemOS paper | 2507.03724 |
| Zep (Graphiti) | 75.1 | Mem0 paper | 2501.13956 |
| Letta (MemGPT filesystem) | 74.0 | Letta blog | 2310.08560 |
| Full context (baseline) | 72.9 | Mem0 reproduction | — |
| MAGMA | 0.700 judge | MAGMA (ACL 2026) | 2601.03236 |
| Mem0 | 66.9 | Mem0 paper | 2504.19413 |
| RAG (baseline) | 61.0 | Mem0 reproduction | — |
| OpenAI Memory | 52.9 | Hindsight paper | — |

### LongMemEval-S — LLM-as-judge accuracy

| System | Overall | Notes |
|---|---|---|
| Chronos-High | 95.6 | Claude Opus 4.6 backbone — not a like-for-like comparison |
| MemMachine (optimal config) | 93.0 | six-dimension ablation |
| **ByteRover** | **92.8** | highest among comparable arXiv systems |
| Chronos-Low | 92.6 | GPT-4o backbone |
| Hindsight (Gemini-3) | 91.4 | peer-reviewed |
| Honcho | 90.4 | — |
| Hindsight (OSS-120B / 20B) | 89.0 / 83.6 | — |
| Zep (Graphiti) | 71.2 | — |
| Full context (GPT-4o) | 60.2 | ~115K-token baseline |

### LongMemEval-V2 — the newest and hardest benchmark (agentic environments)

LME-V2 (arXiv:2605.12493, UCLA) shifts from chat history to **web-agent environments**
(WebArena, WorkArena) with haystacks up to ~115M tokens, testing five abilities:
static state recall, dynamic state tracking, workflow knowledge, environment gotchas,
premise awareness.

| Method | Small | Medium | Latency |
|---|---|---|---|
| No retrieval | 0.013 | 0.013 | 0s |
| Best RAG baseline (slice+notes) | 0.510 | 0.459 | ~0.2s |
| AgentRunbook-R (RAG) | 0.586 | 0.570 | ~26s |
| Codex (off-the-shelf) | 0.699 | 0.687 | ~182s |
| **AgentRunbook-C (file-based + coding agent)** | **0.749** | **0.701** | ~108–140s |

Frontier models answer only 14.1% of LME-V2 questions from parametric knowledge alone.

### Caveat that matters

`Anatomy of Agentic Memory` (arXiv:2602.19320) shows these numbers are fragile:
benchmarks are underscaled, judge metrics are misaligned with semantic utility,
accuracy is strongly backbone-dependent, and cost is usually unreported. Treat a
single "best score" as directional, and prefer papers that report latency and tokens
alongside accuracy — which the recommendation does.

---

## 3. Recommendation: ByteRover (arXiv:2604.01599)

### What it is

An **agent-native memory architecture that inverts the memory pipeline**: the same LLM
that reasons about a task also curates, structures, and retrieves knowledge. Existing
systems (Mem0, Zep, MemOS) treat memory as an *external service* the agent calls into,
delegating to separate chunking/embedding/graph-extraction pipelines. ByteRover argues
that separation causes three failures — semantic drift between intent and what was
captured, lost coordination context, and fragile crash recovery — and replaces it with
markdown files the agent owns.

### Architecture

**Context Tree** — a file-based knowledge graph: `Domain >> Topic >> Subtopic >> Entry`.
Each entry is a standalone markdown file with YAML frontmatter, carrying:

- `ℛ` explicit relation set (edges written as `@domain/topic/file.md`, authored by the LLM)
- `𝒞` raw concept / provenance (task, changes, sources, timestamp, author)
- `𝒱` narrative (dependencies, rules, examples)
- `𝒮` snippets (code, formulas, raw data)
- `ℒ` lifecycle metadata

A **bidirectional reference index** gives O(1) forward and backlink lookup; a symbol
tree gives O(1) path→entry resolution. A compact tree listing (up to 200 entries) is
injected into the system prompt for ambient awareness without dumping contents.

**Adaptive Knowledge Lifecycle (AKL)** — this is the part OnClaw most needs:

- Importance `ι ∈ [0,100]`; `+3` per access, `+5` per update, **daily decay ×0.995**
- Maturity tiers with hysteresis to prevent oscillation:
  `draft → validated` at `ι ≥ 65` (demote `< 35`), `validated → core` at `ι ≥ 85` (demote `< 60`)
- Recency `r = exp(−Δt/30)` → ~21-day half-life
- Compound retrieval score: `w_r·BM25 + w_ι·î + w_t·r`

**Five atomic curate operations**, each carrying a `reason` field as an audit trail:
`ADD`, `UPDATE`, `UPSERT`, `MERGE` (combines two entries, deletes the source), `DELETE`
(single entry or whole subtree). Exposed as MCP tools `brv-query` and `brv-curate`.

**Five-tier progressive retrieval** — tiers 0–2 resolve with **no LLM call**:

| Tier | Mechanism | Latency |
|---|---|---|
| 0 | exact cache hit | ~0 ms |
| 1 | fuzzy cache (Jaccard) | ~50 ms |
| 2 | direct MiniSearch BM25 | ~100 ms |
| 3 | single optimized LLM call (1,024 tok, T=0.3) | < 5 s |
| 4 | full agentic loop (`code_exec`, `readFile`, ≤50 iters) | 8–15 s |

Index: MiniSearch BM25, fuzzy threshold 0.2, field boost title 5× / path 1.5×, max 32
results, max 8,000 chars content. Out-of-domain detection at `θ_OOD = 0.85` prevents
hallucinating answers from tangential hits. Writes use **atomic write-to-temp-then-rename**
and a **sequential task queue** to eliminate write-write conflicts without file locking.

### Reported results

| Benchmark | ByteRover | Next best comparable |
|---|---|---|
| LoCoMo overall | **96.1** | Honcho 89.9 (+6.2 pp) |
| LoCoMo single-hop / multi-hop / temporal | 97.5 / 93.3 / 97.8 | best on 3 of 4 categories |
| LongMemEval-S overall | **92.8** | Hindsight 91.4 |
| Cold query latency p50 / p95 / p99 (LME-S) | 1.6s / 2.3s / 2.5s | — |

**Ablation:** removing tiered retrieval costs **−29.4 points** (92.8 → 63.4). Removing
OOD detection or the relation graph costs only −0.4. The retrieval ladder is the load-bearing
component — the hierarchy alone is not what wins.

**Reported limitations:** the LLM-curated write path is expensive (a reasoning call per
curation event); novel queries that miss cache+index fall back to slow LLM tiers;
quality depends heavily on backbone capability; the in-memory index and sequential queue
target knowledge bases up to **~10K entries**; multi-session (84.2%) and open-domain
(85.9%) are the weak categories.

---

## 4. Why it fits OnClaw (and the others don't)

| ByteRover property | OnClaw today | Verdict |
|---|---|---|
| Markdown files, human-readable | Markdown docs in Postgres | **Aligned** — format matches; storage can stay DB-backed |
| No vector DB, no embeddings, no graph DB | No pgvector/embeddings anywhere in repo | **Hard constraint match** — the alternatives all require new infra |
| Memory ops are first-class agent tools | `memory` tool, runtime-owned writes | **Aligned** — OnClaw is already agent-native |
| Jailed sandbox + file tools for curation | Agent jail with `ls read write edit glob grep`, no shell | **Aligned** — substrate already exists |
| Atomic writes, no write-write conflicts | Store-level atomic append | **Aligned** |
| MCP tool surface (`brv-query`, `brv-curate`) | MCP support + denylist tool config | **Aligned** |
| `ADD/UPDATE/UPSERT/MERGE/DELETE` + `reason` | `append` only | **Gap** — needs store ops + spec change |
| AKL: importance, tiers, decay, forgetting | Flat 32k cap, human trim | **Gap** — solves the cap and the append-only blindness |
| 5-tier progressive retrieval + BM25 index | Full-document read / full-doc injection | **Gap** — the highest-value addition (−29.4 ablation) |
| Context Tree hierarchy | Flat 3 scopes | **Gap** — but the 3 scopes map onto tree roots |

The alternatives each fail the infra test:

- **MemMachine** (arXiv:2604.04853, LoCoMo 0.9169 / LongMemEval-S 93.0, 80% fewer input
  tokens than Mem0) — strong numbers, but needs episodic store + embedding-based
  contextualized retrieval + an LLM extraction pipeline. More machinery than ByteRover
  for a comparable score.
- **Hindsight** (arXiv:2512.12818, LoCoMo 89.6 / LongMemEval 91.4) — the strongest
  *peer-reviewed* design: four networks (world facts, experiences, entity summaries,
  beliefs) with retain/recall/reflect. Excellent conceptual fit for the belief/consolidation
  gap, but needs entity/temporal layers. Best used as a **design influence**, not a port.
- **MAGMA** (arXiv:2601.03236, ACL 2026, LoCoMo 0.700 / LongMemEval 61.2%) — four
  orthogonal graphs with policy-guided traversal. Wins on latency (1.47s) but requires a
  graph store.
- **A-MAC** (arXiv:2603.04549) — memory *admission control*: five interpretable factors
  (future utility, factual confidence, semantic novelty, temporal recency, content type
  prior), F1 0.583 at **−31% latency**, rule-based + a single LLM call. Cheap, no infra.
  **Best adopted as a write-gate complement to ByteRover's AKL.**
- **AgentRunbook / LME-V2** (arXiv:2605.12493) — best on the newest benchmark, and its
  winning variant is literally *file-based memory with a coding agent as the memory
  controller* (files + workflow doc + query-time manifests + helper script). Independent
  evidence that OnClaw's file-based substrate is the right bet, and it argues for keeping
  a Tier-4-style agentic retrieval fallback.

---

## 5. Adoption path (if you want to act on this)

Ordered by value-per-effort, each independently shippable:

1. **Add a BM25/full-text retrieval path to the `memory` tool** (`search` action, or a
   new `memory_search` tool). Postgres `tsvector` + GIN index gets tier-2 behavior with no
   new dependency. This is the −29.4-point component in ByteRover's ablation.
2. **Add `update` / `delete` / `merge` actions** with a `reason` argument, keeping the
   existing append atomicity. Amends the `agent-memories` spec's "no overwrite action"
   requirement — a deliberate contract change, so it belongs in an openspec change.
3. **Add lifecycle metadata** (importance, last-accessed, tier) so the 32k cap becomes a
   *decay* policy instead of a human-trim chore. AKL's constants are directly copyable.
4. **Promote the three scopes into tree roots** — e.g. `workspace/…`, `user/…`, `daily/YYYY-MM-DD/…`
   — and inject a compact tree listing instead of full documents once a scope outgrows
   its budget.
5. **Keep a tier-4 agentic fallback.** OnClaw already has jailed `glob`/`grep`/`read`;
   that is exactly ByteRover's Tier 4 and AgentRunbook-C's mechanism.

Do **not** introduce pgvector/embeddings to chase the last few points — every paper above
that beats ByteRover on a single category does so by adding infrastructure, and the
`Anatomy` paper warns the deltas are backbone- and judge-sensitive.

---

## 6. Reference list

| Paper | arXiv | Key contribution | Best score |
|---|---|---|---|
| **ByteRover: Agent-Native Memory Through LLM-Curated Hierarchical Context** | **2604.01599** | File-based Context Tree, AKL, 5-tier retrieval, no infra | **LoCoMo 96.1**, LME-S 92.8 |
| MemMachine: A Ground-Truth-Preserving Memory System | 2604.04853 | Episodic ground truth + contextualized retrieval + Retrieval Agent | LoCoMo 0.9169, LME-S 93.0 |
| Hindsight is 20/20 | 2512.12818 | retain/recall/reflect; 4 networks | LoCoMo 89.6, LME 91.4 |
| MAGMA: Multi-Graph Agentic Memory | 2601.03236 | Semantic/temporal/causal/entity graphs, policy-guided traversal | LoCoMo 0.700 |
| Adaptive Memory Admission Control (A-MAC) | 2603.04549 | 5-factor admission gating, −31% latency | LoCoMo F1 0.583 |
| LongMemEval-V2 (AgentRunbook) | 2605.12493 | Agentic-environment memory benchmark; file-based + coding-agent memory | LME-V2 72.5% avg |
| Memory for Autonomous LLM Agents (survey) | 2603.07670 | write–manage–read loop; 3D taxonomy; 5 mechanism families | — |
| Anatomy of Agentic Memory (survey) | 2602.19320 | Evaluation fragility: saturation, judge sensitivity, backbone dependence | — |
| Mem0 | 2504.19413 | Vector+graph extraction pipeline | LoCoMo 66.9 |
| Zep / Graphiti | 2501.13956 | Temporal knowledge graph with validity windows | DMR 94.8 |
| MemOS | 2507.03724 | MemCube memory operating system | LoCoMo 75.8 |
| A-MEM | 2502.12110 | Zettelkasten-style atomic notes | — |
| MemGPT / Letta | 2310.08560 | Hierarchical core/buffer/archival | LoCoMo 74.0 |
| LoCoMo benchmark | 2402.17753 | 1,540 long-conversation questions | — |

### Reading order

1. **2604.01599** (ByteRover) — the recommendation, and the only one that matches OnClaw's
   no-infrastructure constraint.
2. **2602.19320** (Anatomy) — read before trusting any number above.
3. **2603.07670** (survey) — for the write–manage–read framing and the advice to start at
   "context + retrieval store" and only graduate to learned control when data demands it.
4. **2605.12493** (LME-V2 / AgentRunbook) — where the benchmark frontier is moving, and
   independent validation of the file-based approach.

---

## 7. Update — papers from August–September 2026

Second sweep, 2026-09-16, via the arXiv API sorted by submission date. ~40 new
memory papers in the last six weeks alone. This section supersedes parts of §3–§5.

### 7.1 The most consequential new finding: ReFind

**`When Your Agent Opens the Chat App: Agent-Controlled Search over Raw Chat Logs Rivals
Structured Memory` (arXiv:2608.12888, 13 Aug 2026).**

This paper asks the question OnClaw should care about most: *how much of the benefit
credited to memory structure comes from the structure itself, versus from competent
retrieval over the raw history?* Its system, ReFind, **builds no semantic structure at
all** — the conversation archive is left unmodified, indexed lexically at turn
granularity, and searched by a generic iterative keyword loop plus four chat-native
controls (session-aware rank fusion, local context expansion, temporal narrowing,
skipping already-inspected sessions).

| Result | ReFind | Comparison |
|---|---|---|
| MemoryAgentBench mean accuracy | **58.2** | HippoRAG 2 (strongest graph/tree system) 53.2 |
| LongMemEval-S | **93.2 ± 3.3** | GPT-5-mini backbone |
| LongMemEval-M | **89.3 ± 6.0** | same |

No LLM-based index construction anywhere in the pipeline. Ablations separately support
the roles of agent control, chat-native controls, and lexical retrieval, and it beats
matched agentic dense/hybrid variants.

**Why this changes the recommendation:** ByteRover's load-bearing component is a 5-tier
ladder whose tiers 0–2 need a prebuilt MiniSearch index. ReFind shows you can skip the
index build entirely and let the agent drive lexical search — which OnClaw can already do
today with the jailed `glob` / `grep` / `read` tools. This is the cheapest possible path
to the −29.4-point component, and it requires no schema, no index, no embeddings.

### 7.2 Corroborating evidence for raw / verbatim storage

**`DreamBench-SWE: A Multi-Session Memory-Hygiene Benchmark for Software Agents`
(arXiv:2608.20664, 21 Aug 2026).** 360 work units, hidden executable oracles:

| Condition | Passes |
|---|---|
| No external memory | 21/180 (11.7%) |
| Deterministic **verbatim event memory** | 82/180 (45.6%) |
| Typed + raw reference probe | 83/180 (46.1%) |
| One pinned hosted Mem0 literal-storage config | 97/180 (53.9%) |

Verbatim raw records capture most of the available gain. The paper is unusually honest
about what it does *not* establish (no superiority claim among memory-bearing conditions,
no mechanism claim). It is also the closest benchmark to OnClaw's actual use case — a
multi-session software-agent workspace.

### 7.3 The new LongMemEval leader (and why it doesn't change the answer)

| Paper | Date | LongMemEval | LoCoMo | Infra required |
|---|---|---|---|---|
| **Agent Zero Memory** (2608.29606) | 30 Aug | **95.60** | 93.60 | **PostgreSQL + pgvector — one database, no graph DB** |
| **post-graph-rag** (2608.24921) | 14 Aug | 94.0 | — | **PostgreSQL only** (pgvector + edge tables) |
| ReFind (2608.12888) | 13 Aug | 93.2 | — | none |
| **ByteRover** (2604.01599) | 2 Apr | 92.8 | **96.1** | none |
| SodaMem (2608.08055) | 8 Aug | 92.8 | — | temporal graph + hybrid index |
| LycheeMemory V2 (2608.12990) | 13 Aug | 92.20 | 89.22 | typed records + structured indexes |
| MindMemOS (2608.12428) | 12 Aug | — | 94.03 | entity-property-time structure |

Agent Zero Memory claims a new SOTA at 95.60% (+0.73 over the strongest prior) and 93.60%
on LoCoMo, but it runs **three parallel memory systems** (episodic timeline, entity-event
knowledge graph, hierarchical documentary memory) with hybrid embedding + lexical search.
Its most interesting secondary result is backbone-insensitivity: accuracy varies only 3.4
points across eight LLMs while per-query cost varies ~30× — "the signature of
memory-driven, rather than model-driven, quality."

**ByteRover still holds LoCoMo at 96.1%.** Note that ECHO's headline 96.29% is Hit@10,
a retrieval metric, not comparable to an LLM-judge score.

**`post-graph-rag` deserves special attention:** it is **PostgreSQL-native** — chunks with
embeddings, a canonical entity graph, and community summaries all in one database, with
pgvector for search and edge tables for traversal. It scores 94.0% on LongMemEval and its
single largest contributor is **temporal grounding in the prompt** (carrying each
relation's validity period through to synthesis), which moves temporal reasoning from
0.496 to 0.881. OnClaw already runs Postgres, so this is the only high-scoring design
that would not require a new datastore class — though it does require pgvector.

### 7.4 Direct critiques of OnClaw's current design

**`SodaMem` (arXiv:2608.08055, 8 Aug 2026)** names the problem explicitly: *"Flat RAG
diaries and Markdown logs optimize needle retrieval but under-serve currency, provenance,
and ordered temporal reasoning."* OnClaw's `MEMORY-DD-MM-YYYY.md` daily logs are exactly
the "markdown logs" being critiqued. SodaMem's fix is typed `FactEvent`s with mandatory
provenance spans plus `SUPERSEDES` / `CONTRADICTS` / `UPDATES` edges — reaching 92.8% on
LongMemEval-S at **$0.00161 per question** (~18.3k tokens).

**`What Eviction Destroys` (arXiv:2609.08279, 8 Aug 2026)** is the most directly actionable
new result. It introduces a *restore counterfactual*: reinstate the gold evidence in the
read-time context and re-run the same reader, then classify each error as recoverable,
irreversible, or residual. Findings on LongMemEval-S:

- At an **8k-token budget, the irreversible share reaches 1.00 for all four eviction
  policies** — everything evicted is gone for good.
- At 80k tokens under top-k retrieval it is 0.67–0.73 (0.60 for LLM-importance eviction).

OnClaw's cap is **32,000 characters ≈ 8k tokens** — precisely the regime where eviction is
100% irreversible. This converts the size cap from an inconvenience into a data-loss
mechanism, and argues that the cap must be paired with consolidation *before* eviction,
not after.

### 7.5 The consolidation / lifecycle cluster (OnClaw's biggest gap)

| Paper | Date | Idea | Result |
|---|---|---|---|
| **MemoryLACE** (2609.03201) | 2 Sep | Sparse merge / supersession / contradiction relations over atomic memories; reconstructs relation-aware evidence units | Highest overall on BEAM + StructMemEval; **−66.6% runtime vs Hindsight** |
| **Dual-Layer Agentic Memory** (2608.22215) | 23 Aug | Write-phase routing (non-write / write-new / write-update) + slow parametric consolidation | Prunes **68%** of redundant memory, retains **>98%** QA EM |
| **REALM** (2609.16053) | 13 Sep | Memory reconsolidation: retrieval *feedback* reorganizes memory | 75.97 LoCoMo / 65.11 LongMemEval (+7.17 / +1.31 over baselines) |
| **MemForest** (2609.08273) | 8 Sep | EventTree partitioning + progressive merging | **97.1%** performance retained at **50% compression**, 1.89× retrieval speedup |
| **Weighted Memory Tree** (2608.20631) | 21 Aug | Dynamic retention scores; event-based updates, selection-based decay | **+9.97 pp** accuracy, **−32.8%** prompt tokens (GAIA-Text) |
| **Selective Forgetting** (2608.28978) | 29 Aug | Prune by recency × access × centrality × age | Removes 9.8% of nodes, F1 unchanged — **but the graph itself lost to a flat vector baseline** |

Two things worth flagging. First, **MemoryLACE's −66.6% runtime vs Hindsight** is the
efficiency headline of the batch: explicit local lifecycle relations beat global
reflection. Second, **Selective Forgetting is a negative result on graphs** — the
extraction-based graph scored token F1 0.417 vs 0.468 for a flat vector baseline
(Δ = −0.050, 95% CI [−0.085, −0.016]), with the worst damage on questions needing a
specific prior assistant turn (0.911 → 0.607), because decomposing a turn into entities
discards the surface form. The *forgetting* module worked; the graph did not. This is
further support for keeping raw text rather than extracting structure.

### 7.6 Operational and enterprise findings

**`Memory as Infrastructure` (arXiv:2609.05510, 31 Aug 2026)** is the most unusual paper in
the batch: an operational record from a single continuous agent session driving a
633,000-line codebase since January 2026, with the memory subsystem instrumented since
July. 78,933 hook invocations, 85 recorded failures, **none silent**, none in the final 20
days. Its contribution is treating the memory subsystem as an SRE surface: a session-start
health gate with discriminated failure modes, heartbeat telemetry designed so no
enumerated failure mode can pass unrecorded, and alert-fatigue budgeting. Its stack —
hybrid lexical-vector retrieval over SQLite, fully local — is close to OnClaw's shape, and
OnClaw already has a hooks system to hang this on. Caveats are stated plainly: N=1, no
control arm, self-reported.

**`Grounding Agent Memory: Environment-Probing Curation for Enterprise Agents`
(arXiv:2609.11060, 10 Sep 2026)** gives a post-task curator agent least-privilege
**read-only world tools** to check, scope, and refresh candidate memories — no retraining,
no change to the task agent, retriever, or memory representation. On a production-like
GitHub Copilot harness: CLBench pass rate **39% → 73%**, pass-discounted reward 8.60 →
22.60, queries per question 8.8 → 4.7, task-agent cost **$3.38 → $1.68**. Across six APEX
worlds all 18 memory-vs-baseline reward comparisons were positive. For a multi-tenant
enterprise workspace, the "least-privilege read-only world tools" pattern is directly
transferable and is a natural extension of OnClaw's jailed tool model.

**`Total Recall at What Cost?` (arXiv:2608.11879, 12 Aug 2026)** benchmarks serving cost
across Mem0, Hindsight, and Mastra Observational Memory vs rolling-window and full
transcript, up to 400 turns. Three findings: serving cost **cannot** be predicted from
conversation length and message size (a regression tracking the reference strategies
misses the memory systems by 18–69%); break-even against full transcript ranges from the
first tens of turns to **never within 400 turns**; and **no system wins on both axes**
(accuracy spans 21–54%, with backbone choice driving cost as much as the memory system).

### 7.7 Security and governance (new, and relevant to multi-tenancy)

A cluster that did not exist in the earlier sweep, all from Aug–Sept 2026:

| Paper | arXiv | Concern |
|---|---|---|
| MemSentry | 2609.08747 | Detecting persistent memory poisoning in agentic AI |
| InjecMEM | 2608.23471 | Memory injection attack on agent memory systems |
| MutMem / MutMem-V2 | 2608.02843 / 2609.01235 | Cryptographically authorized mutation in persistent memory |
| Revoked but Still Authoritative | 2609.08258 | Empirical study of **revocation enforcement** in agent-memory systems |
| MAPLE-Guard | 2608.00426 | Memory-link poisoning in multi-agent systems |
| Utility Under Attack | 2608.21230 | Limits of content screening and provenance ranking |

For a multi-tenant product where memory is scoped per workspace, user, and agent, the
revocation-enforcement and authorized-mutation papers are worth reading before exposing
memory editing over the management API. OnClaw's structural scoping (the tool is
constructed with the run's identity, so no argument can address another principal) is a
good foundation, but these papers show that *revocation* and *provenance ranking* are
separate, unsolved problems.

### 7.8 Others worth a look

- **LSREP + ICE v2** (2609.16730, 15 Sep) — a *longitudinal state-replay* evaluation
  protocol (1,985 turns, 219 probes, 52 checkpoints) plus a local-first memory middleware
  with typed stores, retrieval fusion, and dynamic context budgets. Notably self-critical:
  its fidelity audit found procedural retrieval defective and graph utility unestablished,
  and ICE v2 lost decisively to pure vector-RAG on a dense dataset.
- **EchoPath** (2609.16635, 15 Sep) — converts artifact-validated GUI trajectories into
  parameter-controlled **callable memories shaped like MCP tool calls**, with
  preconditions, GUI evidence, and lifecycle state. Directly relevant given OnClaw's
  browser tool and MCP support.
- **CueMem** (2609.12354, 11 Sep) — memory records as *retrieval cues* linked to source
  turns, reconstructing evidence from the original dialogue rather than treating records
  as self-contained.
- **ECHO** (2608.21755, 22 Aug) — a **cautionary tale**. Headline numbers (96.29% Hit@10,
  97.60% on LongMemEval-S) sit alongside a self-reported post-hoc audit that found
  source-specific phrases in the query-expansion rules, and a matched sample where Mem0
  OSS beat it 64.84% vs 41.76% (McNemar p = 0.00107). It labels its own results
  "descriptive development measurements, not independent confirmation." Read it as a
  lesson about benchmark hygiene.
- **Zeta-Lite** (2609.01818, 1 Sep) — a WebAssembly SQL engine with snapshot-isolated
  concurrent transactions and copy-on-write database **branching** (fork / merge / rebase),
  argued as a fit for agentic memory where cheap branchable state lets an agent explore
  and commit or discard speculative work.
- **ICML** (2609.17088, 15 Sep) and **ThinkFlow** (2609.17010, 15 Sep) — the learned-policy
  end of the spectrum: memory management as a trainable interactive policy (Planner +
  Trigger agents with delayed rewards), and latent probabilistic memory that bypasses text
  entirely. Both are research-grade; the 2026 survey (§6, arXiv:2603.07670) advises
  starting at "context + retrieval store" and graduating to learned control only when
  empirical data demands it.

### 7.9 Revised recommendation

The Aug–Sept batch does not overturn ByteRover as the *structural* blueprint, but it
changes the **retrieval** and **lifecycle** halves of the plan:

1. **Structural blueprint — unchanged: ByteRover (2604.01599).** Still #1 on LoCoMo
   (96.1%), still the only top-scoring design with no external infrastructure. The Context
   Tree, the five curate operations with `reason` fields, and the AKL constants remain the
   right targets.
2. **Retrieval — replace the 5-tier ladder with agent-controlled lexical search.**
   ReFind (2608.12888) shows 93.2 on LongMemEval-S with **no index build at all**, and
   DreamBench-SWE (2608.20664) shows verbatim storage captures most of the gain. OnClaw's
   jailed `glob` / `grep` / `read` already are this mechanism. Postgres `tsvector` + GIN
   remains a cheap accelerator, not a prerequisite. Skip embeddings.
3. **Lifecycle — adopt MemoryLACE-style local relations + dual-layer write routing.**
   Sparse merge / supersession / contradiction over atomic memories, at −66.6% runtime vs
   Hindsight (2609.03201), plus write-phase routing that prunes ~68% of redundant writes
   while retaining >98% EM (2608.22215). This is what makes the cap survivable.
4. **Treat the 32k cap as a data-loss bug, not a policy.** `What Eviction Destroys`
   (2609.08279) shows irreversible loss reaches 1.00 at an 8k-token budget — exactly
   OnClaw's regime. Consolidation must happen *before* eviction.
5. **If you ever do add infrastructure, add Postgres-native bi-temporal memory first.**
   `post-graph-rag` (2608.24921) gets 94.0% on LongMemEval inside one PostgreSQL database,
   with prompt-level temporal grounding as its single largest contributor (temporal
   reasoning 0.496 → 0.881). That is the smallest possible infra step from where OnClaw is.
6. **Read the security cluster before shipping memory-editing endpoints.** Revocation
   enforcement (2609.08258) and authorized mutation (2609.01235) are unsolved adjacent
   problems, and multi-tenancy makes them load-bearing.

---

## 8. Deep dive: Agent Zero Memory (arXiv:2608.29606)

*Agent Zero Memory: Provenance-Aware Long-Term Memory for LLM Agents.* Penyuan Zhu,
Ming Wu (Zero Labs). Submitted 30 Aug 2026. cs.CL.

**Headline:** 95.60% LongMemEval, 93.60% LoCoMo — claimed +0.73 and +1.10 over the
strongest prior systems.

**Correction to §7.3:** the infrastructure requirement is milder than it first appears.
All three memories and both indexes live in a **single PostgreSQL instance**. The
"three parallel memory systems" are *logical* stores, not three databases — the entity-event
graph is stored relationally as node and edge rows with provenance, and dense retrieval is
pgvector. **No graph database, no external vector database.** For OnClaw, which already runs
PostgreSQL, this is a pgvector extension rather than a new datastore class.

### 8.1 The three memories: ℳ = (𝒯, 𝒢_E, 𝒟)

| Memory | What it holds | Answers |
|---|---|---|
| **Memory Events** (𝒯) | A merged cross-source *timeline of events* — what happened, when, how it unfolded. The primary unit of recall; raw chat/file is opened only for detail. | *what*, *when*, *what changed*, from which source → temporal + knowledge-update questions |
| **Ontology Graph** (𝒢_E) | An entity–event graph linking people, teams, and projects to the events involving them, built by an agentic "connect" stage. | multi-hop questions whose evidence is scattered across sessions |
| **Hierarchical Documentary Memory** (𝒟) | Durable facts — profile, standing preferences, key people/projects — in six categories (*profile, preferences, entities, milestones, cases, patterns*). | stable profile/preference questions |

HDM is **hierarchical in detail level**: each entry has an L₀ one-line abstract, an L₁
overview, and the full L₂ text, with a **hard cap on full opens**. A reader orients on the
cheap abstract and escalates only when needed. Critically, HDM is built by a
**deterministic mapping from the user's own curated notes — not by fact extraction** — so it
"cannot invent or drift from what the user wrote."

The build pipeline runs **once, in the background, off the conversational hot path**, in
four stages: preprocess & classify (chats / files / agent sessions) → index (embedding **and**
lexical) → extract (Events + HDM) → connect (the entity-event graph). Raw sources stay
indexed, so a misjudged extraction is recoverable.

### 8.2 Retrieval: intent gate → source router → three parallel agentic searches → integrate

1. **Intent gate** — a small fast classifier decides whether the turn needs memory at all.
   **Self-contained turns pass straight through at zero added latency** (`return ∅`).
2. **Source router** — narrows to the source buckets likely to hold the evidence.
3. **Three concurrent agentic searches**, one per memory, each a **tool-using loop** that
   queries by embedding *and* exact term, applies agent-controlled filters (time window,
   tag, source, speaker), and **opens raw chunks on demand** before finalizing a
   citation-locked partial answer. Reranking is **deliberately skipped when the reader asks
   for temporal order**, so "latest / since T" queries stay chronological.
4. **Integrate** — union of citations across the three partials plus an aggregated
   confidence, returned as one cited answer.

### 8.3 The citation lock — the most transferable idea

Two formal definitions carry the whole design:

- **Provenanced item** — every learned item is a pair `(x, π(x))` where
  `π(x) = ⟨origin, immutable timestamp, evidence pointer⟩`, with origin drawn from
  `{scan, infer, doc, dialogue, manual}`.
- **Citation-locked answer** — given `O` = the set of items the reader **actually opened**,
  an answer `(a, C, κ)` is citation-locked iff **`C ⊆ O`** and **every atomic claim of `a`
  is supported by some item in `C`**. A reader that cannot assemble such a `C` **must return
  `a = ⊥`** — abstention.

Because the reader's interface exposes no channel through which unopened material can be
cited, fabrication is **structurally excluded rather than merely discouraged** — a syntactic
property with a semantic consequence. Three postulates follow: **(P1)** layering, where raw
source is segregated from distilled memory and curated facts from revisable inference *by
type discipline rather than policy*; **(P2)** provenance and calibrated belief, with
abstention over guessing; **(P3)** grounded, structure-aware retrieval, where answers come
from **traversing** navigable structure rather than unconstrained generation.

### 8.4 Results

**LongMemEval (500 questions):** Agent Zero 95.60 · Mastra 94.87 · Hindsight 91.40 ·
EmergenceMem 86.00 · Supermemory 85.20 · Zep 71.20.

**LoCoMo (1,540 questions):** Agent Zero 93.60 · Mem0 92.50 · **ByteRover 2.0** 92.20 ·
Hindsight 89.60 · Memobase 75.80 · Zep 75.10.

> **Version caveat worth knowing.** The LoCoMo comparison is against **ByteRover 2.0
> (92.20)**, not ByteRover **2.1.5**, whose paper reports **96.1**. So the claim "beating the
> strongest prior by +1.10" is measured against Mem0's 92.50 and does not account for the
> newer ByteRover. On LoCoMo, ByteRover 2.1.5 still leads. This is exactly the
> cross-paper comparability problem `Anatomy of Agentic Memory` (2602.19320) warns about —
> the paper's own threats-to-validity section concedes competitor figures come from
> "each obtained under its own harness."

### 8.5 Retrieval-channel ablation — the number that matters most for OnClaw

Backbone fixed at gpt-5.6-sol, all other components unchanged, agentic searches restricted
to a single channel:

| Retrieval channel exposed to the agent | LongMemEval | Δ |
|---|---|---|
| embedding **++** lexical (hybrid) | **95.20** | — |
| Embedding only | 94.00 | −1.2 |
| **grep (exact substring, no ranking, no fuzzy)** | **93.60** | **−1.6** |
| Lexical only (BM25 + fuzzy) | 93.40 | −1.8 |

**`grep` alone reaches 93.60% — within 1.6 points of the hybrid SOTA.** OnClaw's jailed
agent already has `grep` over its files. This is now the **second independent paper** to show
that agent-controlled lexical retrieval over raw records gets you nearly all the way there
(the first being ReFind, 2608.12888, §7.1). The authors' own framing supports this: the
structured, provenanced stores plus the agentic reading discipline "carry most quality; the
hybrid retriever contributes the final margin that sets the SOTA."

The two channels are complementary — embeddings catch paraphrase with no lexical overlap,
lexical catches exact identifiers and rare terms that similarity blurs — but the margin is
small and the cost of adding embeddings is not.

### 8.6 Backbone study across eight LLMs

Only the model varies; memory, retriever, and control logic are fixed. This is the
paper's **controlled** experiment (unlike the cross-system tables).

| Backbone | Acc. | Median latency | Avg cost/query | Prompt tok. | Cached-in tok. |
|---|---|---|---|---|---|
| gpt-5.5 | 95.60 | 16.3 s | $0.034788 | 12,225 | 5,325 |
| gpt-5.6-sol | 95.20 | 15.8 s | $0.030663 | 11,235 | 5,990 |
| gpt-5.6-terra | 94.00 | 21.2 s | $0.018913 | 11,881 | 6,413 |
| opus4.8 | 93.80 | 12.6 s | $0.052221 | 23,779 | 14,898 |
| sonnet5 | 93.20 | 10.8 s | $0.028520 | 24,520 | 17,807 |
| glm5.2fast | 93.00 | **6.2 s** | $0.014139 | 10,986 | 5,118 |
| glm5.2 | 92.60 | 8.3 s | $0.010525 | 10,986 | 4,630 |
| deepseek-v4-pro | 92.20 | 16.9 s | **$0.001768** | 17,436 | 15,373 |

Three findings:

1. **Accuracy spans only 3.4 points across all eight backbones** (92.20–95.60) while
   **cost spans ~30×**. The authors call this "the signature we would predict from a
   memory-driven, rather than model-driven, design."
2. **No backbone wins all three axes:** gpt-5.5 maximizes accuracy, glm5.2fast minimizes
   latency, deepseek-v4-pro minimizes cost.
3. **Cost is not a function of token volume.** Cached-input tokens are billed at reduced
   rates and retrieval-heavy prompts cache extremely well — the cached fraction ranges from
   43.6% (gpt-5.5) to 88.2% (deepseek-v4-pro). opus4.8 and sonnet5 push the *most* tokens yet
   are not the most expensive. Accuracy-per-cent spans from 18.0 (opus4.8) to 521
   (deepseek-v4-pro) — the most cost-efficient backbone delivers ~19× more accuracy per
   dollar for a 3.4-point loss.

### 8.7 The failure taxonomy of competing systems

The related-work section is unusually useful, identifying three recurring axes:

- **Write-side** — LLM-per-message curation (Mem0, Zep, Hindsight, ByteRover) pays ingestion
  cost linear in history, in the hot path. Zep is measured at **over 600k tokens to ingest a
  single LoCoMo conversation**.
- **Read-side** — query-independent reads pay for memory they don't need: ever-present logs
  (Mastra, MemGPT) or retrieval cost growing with corpus (HippoRAG's whole-graph PageRank,
  Emergence's whole-session injection).
- **Epistemic** — overwrite-on-update systems (Mem0, Memobase, Mastra's reflection,
  Hindsight's newest-wins merge) **cannot distinguish a correction from its predecessor**;
  and per-user/per-project stores (Memobase, Hindsight, ByteRover) **cannot become shared
  organizational memory**.

**OnClaw's position against these:** it is structurally immune to the third-axis overwrite
problem, because append-only cannot silently destroy a predecessor. And `WORKSPACE.md` is
already the shared organizational memory that per-user stores cannot provide. OnClaw's
exposure is the **read-side** axis — full-document injection every turn is precisely the
"ever-present logs" pattern this taxonomy criticizes.

### 8.8 Reported limitations

- Component-level ablation of **the three memories and the intent gate is missing** — only
  the retrieval-channel ablation was run. Quantifying each mechanism's marginal contribution
  is future work.
- All accuracies are **single-run** under the benchmark's LLM-judged protocol.
- Cross-system comparisons are **best publicly reported numbers from heterogeneous
  harnesses**, not controlled head-to-head trials. Only the backbone study is controlled.
- Costs drift as list prices change (token counts are reported so analyses can be repriced).
- Open problems the authors name: calibration of confidences into justified probabilities;
  conflict arbitration when several valid versions of a fact coexist (including **when to ask
  the user rather than choose**); and forgetting as graceful down-weighting with tombstones
  for auditability, rather than deletion.

### 8.9 What OnClaw should take from it

1. **The citation lock.** A reader that can only cite what it opened, and must return
   abstention otherwise, is a cheap and genuinely novel guardrail — and it fits a
   multi-tenant product where ungrounded memory claims are a liability. This is the single
   most valuable idea in the paper.
2. **The intent gate.** Skipping memory retrieval entirely on self-contained turns is a
   latency win OnClaw can capture today, since its memory documents are injected every turn.
3. **Provenance as a type, not a policy.** `origin ∈ {scan, infer, doc, dialogue, manual}`
   plus an immutable timestamp and an evidence pointer, with curated facts segregated from
   revisable inference *by type discipline*.
4. **HDM's three detail levels with a hard cap on full opens.** This is a direct answer to
   the 32k-char cap problem: store more, inject less, escalate on demand.
5. **Deterministic mapping beats extraction for durable facts.** HDM is built from the
   user's curated notes rather than by LLM fact extraction precisely so it cannot drift —
   consistent with the negative graph result in `Selective Forgetting` (§7.5).
6. **Don't add embeddings yet.** grep alone is 93.60% here; the hybrid buys 1.6 points.


