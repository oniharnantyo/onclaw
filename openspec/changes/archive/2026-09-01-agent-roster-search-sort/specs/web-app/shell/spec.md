## ADDED Requirements

### Requirement: Agent rows in navigation
Sidebar agent rows SHALL render the agent's avatar configuration when set (initials tile when empty), matching the roster cards, and SHALL list agents newest-created first (the store order). The sidebar ⌘K search SHALL match agent name and role — a deliberately broader scope than the roster's name-only search, so navigation surfaces agents by what they do while the roster narrows by identity.

#### Scenario: Avatar in sidebar rows
- **when** an agent has an avatar configuration
- **then** its sidebar row renders that avatar instead of the initials tile; an agent with an empty avatar keeps the initials fallback

#### Scenario: Newest-first sidebar order
- **when** agents "A", "B", "C" are created in that order
- **then** the sidebar lists them C, B, A

#### Scenario: Sidebar search scope unchanged
- **when** the user types a role word into the ⌘K search
- **then** matching agent rows appear in the sidebar; the roster's name-only search is unaffected and vice versa
