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

### Requirement: The web app uses URL-based routing

The system SHALL serve the management UI through a client-side router with one URL per primary surface, so that views are deep-linkable and browser back/forward navigation works. Agent configuration SHALL have its own URL rather than being a transient dialog. The default entry path SHALL redirect to the chat surface.

#### Scenario: An agent configuration page is deep-linkable

- **WHEN** a user navigates directly to the URL for a specific agent
- **THEN** that agent's configuration page loads without first visiting another view

#### Scenario: Browser navigation works across surfaces

- **WHEN** a user moves between the chat, agents list, and an agent configuration page
- **THEN** the browser back and forward buttons move between those views as expected

### Requirement: Agent configuration is a dedicated page, not a dialog

Creating and editing an agent SHALL happen on a dedicated page rather than a modal dialog. The
create form SHALL be a focused identity+model form — agent name, provider, model and its discovered
metadata, reasoning control, system prompt, workspace, max iterations, and default flag — and SHALL
NOT present resource sections (tools, hooks, skills, MCP servers, memory, persona) at create time.
Those resource sections SHALL appear only when editing an existing agent. A newly created agent
SHALL receive all globally-enabled builtin tools by default, expressed as an empty per-agent tool
allowlist, matching the `onclaw agent add` subcommand (which sets no allowlist). Global-scope
resources (hooks, skills, and the MCP/tool registries) SHALL be manageable from the same page
rendered for a reserved `global` scope, and the top-level navigation entries for those resources
SHALL route there.

#### Scenario: Creating an agent opens a focused page

- **WHEN** the user chooses to add an agent
- **THEN** a dedicated create page opens at a distinct URL (not a modal) showing only the
  identity+model form, with no tools, hooks, skills, MCP, memory, or persona sections

#### Scenario: A new agent defaults to all builtin tools

- **WHEN** the user creates an agent through the web create form and saves
- **THEN** the agent's per-agent tool allowlist is empty and the agent is offered every
  globally-enabled builtin tool at run time, identical to `onclaw agent add <name> --provider <p>`

#### Scenario: Resource sections appear when editing an existing agent

- **WHEN** the user opens an existing agent's configuration page
- **THEN** the page presents the tools, hooks, skills, MCP, memory, and persona sections scoped to
  that agent, and the tools section renders an empty allowlist as "all tools enabled"

#### Scenario: Global resources are managed on the same page

- **WHEN** the user opens the global hooks view from navigation
- **THEN** the agent configuration page renders in its global scope showing global hooks

### Requirement: Editable configuration is rendered as structured fields

Every editable configuration exposed in the agent page — including the per-agent memory configuration and MCP server selection — SHALL be rendered as one typed form field per property (selects, checkboxes, number and text inputs, each labeled with a tooltip and inline validation), driven by the configuration's schema. The UI SHALL NOT expose editable structured configuration as a single raw-JSON textarea. Free-text values whose content is genuinely unstructured (system prompt, persona markdown) MAY remain textareas.

#### Scenario: The memory configuration form uses one field per property

- **WHEN** the user edits an agent's memory configuration
- **THEN** each feature toggle is a switch and each parameter is a typed input, and saving persists the structured configuration

#### Scenario: A raw-JSON configuration editor is not offered

- **WHEN** the user edits per-agent memory or MCP configuration
- **THEN** no raw-JSON textarea is presented as the editing surface for that structured configuration

### Requirement: The web agent-edit form preserves all agent fields across a save

The web agent-edit page SHALL round-trip every persisted agent field unchanged when the user saves, regardless of whether model metadata has finished loading. In particular, a stored `reasoning_effort` and `reasoning_budget_tokens` SHALL be retained unless the user explicitly changes them, and the form SHALL NOT clear those fields as a side effect of a client-side model-capability guess. Reasoning capability SHALL be determined solely from the live model metadata returned by `/api/providers/{name}/models` (`thinking` flag and `reasoning_options`); the UI SHALL NOT infer reasoning support from the model's name (no provider-specific name regexes or prefixes), matching the `agent-update` requirement. When the selected model changes, the form SHALL re-resolve and store that model's discovered metadata into `model_metadata` so the persisted row reflects the chosen model (default metadata for a custom/unknown model), matching the `agent-update` requirement.

#### Scenario: Reasoning effort survives an edit on a non-OpenAI reasoning model

- **WHEN** the user edits an agent whose model is a reasoning model whose name does not start with `o1` or `o3` and whose `reasoning_effort` is `high`, and saves without touching the reasoning field
- **THEN** `GET /api/agents/{name}` still returns `reasoning_effort: high` (the value is not wiped while model metadata loads)

#### Scenario: Reasoning support is not inferred from the model name

- **WHEN** the agent's model is a reasoning model and the `/api/providers/{name}/models` response flags it `thinking: true`
- **THEN** the reasoning controls are shown and the stored reasoning value is preserved, with no fallback to a name-prefix check

#### Scenario: Changing the model refreshes stored metadata

- **WHEN** the user changes the agent's model to a discovered reasoning model and saves
- **THEN** `GET /api/agents/{name}` returns `model_metadata` with `thinking: true` and the Agents list card shows the reasoning badge

#### Scenario: A custom model is stored with default metadata

- **WHEN** the user types a model name absent from enumeration and saves
- **THEN** the agent is persisted with default metadata (context window 0, thinking false, text-only) and no error is shown

### Requirement: The agent workspace is editable from the web UI

The agent-edit Overview form SHALL expose `workspace` as a structured text field with a label, tooltip, and inline hint, and SHALL persist it through the existing agent update endpoint. The field SHALL NOT be presented as a raw-JSON editor. An empty value SHALL be valid and SHALL resolve to the agent's default workspace per the `agent-workspace` resolution precedence.

#### Scenario: A workspace set in the UI is visible to the CLI

- **WHEN** the user enters a workspace path on the agent-edit page and saves
- **THEN** `onclaw agent show <name>` reports that workspace

#### Scenario: An empty workspace resolves to the agent default

- **WHEN** the user leaves the workspace field empty and saves
- **THEN** the stored `workspace` is empty and the agent resolves its default workspace (`~/.onclaw/workspace/<agent>/`)

#### Scenario: An existing workspace round-trips through an edit

- **WHEN** the user opens an agent that already has a workspace and saves the Overview form without changing it
- **THEN** the workspace value is unchanged after the save

### Requirement: The agent-edit form exposes a checkbox-gated max-context override

The web agent-edit Overview form SHALL expose the per-agent max-context override behind an "Override max context" checkbox. The checkbox state SHALL derive from the stored value — checked when `max_context_tokens > 0`, unchecked when it is `0`. When unchecked, the number input SHALL be disabled or hidden and the agent inherits the global default context window on save; when checked, the number input SHALL be enabled and accept a positive integer. Unchecking and saving SHALL store `max_context_tokens = 0` (inherit); checking and saving SHALL store the entered value. The checkbox maps to the existing `0 = inherit` sentinel and introduces no new storage field. The number field SHALL carry a label, tooltip, inline validation (positive integer), and a hint showing the global default, and MAY pre-fill with the selected model's discovered context window when the override is first enabled. The value SHALL be saved through the existing agent update endpoint as a typed integer (never a raw-JSON editor and never dropped due to undefined-omission).

#### Scenario: The override is off by default and inherits the global value

- **WHEN** the user opens an agent whose `max_context_tokens` is `0` (or creates a new agent)
- **THEN** the "Override max context" checkbox is unchecked, the number field is disabled or hidden, and saving leaves the agent inheriting the global default context window

#### Scenario: Checking the box enables the override

- **WHEN** the user checks "Override max context", enters `32000`, and saves
- **THEN** `onclaw agent show <name>` reports `max_context_tokens: 32000` and the agent assembles with a 32000-token context window

#### Scenario: A previously-set override re-opens checked and populated

- **WHEN** the user opens an agent with a stored `max_context_tokens` of `32000`
- **THEN** the checkbox renders checked and the number field shows `32000`; saving without changing it leaves the value unchanged

#### Scenario: Unchecking clears the override back to inherit

- **WHEN** the user unchecks "Override max context" on an agent whose override was `32000` and saves
- **THEN** the stored `max_context_tokens` becomes `0` and the agent assembles with the global default context window

