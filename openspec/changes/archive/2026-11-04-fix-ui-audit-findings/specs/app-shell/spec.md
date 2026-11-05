# Spec Delta

## ADDED Requirements

### Requirement: Renderers load on demand

Heavy message renderers — the syntax highlighter, the diagram (mermaid)
renderer, and the document-preview markdown stack — SHALL stay outside the
application's entry bundle: a fresh app boot SHALL NOT parse them eagerly,
and each loads on first use. Warming a renderer after first paint is
permitted as long as it is not part of the entry bundle and boot does not
wait on it. The diagram renderer in particular SHALL NOT load until a
message actually carries a diagram.

#### Scenario: Boot skips renderer bundles

- **WHEN** the app boots to an empty transcript
- **THEN** the entry bundle contains neither the syntax highlighter nor the
  diagram renderer, and neither chunk is fetched during boot

#### Scenario: First diagram loads its renderer

- **WHEN** a completed message contains a mermaid fence
- **THEN** the diagram renderer loads on demand and renders the diagram;
  sessions without diagrams never load it

#### Scenario: First code block loads its highlighter

- **WHEN** the transcript first renders a code block
- **THEN** highlighting comes from an on-demand chunk (a post-paint warm is
  permitted), not from the entry bundle
