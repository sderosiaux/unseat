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
		r.Get("/providers/{name}/inactive", s.handleInactiveUsers)
		r.Get("/inactive", s.handleAllInactiveUsers)
		r.Get("/orphans", s.handleListOrphans)
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
