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
