ALTER TABLE workspaces
    ADD COLUMN policy text NOT NULL DEFAULT '',
    ADD COLUMN language text;
