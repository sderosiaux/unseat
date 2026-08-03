-- +goose Up

ALTER TABLE decisions ADD COLUMN recommendation_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN rejected_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE decision_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    decision_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    from_status TEXT NOT NULL DEFAULT '',
    to_status TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    payload_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY(decision_id) REFERENCES decisions(id) ON DELETE CASCADE
);

CREATE INDEX idx_decision_events_decision ON decision_events(decision_id, occurred_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS decision_events;
