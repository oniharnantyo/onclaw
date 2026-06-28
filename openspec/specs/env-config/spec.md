# env-config Spec

## Purpose

Environment file-based configuration system supporting .env files with ONCLAW_ prefixed variables, standard .env loading pattern (defaults < .env < env < flags), and search paths in current directory, user home, and system-wide locations.

## Requirements

### Requirement: Load .env file from search paths
The system SHALL load environment variables from a .env file discovered in the configured search paths, in order of priority.

#### Scenario: .env found in current directory
- **WHEN** user runs `onclaw run` with `.env` present in current working directory
- **THEN** system loads environment variables from `./.env`
- **AND** sets search result to first found path
- **AND** does not continue searching other paths

#### Scenario: .env found in user home directory
- **WHEN** user runs `onclaw run` with no `.env` in current directory
- **AND** `.env` exists at `~/.onclaw/.env`
- **THEN** system loads environment variables from `~/.onclaw/.env`
- **AND** sets search result to user home path

#### Scenario: .env found in system directory
- **WHEN** user runs `onclaw run` with no `.env` in current directory or user home
- **AND** `.env` exists at `/etc/onclaw/.env`
- **THEN** system loads environment variables from `/etc/onclaw/.env`
- **AND** sets search result to system path

#### Scenario: No .env file found
- **WHEN** user runs `onclaw run` with no `.env` in any search path
- **THEN** system continues successfully with defaults only
- **AND** does not error or warn
- **AND** config shows empty LoadedFrom field

### Requirement: Respect standard .env priority order
The system SHALL apply configuration layers in the order: hardcoded defaults < .env file < environment variables < CLI flags, with each layer able to override previous layers.

#### Scenario: .env overrides defaults
- **WHEN** defaults set `concurrency: 1`
- **AND** `.env` contains `ONCLAW_CONCURRENCY=2`
- **AND** user has no shell environment variable set
- **THEN** final config uses `concurrency: 2` from .env

#### Scenario: Environment variable overrides .env
- **WHEN** `.env` contains `ONCLAW_LOG_LEVEL=debug`
- **AND** user has shell environment variable `ONCLAW_LOG_LEVEL=warn`
- **THEN** final config uses `log_level: warn` from environment
- **AND** .env value is overridden

#### Scenario: CLI flag overrides everything
- **WHEN** `.env` contains `ONCLAW_CONCURRENCY=4`
- **AND** user has shell environment variable `ONCLAW_CONCURRENCY=2`
- **AND** user runs `onclaw --concurrency=8 run`
- **THEN** final config uses `concurrency: 8` from CLI flag

### Requirement: Parse .env files with gotenv format
The system SHALL parse .env files using standard gotenv format supporting comments, blank lines, KEY=value pairs, and variable export syntax.

#### Scenario: Parse simple key-value pairs
- **WHEN** `.env` contains `ONCLAW_LOG_LEVEL=debug`
- **THEN** system correctly parses key as `ONCLAW_LOG_LEVEL`
- **AND** correctly parses value as `debug`

#### Scenario: Ignore comments and blank lines
- **WHEN** `.env` contains:
  ```
  # This is a comment
  ONCLAW_LOG_LEVEL=debug

  ONCLAW_CONCURRENCY=2
  ```
- **THEN** system ignores comment lines starting with `#`
- **AND** system ignores blank lines
- **AND** system correctly parses the two key-value pairs

#### Scenario: Handle quoted values with spaces
- **WHEN** `.env` contains `ONCLAW_WORKSPACE="/home/user/my project"`
- **THEN** system correctly parses the entire quoted string including spaces
- **AND** final config value is `/home/user/my project`

#### Scenario: Handle unset variables
- **WHEN** `.env` contains `ONCLAW_MODEL=`
- **AND** variable value is empty string
- **THEN** system correctly parses as empty value
- **AND** final config uses default model

### Requirement: Support comma-separated array values
The system SHALL parse comma-separated values for configuration fields that expect arrays, splitting on commas and trimming whitespace.

#### Scenario: Parse shell allowlist
- **WHEN** `.env` contains `ONCLAW_TOOLS_SHELL_ALLOWLIST=ls,cat,git,docker`
- **THEN** system parses as array with four elements
- **AND** final config allowlist is `["ls", "cat", "git", "docker"]`

#### Scenario: Handle empty array
- **WHEN** `.env` contains `ONCLAW_TOOLS_SHELL_ALLOWLIST=`
- **AND** variable value is empty
- **THEN** system parses as empty array
- **AND** final config allowlist is `[]`

#### Scenario: Handle single value array
- **WHEN** `.env` contains `ONCLAW_TOOLS_SHELL_ALLOWLIST=git`
- **THEN** system parses as array with one element
- **AND** final config allowlist is `["git"]`

### Requirement: Maintain type safety for .env values
The system SHALL convert string values from .env files into appropriate Go types (int, bool, string) matching the Config struct fields.

#### Scenario: Parse integer values
- **WHEN** `.env` contains `ONCLAW_CONCURRENCY=4`
- **AND** Config struct expects `int` for Concurrency field
- **THEN** system converts string "4" to integer 4
- **AND** final config Concurrency field has type int

#### Scenario: Parse boolean values
- **WHEN** `.env` contains `ONCLAW_LANGFUSE_MASK=false`
- **AND** Config struct expects `bool` for Mask field
- **THEN** system converts string "false" to boolean false
- **AND** final config Mask field has type bool

#### Scenario: Handle invalid integer
- **WHEN** `.env` contains `ONCLAW_CONCURRENCY=invalid`
- **THEN** system logs error about invalid value
- **AND** falls back to default value
- **AND** continues without crashing

### Requirement: Support provider API keys in .env files
The system SHALL allow provider API keys to be specified in .env files using ONCLAW_PROVIDER_<NAME>_API_KEY format, with precedence over database-stored secrets.

#### Scenario: Provider key from .env
- **WHEN** `.env` contains `ONCLAW_PROVIDER_OPENAI_API_KEY=sk-proj-12345`
- **AND** database has no stored secret for openai profile
- **THEN** system uses API key from .env
- **AND** successfully authenticates with OpenAI API

#### Scenario: .env key overrides database secret
- **WHEN** `.env` contains `ONCLAW_PROVIDER_OPENAI_API_KEY=sk-proj-new`
- **AND** database has stored secret `sk-proj-old` for openai profile
- **THEN** system uses API key from .env (sk-proj-new)
- **AND** ignores database-stored secret
- **AND** does not decrypt stored secret

#### Scenario: Provider keys remain secure
- **WHEN** user runs `onclaw config show`
- **AND** `.env` contains provider API keys
- **THEN** output redacts all provider API key values
- **AND** shows `api_key: "***"` for each provider

### Requirement: Remove config.yaml file support
The system SHALL NOT load or recognize config.yaml files from any search path.

#### Scenario: config.yaml ignored
- **WHEN** user has existing `config.yaml` file
- **AND** user runs `onclaw config show`
- **THEN** system does not read config.yaml
- **AND** config shows LoadedFrom as .env path (if found) or empty
- **AND** does not merge any values from config.yaml

#### Scenario: No YAML validation errors
- **WHEN** user has invalid YAML in config.yaml
- **THEN** system does not attempt to parse config.yaml
- **AND** does not produce YAML-related errors
- **AND** continues successfully

### Requirement: Update search paths for .env files
The system SHALL search for .env files in the following order: current directory (`./.env`), user home directory (`~/.onclaw/.env`), and system directory (`/etc/onclaw/.env`).

#### Scenario: Search path order
- **WHEN** user runs `onclaw config path`
- **THEN** system displays search paths in correct order:
  1. `.`
  2. `~/.onclaw`
  3. `/etc/onclaw`
- **AND** notes that .env file is searched in each location

#### Scenario: First found wins
- **WHEN** user has `.env` in current directory
- **AND** user also has `.env` in `~/.onclaw/.env`
- **THEN** system loads only `./.env`
- **AND** does not load or merge `~/.onclaw/.env`

### Requirement: Display .env configuration location
The system SHALL provide commands to inspect which .env file (if any) was loaded and the search paths used.

#### Scenario: Show loaded .env path
- **WHEN** user runs `onclaw config path`
- **AND** `.env` was loaded from current directory
- **THEN** system displays "Config file: ./.env"
- **AND** lists all search paths

#### Scenario: Show no .env found
- **WHEN** user runs `onclaw config path`
- **AND** no .env file was found
- **THEN** system displays "No .env file found; using defaults + env."
- **AND** lists all search paths that were checked

#### Scenario: Show resolved configuration
- **WHEN** user runs `onclaw config show`
- **THEN** system displays final merged configuration
- **AND** indicates which .env file was loaded (if any)
- **AND** shows all layers merged (defaults, .env, env, flags)

### Requirement: Support all existing configuration in .env format
The system SHALL support all configuration values currently available in config.yaml through .env file format with ONCLAW_ prefix.

#### Scenario: Core configuration values
- **WHEN** `.env` contains core configuration variables:
  ```
  ONCLAW_LOG_LEVEL=info
  ONCLAW_LOG_FORMAT=json
  ONCLAW_CONCURRENCY=2
  ONCLAW_MAX_CONTEXT_TOKENS=32000
  ONCLAW_MODEL=gpt-4o
  ONCLAW_DB_PATH=/data/onclaw.db
  ONCLAW_WORKSPACE=/home/user/projects
  ```
- **THEN** system correctly loads all core configuration values
- **AND** final config matches all .env values

#### Scenario: Nested configuration with underscores
- **WHEN** `.env` contains nested configuration:
  ```
  ONCLAW_TOOLS_SHELL_POLICY=allow
  ONCLAW_TOOLS_SHELL_ALLOWLIST=ls,cat,git
  ONCLAW_AGENT_MAX_ITERATIONS=30
  ONCLAW_LANGFUSE_HOST=https://langfuse.example.com
  ONCLAW_LANGFUSE_PUBLIC_KEY=pk-lf-123
  ONCLAW_LANGFUSE_SECRET_KEY=sk-lf-456
  ONCLAW_LANGFUSE_SESSION_ID=mysession
  ONCLAW_LANGFUSE_RELEASE=v1.0.0
  ONCLAW_LANGFUSE_MASK=false
  ```
- **THEN** system correctly maps underscore-separated names to nested config fields
- **AND** final config has correct nested structure

### Requirement: Document .env security best practices
The system SHALL provide documentation and examples in .env.example file with security warnings for provider API keys.

#### Scenario: .env.example has security warnings
- **WHEN** user views `.env.example` file
- **THEN** file contains commented-out provider API key examples
- **AND** includes warnings about not committing .env to version control
- **AND** instructs users to never share API keys

#### Scenario: .env.example documents all options
- **WHEN** user views `.env.example` file
- **THEN** file contains all available ONCLAW_* variables
- **AND** includes comments explaining each variable's purpose
- **AND** shows default values where applicable
- **AND** provides examples for complex values (arrays, quoted strings)
