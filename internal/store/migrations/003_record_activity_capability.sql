-- +goose Up
-- Whether the provider that produced these rows reports genuine activity data.
--
-- It cannot be recomputed later. For most connectors the capability is static,
-- but GitHub only learns it by calling the org audit log: a freshly constructed
-- provider reports false. Recomputing therefore made `scan` and `audit inactive`
-- disagree about the same provider in the same session — one had asked the API,
-- the other had not. The cache has to carry the provenance of its own data.
ALTER TABLE sync_state ADD COLUMN reports_activity BOOLEAN NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sync_state DROP COLUMN reports_activity;
