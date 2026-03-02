package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
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
		`INSERT INTO provider_users (provider, email, display_name, role, status, provider_id, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, u := range users {
		if _, err := stmt.ExecContext(ctx, provider, u.Email, u.DisplayName, u.Role, u.Status, u.ProviderID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) GetProviderUsers(ctx context.Context, provider string) ([]core.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT email, display_name, role, status, provider_id FROM provider_users WHERE provider = ? ORDER BY email`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []core.User
	for rows.Next() {
		var u core.User
		if err := rows.Scan(&u.Email, &u.DisplayName, &u.Role, &u.Status, &u.ProviderID); err != nil {
			return nil, err
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

func (s *SQLite) InsertPendingRemoval(ctx context.Context, provider, email string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_removals (provider, email, expires_at) VALUES (?, ?, ?)
		 ON CONFLICT(provider, email) DO UPDATE SET expires_at = excluded.expires_at, cancelled = FALSE`,
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

func (s *SQLite) GetExpiredRemovals(ctx context.Context) ([]PendingRemoval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider, email, detected_at, expires_at, cancelled FROM pending_removals
		 WHERE cancelled = FALSE AND expires_at <= ? ORDER BY expires_at`, time.Now().UTC())
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

func (s *SQLite) UpdateSyncState(ctx context.Context, provider string, userCount int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (provider, last_synced_at, user_count, status) VALUES (?, ?, ?, 'ok')
		 ON CONFLICT(provider) DO UPDATE SET last_synced_at = excluded.last_synced_at, user_count = excluded.user_count, status = excluded.status`,
		provider, time.Now().UTC(), userCount)
	return err
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
