package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
)

const (
	credentialSyncOK           = "ok"
	credentialSyncNotSupported = "not_supported"
	credentialSyncFailed       = "failed"
)

type credentialCacheEntry struct {
	Provider    string
	Credentials []core.ClassifiedCredential
	Status      string
	UsageKnown  bool
	Message     string
}

func cacheCredentialSnapshots(ctx context.Context, entries []credentialCacheEntry) {
	if len(entries) == 0 {
		return
	}

	db, err := openStore()
	if err != nil {
		slog.Debug("credential cache unavailable", "error", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Debug("credential cache close failed", "error", err)
		}
	}()

	writeCredentialSnapshots(ctx, db, entries)
}

func writeCredentialSnapshots(ctx context.Context, db *store.SQLite, entries []credentialCacheEntry) {
	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.Provider == "" {
			continue
		}
		if entry.Status == "" {
			entry.Status = credentialSyncOK
		}

		// A successful empty read means "there are no credentials" and must clear
		// stale findings. A failed read leaves the previous inventory intact but
		// updates the state so callers do not mistake stale data for a clean scan.
		if entry.Status == credentialSyncOK || entry.Status == credentialSyncNotSupported {
			if err := db.UpsertProviderCredentials(ctx, entry.Provider, entry.Credentials); err != nil {
				slog.Debug("cache provider credentials failed", "provider", entry.Provider, "error", err)
				continue
			}
		}

		if err := db.UpdateCredentialSyncState(ctx, store.CredentialSyncState{
			Provider:        entry.Provider,
			LastSyncedAt:    now,
			CredentialCount: len(entry.Credentials),
			Status:          entry.Status,
			UsageKnown:      entry.UsageKnown,
			Message:         entry.Message,
		}); err != nil {
			slog.Debug("update credential sync state failed", "provider", entry.Provider, "error", err)
		}
	}
}
