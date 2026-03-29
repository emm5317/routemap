package chicase

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func setup() {
	r := chi.NewRouter()
	r.Use(Logger)

	r.Get("/", handleHome)

	r.Route("/api", func(sub chi.Router) {
		sub.Get("/users", handleListUsers)
		sub.Post("/users", handleCreateUser)
	})

	auth := r.With(AuthMiddleware)
	auth.Delete("/admin", handleAdmin)
}

func Logger(next http.Handler) http.Handler     { return next }
func AuthMiddleware(next http.Handler) http.Handler { return next }
func handleHome(w http.ResponseWriter, r *http.Request)        {}
func handleListUsers(w http.ResponseWriter, r *http.Request)   {}
func handleCreateUser(w http.ResponseWriter, r *http.Request)  {}
func handleAdmin(w http.ResponseWriter, r *http.Request)       {}
