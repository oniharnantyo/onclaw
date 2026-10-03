# Spec Delta — web-app/chat-runtime

## MODIFIED Requirements

### Requirement: Transcript hydration
Opening an agent chat whose session is known to the server SHALL load the server-side transcript for that session and render it in the thread, so transcripts converge across browsers and reloads. Session provenance SHALL NOT be inferred from id shape: sessions created by any client (web composer, scheduler, external API callers naming their own sessions through `metadata.onclaw_session`) SHALL hydrate identically. A session the server does not know (unknown id, deleted session) SHALL hydrate nothing and leave the local thread untouched. Hydrated history SHALL render through the same message components as local history — including markdown, reasoning sections, and tool-card fidelity fields — and SHALL NOT duplicate or displace messages already present locally; in-flight optimistic user messages and streamed-turn tails SHALL survive the replace exactly as they do today.

#### Scenario: External session transcript renders
- **WHEN** the user opens a chat whose session id was minted by an external API client (neither `sess_`- nor `sched_`-prefixed) and the server holds committed events for it
- **THEN** the thread renders the server-side transcript of that session

#### Scenario: Second browser sees history
- **WHEN** the user opens a bound chat from a browser that has never seen it
- **THEN** the thread renders the server-side transcript of the bound session

#### Scenario: Unknown session leaves local thread intact
- **WHEN** a chat carries a session id the server does not know
- **THEN** the local thread renders unchanged and no error surfaces

#### Scenario: Local messages not duplicated
- **WHEN** a chat holds local messages that also exist in the hydrated server transcript
- **THEN** the thread renders them once

## ADDED Requirements

### Requirement: Windowed initial hydration
Hydrating a session SHALL load only the most recent page of events (a bounded window), not the entire event log. The live catch-up stream SHALL attach from the newest loaded event's cursor exactly as it does under full hydration.

#### Scenario: Long session opens on the recent window
- **WHEN** a session with more events than one page is opened
- **THEN** the transcript renders the most recent page and the client holds a cursor to fetch older pages

#### Scenario: Live tail still streams after windowed hydration
- **WHEN** a run is active on a session opened with a windowed hydration
- **THEN** the catch-up stream attaches from the newest loaded event and streams the remainder

### Requirement: Scroll-up history paging
Scrolling to the top of a windowed transcript SHALL fetch the next older page of events using the oldest loaded event as a backward cursor and SHALL prepend it to the thread with the viewport anchored so the reading position does not jump. Paging SHALL stop at the true beginning of the log: when no older events exist, no further fetches fire and no empty-state spinner persists. Only one older-page fetch SHALL be in flight at a time.

#### Scenario: Scrolling up loads older history
- **WHEN** the user scrolls a windowed transcript to its top and older events exist
- **THEN** the next older page is fetched and prepended without displacing the visible messages

#### Scenario: Start of log terminates paging
- **WHEN** the user pages back through a transcript whose oldest events are already loaded
- **THEN** further scroll-up attempts issue no fetch and the transcript renders its full history statically

#### Scenario: Page boundary mid-turn composes
- **WHEN** a backward page boundary falls inside one turn's events
- **THEN** the turn renders as one composed turn across the joined pages, matching a full-load render
