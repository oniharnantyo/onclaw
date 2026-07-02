# web-ui Specification

## Purpose
TBD - created by archiving change add-web-management-ui. Update Purpose after archive.
## Requirements
### Requirement: `onclaw serve` serves an embedded web console

The system SHALL provide `onclaw serve` that assembles the provider manager through the same DB/DEK/service path used by `run` and `chat`, writes the PID file, starts the database watcher and SIGHUP hot-reload handler, and serves an HTTP server on the configured bind address and port. The server SHALL serve the embedded single-page app at `/` and the JSON API under `/api/*`. The device SHALL perform no server-side HTML rendering: the only response bodies SHALL be static assets, JSON, and the chat Server-Sent Events stream.

#### Scenario: serve boots and serves the shell

- **WHEN** `onclaw serve` is run after the UI is built
- **THEN** the SPA loads at `http://<host>:<port>/` and `GET /api/health` returns 200

#### Scenario: profile edits hot-reload into the running server

- **WHEN** a provider is edited through the API while `serve` is running
- **THEN** the running `serve` process reloads the change on the next request via the existing hot-reload mechanism, without restart

#### Scenario: the device renders no HTML

- **WHEN** any `/api/*` route is requested
- **THEN** the response is JSON (or the SSE stream for chat), never a server-rendered HTML page

### Requirement: All management routes require passphrase authentication

The system SHALL gate every `/api/*` route except `/api/login` behind a valid session. Authentication SHALL verify the supplied passphrase against a bcrypt hash stored in the `preferences` key-value table under `web_password_hash`, set via `onclaw serve --set-password`. The server SHALL refuse to start when no password hash is configured, with a message instructing the user to set one. Sessions SHALL be random in-memory tokens carried in a signed, `HttpOnly`, `SameSite=Strict` cookie.

#### Scenario: unauthenticated requests are rejected

- **WHEN** a request to `GET /api/providers` is made with no valid session cookie
- **THEN** the server responds 401 and the client is directed to log in

#### Scenario: a correct passphrase establishes a session

- **WHEN** `POST /api/login` is called with the correct passphrase
- **THEN** a session cookie is set and subsequent `/api/*` requests succeed

#### Scenario: serve refuses to start without a password

- **WHEN** `onclaw serve` is run and no `web_password_hash` is configured
- **THEN** the server refuses to start and instructs the user to run `onclaw serve --set-password`

### Requirement: The web passphrase is independent of data-at-rest encryption

The system SHALL keep the web login passphrase (bcrypt hash in `preferences`) separate from the DEK/keyfile encryption used for secrets. `serve` SHALL decrypt the DEK through the existing keyfile-mode path, identical to `run` and `chat`, so secrets are read and written through the API without an interactive DEK passphrase. v1 SHALL NOT support Argon2id passphrase DEK unlock for `serve`.

#### Scenario: secrets are usable in keyfile mode without an unlock prompt

- **WHEN** `serve` runs in keyfile mode and a provider key is read through the API
- **THEN** the DEK is decrypted unattended via the keyfile and the secret is usable, with no interactive passphrase prompt

### Requirement: Providers and secrets are managed over JSON without disclosing keys

The system SHALL expose JSON endpoints to list, add, show, update, and delete provider profiles, to set the default provider, and to set a provider's API key, each delegating to the existing `llm.Service` methods. A secret-status endpoint SHALL return only a masked indicator (whether a key is set and a non-reversible hint). The system SHALL NEVER return a plaintext API key over the API.

#### Scenario: provider CRUD delegates to the service layer

- **WHEN** a provider is added, edited, or removed through the API
- **THEN** the change is persisted through `llm.Service` and is visible to `onclaw provider list`

#### Scenario: API keys are never returned in plaintext

- **WHEN** `GET /api/providers/:name/secret` is requested
- **THEN** the response contains only a masked set/not-set indicator and hint, never the key itself

### Requirement: Agent profiles are managed over JSON

The system SHALL expose JSON endpoints to list, add, show, update, and delete agent profiles and to set the default agent, delegating to the existing `llm.Service` methods. Field semantics SHALL match the `onclaw agent` subcommands.

#### Scenario: an agent created in the UI is visible to the CLI

- **WHEN** an agent is created through the API with a model, tools, workspace, and system prompt
- **THEN** `onclaw agent show <name>` returns the same configuration

### Requirement: Conversations can be listed and read in the UI

The system SHALL expose JSON endpoints to list conversations (via the new `ConversationStore.ListConversations`) and to read a conversation's messages (via `ListMessages`). The list SHALL include each conversation's id, agent name, timestamps, and message count.

#### Scenario: a prior chat appears in the list and is readable

- **WHEN** a conversation was created by `onclaw chat` and the UI lists conversations
- **THEN** that conversation appears with its message count, and its messages can be read in detail

### Requirement: Chat streams live over Server-Sent Events

The system SHALL provide `POST /api/chat` that resolves the agent and effective profile using the same logic as `run` (reusing the shared session helper), creates or reuses a conversation, and streams output as a `text/event-stream` by iterating the `EventIterator` returned from `(*Agent).Run`. The handler SHALL emit each yielded `*schema.AgenticMessage` as an SSE event (the iterator surfaces model stream chunks, so output arrives incrementally), a final `done` event on success, and an `error` event on failure. The handler SHALL stop the run when the client disconnects. Messages SHALL persist through the existing history middleware, so a streamed exchange is visible in the conversation viewer afterward.

#### Scenario: a prompt streams message by message

- **WHEN** a user submits a prompt in the chat screen
- **THEN** assistant output arrives as SSE events, including incremental deltas when the model streams, followed by `done`, rendered live in the UI

#### Scenario: a client disconnect cancels the run

- **WHEN** the browser closes the stream mid-run
- **THEN** the handler observes request-context cancellation and stops the run

#### Scenario: a streamed exchange is persisted

- **WHEN** a chat turn completes and the user reloads
- **THEN** the user and assistant messages are stored and visible in the conversation viewer

#### Scenario: the chat starts with an init event containing the conversation ID

- **WHEN** a user initiates a chat session
- **THEN** the server first emits an `init` event containing `conversation_id` so the client can track and associate the session

### Requirement: The management server separates transport from domain logic

The `onclaw serve` HTTP server SHALL be organized into a transport layer and a
domain layer as separate Go packages. The domain layer (`internal/api/service`)
SHALL contain all business logic and SHALL NOT import `net/http` or
`database/sql`. Transport-layer packages (`internal/api` and its `handler`,
`httpx`, and `auth` sub-packages) SHALL access persisted data only through the
existing `store.*` interfaces (`internal/store/store.go`) — they SHALL NOT issue
raw SQL. HTTP handlers SHALL be thin: decode the request, delegate to the domain
layer, and encode the response. The Go package SHALL be named `api` (renamed from
`web`) so it is distinct from the frontend Vite project at the repository root.

#### Scenario: production handlers issue no raw SQL

- **WHEN** the transport layer is searched for direct database calls
- **THEN** no production handler contains `QueryRowContext`, `QueryContext`, or
  `ExecContext` against `*sql.DB`; all persisted reads and writes flow through
  `store.*` interfaces

#### Scenario: the domain layer is free of HTTP and database imports

- **WHEN** the `internal/api/service` package imports are inspected
- **THEN** the package imports neither `net/http` nor `database/sql`

#### Scenario: handlers delegate business logic to the service layer

- **WHEN** a provider, agent, conversation, or chat request is handled
- **THEN** the HTTP handler decodes the request, calls a method on the domain
  `Service`, and encodes the returned view — assembling derived response fields
  (such as the default flag and secret-set indicator) inside the service layer,
  not the handler

#### Scenario: the Go package is renamed and the frontend is untouched

- **WHEN** the server package is relocated
- **THEN** the Go package lives at `internal/api` (package name `api`) and the
  frontend Vite project at the repository root `web/` is unchanged except for its
  build-output path (`web/vite.config.ts` `outDir` → `../internal/api/assets`)

### Requirement: The hook configuration dialog validates handler inputs before submission

The Configure New Hook dialog SHALL validate handler inputs in the browser before they are submitted and SHALL prevent the save when a hard validation error is present. The tool-name matcher SHALL be validated as a regex and the dialog SHALL warn when the pattern contains constructs that Go's RE2 `regexp` engine rejects (lookahead, look-behind, or backreferences), because the server validates matchers with RE2. The shell command SHALL be checked for non-emptiness and balanced quoting. The JavaScript source SHALL be checked for syntax and SHALL be checked to define a `handle(ctx)` function, matching the script-handler contract. Guidance shown in the dialog (placeholders, hints) SHALL reflect the real `function handle(ctx)` returning `{decision, reason}` contract and SHALL NOT reference functions or globals the sandbox does not provide.

#### Scenario: an invalid regex blocks save with an inline error

- **WHEN** the user enters an unbalanced regex matcher such as `(`
- **THEN** the dialog shows an inline error on the matcher field and the save action is disabled

#### Scenario: an RE2-incompatible regex warns the user

- **WHEN** the user enters a regex using lookahead such as `(?=foo)`
- **THEN** the dialog warns that the construct is not supported by the server's RE2 engine

#### Scenario: a script missing handle(ctx) blocks save

- **WHEN** the user enters JavaScript that does not define `handle(ctx)`
- **THEN** the dialog shows an inline error explaining the required contract and the save action is disabled

#### Scenario: the JavaScript placeholder reflects the real contract

- **WHEN** the user opens the JavaScript source field
- **THEN** the placeholder shows a `function handle(ctx)` example returning `{decision, reason}` and references only `ctx.*` event fields

### Requirement: The hook configuration dialog offers a dry-run test

The Configure New Hook dialog SHALL provide a Test action that submits the in-progress hook to the existing `POST /api/hooks/test` dry-run endpoint and displays the returned decision, and any error, inline. The Test action SHALL run client-side validation first and SHALL NOT call the endpoint when a hard validation error is present. The dry-run SHALL NOT persist the hook or write an audit row.

#### Scenario: a valid script tests successfully

- **WHEN** the user clicks Test with a syntactically valid script that defines `handle(ctx)`
- **THEN** the dialog displays the decision returned by the dry-run endpoint

#### Scenario: a failing script shows the server error

- **WHEN** the user clicks Test with a script that compiles but throws at runtime
- **THEN** the dialog displays the error returned by the dry-run endpoint without saving the hook

### Requirement: The hook configuration dialog provides per-field guidance

The Configure New Hook dialog SHALL present a tooltip on each field that explains its meaning and effect, including which lifecycle events are blocking, the fail-closed timeout policy, priority ordering, that the matcher is RE2 and applies only to tool events, the command exit-code semantics, the environment-variable allowlist baseline, and the JavaScript sandbox contract. The lifecycle-event and handler-type selects SHALL provide per-option explanations.

#### Scenario: a user can discover what each field does

- **WHEN** the user invokes the guidance control on any field (hover or keyboard focus)
- **THEN** an explanation of that field's meaning and effect is shown

### Requirement: The web console provides an MCP servers management page

The web console SHALL include an MCP servers page, reachable from the shell navigation, that lists
every configured MCP server with its name, transport, and enabled state. The page SHALL let the
user add and edit servers through a modal whose fields adapt to the selected transport (stdio:
command, arguments, and environment variables; http/sse: URL), toggle a server between enabled and
disabled, remove a server, and test an unsaved configuration to preview its discovered tools before
saving. All page actions SHALL operate exclusively through the authenticated `/api/mcp` endpoints.

#### Scenario: The MCP page is reachable from navigation

- **WHEN** an authenticated user clicks the MCP Servers navigation item
- **THEN** the MCP servers page renders and lists the configured servers

#### Scenario: A server is added through the modal

- **WHEN** the user opens the add modal, selects transport `stdio`, fills in a command and args, and
  saves
- **THEN** the server is created via `POST /api/mcp` and appears enabled in the list

#### Scenario: An unsaved server is tested from the modal

- **WHEN** the user fills in a server configuration in the modal and clicks Test
- **THEN** the page calls `POST /api/mcp/test` and displays the discovered tool names without saving

#### Scenario: A server is toggled from the list

- **WHEN** the user clicks the enabled toggle on a server row
- **THEN** the page calls `POST /api/mcp/{name}/toggle` and the row's state updates

