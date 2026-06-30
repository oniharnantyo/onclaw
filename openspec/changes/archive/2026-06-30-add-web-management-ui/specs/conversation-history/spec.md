## ADDED Requirements

### Requirement: Conversations can be enumerated

The system SHALL provide `ConversationStore.ListConversations(ctx) ([]*ConversationRow, error)` returning each conversation's id, agent name, created and updated timestamps, and message count. The store interface, the `ConversationRow` DTO, and the SQLite implementation SHALL follow the existing contract/types/implementation separation. This enumeration supports the web UI's conversation list and any future listing surface.

#### Scenario: conversations are listed with counts

- **WHEN** a conversation with several messages exists and `ListConversations` is called
- **THEN** the result includes that conversation's id, agent name, timestamps, and the correct message count

#### Scenario: an empty store lists nothing

- **WHEN** `ListConversations` is called and no conversations exist
- **THEN** it returns an empty (or nil) slice and no error