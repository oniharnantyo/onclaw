## ADDED Requirements

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
