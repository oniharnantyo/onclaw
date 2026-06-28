## Why

onclaw targets low-resource single-board computers (~2GB RAM, 8GB storage) where users may not be familiar with YAML syntax. The current config.yaml requirement adds unnecessary complexity and contradicts 12-factor app methodology which favors environment variables for configuration. Adding .env support simplifies first-run experience while maintaining all configuration capabilities.

## What Changes

- **BREAKING**: Remove config.yaml file support entirely
- **NEW**: Add .env file support with automatic loading from search paths (`./.env`, `~/.onclaw/.env`, `/etc/onclaw/.env`)
- **UPDATE**: Configuration priority order from `defaults < config.yaml < env < flags` to `defaults < .env < env < flags`
- **NEW**: First-found .env file wins (no merging across multiple .env files)
- **UPDATE**: SearchPaths() function to return `.env` file locations instead of `config.yaml`
- **BREAKING**: Update all documentation references from config.yaml to .env

## Capabilities

### New Capabilities

- `env-config`: Environment file-based configuration system supporting .env files with ONCLAW_* prefixed variables, standard .env loading pattern (defaults < .env < env < flags), and search paths in current directory, user home, and system-wide locations.

### Modified Capabilities

- `config`: Existing configuration capability (see `openspec/specs/config/spec.md`) is being entirely replaced by env-config. All config.yaml file handling requirements are removed.

## Impact

**Code Changes:**
- `internal/config/config.go`: Remove YAML config file loading, add .env support using gotenv/Viper
- `internal/config/defaults.go`: No changes (defaults remain unchanged)
- `internal/config/config_test.go`: Update tests for .env instead of YAML, add .env-specific test cases
- `internal/cli/config_cmd.go`: Update output messages to reference .env instead of config.yaml
- `README.md`: Update configuration examples and documentation

**Breaking Changes:**
- Users with existing `~/.config/onclaw/config.yaml` or `/etc/onclaw/config.yaml` files will need to migrate to .env format
- CI/CD pipelines using config.yaml must be updated
- No migration tool provided (manual migration required)

**What Remains Unchanged:**
- All ONCLAW_* environment variable handling (still works)
- CLI flags (still work and have highest priority)
- Provider database and secrets storage
- Encrypted secrets at rest
- Hot-reload capability

**Dependencies:**
- No new dependencies (gotenv v1.6.0 already available as Viper dependency)
- May remove direct YAML dependency if it becomes unused