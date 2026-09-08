## MODIFIED Requirements

### Requirement: Tools pane (API-backed)
The Tools pane SHALL list every tool from the workspace tools endpoint as a flat list — one row per tool with its display name, one-line description, and an enable toggle reflecting (and driving) the workspace-wide tool status; toggling requires the settings-management permission and SHALL surface guard rejections as toasts. Configurable tools SHALL additionally render a gear button opening a structured config dialog rendered from the tool's config-field schema: one labeled control per field — `secret` fields as password-style write-only inputs (existing values shown only as hints, replaced on save), `text` as inputs, `number` as numeric inputs, `boolean` as toggles, `enum` as selects fed by the schema's options — with help text and a Save action. For `web.search` the dialog SHALL additionally render a provider-stack list editor: one row per configured entry with a reorder control pair (disabled at the list bounds), a name input, a provider select fed by the registry options, the credential field that provider requires (a password-style write-only key input showing the stored last-4 hint, or a base-URL input for SearXNG), and a remove action; an Add control appends a new row, and a timeout field bounds the per-attempt request timeout. Rows in the first three positions SHALL be labeled "in rotation"; rows below SHALL render dimmed and labeled "standby", and reordering SHALL re-evaluate the labels immediately. Removing a row SHALL not persist until Save; saving SHALL submit the whole ordered list with entry ids, and a row whose credential field is left empty SHALL keep its stored credential (reorder-safe via the stable entry id). Validation failures SHALL render inline per row (empty name, missing credential for a key-requiring provider, duplicate names) and block Save. The pane SHALL NOT offer raw JSON editing for any structured value.

#### Scenario: Flat list with toggles
- **WHEN** a Member opens the Tools pane
- **THEN** every catalog tool appears as a row — display name, description, toggle — with no group headings, driven by the workspace tools endpoint

#### Scenario: Gear opens config dialog
- **WHEN** the user activates the gear on "Web Search"
- **THEN** the dialog opens as a provider-stack list editor — per-row name, provider select, credential, reorder, remove, an Add control, and the per-attempt timeout field

#### Scenario: Rotation labels track order
- **WHEN** the user moves a standby row into the top three (or a rotation row out)
- **THEN** the in-rotation/standby labels and dimming re-evaluate immediately, before Save

#### Scenario: Reorder keeps credentials
- **WHEN** the user swaps two configured rows and saves without retyping either key
- **THEN** each row keeps its own stored credential, matched by entry id rather than position

#### Scenario: Browser config dialog
- **WHEN** the user opens the Browser config dialog
- **THEN** it exposes headless toggle, remote CDP URL, max pages, idle timeout, and action timeout, one labeled control per property, with help text explaining that a set CDP URL makes headless irrelevant

#### Scenario: Disabled until configured
- **WHEN** no search entry is configured for the workspace
- **THEN** the Web Search row shows its toggle as unavailable with an explanatory hint, and saving one valid entry enables the toggle

#### Scenario: Secret never echoed
- **WHEN** the user reopens the Web Search dialog after saving an API key
- **THEN** each row's key field is empty with the stored hint displayed beside it, and saving without typing preserves the stored keys

#### Scenario: Toggle rejection surfaces as toast
- **WHEN** a Member attempts to toggle a tool
- **THEN** the request is refused with a 403 and the pane surfaces a toast

#### Scenario: Invalid config reported inline
- **WHEN** the user saves with a row missing its name, a row missing a key for a key-requiring provider, or two rows with the same name
- **THEN** the dialog shows the validation error on the offending rows and does not close
