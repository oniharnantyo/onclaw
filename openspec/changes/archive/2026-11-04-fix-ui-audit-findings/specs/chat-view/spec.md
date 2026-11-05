# Spec Delta

## ADDED Requirements

### Requirement: Transcript live announcements

The transcript viewport SHALL be exposed to assistive technology as a live
log region so streamed assistant text and running tool/turn activity are
announced without requiring focus. While a session's messages are loading,
the transcript SHALL expose a busy state that assistive technology
announces, instead of carrying the state on a label attached to a generic
element.

#### Scenario: Streamed text reaches screen readers

- **WHEN** an assistant turn streams text into the transcript
- **THEN** the transcript's live log region exposes the growing message
  text so screen-reader users receive the streamed content without
  navigating

#### Scenario: Loading a session announces itself

- **WHEN** the user opens a session and its messages are still loading
- **THEN** the transcript exposes a busy state that assistive technology
  announces, and the state clears when the messages arrive

## MODIFIED Requirements

### Requirement: Streaming-safe markdown

Partial markdown rendered mid-stream MUST NOT produce transient artifacts
that misrepresent the text: an unclosed code fence MUST NOT swallow the
following text as code, and an unclosed emphasis marker MUST NOT style the
remainder of the message. The completed message SHALL render identically
to the same text rendered without streaming. Per-delta rendering work
SHALL stay bounded so streaming paint latency does not grow with session
or message length: text-delta store updates SHALL be coalesced to at most
one store commit per frame window before they reach the render path,
message-to-view conversion SHALL reuse each entry's converted result so a
delta does not re-map messages that did not change, and the per-token
markdown stabilization SHALL NOT rescan the whole rendered message for
every delta. These bounds MUST NOT change the completed-render invariant
above.

#### Scenario: Unclosed fence mid-stream

- **WHEN** a streaming message has opened a code fence that has not yet
  closed
- **THEN** only the fenced text so far renders as code and the message does
  not flash a block that later disappears

#### Scenario: Final render matches a static render

- **WHEN** a streamed message completes
- **THEN** its rendering is identical to rendering the same complete text in
  one pass

#### Scenario: Long-session streaming stays flat

- **WHEN** text deltas arrive for a turn in a session containing hundreds
  of messages
- **THEN** the per-delta work does not re-map the whole transcript, and
  paint latency per chunk stays bounded as the session grows

#### Scenario: Long answer keeps painting

- **WHEN** a single streamed answer grows to thousands of characters of
  markdown
- **THEN** continued deltas keep painting without per-token cost that
  compounds with the message length
