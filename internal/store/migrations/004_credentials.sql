-- +goose Up

CREATE TABLE provider_credentials (
    provider TEXT NOT NULL,
    kind TEXT NOT NULL,
    credential_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP,
    created_by TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMP,
    scopes_json TEXT NOT NULL DEFAULT '[]',
    privileged_scopes_json TEXT NOT NULL DEFAULT '[]',
    reach TEXT NOT NULL DEFAULT '',
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    disabled_at TIMESTAMP,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    class TEXT NOT NULL DEFAULT 'unowned',
    reason TEXT NOT NULL DEFAULT '',
    overreaching BOOLEAN NOT NULL DEFAULT FALSE,
    reach_reason TEXT NOT NULL DEFAULT '',
    synced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(provider, kind, credential_id, label)
);

CREATE INDEX idx_provider_credentials_provider ON provider_credentials(provider);
CREATE INDEX idx_provider_credentials_class ON provider_credentials(class);
CREATE INDEX idx_provider_credentials_overreaching ON provider_credentials(overreaching);

CREATE TABLE credential_sync_state (
    provider TEXT PRIMARY KEY,
    last_synced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    credential_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'ok',
    usage_known BOOLEAN NOT NULL DEFAULT FALSE,
    message TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS credential_sync_state;
DROP TABLE IF EXISTS provider_credentials;
