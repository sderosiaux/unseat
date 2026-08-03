-- +goose Up

CREATE TABLE decisions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    object_kind TEXT NOT NULL DEFAULT '',
    object_id TEXT NOT NULL DEFAULT '',
    action_class TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    risk TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    policy_version TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    approved_by TEXT NOT NULL DEFAULT '',
    rejected_by TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_decisions_status ON decisions(status);
CREATE INDEX idx_decisions_subject ON decisions(subject);
CREATE INDEX idx_decisions_provider ON decisions(provider);
CREATE INDEX idx_decisions_updated ON decisions(updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS decisions;
