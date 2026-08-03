package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
)

//go:embed dashboard/*
var dashboardFS embed.FS

type Server struct {
	store  store.Store
	config *config.Config
	router chi.Router
}

func NewServer(s store.Store, cfg *config.Config) *Server {
	srv := &Server{store: s, config: cfg}
	srv.setupRoutes()
	return srv
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.SetHeader("Content-Type", "application/json"))
		r.Get("/providers", s.handleListProviders)
		r.Get("/providers/{name}/users", s.handleProviderUsers)
		r.Get("/providers/{name}/billing", s.handleProviderBilling)
		r.Get("/providers/{name}/credentials", s.handleProviderCredentials)
		r.Get("/providers/{name}/inactive", s.handleInactiveUsers)
		r.Get("/billing", s.handleListBilling)
		r.Get("/inactive", s.handleAllInactiveUsers)
		r.Get("/credentials", s.handleListCredentials)
		r.Get("/credentials/summary", s.handleCredentialsSummary)
		r.Get("/decisions", s.handleListDecisions)
		r.Post("/decisions/{id}/approve", s.handleApproveDecision)
		r.Post("/decisions/{id}/reject", s.handleRejectDecision)
		r.Post("/decisions/{id}/attest-owner", s.handleAttestOwnerDecision)
		r.Get("/offboarding/certificates", s.handleListOffboardingCertificates)
		r.Get("/offboarding/certificates/{id}", s.handleGetOffboardingCertificate)
		// Named for what it reads. As /orphans it invited "empty means no
		// orphaned accounts", when it only ever meant "no removal is currently
		// counting down" — a table that stays empty until a non-dry-run sync
		// with a grace period has run.
		r.Get("/pending-removals", s.handleListPendingRemovals)
		r.Get("/history/events", s.handleListEvents)
		r.Get("/mappings", s.handleGetMappings)
	})

	sub, _ := fs.Sub(dashboardFS, "dashboard")
	r.Handle("/*", http.FileServer(http.FS(sub)))

	s.router = r
}

func (s *Server) Handler() http.Handler {
	return s.router
}
