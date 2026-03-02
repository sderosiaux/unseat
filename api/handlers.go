package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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

func (s *Server) handleListOrphans(w http.ResponseWriter, r *http.Request) {
	states, err := s.store.ListSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type orphan struct {
		Provider string `json:"provider"`
		Email    string `json:"email"`
	}
	var orphans []orphan
	for _, ss := range states {
		removals, err := s.store.GetPendingRemovals(r.Context(), ss.Provider)
		if err != nil {
			continue
		}
		for _, rem := range removals {
			orphans = append(orphans, orphan{Provider: rem.Provider, Email: rem.Email})
		}
	}
	writeJSON(w, http.StatusOK, orphans)
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
