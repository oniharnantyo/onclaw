## ADDED Requirements

### Requirement: Skills are resolved from a tiered set of directories

The system SHALL resolve an agent's skills from three directories in precedence order — `<home>/workspace/<agent>/skills`, `<home>/workspace/<agent>/.agents/skills`, and `<home>/skills` (global) — where `<home>` is the onclaw data directory. When the same skill name appears in more than one directory, the first directory in the order SHALL win. An agent with no skill directories SHALL behave exactly as if skills were absent.

#### Scenario: per-agent skill overrides global

- **WHEN** a skill named `pdf` exists in both `<home>/workspace/<agent>/skills/pdf` and `<home>/skills/pdf`
- **THEN** the agent resolves and serves the per-agent copy

#### Scenario: an agent with no skill dirs is unchanged

- **WHEN** none of the three skill directories exist for an agent
- **THEN** no skill tool or skill instruction is added and the agent behaves as before

### Requirement: Skills are exposed via progressive disclosure

The system SHALL expose resolved skills through eino's skill middleware: a "Skills System" instruction and a `skill` tool whose description lists each skill's name and description, with the full skill body returned only when the agent invokes the tool. Skills SHALL run in inline mode; fork and fork_with_context modes SHALL NOT be used.

#### Scenario: the catalog is shown without the full bodies

- **WHEN** an agent with installed skills assembles
- **THEN** the `skill` tool description lists each skill's name and description, and the full instructions are loaded only on invocation

#### Scenario: a fork-mode skill is normalized to inline

- **WHEN** an installed skill's frontmatter declares `context: fork` or `context: fork_with_context`
- **THEN** it is normalized to inline on install and runs inline at runtime without error

### Requirement: Skills use a canonical on-disk format

The system SHALL store each installed skill as a directory `<target>/<name>/SKILL.md` whose YAML frontmatter contains at least `name` and `description`, with optional bundled files alongside. The filename SHALL be exactly `SKILL.md`.

#### Scenario: a lowercase skill.md is canonicalized

- **WHEN** a source provides `skill.md`
- **THEN** it is installed as `SKILL.md`

#### Scenario: bundled files are preserved

- **WHEN** a skill ships helper scripts or references alongside SKILL.md
- **THEN** those files are copied into the skill directory and are resolvable by relative path at runtime

### Requirement: Skills can be installed from external sources

The system SHALL install skills from GitHub repositories (`owner/repo` or a GitHub URL, which also covers skills.sh), HTTP archives (`.tar.gz`/`.tgz`/`.zip`), local filesystem paths, and Claude Code plugins (a repository containing `.claude-plugin/plugin.json`). Fetch SHALL use only the Go standard library so the build remains `CGO_ENABLED=0` with no new dependencies.

#### Scenario: a GitHub repo installs its skills

- **WHEN** the user runs `onclaw skill install anthropics/skills`
- **THEN** the repo is fetched as a tarball and every `SKILL.md` it contains is discovered

#### Scenario: a Claude plugin imports only its skills

- **WHEN** the source is a Claude plugin repository
- **THEN** only the `skills/` subdirectory is imported, each as an individual skill

#### Scenario: skills.sh sources use the GitHub path

- **WHEN** a skills.sh skill is addressed by its underlying `owner/repo`
- **THEN** it installs via the GitHub tarball adapter with no separate skills.sh adapter

### Requirement: Imported skills are normalized to the canonical format

The system SHALL normalize each discovered skill on install: force the `SKILL.md` filename; synthesize `name` (from the directory) and `description` (from the first line) when missing, installing with a warning rather than rejecting; strip `context: fork*`; preserve unknown frontmatter keys; and copy bundled files verbatim.

#### Scenario: a skill with no frontmatter is still installed

- **WHEN** a discovered SKILL.md has no frontmatter or is missing name/description
- **THEN** name and description are synthesized and the skill installs with a warning

### Requirement: Skills are installed under their bare name by default

The system SHALL install every skill under its bare name, regardless of whether the source contains one or more skills. If a skill collision occurs (e.g., a skill with the same name from a different source), the install SHALL offer custom renaming (`--as`).

#### Scenario: a multi-skill repo is installed using bare names

- **WHEN** `anthropics/skills` (many skills) is installed
- **THEN** each skill is stored under its bare name (e.g. `skillA` and `skillB`)

#### Scenario: a single-skill repo is bare

- **WHEN** a repo containing exactly one skill is installed
- **THEN** the skill is stored under its bare name

### Requirement: Installing many skills prompts a selection

When a source contains more than one skill, the system SHALL NOT install all of them silently. The CLI SHALL list the discovered skills and, on a TTY, prompt a multi-select (choose one, many, or all); in a non-interactive context it SHALL require an explicit `--path <name>` or `--all`. A single-skill source SHALL install immediately.

#### Scenario: the CLI prompts a multi-select for a multi-skill repo

- **WHEN** `onclaw skill install anthropics/skills` runs on a TTY
- **THEN** the discovered skills are listed and the user selects which to install, or all

#### Scenario: a single-skill source installs without prompting

- **WHEN** a source with exactly one skill is installed
- **THEN** it is installed immediately under its bare name

### Requirement: Re-installing a skill is idempotent

The system SHALL treat install as an idempotent upsert, per skill and per scope: a skill not present is installed; one present from the same source with an unchanged content hash is a no-op; one present from the same source with a changed hash is updated in place; one present from a different source is a collision that SHALL NOT be silently overwritten and SHALL offer an alias (`--as`) or force (`--force`). The same name in a different scope is not a collision.

#### Scenario: an unchanged re-install is a no-op

- **WHEN** a skill already installed from source S is reinstalled from S with the same content
- **THEN** no files change and the system reports it is up to date

#### Scenario: a changed re-install updates in place

- **WHEN** a skill already installed from source S is reinstalled from S with changed content
- **THEN** the on-disk skill and ledger row are updated

#### Scenario: a same-name different-source collision is not overwritten

- **WHEN** a skill installed from source A is installed again from a different source B into the same scope
- **THEN** the install is rejected with guidance rather than silently replacing the skill

### Requirement: Installed skills are recorded in a SQLite ledger

The system SHALL persist an install record per skill in a `skills` table (primary key `name`) holding source type, source, skill path, version, content hash, scope, cached description, and timestamps. Runtime skill resolution SHALL read from disk only and SHALL NOT depend on this ledger.

#### Scenario: the ledger records provenance

- **WHEN** a skill is installed
- **THEN** a row recording its source, hash, scope, and timestamps is written

#### Scenario: runtime does not require the ledger

- **WHEN** an agent resolves skills
- **THEN** it reads the skill directories from disk without querying the ledger

### Requirement: Skills are managed from the CLI

The system SHALL provide `onclaw skill` with `install`, `list`, `show`, `remove`, and `update` subcommands. `install` SHALL classify its `<source>` argument into a provider (local, HTTP archive, GitHub, or plugin) and accept `--scope`, `--branch`, `--path`, `--all`, `--dry-run`, `--as`, and `--plugin` flags. `remove` SHALL delete both the on-disk skill and its ledger row.

#### Scenario: a skill is removed cleanly

- **WHEN** the user runs `onclaw skill remove pdf`
- **THEN** the skill directory and its ledger row are both deleted

#### Scenario: a skill is updated from its recorded source

- **WHEN** the user runs `onclaw skill update pdf`
- **THEN** the skill is re-fetched from its recorded source and updated if changed

### Requirement: Skills are managed over the JSON API and web console

The system SHALL expose skills over the existing authenticated JSON API: list, discover (returns candidates for a source), install (with a selected subset and scope), get, remove, and update. The web console SHALL provide a Skills section with a two-step install flow (enter source → discover → select → install) and list/remove/update actions, reusing the existing session authentication.

#### Scenario: the web console installs a selected subset

- **WHEN** a user enters a multi-skill source in the Skills tab
- **THEN** the discovered skills are listed with a selection control and only the chosen ones are installed

#### Scenario: skills routes require authentication

- **WHEN** an unauthenticated request hits `/api/skills`
- **THEN** it is rejected by the existing session middleware