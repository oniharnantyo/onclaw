## ADDED Requirements

### Requirement: Multimodal input parts
The Responses endpoint SHALL accept image and file content parts in message input items, following the OpenResponses content-type schemas: `input_image` with an `image_url` (absolute URL or `data:` URL) and optional `detail`; `input_file` with a `file_url` and `filename`. A `file_url` MAY be an onclaw capability URL (resolved locally, never fetched over HTTP from itself) or an inline `data:` URL; `file_data` SHALL be tolerated as an alias for the inline file form. A `file_id` part SHALL be rejected with `invalid_param`. Remote third-party URLs SHALL be rejected `invalid_param` in v1 (fetching client-supplied remote URLs is out of scope). String-only and text-part-only inputs remain valid unchanged.

#### Scenario: Turn with image and text
- **WHEN** a request's user message contains an `input_text` part and an `input_image` part whose `image_url` is an onclaw capability URL
- **THEN** the turn executes with the image visible to the model, and the response streams exactly as a text-only turn does

#### Scenario: Inline data URL demoted to storage
- **WHEN** a request carries `input_image` with a `data:image/png;base64,…` URL
- **THEN** the server stores the bytes as an attachment in the key's workspace, replaces the inline form with a reference before persisting session events, and the turn proceeds identically to an uploaded attachment

#### Scenario: Text-like file inlined as fenced text
- **WHEN** a request carries `input_file` referencing a small text-like attachment
- **THEN** the turn message carries the file's content as a fenced text part rather than a file block

#### Scenario: OpenAI-style file_id rejected
- **WHEN** a request contains `input_file` with `file_id` only
- **THEN** the endpoint responds `400` `invalid_param` naming the unsupported `file_id` field

#### Scenario: Remote file URL rejected in v1
- **WHEN** a request contains `input_file` whose `file_url` is `https://example.com/doc.pdf`
- **THEN** the endpoint responds `400` `invalid_param` explaining that remote URLs are not accepted

#### Scenario: String-only input unchanged
- **WHEN** a request's input is a plain string or text-part array, as before this change
- **THEN** the turn executes exactly as it did before — no behavioral change for existing callers

### Requirement: Attachment resolution is workspace-scoped
Attachment references in input parts SHALL resolve against the API key's workspace before the turn executes; an unknown or foreign reference SHALL fail the request with `invalid_param` before any run starts.

#### Scenario: Unknown attachment id in URL path
- **WHEN** an input part references a capability URL whose key does not resolve to a stored attachment in the key's workspace
- **THEN** the request fails `400` `invalid_param` and no run is created

### Requirement: Compact turns reject attachments
Compact-command turns SHALL accept text input only; a compact request carrying image or file parts SHALL fail with `invalid_param` before resolution, consistent with compact's bind-only strictness.

#### Scenario: Compact with attachment rejected
- **WHEN** an `onclaw_command: "compact"` request carries an `input_image` part
- **THEN** the request fails `400` `invalid_param` stating compaction accepts text only, and the session is untouched
