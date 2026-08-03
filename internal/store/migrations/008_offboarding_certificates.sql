-- +goose Up

CREATE TABLE offboarding_certificates (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT '',
    trigger_source TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP NOT NULL,
    provider_count INTEGER NOT NULL DEFAULT 0,
    decision_count INTEGER NOT NULL DEFAULT 0,
    unknown_count INTEGER NOT NULL DEFAULT 0,
    payload_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_offboarding_certificates_subject_started
    ON offboarding_certificates(subject, started_at DESC);
CREATE INDEX idx_offboarding_certificates_status
    ON offboarding_certificates(status);
CREATE INDEX idx_offboarding_certificates_started
    ON offboarding_certificates(started_at DESC);

-- +goose Down
DROP TABLE IF EXISTS offboarding_certificates;
