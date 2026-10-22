-- Schema v6: agents gain a stored procedural-avatar config (the create/edit
-- wizard's AvatarPicker output, onclaw-v2 parity). The column holds the
-- react-nice-avatar config as a JSON object persisted verbatim from the
-- client (opaque JSON, the Hook.Config pattern); '' means "no stored avatar
-- — derive the face from the agent id" (the pre-v6 behavior).

ALTER TABLE agents ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
