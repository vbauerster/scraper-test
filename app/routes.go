package app

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/render"
)

func (s *server) initRoutes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(render.SetContentType(render.ContentTypeJSON))

	s.router.Get("/bounds", s.queryBounds)

	s.router.Route("/services", func(r chi.Router) {
		r.Get("/{serviceName}", s.queryService)
	})

	s.router.Get("/admins", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("all good admins save logs"))
	})
}
