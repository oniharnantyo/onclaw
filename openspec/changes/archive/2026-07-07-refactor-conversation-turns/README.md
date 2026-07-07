# refactor-conversation-turns

Refactor conversation_messages from one-message-per-row to one-turn-per-row (Reading A): each row stores the turn's message array as a JSON delta plus model, per-turn token usage, question/answer text, and response_id/previous_response_id. Replay concatenates turn deltas; the live send policy is all turns until the context threshold, then summary + last 3 turns.
