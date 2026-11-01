# Design

## D1: Why tool descriptions + contract, not the skill

The skill body is invocation-gated by design (fresh-per-turn discovery; the body loads on `$documents` or an invoked match) — the drift turn never loaded it. The only text guaranteed present is (a) the operating contract composed every turn (`internal/agents/contract.go`) and (b) the descriptions of tools actually offered. Steering therefore goes to (b) at the two points of choice — the create_document description the model quotes before generating, and the read_file description it quotes before reading — with (a) carrying one routing line so the rule holds even in turns where only one of the two tools is offered. This matches the established "authoring TAUGHT not tooled" prompt-contract precedent.

## D2: Wording constraints

- Name the failure modes observed: scripted generators (`python-docx`) and hand-assembled format containers (stdlib zip+XML) for production; shell unpacking (`unzip` + XML parse) for reads. Steering that names concrete behaviors outperforms generic "prefer the tool".
- Scope the prohibition to document files only (PDF/DOCX/PPTX production; DOCX/XLSX/PPTX reads). Shell stays legitimate for everything else — an over-broad "don't use shell for files" would fight the workspace tools.
- The create_document description keeps its existing sentences (format-by-extension, presets, overwrite semantics) and gains the routing sentence; the read_file catalog description gains one clause; the contract line covers both directions in one sentence.

## D3: The shell alias is out of scope

The alias tool (`internal/agents/compose/shell_alias.go`) copies the filesystem middleware's execute description, which the app does not own; extending it would fork upstream text. The contract line and the two document-tool descriptions cover the drift without touching it. If drift persists after this change, a follow-up can wrap the alias's description.

## D4: Non-goals

- No changes to the seeded `documents` skill text (already correct), the preset appendix (already correct for its job), or any tool execution path.
- No hard gate (post-hoc detection of shell-built DOCX) — prompt-contract steering first; escalate only if drift is observed again after this change.
