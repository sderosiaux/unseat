-- +goose Up

CREATE TABLE provider_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    provider_id TEXT NOT NULL DEFAULT '',
    synced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, email)
);

CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    provider TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '',
    trigger_source TEXT NOT NULL DEFAULT 'system',
    occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_events_provider ON events(provider);
CREATE INDEX idx_events_type ON events(type);
CREATE INDEX idx_events_occurred_at ON events(occurred_at);

CREATE TABLE pending_removals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    email TEXT NOT NULL,
    detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    cancelled BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE(provider, email)
);

CREATE TABLE sync_state (
    provider TEXT PRIMARY KEY,
    last_synced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'ok'
);

-- +goose Down
DROP TABLE IF EXISTS sync_state;
DROP TABLE IF EXISTS pending_removals;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS provider_users;
