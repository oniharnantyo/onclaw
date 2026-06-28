## 1. Config Loading Implementation

- [x] 1.1 Update `Load()` function in `internal/config/config.go` to use `.env` instead of `config.yaml`
- [x] 1.2 Change `v.SetConfigName(".env")` (with leading dot) instead of `v.SetConfigName("config")`
- [x] 1.3 Change `v.SetConfigType("env")` instead of `v.SetConfigType("yaml")`
- [x] 1.4 Update `SearchPaths()` function documentation to reference `.env` files instead of `config.yaml`
- [x] 1.5 Update config file search loop comments to reference `.env` loading
- [x] 1.6 Update `LoadedFrom` field documentation and comments to mention `.env` instead of `config.yaml`
- [x] 1.7 Verify that missing `.env` files do not cause errors (silent fallback to defaults)

## 2. Test Updates

- [x] 2.1 Rename and update `TestLoadFileOverridesDefault` to use `.env` format instead of YAML
- [x] 2.2 Update test to create `.env` file with KEY=value format instead of YAML syntax
- [x] 2.3 Add new test `TestLoadEnvFileWithComments` to verify comment lines are ignored
- [x] 2.4 Add new test `TestLoadEnvFileWithQuotedValues` to verify quoted string parsing
- [x] 2.5 Add new test `TestEnvFileCommaSeparatedArrays` to verify array parsing from comma-separated strings
- [x] 2.6 Add new test `TestEnvFileTypeConversion` to verify int/bool/string type conversion
- [x] 2.7 Update `TestSearchPathsAlwaysIncludesCwdAndEtc` to verify `.env` search paths
- [x] 2.8 Update all test comments and variable names referencing `config.yaml` to reference `.env`
- [x] 2.9 Run `rtk go test ./internal/config/...` and verify all tests pass

## 3. CLI Updates

- [x] 3.1 Update `config show` command help text in `internal/cli/config_cmd.go` to reference `.env` instead of `config.yaml`
- [x] 3.2 Update `config path` command help text to reference `.env` search paths
- [x] 3.3 Update command descriptions to mention `.env` file format
- [x] 3.4 Update any comments referencing `config.yaml` or YAML format

## 4. Documentation Updates

- [x] 4.1 Update `README.md` Configuration section to replace YAML example with `.env` example
- [x] 4.2 Replace `config.yaml` example with `.env` format showing KEY=value syntax
- [x] 4.3 Update search paths documentation to show `.env` file locations
- [x] 4.4 Update priority order description from `defaults < config file < env < flags` to `defaults < .env < env < flags`
- [x] 4.5 Add instruction to copy `.env.example` to `.env` for configuration
- [x] 4.6 Add migration note explaining that `config.yaml` is no longer supported
- [x] 4.7 Update `onclaw config show` output description
- [x] 4.8 Verify `.env.example` file has all ONCLAW_* variables documented
- [x] 4.9 Verify `.env.example` has security warnings for provider API keys
- [x] 4.10 Verify `.env.example` has examples for complex values (arrays, quoted strings)

## 5. Verification and Testing

- [x] 5.1 Run `rtk go test ./...` to verify all tests pass
- [x] 5.2 Run `rtk go build -o bin/onclaw .` to verify binary builds successfully
- [x] 5.3 Create test `.env` file in current directory and run `./bin/onclaw config show`
- [x] 5.4 Verify `.env` values are loaded correctly and shown in config output
- [x] 5.5 Run `./bin/onclaw config path` and verify correct `.env` search paths are shown
- [x] 5.6 Test with no `.env` file and verify defaults work correctly
- [x] 5.7 Test environment variable overrides by setting `ONCLAW_LOG_LEVEL` and verifying it overrides `.env`
- [x] 5.8 Test with invalid `.env` syntax and verify graceful fallback
- [x] 5.9 Test provider API key from `.env` and verify it works (if provider setup available)

## 6. Dependency Cleanup

- [x] 6.1 Search codebase for YAML usage: `rtk grep -r "yaml" --include="*.go" | grep -v test | grep -v change`
- [x] 6.2 If YAML only used in config, remove `gopkg.in/yaml.v3` from `go.mod` (N/A: transitive dependency remains)
- [x] 6.3 Run `rtk go mod tidy` to clean up dependencies
- [x] 6.4 If YAML used elsewhere (e.g., OpenSpec), document why it remains in dependencies (transitive dependency of eino)
- [x] 6.5 Verify final binary size has not increased significantly

## 7. Final Checks

- [x] 7.1 Run `rtk make vet` to verify code passes go vet
- [x] 7.2 Run `rtk make fmt` to ensure code is properly formatted
- [x] 7.3 Run `rtk make lint` to verify no linting issues (if golangci-lint available)
- [x] 7.4 Review all changed files for any remaining `config.yaml` or `yaml` references
- [x] 7.5 Verify `.gitignore` includes `.env` to prevent accidental commits
- [x] 7.6 Cross-reference README.md example with actual `.env.example` to ensure consistency
