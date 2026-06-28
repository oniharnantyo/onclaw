## Context

onclaw currently uses Viper with YAML file configuration (`config.yaml`) searched in `.` → `~/.onclaw` → `/etc/onclaw`. This works but adds complexity for low-resource SBC users who may not be familiar with YAML syntax. The project already has `.env` and `.env.example` files in the repository, but these are documentation only - the application does not load them.

**Current State:**
- Config loading: `internal/config/config.go` using Viper with YAML support
- Search paths: `.` → `~/.onclaw` → `/etc/onclaw`
- Priority: `defaults < config.yaml < env < flags`
- Dependencies: Viper v1.21.0 (includes gotenv v1.6.0 as indirect dependency)

**Constraints:**
- Target: Low-resource devices (~2GB RAM, 8GB storage)
- Static binary (CGO_ENABLED=0) - no external dependencies
- Breaking change acceptable (hard break, no migration tool)
- Must maintain all current configuration capabilities

## Goals / Non-Goals

**Goals:**
- Simplify configuration for SBC users by removing YAML requirement
- Align with 12-factor app methodology (environment-based configuration)
- Maintain all existing configuration capabilities via .env format
- Keep configuration layering (defaults < file < env < flags)
- Support provider API keys in .env with security documentation

**Non-Goals:**
- Provide migration tool from config.yaml to .env (manual migration only)
- Support both config.yaml and .env simultaneously (remove YAML entirely)
- Add new configuration capabilities beyond current feature set
- Change how provider API keys are stored in database
- Modify hot-reload or PID file mechanisms

## Decisions

### 1. Use Viper's native .env support
**Decision:** Use Viper's `SetConfigType("env")` and `ReadInConfig()` to parse .env files instead of using gotenv directly.

**Rationale:**
- Viper already supports env file format parsing
- No new dependencies needed (gotenv already included via Viper)
- Consistent with existing Viper-based config architecture
- Clean separation from os.Environ() (no environment pollution)

**Alternatives Considered:**
- **godotenv.Load()**: Would pollute os.Environ() with .env values, could conflict with other tools
- **Manual parsing**: Reinventing the wheel, error-prone for edge cases (quoting, comments)
- **Keep YAML**: Rejected due to user complexity concerns

### 2. Maintain first-found-wins semantics
**Decision:** Load the first .env file found in search paths and stop searching (no merging).

**Rationale:**
- Matches current config.yaml behavior (predictable for upgraders)
- Simpler mental model for users (one file wins)
- Avoids complex merge conflict scenarios
- Performance: stop searching after first match

**Alternatives Considered:**
- **Merge all .env files**: Could cause confusing conflicts between project, user, and system configs
- **Priority-based merge**: More complex, harder to debug for users

### 3. Standard .env priority (defaults < .env < env < flags)
**Decision:** .env files set defaults that can be overridden by shell environment variables.

**Rationale:**
- Aligns with standard .env tools (docker-compose, python-dotenv, Node.js dotenv)
- Supports development workflow (local .env for convenience, env vars for overrides)
- Matches 12-factor app expectations
- Shell environment always wins (explicit > implicit)

**Alternatives Considered:**
- **Reverse priority (env < .env)**: Non-standard, would confuse users familiar with .env pattern
- **No env override**: Would break current workflow where env vars override file config

### 4. Keep search paths unchanged
**Decision:** Search for .env in `.` → `~/.onclaw` → `/etc/onclaw` (same locations, different filename).

**Rationale:**
- Consistent user experience (same paths, just different file)
- Existing users familiar with these locations
- Documentation updates are minimal (just filename change)

**Alternatives Considered:**
- **Standard .env only (project root)**: Would break system-wide config capability
- **XDG config directories**: More complex, doesn't match current architecture

### 5. Type conversion via Viper mapstructure
**Decision:** Let Viper handle string-to-type conversion for int, bool, and array fields.

**Rationale:**
- Viper already has this capability via mapstructure
- Consistent with current config.yaml parsing
- No custom parsing code needed

**Implementation Details:**
- Comma-separated strings → arrays (e.g., `ONCLAW_TOOLS_SHELL_ALLOWLIST=ls,cat,git`)
- String → int (e.g., `ONCLAW_CONCURRENCY=4`)
- String → bool (e.g., `ONCLAW_LANGFUSE_MASK=false`)
- Viper handles errors gracefully (falls back to defaults)

### 6. Remove YAML dependencies if unused
**Decision:** Check if YAML is used elsewhere in codebase after removing config.yaml support. If not, remove the dependency.

**Rationale:**
- Reduce binary size (minor but meaningful for SBC targets)
- Reduce dependency attack surface
- Simplify dependency tree

**Implementation:**
- Search codebase for YAML usage
- If only used in config, remove `gopkg.in/yaml.v3` from go.mod
- If used elsewhere (e.g., OpenSpec artifacts), keep it

## Risks / Trade-offs

### Risk 1: Breaking change for existing users
**Risk:** Users with existing `config.yaml` files will have non-functional configuration.

**Mitigation:**
- Document breaking change clearly in proposal and design
- Update README.md with migration instructions
- Provide .env.example as reference for new format
- Accept this as intentional hard break (no migration tool)

### Risk 2: Type safety regression
**Risk:** .env files are string-only, may lead to more configuration errors than YAML.

**Mitigation:**
- Viper provides type conversion with error handling
- Comprehensive .env.example with all value types
- Clear error messages when type conversion fails
- Fallback to defaults on parse errors

### Risk 3: Security risk with provider API keys
**Risk:** Users may accidentally commit .env files with real API keys to version control.

**Mitigation:**
- .env.example uses placeholder values only
- Add .env to .gitignore (if not already present)
- Document security best practices in .env.example comments
- Existing provider database secrets remain secure (encryption unchanged)

### Risk 4: Nested configuration becomes flat
**Risk:** Loss of visual hierarchy - YAML nesting becomes underscore-separated flat names (e.g., `tools.shell.policy` → `ONCLAW_TOOLS_SHELL_POLICY`).

**Mitigation:**
- .env.example comments clearly group related variables
- Documentation shows mapping from YAML to .env format
- Internal code structure unchanged (still nested in Config struct)

### Trade-off 1: Simplicity vs. expressiveness
**Trade-off:** .env is simpler (KEY=value) but less expressive than YAML for complex nested structures.

**Assessment:** Acceptable - current config structure is relatively flat, .env format is sufficient for all current needs.

### Trade-off 2: Developer convenience vs. production rigor
**Trade-off:** .env is great for local development but less ideal for production (12-factor apps use real environment variables in production).

**Assessment:** Acceptable - priority order (defaults < .env < env < flags) supports both workflows: .env for local dev, real env vars for production.

## Implementation Overview

### Code Changes

**1. Update `internal/config/config.go`:**
- Remove `v.SetConfigType("yaml")` and `v.SetConfigName("config")`
- Add `v.SetConfigType("env")` and `v.SetConfigName(".env")` (note leading dot)
- Update `SearchPaths()` to return `.env` locations
- Update search loop to look for `.env` instead of `config.yaml`
- Remove YAML-specific error handling (keep generic file-not-found logic)
- Update `LoadedFrom` comment to reference .env instead of config.yaml

**2. Update `internal/config/config_test.go`:**
- Change `TestLoadFileOverridesDefault` to use .env format instead of YAML
- Add new test for .env-specific parsing (comments, quoted values)
- Add test for comma-separated array parsing
- Add test for type conversion (int, bool)
- Update all config.yaml references to .env

**3. Update `internal/cli/config_cmd.go`:**
- Update `config show` help text to reference .env instead of config.yaml
- Update `config path` help text to reference .env search paths
- Update redaction logic comment (if mentions config.yaml)

**4. Update `README.md`:**
- Replace YAML example with .env example in Configuration section
- Update search paths documentation
- Add .env-specific instructions (copy .env.example to .env)
- Update `onclaw config show` output description
- Add migration note for existing config.yaml users

**5. Update `.env.example`:**
- Verify all ONCLAW_* variables are documented
- Ensure security warnings are present
- Add examples for complex values (arrays, quoted strings)
- Update priority order comment if needed

### Dependency Cleanup

**Check for YAML usage elsewhere:**
```bash
grep -r "yaml" --include="*.go" | grep -v "test" | grep -v "change"
```

**If YAML only used in config:**
- Remove `gopkg.in/yaml.v3` and related transitive dependencies
- Run `go mod tidy` to clean up

**If YAML used elsewhere (likely):**
- Keep YAML dependency (used by OpenSpec artifacts)
- Document why YAML remains in go.mod

### Search Path Implementation

**Current search paths function:**
```go
func SearchPaths() []string {
    paths := []string{"."}
    if home, err := os.UserHomeDir(); err == nil && home != "" {
        paths = append(paths, filepath.Join(home, ".onclaw"))
    }
    paths = append(paths, "/etc/onclaw")
    return paths
}
```

**No changes needed** - same paths, just looking for `.env` instead of `config.yaml`.

**Config file resolution:**
```go
for _, p := range cfg.SearchPaths {
    v.AddConfigPath(p)
}
v.SetConfigName(".env")  // Was: "config"
v.SetConfigType("env")   // Was: "yaml"
```

### Error Handling

**Missing .env file:**
- Current behavior for config.yaml: not an error (silent)
- **Keep same behavior** for .env - missing file is OK

**Invalid .env syntax:**
- Viper's gotenv parser handles errors gracefully
- Invalid lines are skipped with warnings
- Invalid type conversions fall back to defaults

**Explicit --config flag:**
- If user specifies `--config /path/to/custom.env`, use that file
- If file not found, return error (same as current behavior)

## Open Questions

**Question 1:** Should we add a .env file validation command?
- **Proposal:** Add `onclaw config validate` to check .env syntax without loading
- **Status:** Out of scope for this change (can be added later)
- **Reasoning:** Nice to have but not required for MVP

**Question 2:** Should we support multiple .env files for different environments?
- **Proposal:** Support `.env`, `.env.development`, `.env.production` with `ONCLAW_ENV` variable
- **Status:** Out of scope (single .env file only)
- **Reasoning:** Adds complexity, users can use shell env vars for environment-specific overrides

**Question 3:** Should we add environment variable expansion in .env values?
- **Proposal:** Support `$HOME` or `${HOME}` expansion in .env paths
- **Status:** Out of scope
- **Reasoning:** Viper doesn't support this natively, would require custom implementation

## Migration Plan

### Phase 1: Implementation (No User Impact)
- Implement code changes
- Update tests
- Verify all tests pass
- No release yet

### Phase 2: Release and Documentation
- Update README.md and documentation
- Release new version with breaking change notice
- Clearly communicate in release notes

### Phase 3: User Migration
- Users with config.yaml must manually convert to .env
- Reference .env.example for format
- No automated migration tool provided

### Rollback Strategy
If critical issues arise:
- Revert to previous version (keep config.yaml support)
- Add .env as additional format (coexist with YAML)
- Plan migration approach with deprecation period

**Note:** Hard break means no rollback to old format in same codebase - users must downgrade binary version.
