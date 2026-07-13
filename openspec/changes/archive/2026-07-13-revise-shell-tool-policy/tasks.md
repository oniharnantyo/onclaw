# Tasks: Revise Shell Tool Policy

## Config (`internal/config/`)
- [x] Add `Denylist []string` to `ShellConfig` (`config.go`) with `mapstructure:"denylist"`.
- [x] Set the default policy to `"denylist"` in `defaults.go` and seed a default catastrophic
      `Denylist` (mass-destruction/halt, RCE-pipe, reverse-shell families).
- [x] Register the `tools.shell.denylist` Viper default (`config.go`).
- [x] Update `config_test.go` to assert the new default policy is `denylist` and that the default
      denylist is non-empty.

## Shell tool (`internal/agent/tools/shell.go`)
- [x] Add a `denylist` branch ahead of the existing `allowlist`/`ask` checks: when
      `policy == "denylist"`, evaluate the **full** `input.Command` against `scope.ShellDenylist`.
- [x] Add `ShellDenylist []string` to `Scope` (`tools.go`) and wire it through `AssembleAgent`
      (`agent.go`) and the session assembler (`internal/cli/agent_session.go`), mirroring
      `ShellAllowlist`.
- [x] Implement `matchesCatastrophic(command string, denylist []string) (matched bool, reason string)`
      — whole-string evaluation (compiled patterns), returning the matched pattern/category.
- [x] On match: do **not** execute; return a blocked result string that names the matched
      category/pattern, e.g. `"Command blocked (catastrophic-pattern: rce-pipe)"`.
- [x] Log every match (`slog.Warn`, command + matched category) for audit.
- [x] Compile patterns once (package-level or cached) rather than per call.

## Tests (`internal/agent/tools/shell_test.go`)
- [x] `denylist` allows an ordinary compound command (`cd web && npm run build` → runs).
- [x] `denylist` allows a benign pipe (`git log | grep foo` where no RCE → runs).
- [x] `denylist` blocks `rm -rf /`, `rm -rf ~`, `rm -rf *`; **allows** `rm -rf build/`.
- [x] `denylist` blocks `curl … | sh`, `wget … | bash`, `curl … | python` (spacing variants).
- [x] `denylist` blocks `/dev/tcp/…`, `bash -i >& …`, `nc -e …`, `mkfifo` reverse-shell shape.
- [x] `denylist` blocks fork bomb and `mkfs`/`dd … of=/dev/sd…` and `shutdown`/`reboot`.
- [x] A blocked result names the matched category and the command does **not** execute.
- [x] Existing `deny`/`allowlist` scenarios in `TestShellToolPolicy` remain green.
- [x] `ONCLAW_TOOLS_SHELL_DENYLIST=…` override flows through to the matcher (config-layering test).

## Agent wiring (`internal/agent/agent.go`, `internal/cli/agent_session.go`)
- [x] Thread `shellDenylist` through `AssembleAgent` and the session assembler call site.

## Verification
- [x] `make test` green for `internal/agent/tools/...` and `internal/config/...`.
- [x] `make vet` clean.
- [x] `openspec validate revise-shell-tool-policy --strict` passes.
- [ ] Manual (web UI): send a prompt that the agent satisfies with a normal command (e.g. `git
      status`) and confirm it runs; send/attempt `curl … | sh` and confirm it is blocked with a
      named reason and logged.
