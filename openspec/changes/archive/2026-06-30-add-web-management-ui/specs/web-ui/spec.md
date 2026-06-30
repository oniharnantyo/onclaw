## ADDED Requirements

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