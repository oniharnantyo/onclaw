# chat-tool-cards Specification (delta)

> Note: this delta MODIFIES the `### Requirement: Browser screenshot access`
> of the archived `chat-tool-cards` main spec. The screenshots directory the
> bound read serves from moved with the browser tool's write location
> (add-browser-login-handover follow-up): captures now land inside the
> calling session's workspace so the model can read them back with
> `read_file`, and the bound read follows.

## MODIFIED Requirements

### Requirement: Browser screenshot access

The webview SHALL obtain a browser screenshot's bytes only through a bound read addressed by the file name the browser tool wrote plus the session the card belongs to. The read MUST accept only names of that exact shape, MUST resolve them solely inside the calling session's workspace screenshots directory (`sessions/<session id>/browser/` under the calling agent's workspace — the same directory the browser tool writes through its per-call `_meta`), and MUST refuse any name carrying directory components or not matching the shape before touching the filesystem. A session that resolves to no workspace or no screenshots directory MUST yield a wiring or lookup error, never a fallback read outside the session's workspace. The on-disk path MUST NOT be exposed to the webview. A screenshot SHALL render in the card at the dimensions the tool reported so the transcript does not shift when the image arrives.

#### Scenario: Valid screenshot name served

- **WHEN** a card requests the screenshot named in its result
- **THEN** the image bytes are returned from the calling session's workspace screenshots directory and the thumbnail renders at the reported dimensions

#### Scenario: Traversal refused

- **WHEN** a read names a path with directory components or a name outside the tool's naming shape
- **THEN** the read is refused and no file content is returned

#### Scenario: Missing screenshot

- **WHEN** the screenshot file no longer exists on disk
- **THEN** the card shows an unavailable state in place of the thumbnail and the rest of the card renders normally

#### Scenario: Another session's screenshot is never served

- **WHEN** a card's read names a screenshot that exists only in a different session's screenshots directory
- **THEN** the read resolves only inside the calling session's directory and reports not found
