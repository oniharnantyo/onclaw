## ADDED Requirements

### Requirement: stdio child environment is constructed, not inherited
The system SHALL launch stdio MCP server processes with a constructed environment consisting of (a) a fixed safe baseline and (b) exactly the connection's configured env rows, and SHALL NOT pass the OnClaw server process's other environment variables to the child. The baseline SHALL include at minimum `PATH`, `HOME`, `TMPDIR`, `LANG`, `LC_ALL`, `TZ`, and the standard proxy variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY` and their lowercase forms). A configured env row MUST override a baseline entry with the same name. Probe dials and run dials SHALL construct the child environment identically. URL-transport (streamable HTTP, SSE) connections are unaffected by this requirement.

#### Scenario: Child sees baseline plus configured rows
- **WHEN** a stdio MCP server with env rows `FOO=bar` and `PATH=/custom/bin` is dialed for a run
- **THEN** the child process environment contains `FOO=bar`, `PATH=/custom/bin` (configured row wins over baseline), and the baseline variables (e.g. `HOME`, `HTTPS_PROXY`) when set on the host

#### Scenario: Parent-only variables are not leaked
- **WHEN** the OnClaw server process has a variable `DATABASE_URL` that is not in the MCP connection's configured env rows
- **THEN** the stdio child process environment does not contain `DATABASE_URL`, whether dialed by a run or by a probe

#### Scenario: Probe and run environments match
- **WHEN** the same stdio MCP server is dialed once by a probe and once by an agent run with identical configuration
- **THEN** the child observes the same environment variable set in both dials

#### Scenario: URL transports unchanged
- **WHEN** a streamable HTTP or SSE MCP connection is dialed
- **THEN** only the configured header rows are attached to requests and the parent environment plays no role
