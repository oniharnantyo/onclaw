# workspace-skills-ui Delta: web-app/chat

## ADDED Requirements

### Requirement: Skill invocation menu
In any composer, typing `$` SHALL open a skill invocation menu following the established `/` and `@` menu mechanics: entries list each available skill's name and description, grouped System / Workspace / This agent, filtered by the token typed after `$`, navigable by arrow keys, with Enter (or click) replacing the token with `$name ` and returning focus to the input. Skills whose tier is unavailable in the current surface (a disabled workspace skill, or another agent's skills) SHALL NOT appear. An unmatched `$name` token sends as ordinary text.

#### Scenario: Menu opens and filters
- **WHEN** the user types `$web` in a direct chat with an agent
- **THEN** the menu shows the `web-research` entry (name and description) and arrow keys move selection

#### Scenario: Pick inserts the token
- **WHEN** the user picks `web-research` from the menu
- **THEN** the composer text contains `$web-research ` with focus back in the input, ready for the rest of the message

#### Scenario: Disabled skill absent from the menu
- **WHEN** a workspace skill's master switch is off
- **THEN** no menu in that workspace lists it
