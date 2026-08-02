-- +goose Up
-- +goose StatementBegin

CREATE TABLE billing_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    plan TEXT NOT NULL DEFAULT '',
    billed_seats INTEGER,
    filled_seats INTEGER,
    monthly_amount_minor INTEGER,
    cost_per_seat_minor INTEGER,
    currency TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    confidence TEXT NOT NULL DEFAULT '',
    unavailable_reason TEXT NOT NULL DEFAULT '',
    period_start TIMESTAMP,
    period_end TIMESTAMP,
    next_billing_at TIMESTAMP
);

CREATE INDEX idx_billing_snapshots_provider_fetched
    ON billing_snapshots(provider, fetched_at DESC, id DESC);

CREATE TABLE billing_line_items (
    snapshot_id INTEGER NOT NULL REFERENCES billing_snapshots(id) ON DELETE CASCADE,
    line_order INTEGER NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    quantity INTEGER,
    amount_minor INTEGER,
    currency TEXT NOT NULL DEFAULT '',
    period_start TIMESTAMP,
    period_end TIMESTAMP,
    PRIMARY KEY(snapshot_id, line_order)
);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS billing_line_items;
DROP TABLE IF EXISTS billing_snapshots;
