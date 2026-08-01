package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	states, err := s.store.ListSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, states)
}

func (s *Server) handleProviderUsers(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	users, err := s.store.GetProviderUsers(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// handleListPendingRemovals returns seats inside their grace period.
//
// This is NOT the set of orphaned accounts: it stays empty until a non-dry-run
// sync with a grace period has detected departures. Orphans are computed live
// against the directory by `unseat audit orphans`.
func (s *Server) handleListPendingRemovals(w http.ResponseWriter, r *http.Request) {
	states, err := s.store.ListSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type pending struct {
		Provider  string    `json:"provider"`
		Email     string    `json:"email"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	var out []pending
	for _, ss := range states {
		removals, err := s.store.GetPendingRemovals(r.Context(), ss.Provider)
		if err != nil {
			continue
		}
		for _, rem := range removals {
			out = append(out, pending{Provider: rem.Provider, Email: rem.Email, ExpiresAt: rem.ExpiresAt})
		}
	}
	// An empty array rather than null: a client cannot tell "no countdowns" from
	// "field missing" otherwise.
	if out == nil {
		out = []pending{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pending_removals": out,
		"note":             "seats inside their grace period; empty until a non-dry-run sync with a grace period has run. For orphaned accounts use `unseat audit orphans`.",
	})
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	events, err := s.store.ListEvents(r.Context(), store.EventFilter{Limit: limit})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleGetMappings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.config.Mappings)
}

func (s *Server) handleAllInactiveUsers(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			days = n
		}
	}
	since := time.Now().AddDate(0, 0, -days)
	reporting, silent, err := s.activityProviders()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	users, err := s.store.GetInactiveUsers(r.Context(), since, reporting)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"threshold_days":      days,
		"evaluated_providers": reporting,
		"unevaluable":         silent,
		"users":               users,
	})
}

// activityProviders splits configured providers into those whose API reports
// activity and those that cannot answer the question at all. Callers must
// surface the second list: an empty result otherwise reads as "all active".
func (s *Server) activityProviders() (reporting, silent []string, err error) {
	reporting, err = provider.ActivityReportingProviders(s.config)
	if err != nil {
		return nil, nil, err
	}
	silent, err = provider.NonActivityReportingProviders(s.config)
	if err != nil {
		return nil, nil, err
	}
	return reporting, silent, nil
}

func (s *Server) handleInactiveUsers(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			days = n
		}
	}
	name := chi.URLParam(r, "name")

	reporting, _, err := s.activityProviders()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !slices.Contains(reporting, name) {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider":    name,
			"unevaluable": true,
			"reason":      "this provider's API exposes no activity data",
			"users":       []store.InactiveUser{},
		})
		return
	}

	since := time.Now().AddDate(0, 0, -days)
	users, err := s.store.GetInactiveUsers(r.Context(), since, []string{name})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"provider":       name,
		"threshold_days": days,
		"users":          users,
	})
}
