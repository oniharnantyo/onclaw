-- Reverse of 000047_gateway_tables: drop the gateway tables. No FKs exist
-- between them, so order is cosmetic.

DROP TABLE IF EXISTS gateway_outbox;
DROP TABLE IF EXISTS gateway_chat_bindings;
DROP TABLE IF EXISTS gateway_pairing_tokens;
DROP TABLE IF EXISTS gateway_user_links;
DROP TABLE IF EXISTS workspace_gateways;
