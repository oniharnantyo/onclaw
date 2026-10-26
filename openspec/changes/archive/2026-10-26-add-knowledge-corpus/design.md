# Design

## Context

See proposal.md — Why. The load-bearing constraint: retrieval is already solved. The jailed filesystem middleware (`ls`/`glob`/`grep`/`read_file` + multimodal PDF reads + `execute`) rooted at `/workspace` is exactly the Direct Corpus Interaction interface the DCI paper (arXiv 2605.05242) advocates — raw corpus, terminal tools, no retriever. This change therefore builds only what is missing: a durable scoped corpus, ingestion paths, exposure wiring, and management surfaces.

Current state that shapes the approach:

- The jail (`internal/agents/backend/fs_jailed_backend.go`) has exactly one virtual mount (`/workspace` → primary root, `unmount()`); extra roots (the skills tree) are reachable only via raw host-absolute paths, which leak machine-specific paths into prompts. Writable resolution is primary-root-only.
- Attachments are a flat global registry (`internal/attachments`, migration 000009) keyed by opaque capability keys — unusable as a corpus: no scoping, no listing, model cannot re-read them in later turns.
- The right bar (`RightBar.tsx` + `panelRenderers.tsx`) has a typed panel registry — a new tab kind is compiler-forced. Nav menus are a 5-step pattern (`nav.ts` Menu union → `App.tsx`). All uploads today go FileReader → base64 → bound service call.
- System guidance seam: `composeSystem` (runner.go) = persona → operating contract → optional summary; capability middlewares append their own guidance (precedent: the fs middleware's `CustomSystemPrompt`).

## Goals / Non-Goals

**Goals:**

- Documents durable across restarts, scoped per-agent or global, promotable both directions.
- Corpus reachable by the model through the tools it already has, read-only, with a stable prompt-referable path.
- Three ingestion paths sharing one gate: agent tool, right-bar upload, Knowledge menu upload.
- Zero new external dependencies.

**Non-Goals:**

- No embeddings, vector store, chunking, reranking, or `knowledge_search` tool — deliberately, per the DCI approach the user locked.
- No text extraction or companion files; no OOXML (docx/pptx/xlsx) support in this change.
- No images or video in knowledge lanes; no cross-device sync; no document versioning.

## Decisions

### D1 — DCI-pure retrieval: raw corpus, existing tools only

Documents are stored as raw bytes and searched with `ls`/`glob`/`grep`/`read_file` (text greps directly; PDFs are located by filename/description and read multimodally, ≤20 MiB per the existing `MultiModalRead` cap). Alternatives considered and rejected: vector RAG (contradicts the paper's thesis, adds an embedding model + index to operate), ingest-time `.md` text companions (dual files, sync-on-replace complexity, extraction dependency for PDFs). The user explicitly chose raw-only; the recorded future path for binaries is decode-at-read via Go stdlib `archive/zip` + `encoding/xml` if OOXML ever returns.

### D2 — Disk layout: `~/.buddies/knowledge/global/` + `knowledge/agents/<slug>/`, catalog in SQLite

Disk is the truth for bytes; a `documents` table (migration 000014) is the catalog. Filesystem-native scoping means per-agent visibility needs no filtering logic — the turn simply mounts two roots. Agent-scoped documents live OUTSIDE the agent's workspace (`workspaces/<slug>/`) so workspace lifecycle (notably agent deletion) does not silently destroy knowledge, and so the corpus can stay read-only to workspace tools. Alternatives: knowledge inside the workspace (couples deletion, makes corpus writable via `write_file` — both wrong), DB-blob storage (the jail and `grep` need real files on disk).

### D3 — Jail exposure: a second, read-only virtual mount `/knowledge`

Extend the jailed backend with a second mount prefix structurally identical to `unmount()`: `/knowledge/...` resolves against the knowledge roots (global dir + the session agent's dir), read operations only. The mount presents the two roots as explicit subdirectories — `/knowledge/global/` and `/knowledge/agent/` — so scope structure is visible to the model and per-scope filename uniqueness can never surface a collision inside one listing. Write resolution is untouched — mount-scoped writes stay exclusive to `/workspace`, so `write_file`/`edit_file`/`delete_file` can never touch the corpus. Alternatives: reusing extraRoots with host-absolute paths (leaks `~/.buddies/...` machine paths into the model's context and prompts — not stable or shareable across hosts); a `/workspace/knowledge` subdirectory (writable, delete-coupled — rejected for D2's reasons); a flat single-directory merge of both roots (ambiguous on cross-scope filename collisions, hides scope from the model).

### D4 — `save_document` is the sole write path; saves go to the session agent's scope

One new direct catalog tool (name constant + `catalogEntry` + `buildDirectTool` case; the gate enumerates the catalog automatically). It accepts a chat attachment key or inline text + filename, an optional description, and always saves into the session agent's scope — no `scope` argument in v1. Rationale: promotion to all-agents is a curation decision the user owns (from the panel or menu); letting the model widen its own trust surface is the wrong default. The tool handler runs host-side with access to both the attachments store and the knowledge store, so saving an attachment is a copy from `attachments/<key>` into the corpus — no new trust boundary crossed. Alternatives: making the corpus fs-writable with catalog side-car indexing (two write paths drift), an agent `scope` argument (deferred; trivially added later if wanted).

### D5 — Formats: `text/*` + `application/pdf` only; caps 1 MiB text / 20 MiB PDF

20 MiB is the existing multimodal-read ceiling — a larger PDF could be stored but never read, so it is rejected at every ingestion path with the reason shown. Text caps at 1 MiB for parity with the inline text lane. OOXML, video, images, and archives are rejected with a one-line reason naming the supported kinds. The gate lives in the knowledge store (single enforcement point) and both UI upload lanes and `save_document` surface its verdicts.

### D6 — Catalog semantics: unique filename per scope, atomic promote/demote, load-bearing description

`(scope, filename)` uniqueness rejects duplicates rather than overwriting. Promotion/demotion is one store operation: move the file, flip the row, in a transaction with a compensating file move-back on failure — a cataloged document always has its file at its scope's location. Because grep cannot see inside PDFs (D1), the description field is a primary discovery handle: editable in both surfaces, recorded by `save_document` when provided, and the tool's description nudges the model toward descriptive filenames.

### D7 — Turn wiring: gate-resolved roots + conditional guidance after the contract

Per turn, when the agent is workspace-rootable, the gate composes the knowledge roots (global + agent dir) into the fs capability, exactly like the workspace dir today; subagents inherit the corpus automatically because they share the parent's fs capability. The `/knowledge` search guidance is appended at the `composeSystem` seam (after the operating contract, before the optional summary) only when the turn exposes the mount — the operating-contract constant and its pinned ordering tests stay untouched, and agents with no filesystem tools get no orphan guidance. The guidance additionally directs the model to activate the `knowledge-search` skill when the request may concern saved documents (D10), so auto-activation has a deterministic instruction path instead of relying on the skill description alone. Alternative rejected: baking a knowledge section into the contract constant (forces guidance even when tools are gated off; churns the pinned contract tests).

### D8 — Service surface: one bound Knowledge service + `knowledge:changed` event; base64 uploads

A single service backs both surfaces (menu + panel): list (with scope filter), upload, delete, set scope, set description, content read for previews. Bindings → `transport.ts` → `client.ts` follow the existing seam; a `knowledge:changed` event (pattern: `schedules:changed`) drives the live-update requirement — open panels and the menu re-list on the event, which also covers mid-turn `save_document` saves. Uploads reuse the FileReader → base64 → bound-call pattern (same payload shape as today's 20 MiB chat PDFs). A native file-dialog flow remains a later polish, not a dependency of this change.

### D9 — Agent deletion: state the consequence, then cascade

The existing agent-delete confirmation is extended to mention scoped documents when any exist; confirming deletes rows + files (FK cascade on the agent linkage, like hook assignments). This honors "no silent destruction" while keeping one deletion flow. Alternative rejected: auto-promoting scoped documents to global on agent delete (silently widens every other agent's corpus — a scope decision made without the user).

### D10 — Seeded `knowledge-search` skill with directed auto-activation

One skill ships with the app: the knowledge store seeds `~/.buddies/skills/knowledge-search/SKILL.md` at startup when missing (repair-if-deleted — same precedent as the browser `default` profile). It rides the existing two-tier skill machinery unchanged: fresh-per-turn discovery, the standard eligibility gate (global skills toggle × agent allowlist), `$knowledge-search` explicit invocation, and agent-tier shadowing — a workspace-tier `knowledge-search` overrides the seeded playbook per agent, which is a feature (tailorable playbook), not drift. Auto-activation is **directed, not heuristic**: the skill's frontmatter description states its trigger (use whenever the user asks about, quotes, or references saved documents), and the D7 corpus guidance explicitly instructs activation on document-related requests — deterministic instruction text on every exposed turn. The skill body is the DCI playbook: read the scope indexes first, grep for lexical anchors, read around hits, read PDFs by page range, refine with synonyms and broader terms, and when to persist findings with `save_document`. Alternatives rejected: keyword-matching middleware that force-loads the body on "document intent" (brittle boundary, untestable), always-injecting the body when the mount is exposed (defeats progressive disclosure — pays playbook tokens on every turn), and a virtual built-in skill tier (new machinery; loses shadow-overridability — the virtual-AGENTS.md precedent fits normative harness text, not a user-tailorable playbook).

### D11 — Store-maintained scope index (`INDEX.md`)

Each scope directory carries `INDEX.md`, regenerated by the store on every catalog mutation (upload, agent save, scope change, delete, description edit): one line per document — filename, description, size. It exists because descriptions live in the catalog and are otherwise invisible to `ls`/`grep`, while DCI-pure discovery is filename- and description-driven (it is the only content signal for PDFs). The index is derived state, not a document: excluded from the catalog, from the menu/panel document lists, and from duplicate-name checks; read-only through the mount, so the model can read it but never corrupt it. A drifted index (manual filesystem edits outside the app) is repaired by regeneration on the next mutation — the index is always derivable from disk + catalog truth. Alternative rejected: a mount-time synthesized root index (the mount root is a view over two roots; a virtual file adds mount machinery for no gain over two physical files).

## Risks / Trade-offs

- [grep is blind inside PDFs] → Accepted by design (D1). Mitigated by descriptive filenames + descriptions (D6) and guidance that says list-then-read. The decode-at-read path documents the escape hatch.
- [Base64 upload payloads for 20 MiB PDFs (~27 MB encoded)] → Same as chat attachments today; if it bites, a native-dialog + streamed-copy flow can replace the panel's uploader without touching the spec surface.
- [Filename collision on promote into a scope that has the same name] → The promote operation runs the same uniqueness check as uploads and rejects with a reason; no silent overwrite anywhere.
- [Corpus growth is unbounded (no GC)] → Sizes are small and both surfaces offer delete; a usage/size display in the menu is cheap follow-up if needed.
- [Second jail mount adds resolve-path surface] → The mount is a prefix mapping in read resolution only; writable resolution is untouched, and the existing escape-attempt tests extend naturally to `/knowledge` write attempts.
- [Subagents read the whole parent-visible corpus] → Same trust domain as `/workspace` today; delegation already inherits the parent's jail.
- [Model may skip skill activation despite the instruction] → Triple path: description trigger (model-side selection), guidance directive (deterministic text), and the user's `$knowledge-search` escape hatch. The skill is an accelerator, not a gate — the corpus stays directly reachable through the filesystem tools regardless.
- [`INDEX.md` drift after manual file edits outside the app] → The index is derived state; the next catalog mutation regenerates it, and `ls`/`glob` always show ground truth.

## Migration Plan

Migration 000014 is purely additive: the `documents` table (with the agent FK cascading on delete) and no data to backfill. First run creates the `knowledge/` tree (home-layout requirement). Startup seeding and index generation are idempotent: existing installs gain the knowledge tree, the seeded `knowledge-search` skill, and (once documents exist) the scope indexes on their next launch. Rollback is dropping the table and directory — nothing else references them at rest. Existing sessions, attachments, and workspaces are untouched.
