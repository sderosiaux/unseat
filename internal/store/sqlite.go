package store

import (
	"context"
	"database/sql"
	"embed"
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
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	goose.SetBaseFS(migrations)
	// Migrations run on every command. Their progress on stdout corrupted
	// --json output, so it is silenced; failures still surface as errors.
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		db.Close()
		return nil, fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		db.Close()
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
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_users WHERE provider = ?`, provider); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO provider_users (provider, email, display_name, role, status, provider_id, synced_at, last_activity_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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
	defer rows.Close()

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
