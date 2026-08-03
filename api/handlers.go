package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write json response failed", "error", err)
	}
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

func (s *Server) handleListBilling(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.store.ListLatestBillingSnapshots(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if snapshots == nil {
		snapshots = []core.BillingSnapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"billing": snapshots})
}

func (s *Server) handleProviderBilling(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	snapshot, err := s.store.LatestBillingSnapshot(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if snapshot == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider": name,
			"billing":  nil,
			"reason":   "no billing snapshot has been cached; run `unseat scan`",
		})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type credentialsResponse struct {
	Credentials []core.ClassifiedCredential `json:"credentials"`
	SyncStates  []store.CredentialSyncState `json:"sync_states"`
}

type providerCredentialsResponse struct {
	Provider    string                      `json:"provider"`
	Credentials []core.ClassifiedCredential `json:"credentials"`
	SyncState   *store.CredentialSyncState  `json:"sync_state,omitempty"`
}

type credentialsSummaryResponse struct {
	Summary    []core.CredentialSummary    `json:"summary"`
	SyncStates []store.CredentialSyncState `json:"sync_states"`
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	credentials, err := s.store.ListProviderCredentials(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	states, err := s.store.ListCredentialSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if credentials == nil {
		credentials = []core.ClassifiedCredential{}
	}
	if states == nil {
		states = []store.CredentialSyncState{}
	}
	writeJSON(w, http.StatusOK, credentialsResponse{
		Credentials: credentials,
		SyncStates:  states,
	})
}

func (s *Server) handleProviderCredentials(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	credentials, err := s.store.GetProviderCredentials(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	states, err := s.store.ListCredentialSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var state *store.CredentialSyncState
	for i := range states {
		if states[i].Provider == name {
			state = &states[i]
			break
		}
	}
	if credentials == nil {
		credentials = []core.ClassifiedCredential{}
	}
	writeJSON(w, http.StatusOK, providerCredentialsResponse{
		Provider:    name,
		Credentials: credentials,
		SyncState:   state,
	})
}

func (s *Server) handleCredentialsSummary(w http.ResponseWriter, r *http.Request) {
	credentials, err := s.store.ListProviderCredentials(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	states, err := s.store.ListCredentialSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	summary := summarizeCredentials(credentials, states)
	if states == nil {
		states = []store.CredentialSyncState{}
	}
	writeJSON(w, http.StatusOK, credentialsSummaryResponse{
		Summary:    summary,
		SyncStates: states,
	})
}

type decisionMutationRequest struct {
	Owner  string `json:"owner"`
	By     string `json:"by"`
	Reason string `json:"reason"`
}

type decisionsResponse struct {
	Decisions []core.Decision `json:"decisions"`
}

type certificatesResponse struct {
	Certificates []core.OffboardingCertificate `json:"certificates"`
}

func (s *Server) handleListDecisions(w http.ResponseWriter, r *http.Request) {
	filter, err := decisionFilterFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	decisions, err := s.store.ListDecisions(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if decisions == nil {
		decisions = []core.Decision{}
	}
	writeJSON(w, http.StatusOK, decisionsResponse{Decisions: decisions})
}

func (s *Server) handleApproveDecision(w http.ResponseWriter, r *http.Request) {
	body, err := readDecisionMutationRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	decision, err := s.store.ApproveDecision(r.Context(), chi.URLParam(r, "id"), body.By)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) handleRejectDecision(w http.ResponseWriter, r *http.Request) {
	body, err := readDecisionMutationRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	decision, err := s.store.RejectDecision(r.Context(), chi.URLParam(r, "id"), body.By, body.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) handleAttestOwnerDecision(w http.ResponseWriter, r *http.Request) {
	body, err := readDecisionMutationRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	decision, err := s.store.AttestOwner(r.Context(), chi.URLParam(r, "id"), body.Owner, body.By, body.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) handleListOffboardingCertificates(w http.ResponseWriter, r *http.Request) {
	filter, err := certificateFilterFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	certs, err := s.store.ListOffboardingCertificates(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if certs == nil {
		certs = []core.OffboardingCertificate{}
	}
	writeJSON(w, http.StatusOK, certificatesResponse{Certificates: certs})
}

func (s *Server) handleGetOffboardingCertificate(w http.ResponseWriter, r *http.Request) {
	cert, err := s.store.GetOffboardingCertificate(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cert == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "certificate not found"})
		return
	}
	writeJSON(w, http.StatusOK, cert)
}

func decisionFilterFromRequest(r *http.Request) (store.DecisionFilter, error) {
	query := r.URL.Query()
	filter := store.DecisionFilter{Limit: 100}
	if v := query.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return store.DecisionFilter{}, err
		}
		filter.Limit = n
	}
	if v := query.Get("provider"); v != "" {
		provider := strings.ToLower(strings.TrimSpace(v))
		filter.Provider = &provider
	}
	if v := query.Get("subject"); v != "" {
		subject := strings.ToLower(strings.TrimSpace(v))
		filter.Subject = &subject
	}
	if v := query.Get("status"); v != "" {
		status := core.DecisionStatus(strings.TrimSpace(v))
		if !validDecisionStatus(status) {
			return store.DecisionFilter{}, fmt.Errorf("unknown decision status %q", v)
		}
		filter.Status = &status
	}
	return filter, nil
}

func certificateFilterFromRequest(r *http.Request) (store.CertificateFilter, error) {
	query := r.URL.Query()
	filter := store.CertificateFilter{Limit: 50}
	if v := query.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return store.CertificateFilter{}, err
		}
		filter.Limit = n
	}
	if v := query.Get("subject"); v != "" {
		subject := strings.ToLower(strings.TrimSpace(v))
		filter.Subject = &subject
	}
	if v := query.Get("status"); v != "" {
		status := core.CertificateStatus(strings.TrimSpace(v))
		if !validCertificateStatus(status) {
			return store.CertificateFilter{}, fmt.Errorf("unknown certificate status %q", v)
		}
		filter.Status = &status
	}
	return filter, nil
}

func readDecisionMutationRequest(r *http.Request) (decisionMutationRequest, error) {
	var body decisionMutationRequest
	if r.Body == nil {
		return body, nil
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&body)
	if err == io.EOF {
		return body, nil
	}
	if err != nil {
		return decisionMutationRequest{}, err
	}
	body.By = strings.TrimSpace(body.By)
	body.Owner = strings.TrimSpace(body.Owner)
	body.Reason = strings.TrimSpace(body.Reason)
	return body, nil
}

func validDecisionStatus(status core.DecisionStatus) bool {
	switch status {
	case core.DecisionProposed,
		core.DecisionApproved,
		core.DecisionRejected,
		core.DecisionExecuted,
		core.DecisionVerified,
		core.DecisionBlocked,
		core.DecisionStale,
		core.DecisionVerificationFailed:
		return true
	default:
		return false
	}
}

func validCertificateStatus(status core.CertificateStatus) bool {
	switch status {
	case core.CertificateComplete,
		core.CertificateCompleteWithProviderLimits,
		core.CertificateBlocked,
		core.CertificateIncomplete,
		core.CertificateStale:
		return true
	default:
		return false
	}
}

func summarizeCredentials(credentials []core.ClassifiedCredential, states []store.CredentialSyncState) []core.CredentialSummary {
	byProvider := make(map[string][]core.ClassifiedCredential)
	for _, c := range credentials {
		byProvider[c.Credential.Provider] = append(byProvider[c.Credential.Provider], c)
	}
	usageKnown := make(map[string]bool)
	for _, st := range states {
		usageKnown[st.Provider] = st.UsageKnown
	}

	providers := make([]string, 0, len(byProvider))
	for provider := range byProvider {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	out := make([]core.CredentialSummary, 0, len(providers))
	for _, provider := range providers {
		out = append(out, core.SummarizeCredentials(provider, byProvider[provider], usageKnown[provider]))
	}
	return out
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
	reporting, silent, err := s.activityProviders(r.Context())
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

// activityProviders splits scanned providers into those whose API reported
// activity and those that could not answer the question at all. Callers must
// surface the second list: an empty result otherwise reads as "all active".
//
// Read from what was observed, not recomputed from config. Most connectors
// know their capability statically, but GitHub only learns it by calling the
// org audit log — so a provider rebuilt from config alone answers false, and
// this endpoint contradicted the scan that produced the very rows it serves.
func (s *Server) activityProviders(ctx context.Context) (reporting, silent []string, err error) {
	return s.store.ActivityReportingProviders(ctx)
}

func (s *Server) handleInactiveUsers(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil {
			days = n
		}
	}
	name := chi.URLParam(r, "name")

	reporting, _, err := s.activityProviders(r.Context())
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
