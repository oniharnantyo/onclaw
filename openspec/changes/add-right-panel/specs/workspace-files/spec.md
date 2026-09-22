## Purpose

Authenticated read and listing of an agent's workspace files — the files the agent's tools wrote into its workspace directory (reports, screenshots, generated assets) — served with path confinement and content-type-safe headers so the web app can display them without exposing the host filesystem or executing stored content.

## ADDED Requirements

### Requirement: Agent file read
The system SHALL expose an authenticated endpoint that returns the content of one file from a named agent's workspace files, addressed by a path relative to that agent's workspace root. The requested path SHALL resolve strictly inside the agent's workspace root: path segments that escape it (parent traversal, absolute paths, or symbolic links that resolve outside) SHALL be rejected as not found, never served. A missing file SHALL return not found.

#### Scenario: Read an authored markdown file
- **WHEN** a workspace member requests `report.md` for an agent whose workspace contains that file
- **THEN** the response carries the file bytes with the file's content type

#### Scenario: Parent traversal is rejected
- **WHEN** a request addresses `../../another-agent/secrets.md`
- **THEN** the endpoint responds not found and no file content is served

#### Scenario: Absolute path is rejected
- **WHEN** a request addresses an absolute filesystem path such as `/etc/passwd`
- **THEN** the endpoint responds not found and no file content is served

#### Scenario: Symbolic-link escape is rejected
- **WHEN** a path inside the workspace root is a symbolic link resolving outside it
- **THEN** the endpoint responds not found and no file content is served

#### Scenario: Missing file
- **WHEN** a request names a file that does not exist under the agent's workspace root
- **THEN** the endpoint responds not found

### Requirement: Agent file listing
The system SHALL expose a listing mode on the same endpoint that returns one directory level of the agent's workspace files at a time — each entry carrying its name, file-or-directory kind, size, and last-modified time — so a caller can walk the tree lazily without a recursive listing. Listing a path that is not a directory SHALL fail as not found.

#### Scenario: List the workspace root
- **WHEN** a workspace member lists the agent's workspace root
- **THEN** the response contains one entry per direct child with name, kind, size, and modified time, and no deeper descendants

#### Scenario: List a nested directory
- **WHEN** a workspace member lists a subdirectory such as `browser/`
- **THEN** the response contains only that directory's direct children

#### Scenario: List a non-directory
- **WHEN** a workspace member lists a path that names a regular file
- **THEN** the endpoint responds not found

### Requirement: Workspace scoping
Access SHALL be scoped by the authenticated member's workspace: the workspace in scope comes from the request's authentication context, never from a caller-supplied identifier alone, and a member of one workspace SHALL NOT read or list another workspace's agent files. A member without access to the workspace in scope SHALL get the same not-found treatment as a missing agent.

#### Scenario: Cross-workspace access is denied
- **WHEN** a member authenticates against workspace A and requests files of an agent belonging to workspace B
- **THEN** the endpoint responds not found and no file content is served

### Requirement: Content-type-safe serving
Responses SHALL always carry `X-Content-Type-Options: nosniff`. Content types that can execute in a browser context — HTML and SVG — SHALL NOT be served inline: they SHALL carry an attachment disposition (or equivalent download treatment) so stored content cannot execute on the application origin. Display-safe types (text, code, markdown, images other than SVG, PDF) MAY serve inline for viewing.

#### Scenario: Stored HTML never executes inline
- **WHEN** an agent's workspace contains `payload.html` and a member requests it
- **THEN** the response carries an attachment disposition and the browser downloads it instead of rendering it

#### Scenario: Stored SVG never executes inline
- **WHEN** an agent's workspace contains `logo.svg` and a member requests it
- **THEN** the response carries an attachment disposition

#### Scenario: nosniff on every response
- **WHEN** any file read or listing response is served
- **THEN** the response carries `X-Content-Type-Options: nosniff`
