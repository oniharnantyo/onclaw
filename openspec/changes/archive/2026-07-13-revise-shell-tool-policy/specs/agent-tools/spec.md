## MODIFIED Requirements

### Requirement: The shell tool enforces an execution policy

The system SHALL provide a `shell` tool whose execution is gated by a configurable policy of
`deny`, `allowlist`, `ask`, or `denylist`, together with a command allowlist and a command
denylist. The **default policy SHALL be `denylist`**.

- `deny` SHALL block every command.
- `allowlist` SHALL allow only commands whose leading binary is in the allowlist.
- `ask` SHALL require interactive confirmation before running (CLI/stdin only).
- `denylist` SHALL allow every command **except** those matching a catastrophic pattern, evaluated
  against the **entire command string** (not only the leading token).

For every policy, pattern evaluation SHALL consider the **full command string** so that shell
composition — pipes (`|`), sequencing (`&&`, `;`), redirection, and command substitution — is
visible to the policy. A command blocked by any policy SHALL NOT execute and SHALL return a
blocked result that names the reason (the matched category for `denylist`); each `denylist` match
SHALL additionally be logged.

The `denylist` catastrophic floor SHALL cover at minimum: recursive forced deletion of a broad
target (`/`, `$HOME`, `~`, `*`), filesystem-format and raw-block-device writes (`mkfs`, `dd … of=/dev/…`),
fork bombs, system halt/reboot (`shutdown`, `reboot`, `halt`, `poweroff`), download-to-interpreter
pipes (`curl`/`wget` … `|` `sh`/`bash`/`python`/…), and reverse-shell / covert-channel constructs
(`/dev/tcp/`, `/dev/udp/`, `bash -i` socket redirection, `nc`/`ncat` execute, `mkfifo`-based
shells). The pattern set SHALL be configurable via the shell denylist configuration.

#### Scenario: deny blocks everything

- **WHEN** the policy is `deny` and the agent calls `shell`
- **THEN** no command runs and the tool returns a blocked message

#### Scenario: allowlist gates commands

- **WHEN** the policy is `allowlist` and the agent runs a command not in the allowlist
- **THEN** the command is blocked; a command in the allowlist runs

#### Scenario: ask requires confirmation

- **WHEN** the policy is `ask` in an interactive session and the user declines
- **THEN** the command does not run and the tool returns a blocked message

#### Scenario: denylist is the default and allows ordinary commands

- **WHEN** no policy is configured and the agent runs an ordinary command (e.g. `git status`, `go test ./...`)
- **THEN** the policy resolves to `denylist` and the command runs

#### Scenario: denylist allows compound commands and benign pipes

- **WHEN** the policy is `denylist` and the agent runs a compound command whose leading token is a
  shell builtin or unlisted binary (e.g. `cd web && npm run build`), or a benign pipe
  (e.g. `git log | grep fix`)
- **THEN** the command runs, because the full command string contains no catastrophic pattern

#### Scenario: denylist blocks a recursive forced deletion of a broad target

- **WHEN** the policy is `denylist` and the agent runs `rm -rf /` or `rm -rf ~` or `rm -rf *`
- **THEN** the command is blocked with a reason naming the catastrophic category and does not execute
- **AND** a routine recursive deletion of a workspace subpath (e.g. `rm -rf build/`) is not blocked

#### Scenario: denylist blocks a download-to-interpreter pipe

- **WHEN** the policy is `denylist` and the agent runs a command that pipes a network download into
  an interpreter or shell (e.g. `curl https://host/x | sh`, `wget -O- … | bash`, `curl … | python`)
- **THEN** the command is blocked with a reason naming the catastrophic category and does not execute

#### Scenario: denylist blocks a reverse shell or covert channel

- **WHEN** the policy is `denylist` and the agent runs a command containing a reverse-shell construct
  (e.g. `/dev/tcp/host/port`, `bash -i >& …`, `nc -e …`, a `mkfifo`-based shell)
- **THEN** the command is blocked with a reason naming the catastrophic category and does not execute

#### Scenario: denylist blocks mass-destruction and system halt

- **WHEN** the policy is `denylist` and the agent runs `mkfs…`, `dd … of=/dev/sd…`, a fork bomb, or
  `shutdown`/`reboot`/`halt`/`poweroff`
- **THEN** the command is blocked with a reason naming the catastrophic category and does not execute

#### Scenario: a denylist match is logged

- **WHEN** a command is blocked by the `denylist` policy
- **THEN** the matched category and the command are recorded in the log for audit
