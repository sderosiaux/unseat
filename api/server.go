package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
)

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
	r.Use(middleware.SetHeader("Content-Type", "application/json"))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/providers", s.handleListProviders)
		r.Get("/providers/{name}/users", s.handleProviderUsers)
		r.Get("/orphans", s.handleListOrphans)
		r.Get("/history/events", s.handleListEvents)
		r.Get("/mappings", s.handleGetMappings)
	})

	s.router = r
}

func (s *Server) Handler() http.Handler {
	return s.router
}
