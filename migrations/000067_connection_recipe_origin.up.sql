-- Connection recipe origin (add-recipe-base-url tasks.md 2.1/2.2, design.md
-- D2/D3): the resolved base-URL origin a parametrized recipe's connection was
-- connected against.
--
-- origin is the ParseOrigin-normalized scheme://host[:port] the recipe's
-- endpoint/verb paths resolve against at materialization (domain.
-- ResolveRecipeBase). NOT NULL with an empty default rather than nullable so
-- the value maps one-to-one onto the Go field's empty-means-unset rule and the
-- uniqueness key below is total — no NULL-distinctness exceptions. Existing
-- rows (and every non-parametrized connection) hold ''; the column is inert
-- for them.
--
-- Uniqueness narrows from one-connection-per-service (000062's
-- uq_workspace_connections_workspace_id_service) to per (workspace, service,
-- origin): a workspace may hold gitlab.com and gitlab.linkaja.com side by
-- side, while the same service on the same origin is still rejected. For
-- origin='' rows the composite index is byte-equivalent to the old constraint,
-- so plain per-service uniqueness is preserved exactly.

ALTER TABLE workspace_connections
    ADD COLUMN origin text NOT NULL DEFAULT '';

ALTER TABLE workspace_connections
    DROP CONSTRAINT uq_workspace_connections_workspace_id_service;

CREATE UNIQUE INDEX uq_workspace_connections_workspace_service_origin
    ON workspace_connections (workspace_id, service, origin);
