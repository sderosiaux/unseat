-- +goose Up
ALTER TABLE provider_users ADD COLUMN last_activity_at TIMESTAMP;

-- +goose Down
-- SQLite doesn't support DROP COLUMN before 3.35.0; acceptable for personal tooling
