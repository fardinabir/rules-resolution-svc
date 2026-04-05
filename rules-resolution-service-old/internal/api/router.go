package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/abir/rules-resolution-service/internal/api/handler"
	"github.com/abir/rules-resolution-service/internal/api/middleware"
)

func NewRouter(resolveH *handler.ResolveHandler, overrideH *handler.OverrideHandler) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(middleware.CORS)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api", func(r chi.Router) {
		r.Post("/resolve", resolveH.Resolve)
		r.Post("/resolve/explain", resolveH.Explain)

		r.Route("/overrides", func(r chi.Router) {
			// /conflicts must be registered BEFORE /{id} to avoid chi matching "conflicts" as an id param
			r.Get("/conflicts", overrideH.GetConflicts)
			r.Get("/", overrideH.List)
			r.Post("/", overrideH.Create)
			r.Get("/{id}", overrideH.GetByID)
			r.Put("/{id}", overrideH.Update)
			r.Patch("/{id}/status", overrideH.UpdateStatus)
			r.Get("/{id}/history", overrideH.GetHistory)
		})
	})

	return r
}
