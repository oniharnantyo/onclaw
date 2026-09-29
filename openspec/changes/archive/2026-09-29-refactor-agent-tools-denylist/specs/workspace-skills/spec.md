# Spec Delta — workspace-skills

## MODIFIED Requirements

### Requirement: Skill dependency resolution
Skills SHALL declare dependencies in SKILL.md frontmatter (`dependencies.tools`, `dependencies.binaries`, `dependencies.python` — an OnClaw extension ignored by the middleware) and ecosystem manifests (`requirements.txt` beside scripts). At import, undeclared dependencies SHALL be inferred (tool-name patterns in the body, script imports/manifests) and the inferred set SHALL be stored on the registry row without modifying imported files. Resolution per kind: **tools** resolve against the workspace tool gate and agent tool denylists, with a pre-checked "enable everywhere" install option that adds the tool to the gate and removes it from every agent's `disabled_tools` (agent-tier skills scope this to the owning agent); a non-empty `scripts/` directory implies the reserved `execute` shell tool as a dependency; **binaries** are probed on the server PATH and never auto-installed — the report names the package-manager command per platform with a re-check action; **python packages** provision into one shared per-workspace venv (pip union install across all enabled skills; a version conflict between skills SHALL name both sides and fail the dependency step) automatically when python is present on the server, otherwise reported as guidance. Unmet dependencies SHALL NOT gate execution: missing tools and missing binaries surface as natural tool errors in the transcript. Dependency status SHALL be re-checkable on demand.

#### Scenario: Requirements.txt feeds the declaration
- **WHEN** a skill is imported with `scripts/requirements.txt` containing `pypdf>=4.0` and no frontmatter declaration
- **THEN** the registry row stores `pypdf>=4.0` as a python dependency and the imported files are unchanged

#### Scenario: Enable-everywhere satisfies tool dependencies
- **WHEN** a skill declaring `dependencies.tools: [web.search]` is installed with the pre-checked enable option on
- **THEN** the workspace tool gate allows `web.search` and no agent's `disabled_tools` contains it

#### Scenario: Missing binary reports guidance
- **WHEN** a declared binary `pdftotext` is absent from the server PATH at install
- **THEN** the report shows the per-platform install command and a re-check action, and the skill still installs with a persistent warning chip

#### Scenario: Python packages provision into the workspace venv
- **WHEN** a skill with a python dependency is installed on a server with python3 present
- **THEN** the workspace shared venv installs the package (union with other enabled skills) and verification of the import succeeds

#### Scenario: Cross-skill version conflict named
- **WHEN** two enabled skills require incompatible versions of the same python package
- **THEN** the dependency step fails naming both skills' requirements and previously installed skills are untouched
