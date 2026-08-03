package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"
	"github.com/sderosiaux/unseat/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

// SQLite implements Store backed by a SQLite database.
type SQLite struct {
	db *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at dsn and runs migrations.
func NewSQLite(dsn string) (*SQLite, error) {
	db, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	goose.SetBaseFS(migrations)
	// Migrations run on every command. Their progress on stdout corrupted
	// --json output, so it is silenced; failures still surface as errors.
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("goose up: %w", err)
	}

	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

// --- Provider Users ---

func (s *SQLite) UpsertProviderUsers(ctx context.Context, provider string, users []core.User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_users WHERE provider = ?`, provider); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO provider_users (provider, email, display_name, role, status, provider_id, synced_at, last_activity_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	for _, u := range users {
		var lastActivity *time.Time
		if u.LastActivityAt != nil {
			t := u.LastActivityAt.UTC()
			lastActivity = &t
		}
		if _, err := stmt.ExecContext(ctx, provider, u.Email, u.DisplayName, u.Role, u.Status, u.ProviderID, now, lastActivity); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) GetProviderUsers(ctx context.Context, provider string) ([]core.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT email, display_name, role, status, provider_id, last_activity_at FROM provider_users WHERE provider = ? ORDER BY email`, provider)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []core.User
	for rows.Next() {
		var u core.User
		var lastActivity sql.NullTime
		if err := rows.Scan(&u.Email, &u.DisplayName, &u.Role, &u.Status, &u.ProviderID, &lastActivity); err != nil {
			return nil, err
		}
		if lastActivity.Valid {
			t := lastActivity.Time
			u.LastActivityAt = &t
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetInactiveUsers returns users with no activity since the given time,
// restricted to providers whose API actually reports activity.
//
// Restricting by provider is not an optimisation: a NULL last_activity_at
// means "never seen active" for an instrumented provider, but "unknown" for
// every other one. Querying across both makes the result meaningless.
// An empty providers slice returns no rows, which is the honest answer when
// nothing is instrumented.
func (s *SQLite) GetInactiveUsers(ctx context.Context, since time.Time, providers []string) ([]InactiveUser, error) {
	if len(providers) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(providers)+1)
	placeholders := make([]string, len(providers))
	for i, p := range providers {
		placeholders[i] = "?"
		args = append(args, p)
	}
	args = append(args, since.UTC())

	// Deactivated seats are excluded. They are trivially unused, so counting
	// them as inactivity double-reports the same seat — once here and once as
	// a suspended account — and buries the live, billed, idle seats that are
	// the only actionable ones. core.Scan makes the same exclusion for the
	// same reason.
	query := `SELECT provider, email, display_name, last_activity_at, status FROM provider_users
		 WHERE provider IN (` + strings.Join(placeholders, ",") + `)
		   AND status <> 'suspended'
		   AND (last_activity_at IS NULL OR last_activity_at < ?)
		 ORDER BY last_activity_at ASC, provider, email`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []InactiveUser
	for rows.Next() {
		var u InactiveUser
		var lastActivity sql.NullTime
		if err := rows.Scan(&u.Provider, &u.Email, &u.DisplayName, &lastActivity, &u.Status); err != nil {
			return nil, err
		}
		if lastActivity.Valid {
			t := lastActivity.Time
			u.LastActivityAt = &t
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// --- Events ---

func (s *SQLite) InsertEvent(ctx context.Context, event core.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (type, provider, email, details, trigger_source, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		string(event.Type), event.Provider, event.Email, event.Details, event.Trigger, event.OccurredAt.UTC())
	return err
}

func (s *SQLite) ListEvents(ctx context.Context, filter EventFilter) ([]core.Event, error) {
	query := `SELECT type, provider, email, details, trigger_source, occurred_at FROM events WHERE 1=1`
	var args []any

	if filter.Provider != nil {
		query += ` AND provider = ?`
		args = append(args, *filter.Provider)
	}
	if filter.Type != nil {
		query += ` AND type = ?`
		args = append(args, string(*filter.Type))
	}
	if filter.Since != nil {
		query += ` AND occurred_at >= ?`
		args = append(args, filter.Since.UTC())
	}

	query += ` ORDER BY occurred_at DESC`

	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		if err := rows.Scan(&e.Type, &e.Provider, &e.Email, &e.Details, &e.Trigger, &e.OccurredAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Pending Removals ---

// InsertPendingRemoval records a seat as due for removal once the grace period
// elapses.
//
// An existing, still-active countdown keeps its original deadline. Refreshing
// expires_at on every run made the grace period unreachable: with an hourly
// sync and a 72h grace, the deadline moved forward faster than the clock.
// A previously cancelled row does restart, because the user left again.
func (s *SQLite) InsertPendingRemoval(ctx context.Context, provider, email string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_removals (provider, email, expires_at) VALUES (?, ?, ?)
		 ON CONFLICT(provider, email) DO UPDATE SET
		   expires_at = CASE WHEN pending_removals.cancelled THEN excluded.expires_at
		                     ELSE pending_removals.expires_at END,
		   cancelled = FALSE`,
		provider, email, expiresAt.UTC())
	return err
}

func (s *SQLite) GetPendingRemovals(ctx context.Context, provider string) ([]PendingRemoval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider, email, detected_at, expires_at, cancelled FROM pending_removals
		 WHERE provider = ? AND cancelled = FALSE ORDER BY detected_at`, provider)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var removals []PendingRemoval
	for rows.Next() {
		var r PendingRemoval
		if err := rows.Scan(&r.Provider, &r.Email, &r.DetectedAt, &r.ExpiresAt, &r.Cancelled); err != nil {
			return nil, err
		}
		removals = append(removals, r)
	}
	return removals, rows.Err()
}

func (s *SQLite) CancelPendingRemoval(ctx context.Context, provider, email string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE pending_removals SET cancelled = TRUE WHERE provider = ? AND email = ?`, provider, email)
	return err
}

func (s *SQLite) GetExpiredRemovals(ctx context.Context, provider string) ([]PendingRemoval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider, email, detected_at, expires_at, cancelled FROM pending_removals
		 WHERE provider = ? AND cancelled = FALSE AND expires_at <= ? ORDER BY expires_at`,
		provider, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var removals []PendingRemoval
	for rows.Next() {
		var r PendingRemoval
		if err := rows.Scan(&r.Provider, &r.Email, &r.DetectedAt, &r.ExpiresAt, &r.Cancelled); err != nil {
			return nil, err
		}
		removals = append(removals, r)
	}
	return removals, rows.Err()
}

// --- Sync State ---

// UpdateSyncState records what was read and whether the provider that produced
// it reports activity.
//
// The capability is stored, not recomputed. GitHub only learns it by calling
// the org audit log, so a freshly constructed provider answers false — which
// made `scan` and `audit inactive` contradict each other about the same
// provider in the same session.
func (s *SQLite) UpdateSyncState(ctx context.Context, provider string, userCount int, reportsActivity bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (provider, last_synced_at, user_count, status, reports_activity)
		 VALUES (?, ?, ?, 'ok', ?)
		 ON CONFLICT(provider) DO UPDATE SET last_synced_at = excluded.last_synced_at,
		   user_count = excluded.user_count, status = excluded.status,
		   reports_activity = excluded.reports_activity`,
		provider, time.Now().UTC(), userCount, reportsActivity)
	return err
}

// ActivityReportingProviders returns the providers whose last read demonstrably
// reported activity, split from those that did not.
//
// Answered from what was actually observed rather than from a capability
// recomputed on a provider that has made no request.
func (s *SQLite) ActivityReportingProviders(ctx context.Context) (reporting, silent []string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider, reports_activity FROM sync_state ORDER BY provider`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		var reports bool
		if err := rows.Scan(&name, &reports); err != nil {
			return nil, nil, err
		}
		if reports {
			reporting = append(reporting, name)
		} else {
			silent = append(silent, name)
		}
	}
	return reporting, silent, rows.Err()
}

func (s *SQLite) GetSyncState(ctx context.Context, provider string) (*SyncState, error) {
	var st SyncState
	err := s.db.QueryRowContext(ctx,
		`SELECT provider, last_synced_at, user_count, status FROM sync_state WHERE provider = ?`, provider).
		Scan(&st.Provider, &st.LastSyncedAt, &st.UserCount, &st.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *SQLite) ListSyncStates(ctx context.Context) ([]SyncState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider, last_synced_at, user_count, status FROM sync_state ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var states []SyncState
	for rows.Next() {
		var st SyncState
		if err := rows.Scan(&st.Provider, &st.LastSyncedAt, &st.UserCount, &st.Status); err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	return states, rows.Err()
}

// --- Billing Snapshots ---

func (s *SQLite) InsertBillingSnapshot(ctx context.Context, snapshot core.BillingSnapshot) error {
	if snapshot.Provider == "" {
		return fmt.Errorf("billing snapshot provider is required")
	}
	fetchedAt := snapshot.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	} else {
		fetchedAt = fetchedAt.UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `INSERT INTO billing_snapshots (
		provider, account_id, fetched_at, plan, billed_seats, filled_seats,
		monthly_amount_minor, cost_per_seat_minor, currency, source, confidence,
		unavailable_reason, period_start, period_end, next_billing_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.Provider,
		snapshot.AccountID,
		fetchedAt,
		snapshot.Plan,
		nullableInt(snapshot.BilledSeats),
		nullableInt(snapshot.FilledSeats),
		nullableInt64(snapshot.MonthlyAmountMinor),
		nullableInt64(snapshot.CostPerSeatMinor),
		snapshot.Currency,
		string(snapshot.Source),
		string(snapshot.Confidence),
		snapshot.UnavailableReason,
		utcPtr(snapshot.PeriodStart),
		utcPtr(snapshot.PeriodEnd),
		utcPtr(snapshot.NextBillingAt),
	)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if len(snapshot.LineItems) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO billing_line_items (
		snapshot_id, line_order, external_id, description, quantity, amount_minor,
		currency, period_start, period_end
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for i, line := range snapshot.LineItems {
		if _, err := stmt.ExecContext(ctx,
			id,
			i,
			line.ID,
			line.Description,
			nullableInt(line.Quantity),
			nullableInt64(line.AmountMinor),
			line.Currency,
			utcPtr(line.PeriodStart),
			utcPtr(line.PeriodEnd),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLite) LatestBillingSnapshot(ctx context.Context, provider string) (*core.BillingSnapshot, error) {
	row := s.db.QueryRowContext(ctx, latestBillingSnapshotQuery(`WHERE provider = ?`), provider)
	snapshot, id, err := scanBillingSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines, err := s.billingLineItems(ctx, id)
	if err != nil {
		return nil, err
	}
	snapshot.LineItems = lines
	return &snapshot, nil
}

func (s *SQLite) ListLatestBillingSnapshots(ctx context.Context) ([]core.BillingSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider, account_id, fetched_at, plan,
		billed_seats, filled_seats, monthly_amount_minor, cost_per_seat_minor, currency,
		source, confidence, unavailable_reason, period_start, period_end, next_billing_at
		FROM billing_snapshots AS bs
		WHERE bs.id = (
			SELECT latest.id FROM billing_snapshots AS latest
			WHERE latest.provider = bs.provider
			ORDER BY latest.fetched_at DESC, latest.id DESC
			LIMIT 1
		)
		ORDER BY provider`)
	if err != nil {
		return nil, err
	}

	var snapshots []core.BillingSnapshot
	var ids []int64
	for rows.Next() {
		snapshot, id, err := scanBillingSnapshot(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for i, id := range ids {
		lines, err := s.billingLineItems(ctx, id)
		if err != nil {
			return nil, err
		}
		snapshots[i].LineItems = lines
	}
	return snapshots, nil
}

func latestBillingSnapshotQuery(where string) string {
	return `SELECT id, provider, account_id, fetched_at, plan,
		billed_seats, filled_seats, monthly_amount_minor, cost_per_seat_minor, currency,
		source, confidence, unavailable_reason, period_start, period_end, next_billing_at
		FROM billing_snapshots ` + where + `
		ORDER BY fetched_at DESC, id DESC
		LIMIT 1`
}

type billingSnapshotScanner interface {
	Scan(dest ...any) error
}

func scanBillingSnapshot(row billingSnapshotScanner) (core.BillingSnapshot, int64, error) {
	var snapshot core.BillingSnapshot
	var id int64
	var billedSeats, filledSeats, monthlyAmount, costPerSeat sql.NullInt64
	var source, confidence string
	var periodStart, periodEnd, nextBillingAt sql.NullTime
	err := row.Scan(
		&id,
		&snapshot.Provider,
		&snapshot.AccountID,
		&snapshot.FetchedAt,
		&snapshot.Plan,
		&billedSeats,
		&filledSeats,
		&monthlyAmount,
		&costPerSeat,
		&snapshot.Currency,
		&source,
		&confidence,
		&snapshot.UnavailableReason,
		&periodStart,
		&periodEnd,
		&nextBillingAt,
	)
	if err != nil {
		return core.BillingSnapshot{}, 0, err
	}
	snapshot.BilledSeats = nullIntPtr(billedSeats)
	snapshot.FilledSeats = nullIntPtr(filledSeats)
	snapshot.MonthlyAmountMinor = nullInt64Ptr(monthlyAmount)
	snapshot.CostPerSeatMinor = nullInt64Ptr(costPerSeat)
	snapshot.Source = core.BillingSource(source)
	snapshot.Confidence = core.BillingConfidence(confidence)
	if periodStart.Valid {
		t := periodStart.Time
		snapshot.PeriodStart = &t
	}
	if periodEnd.Valid {
		t := periodEnd.Time
		snapshot.PeriodEnd = &t
	}
	if nextBillingAt.Valid {
		t := nextBillingAt.Time
		snapshot.NextBillingAt = &t
	}
	return snapshot, id, nil
}

func (s *SQLite) billingLineItems(ctx context.Context, snapshotID int64) ([]core.BillingLine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT external_id, description, quantity,
		amount_minor, currency, period_start, period_end
		FROM billing_line_items
		WHERE snapshot_id = ?
		ORDER BY line_order`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var lines []core.BillingLine
	for rows.Next() {
		var line core.BillingLine
		var quantity, amount sql.NullInt64
		var periodStart, periodEnd sql.NullTime
		if err := rows.Scan(&line.ID, &line.Description, &quantity, &amount, &line.Currency, &periodStart, &periodEnd); err != nil {
			return nil, err
		}
		line.Quantity = nullIntPtr(quantity)
		line.AmountMinor = nullInt64Ptr(amount)
		if periodStart.Valid {
			t := periodStart.Time
			line.PeriodStart = &t
		}
		if periodEnd.Valid {
			t := periodEnd.Time
			line.PeriodEnd = &t
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// --- Decisions ---

func (s *SQLite) UpsertDecisions(ctx context.Context, decisions []core.Decision) error {
	if len(decisions) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	for _, d := range decisions {
		if d.ID == "" {
			return fmt.Errorf("decision id is required")
		}
		stored, fromStatus, eventType, err := mergeIncomingDecision(ctx, tx, d)
		if err != nil {
			return err
		}
		hash := decisionRecommendationHash(stored)
		if err := upsertDecisionTx(ctx, tx, stored, hash, now); err != nil {
			return err
		}
		if eventType != "" {
			if err := insertDecisionEventTx(ctx, tx, stored, eventType, fromStatus, stored.Status, "system", stored.Reason, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func mergeIncomingDecision(ctx context.Context, tx *sql.Tx, incoming core.Decision) (core.Decision, core.DecisionStatus, string, error) {
	existing, existingHash, found, err := getDecisionTx(ctx, tx, incoming.ID)
	if err != nil {
		return core.Decision{}, "", "", err
	}
	if !found {
		return incoming, "", "upserted", nil
	}

	incomingHash := decisionRecommendationHash(incoming)
	if existingHash == "" {
		existingHash = decisionRecommendationHash(existing)
	}

	switch existing.Status {
	case core.DecisionApproved, core.DecisionRejected, core.DecisionExecuted, core.DecisionVerified:
		if existingHash == incomingHash {
			return existing, "", "", nil
		}
		stale := incoming
		stale.Status = core.DecisionStale
		stale.ApprovedBy = existing.ApprovedBy
		stale.RejectedBy = existing.RejectedBy
		stale.RejectedReason = existing.RejectedReason
		if stale.Metadata == nil {
			stale.Metadata = map[string]string{}
		}
		stale.Metadata["previous_status"] = string(existing.Status)
		stale.Metadata["stale_reason"] = "recommendation changed after human decision"
		return stale, existing.Status, "stale", nil
	default:
		if existing.Status != incoming.Status {
			return incoming, existing.Status, "status_changed", nil
		}
		return incoming, "", "", nil
	}
}

func (s *SQLite) GetDecision(ctx context.Context, id string) (*core.Decision, error) {
	decision, _, found, err := getDecision(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return &decision, nil
}

type decisionQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getDecisionTx(ctx context.Context, tx *sql.Tx, id string) (core.Decision, string, bool, error) {
	return getDecision(ctx, tx, id)
}

func getDecision(ctx context.Context, q decisionQueryer, id string) (core.Decision, string, bool, error) {
	var payload, hash string
	err := q.QueryRowContext(ctx, `SELECT payload_json, recommendation_hash FROM decisions WHERE id = ?`, id).Scan(&payload, &hash)
	if err == sql.ErrNoRows {
		return core.Decision{}, "", false, nil
	}
	if err != nil {
		return core.Decision{}, "", false, err
	}
	decision, err := decodeDecision(payload)
	if err != nil {
		return core.Decision{}, "", false, err
	}
	return decision, hash, true, nil
}

func (s *SQLite) ListDecisions(ctx context.Context, filter DecisionFilter) ([]core.Decision, error) {
	query := `SELECT payload_json FROM decisions WHERE 1=1`
	var args []any
	if filter.Provider != nil {
		query += ` AND provider = ?`
		args = append(args, *filter.Provider)
	}
	if filter.Subject != nil {
		query += ` AND subject = ?`
		args = append(args, *filter.Subject)
	}
	if filter.Status != nil {
		query += ` AND status = ?`
		args = append(args, string(*filter.Status))
	}
	query += ` ORDER BY updated_at DESC, id`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var decisions []core.Decision
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		decision, err := decodeDecision(payload)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

func decodeDecision(payload string) (core.Decision, error) {
	var decision core.Decision
	if err := json.Unmarshal([]byte(payload), &decision); err != nil {
		return core.Decision{}, err
	}
	return decision, nil
}

func upsertDecisionTx(ctx context.Context, tx *sql.Tx, d core.Decision, hash string, updatedAt time.Time) error {
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO decisions (
		id, tenant_id, subject, provider, object_kind, object_id, action_class,
		status, risk, reason, policy_version, idempotency_key, approved_by,
		rejected_by, rejected_reason, recommendation_hash, payload_json, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		tenant_id = excluded.tenant_id,
		subject = excluded.subject,
		provider = excluded.provider,
		object_kind = excluded.object_kind,
		object_id = excluded.object_id,
		action_class = excluded.action_class,
		status = excluded.status,
		risk = excluded.risk,
		reason = excluded.reason,
		policy_version = excluded.policy_version,
		idempotency_key = excluded.idempotency_key,
		approved_by = excluded.approved_by,
		rejected_by = excluded.rejected_by,
		rejected_reason = excluded.rejected_reason,
		recommendation_hash = excluded.recommendation_hash,
		payload_json = excluded.payload_json,
		updated_at = excluded.updated_at`,
		d.ID,
		d.TenantID,
		d.Subject,
		d.Provider,
		string(d.ObjectKind),
		d.ObjectID,
		string(d.ActionClass),
		string(d.Status),
		string(d.Risk),
		d.Reason,
		d.PolicyVersion,
		d.IdempotencyKey,
		d.ApprovedBy,
		d.RejectedBy,
		d.RejectedReason,
		hash,
		string(payload),
		updatedAt.UTC(),
	)
	return err
}

func insertDecisionEventTx(ctx context.Context, tx *sql.Tx, d core.Decision, eventType string, from, to core.DecisionStatus, actor, reason string, occurredAt time.Time) error {
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO decision_events (
		decision_id, event_type, from_status, to_status, actor, reason, occurred_at, payload_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID,
		eventType,
		string(from),
		string(to),
		actor,
		reason,
		occurredAt.UTC(),
		string(payload),
	)
	return err
}

func decisionRecommendationHash(d core.Decision) string {
	return core.HashEvidencePayload(struct {
		Subject           string
		ObjectKind        core.ObjectKind
		ObjectID          string
		Provider          string
		ActionClass       core.ActionClass
		RecommendedAction string
		Risk              core.DecisionRisk
		Reason            string
		PolicyVersion     string
		RequiredEvidence  []string
		BlockedBy         []string
		Metadata          map[string]string
	}{
		Subject:           d.Subject,
		ObjectKind:        d.ObjectKind,
		ObjectID:          d.ObjectID,
		Provider:          d.Provider,
		ActionClass:       d.ActionClass,
		RecommendedAction: d.RecommendedAction,
		Risk:              d.Risk,
		Reason:            d.Reason,
		PolicyVersion:     d.PolicyVersion,
		RequiredEvidence:  d.RequiredEvidence,
		BlockedBy:         d.BlockedBy,
		Metadata:          d.Metadata,
	})
}

func (s *SQLite) ListDecisionEvents(ctx context.Context, decisionID string) ([]DecisionEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, decision_id, event_type, from_status,
		to_status, actor, reason, occurred_at, payload_json
		FROM decision_events
		WHERE decision_id = ?
		ORDER BY occurred_at DESC, id DESC`, decisionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []DecisionEvent
	for rows.Next() {
		var event DecisionEvent
		var from, to, payload string
		if err := rows.Scan(&event.ID, &event.DecisionID, &event.EventType, &from, &to, &event.Actor, &event.Reason, &event.OccurredAt, &payload); err != nil {
			return nil, err
		}
		event.FromStatus = core.DecisionStatus(from)
		event.ToStatus = core.DecisionStatus(to)
		if payload != "" && payload != "{}" {
			decision, err := decodeDecision(payload)
			if err != nil {
				return nil, err
			}
			event.Payload = decision
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLite) ApproveDecision(ctx context.Context, id, approver string) (*core.Decision, error) {
	if approver == "" {
		approver = "cli"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	decision, hash, found, err := getDecisionTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("decision %q not found", id)
	}
	if decision.Status != core.DecisionProposed {
		return nil, fmt.Errorf("decision %q is %s, not proposed", id, decision.Status)
	}
	from := decision.Status
	decision.Status = core.DecisionApproved
	decision.ApprovedBy = approver
	decision.RejectedBy = ""
	decision.RejectedReason = ""
	now := time.Now().UTC()
	if err := upsertDecisionTx(ctx, tx, decision, hash, now); err != nil {
		return nil, err
	}
	if err := insertDecisionEventTx(ctx, tx, decision, "approved", from, decision.Status, approver, "", now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &decision, nil
}

func (s *SQLite) RejectDecision(ctx context.Context, id, rejector, reason string) (*core.Decision, error) {
	if rejector == "" {
		rejector = "cli"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reject reason is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	decision, hash, found, err := getDecisionTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("decision %q not found", id)
	}
	switch decision.Status {
	case core.DecisionProposed, core.DecisionApproved:
	default:
		return nil, fmt.Errorf("decision %q is %s, not proposed or approved", id, decision.Status)
	}
	from := decision.Status
	decision.Status = core.DecisionRejected
	decision.RejectedBy = rejector
	decision.RejectedReason = reason
	decision.ApprovedBy = ""
	now := time.Now().UTC()
	if err := upsertDecisionTx(ctx, tx, decision, hash, now); err != nil {
		return nil, err
	}
	if err := insertDecisionEventTx(ctx, tx, decision, "rejected", from, decision.Status, rejector, reason, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &decision, nil
}

// --- Provider Credentials ---

func (s *SQLite) UpsertProviderCredentials(ctx context.Context, provider string, credentials []core.ClassifiedCredential) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_credentials WHERE provider = ?`, provider); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO provider_credentials (
		provider, kind, credential_id, label, created_at, created_by, last_used_at,
		scopes_json, privileged_scopes_json, reach, disabled, disabled_at, metadata_json,
		class, reason, overreaching, reach_reason, synced_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC()
	for _, classified := range credentials {
		cred := classified.Credential
		cred.Provider = provider
		scopes, err := jsonText(cred.Scopes, []string{})
		if err != nil {
			return err
		}
		privilegedScopes, err := jsonText(cred.PrivilegedScopes, []string{})
		if err != nil {
			return err
		}
		metadata, err := jsonText(cred.Metadata, map[string]string{})
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			cred.Provider,
			string(cred.Kind),
			cred.ID,
			cred.Label,
			utcPtr(cred.CreatedAt),
			cred.CreatedBy,
			utcPtr(cred.LastUsedAt),
			scopes,
			privilegedScopes,
			cred.Reach,
			cred.Disabled,
			utcPtr(cred.DisabledAt),
			metadata,
			string(classified.Class),
			classified.Reason,
			classified.Overreaching,
			classified.ReachReason,
			now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLite) GetProviderCredentials(ctx context.Context, provider string) ([]core.ClassifiedCredential, error) {
	return s.queryCredentials(ctx, `WHERE provider = ?`, provider)
}

func (s *SQLite) ListProviderCredentials(ctx context.Context) ([]core.ClassifiedCredential, error) {
	return s.queryCredentials(ctx, ``)
}

func (s *SQLite) queryCredentials(ctx context.Context, where string, args ...any) ([]core.ClassifiedCredential, error) {
	query := `SELECT provider, kind, credential_id, label, created_at, created_by, last_used_at,
		scopes_json, privileged_scopes_json, reach, disabled, disabled_at, metadata_json,
		class, reason, overreaching, reach_reason
		FROM provider_credentials ` + where + `
		ORDER BY provider,
			CASE class
				WHEN 'orphaned' THEN 0
				WHEN 'external' THEN 1
				WHEN 'dormant' THEN 2
				WHEN 'unowned' THEN 3
				WHEN 'live' THEN 4
				ELSE 5
			END,
			created_at IS NULL, created_at, label`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var credentials []core.ClassifiedCredential
	for rows.Next() {
		var classified core.ClassifiedCredential
		var kind, class string
		var createdAt, lastUsedAt, disabledAt sql.NullTime
		var scopesJSON, privilegedScopesJSON, metadataJSON string
		if err := rows.Scan(
			&classified.Credential.Provider,
			&kind,
			&classified.Credential.ID,
			&classified.Credential.Label,
			&createdAt,
			&classified.Credential.CreatedBy,
			&lastUsedAt,
			&scopesJSON,
			&privilegedScopesJSON,
			&classified.Credential.Reach,
			&classified.Credential.Disabled,
			&disabledAt,
			&metadataJSON,
			&class,
			&classified.Reason,
			&classified.Overreaching,
			&classified.ReachReason,
		); err != nil {
			return nil, err
		}
		classified.Credential.Kind = core.CredentialKind(kind)
		classified.Class = core.CredentialClass(class)
		if createdAt.Valid {
			t := createdAt.Time
			classified.Credential.CreatedAt = &t
		}
		if lastUsedAt.Valid {
			t := lastUsedAt.Time
			classified.Credential.LastUsedAt = &t
		}
		if disabledAt.Valid {
			t := disabledAt.Time
			classified.Credential.DisabledAt = &t
		}
		if err := json.Unmarshal([]byte(scopesJSON), &classified.Credential.Scopes); err != nil {
			return nil, err
		}
		if classified.Credential.Scopes == nil {
			classified.Credential.Scopes = []string{}
		}
		if err := json.Unmarshal([]byte(privilegedScopesJSON), &classified.Credential.PrivilegedScopes); err != nil {
			return nil, err
		}
		if classified.Credential.PrivilegedScopes == nil {
			classified.Credential.PrivilegedScopes = []string{}
		}
		if err := json.Unmarshal([]byte(metadataJSON), &classified.Credential.Metadata); err != nil {
			return nil, err
		}
		if classified.Credential.Metadata == nil {
			classified.Credential.Metadata = map[string]string{}
		}
		credentials = append(credentials, classified)
	}
	return credentials, rows.Err()
}

func (s *SQLite) UpdateCredentialSyncState(ctx context.Context, state CredentialSyncState) error {
	now := state.LastSyncedAt
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO credential_sync_state (provider, last_synced_at, credential_count, status, usage_known, message)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider) DO UPDATE SET last_synced_at = excluded.last_synced_at,
		   credential_count = excluded.credential_count, status = excluded.status,
		   usage_known = excluded.usage_known, message = excluded.message`,
		state.Provider, now, state.CredentialCount, state.Status, state.UsageKnown, state.Message)
	return err
}

func (s *SQLite) ListCredentialSyncStates(ctx context.Context) ([]CredentialSyncState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider, last_synced_at, credential_count, status, usage_known, message
		 FROM credential_sync_state ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var states []CredentialSyncState
	for rows.Next() {
		var st CredentialSyncState
		if err := rows.Scan(&st.Provider, &st.LastSyncedAt, &st.CredentialCount, &st.Status, &st.UsageKnown, &st.Message); err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	return states, rows.Err()
}

func jsonText[T any](value T, empty T) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(data) == "null" {
		data, err = json.Marshal(empty)
		if err != nil {
			return "", err
		}
	}
	return string(data), nil
}

func utcPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return utc
}
