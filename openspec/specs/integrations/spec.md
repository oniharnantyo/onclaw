# integrations Specification

## Purpose

Let users connect external services (GitLab, Google Workspace, Slack, …) by picking the service instead of hand-configuring an MCP server: a catalog-driven Integrations pane supplies endpoints, auth methods, and setup guidance, creating ordinary MCP server definitions that agents consume through the existing machinery.

## Requirements

### Requirement: Service catalog grid

The Integrations pane SHALL present the service catalog as a flat grid of tiles, one per service, each tile showing the service's brand mark (a known glyph; unknown services fall back to an initial-glyph tile of the same size), its label, and a connection status dot — success for connected, danger for failed, warning for authorization required, neutral for not connected. A failed tile SHALL surface the underlying reason. Tiles SHALL render legibly in both themes. The grid SHALL NOT group tiles by category; category is expressed only through the filter rail.

#### Scenario: Tile shows identity and state

- **WHEN** the Integrations pane lists a connected service with a known brand mark
- **THEN** its tile shows the brand glyph, the service label, and a success status dot

#### Scenario: Failed connection shows the reason

- **WHEN** an integration's underlying MCP server is in the failed state
- **THEN** its tile shows a danger dot and the failure reason is visible from the tile or its detail

#### Scenario: No category headers

- **WHEN** the full catalog is shown with no filters
- **THEN** the tiles render as one flat grid without category section headers

### Requirement: Filter rail

The Integrations pane SHALL offer a filter rail above the grid with two controls that compose: category chips — including an All chip — each labeled with the number of catalog services in that category, and a search input that filters tiles by label and catalog keywords, case-insensitively. Chip counts SHALL reflect the catalog, not the active search. When no tile matches the active filters, the pane SHALL show an empty state with an affordance that clears both filters.

#### Scenario: Category chip narrows the grid

- **WHEN** the user selects the Productivity chip
- **THEN** the grid shows only productivity services and the chip's count is unchanged by any search text

#### Scenario: Search composes with the category

- **WHEN** a category chip is selected and the user types a query matching one service inside it
- **THEN** only that service's tile is shown

#### Scenario: Empty state clears filters

- **WHEN** the active category and query match no service
- **THEN** the pane shows an empty state and its clear action restores the full grid with All selected

### Requirement: Initial catalog

The catalog SHALL ship with at least: GitLab (instance-template endpoint; OAuth and fine-grained token methods), GitHub (OAuth and token), Atlassian (OAuth), Linear (OAuth and API key), Notion (OAuth), Sentry (OAuth), Slack (OAuth with a pre-registered client), the Google Workspace family (Gmail, Calendar, Drive, Sheets, Docs; OAuth with a pre-registered client), and Google Maps (API-key header). Each entry SHALL declare the auth methods its endpoint accepts, and the connect sheet SHALL offer exactly those methods — a service whose endpoint refuses token headers SHALL NOT offer a token option.

#### Scenario: Launch services are present

- **WHEN** the Integrations pane opens with a fresh install
- **THEN** tiles for GitLab, GitHub, Atlassian, Linear, Notion, Sentry, Slack, the Google Workspace products, and Google Maps are offered

#### Scenario: Auth methods follow the catalog

- **WHEN** the user opens the connect sheet for a service that declares OAuth only
- **THEN** no token-entry option is offered

### Requirement: Connect flow

Clicking a not-connected tile SHALL open a connect sheet driven by the catalog entry: an editable instance URL when the endpoint is templated, a choice among the declared auth methods, and the inputs the chosen method needs — a credential field for token methods, a client ID and secret for pre-registered OAuth clients. Connecting SHALL create an MCP server definition tagged with the service id, carrying the chosen endpoint and auth settings, and then probe it: a successful probe flips the tile to connected; a 401 whose protection advertises OAuth marks the tile authorization-required and offers Authorize; any other failure shows the reason. Editing an integration reopens the same sheet with the stored non-secret values; removing an integration deletes its server definition.

#### Scenario: Connect creates a tagged server definition

- **WHEN** the user connects GitLab through the sheet
- **THEN** an MCP server definition tagged `gitlab` exists with the chosen endpoint and auth settings, and the tile reflects the probe outcome

#### Scenario: Templated endpoint asks for the instance

- **WHEN** the user opens the connect sheet for a self-managed-capable service
- **THEN** the instance URL field is editable and defaults to the catalog's public default

#### Scenario: Authorization required after connect

- **WHEN** connecting via OAuth is rejected as unauthenticated and the server advertises OAuth metadata
- **THEN** the tile shows authorization-required with an Authorize action that starts the browser flow

#### Scenario: Edit keeps the stored credential

- **WHEN** the user edits a token-authenticated integration and submits with the credential field blank
- **THEN** the stored credential is preserved and the key-set state is unchanged

### Requirement: Integration credential secrecy

Integration credentials — token credentials and pre-registered client secrets — MUST be returned to no frontend read or listing. Reads and listings SHALL expose only whether a credential is set and at most a short non-secret hint derived from it (the last 4 characters). An integration credential MUST NOT be stored in the MCP server definition's round-tripping header map; the connection layer SHALL inject it at dial time. Updating with a blank credential SHALL keep the stored value; a non-blank value replaces it.

#### Scenario: Credential never round-trips

- **WHEN** the frontend lists or reads an integration with a stored token
- **THEN** no response contains the credential, only key-set state and the last-4 hint

#### Scenario: Credential is not a raw header

- **WHEN** an integration with a token credential is edited in the raw MCP pane
- **THEN** the credential is not visible among the definition's headers

### Requirement: Family entries

A catalog entry MAY declare a family of related services sharing one setup. Connecting a family SHALL present one sheet — the shared setup inputs plus a checklist of the family's products — and SHALL create one tagged MCP server definition per selected product. Each product SHALL appear as its own tile with its own connection state, and a product may be connected or removed independently after the shared setup exists; opening any family tile SHALL reuse the stored shared setup and offer the product checklist again.

#### Scenario: Family connect creates product definitions

- **WHEN** the user completes the Google Workspace sheet with Gmail and Calendar checked
- **THEN** two tagged MCP server definitions are created, and both tiles enter their own probe-driven states

#### Scenario: Adding a product later reuses the setup

- **WHEN** the user opens any Google Workspace tile after the family setup exists
- **THEN** the sheet shows the stored setup and offers the not-yet-connected products

### Requirement: Integrations are ordinary MCP servers

Integration-created definitions SHALL behave as ordinary MCP servers everywhere else: listed in the MCP pane, subject to the per-agent server allowlist, refreshable, disableable, and removable from either place. The service tag SHALL survive edits made in the MCP pane, and the integration tile SHALL stay in sync with the definition's live state. A definition whose service tag is removed or was never set SHALL NOT appear in the Integrations grid.

#### Scenario: Toggle and allowlist semantics carry over

- **WHEN** the user disables an integration's server in the MCP pane
- **THEN** agents no longer receive its tools and the integration tile reflects the disabled state

#### Scenario: Custom servers stay out of the grid

- **WHEN** a user adds a raw MCP server without a service tag
- **THEN** it appears only in the MCP pane, not in the Integrations grid
