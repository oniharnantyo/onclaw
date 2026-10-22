-- Schema v10: provider model rows (settings-master-detail design D2). One row per
-- (provider, model) combination: store-assigned autoincrement id, provider_id FK
-- with ON DELETE CASCADE, model_id, context_window, image_input, pdf_input,
-- enabled (default on), and timestamps. Enforces UNIQUE(provider_id, model_id).
-- Seeding runs in Go, not SQL (design D3).

CREATE TABLE provider_models (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id    TEXT NOT NULL,
    model_id       TEXT NOT NULL,
    context_window INTEGER NOT NULL DEFAULT 0,
    image_input    INTEGER NOT NULL DEFAULT 0,
    pdf_input      INTEGER NOT NULL DEFAULT 0,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    FOREIGN KEY (provider_id) REFERENCES providers (id) ON DELETE CASCADE,
    CONSTRAINT uq_provider_models_provider_model UNIQUE (provider_id, model_id)
);
