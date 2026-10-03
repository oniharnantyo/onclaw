# Skill Curation Roadmap (v1 → vN)

The staged plan for the self-improvement loop that curates skills from long tool-call
traces. v1 is captured as the OpenSpec change `add-skill-curation-from-traces` (strict-valid,
not yet implemented); every later version is a separate, evidence-gated change. Each stage
loosens exactly one dependency on human attention — and only after the previous stage's
evidence machinery makes that safe.

```
 v1  CURATE, HUMAN-GATED          v2  MEASURED TRUST              v3  THE LOOP TESTS ITSELF         v4+  RECURSION
 ─────────────────────────        ─────────────────────           ──────────────────────────        ────────────────
 trace → qualify → wiki →         + shadow evaluation             + executing author                + skill lineage DAG
 propose → HUMAN GATE →           + auto-approve (PACE gate)      + surrogate verifiers             + meta-level: the curation
 materialize (agent-scoped)       + agentic maintainer            + outcome-trained curator           pipeline tunes itself
 + probation, budget, audit       + agent → workspace promotion   (SkillOS / CoEvoSkills)           (MetaSkill-Evolve slow loop)

 gate = human                     gate = evidence + spot-check    gate = execution + statistics     gate = lineage fitness
```

Ground rules that hold at every version:

- **No agent ever writes a skill directly** — models draft text; Go code and (shrinking amounts of) human attention materialize artifacts. The acceptor must get *stronger* as artifacts get more dangerous (the verification-hierarchy rule from the RSI survey).
- **The skill catalog is a curated surface, never an append-only log** — growth rate is a health metric at every stage.
- **Wiki / pattern knowledge never reaches an agent's composed context** — only distilled skills cross that boundary (WikiSkill's withholding ablation).
- **Agent-scoped is the default production tier at every stage**; wider scopes are earned, never automatic.
- Each version ships as its own OpenSpec change (or small set) with strict validation; no version starts before the previous one's health metrics are observable in the morning report.

---

## v1 — Curate from traces, human-gated

**Status:** captured (`add-skill-curation-from-traces`), not implemented.

The full WikiSkill-shaped loop on OnClaw's seams: `internal/ingest` extraction (task 0),
five hard qualification gates over existing `IsError`/`Latency` payload fields, workspace
`skillwiki/` (patterns + logs, never agent-facing), proposer reading wiki index + matched
patterns + rejection audit, one atomic create-or-edit per cluster per cycle, mandatory human
approval, atomic `SKILL.md` + `PURPOSE.md` materialization into `AgentSkillsDir`, probation
+ catalog budget + edit-over-sibling, nightly cadence + manual trigger. Two side-LLM calls
per qualified run behind one config knob.

**Sources:** WikiSkill (layers, audit trail, withholding), CoEvoSkills (verification is
where quality comes from), SkillOS (outcome signals as the future dataset), MetaSkill-Evolve
(versioned lineage), PACE (the acceptor is the weak point), behavioral-rules paper (human
gate, slow growth).

**Exit criteria** (measured via the morning-report health metrics):
- Qualification rate stabilized in the 5–15% band; approval rate ≥ 20%.
- A curated skill reaches **0% recurrence** on its covered error class (the behavioral-rules benchmark).
- At least one skill graduates probation with a positive helpful/harmful ratio.
- Catalog growth stays at "a few per month" — a week adding ~10 skills is an alarm, not progress.

## v2 — Measured trust

**Theme:** the human moves from *gate* to *auditor* — but only where counters prove it's safe.

Additions, in dependency order:

1. **Usage-outcome counters graduate into shadow evaluation.** v1 tallies outcomes of runs
   that happened to attach a skill; v2 *intervenes*: for eligible task families, the
   scheduler fires matched task pairs with and without the candidate skill and compares
   outcomes. This turns correlation into measurement. Entry requirement: a replayability
   assessment per task family (the one open question deliberately deferred from v1) —
   families whose tasks can't be matched don't get shadow eval and stay human-gated.
2. **Auto-approval behind a PACE-style acceptance gate.** Candidates materialize
   automatically when their acceptance test passes (anytime-valid paired evaluation vs the
   incumbent — PACE 2606.08106), eliminating the "keep it if the score went up" p-hacking
   failure mode. Auto-approved skills still serve probation; the Audit tab becomes the
   human's spot-check surface instead of a queue. Rejected-auto-candidates surface with
   their evidence for optional review.
3. **Agentic maintainer.** The wiki-maintainer upgrades from a bounded side-call to an
   unattended agent run (`ExecRequest`, scheduler-origin precedent) holding narrow
   `trace.read` + `wiki.write` tools — on-demand dives for patterns that need them
   (WikiSkill-faithful). The proposer stays a bounded side-call.
4. **Agent → workspace promotion goes live.** The disabled v1 affordance activates, gated on:
   positive outcome counters over a minimum window, convergence of ≥ N agents on the same
   procedure in the cluster graph, and a cross-agent shadow check. Promotion = create the
   workspace registry row (new source value `curated`) + **remove the agent-scoped copy**
   (same name at both tiers risks double-mount; agent > workspace collision precedence makes
   the agent copy win silently). Requires a `workspace-skills` spec delta.
5. Workspace-level catalog budget and cross-agent pattern reuse telemetry.

**Sources:** PACE (acceptance statistics), SkillOS (delayed-outcome evaluation structure),
Self-Harness promote ladder (W1 mine → W2 shadow → W3 promote).

**Exit criteria:** an auto-approved skill survives probation without human touch; the first
workspace-promoted skill is in service; auto-approval false-commit rate below the PACE
configurable bound.

## v3 — The loop tests itself

**Theme:** artifacts earn existence by *executing*, and the curation *policy* starts learning
from outcomes.

Additions:

1. **Executing author / self-verifying skill packages.** Extraction upgrades from side-call
   to a full agent run with file tools for eligible task families, producing multi-file
   packages — SKILL.md + `scripts/` that run and test themselves before graduating
   (OnClaw's `dependencies.{binaries,python}` frontmatter and venv provisioning already
   anticipate this). This is the documented flip condition: execution capability is what
   makes skill-creator-style machinery necessary.
2. **Surrogate verifiers with bit-channel escalation** (CoEvoSkills mechanics) for
   artifact-producing task families: a verifier session that never sees ground-truth tests,
   escalates its own suite only on surrogate-pass/oracle-fail, receiving a single bit — the
   discipline that keeps verification from overfitting. Oracle = execution feedback where
   artifacts are checkable.
3. **Outcome-trained curation policy** (SkillOS 2605.06614): curation decisions (thresholds,
   proposal rules, cluster merge policy) are evaluated against delayed downstream outcomes
   via grouped task streams — v1's counters and v2's shadow results are the training data.
   Hand-tuned constants become measured ones.
4. **Meta-prompt tuning** (MetaSkill-Evolve's meta-failure-trace trick): the curation
   pipeline's own history — which proposals it drafted, which verdicts they got, which
   skills survived — is itself a trace that iteratively improves the extraction and
   maintenance prompts. Same machinery, one level up.

**Sources:** CoEvoSkills (71.1% vs human 53.5% — the loop is the method), SkillOS (first
learnable gate), MetaSkill-Evolve (two-timescale recursion), skill-curation design record
(the no-oracle gate stack this version completes).

**Exit criteria:** a self-verified package ships without human review and beats the
human-gated baseline on its task family; curation-policy changes ship with outcome-based
evaluation rather than intuition.

## v4+ — Recursion horizon

**Theme:** the lanes converge; recursion becomes a property, not a feature.

- **Skill lineage DAG:** versioned skill history as a graph with cross-cluster *inspiration
  edges* (MetaSkill-Evolve): retrieval over lineage, ΔU>0 archive admission, no in-place
  revision. v1's supersede chains grow up into a queryable graph.
- **Meta-skill evolution:** branch-local curation policies (m) that evolve on a slow
  timescale while skills evolve fast — the same five-beat loop applied to itself, with the
  meta-failure-trace trick as the bridge. Recursion as a property of the architecture.
- **Bidirectional memory ↔ skill contracts:** skills cite the memory facts they depend on
  with freshness expectations; the consolidator's stale-skill *flagging* (v1) graduates to
  auto-proposed superseding edits (v3's edit grammar) and eventually to contract enforcement.
- **Procedural cross-pollination:** cross-lineage skill hybridization (MGM's comparative
  evolution) at the knowledge-artifact level — mixing procedures from different agents'
  lineages under the standard gates.

**Deliberate permanent non-goals** (revisit only with a design doc and a strong argument):

- **Harness/code self-modification in core** (DGM/MGM/HSI/AIDE² lane). OnClaw's
  plugins-first architecture treats the agent runtime as fixed; harness evolution belongs in
  plugins, and the Phantom Guardrails finding plus the verification hierarchy say the gate
  cost for code-level self-writes exceeds anything a workspace deployment should bear.
- **System-tier skill production.** `SyncSystemSkills` is machine-owned by design; the loop
  never writes there at any version.
- **Cross-tenant learning.** The workspace is the tenant boundary; patterns and skills never
  leak across it. (A future, explicit, opt-in "skill pack" export/import is product
  territory, not loop behavior.)

---

*Traceability: every stage's mechanisms trace to session-verified papers — WikiSkill
2608.27454, SkillOS 2605.06614, CoEvoSkills 2604.01687, MetaSkill-Evolve 2607.05297,
PACE 2606.08106, SkillRL 2602.08234, ACE 2510.04618, Combee 2604.04247, behavioral-rules
2607.13091, DGM 2505.22954 / MGM 2608.07645, AIDE² 2609.26457. The through-line: v1 builds
the loop, v2 earns autonomy with evidence, v3 makes artifacts self-verifying and the policy
learnable, v4 makes the loop itself the object.*
