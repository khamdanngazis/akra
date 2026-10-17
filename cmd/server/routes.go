package main

import (
	"fmt"
	"html"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/auth"
	"akra/internal/platform/csrf"
	"akra/internal/platform/health"
)

type routerDeps struct {
	pool        *pgxpool.Pool
	csrf        func(http.Handler) http.Handler
	sessions    *scs.SessionManager
	authHandler *auth.Handler
}

// newRouter menyusun semua route aplikasi.
//
// Route publik: /healthz, /healthz/db, /login, /logout, dan nanti /static/.
// Semua route lain wajib berada di grup RequireLogin.
func newRouter(d routerDeps) http.Handler {
	r := chi.NewRouter()

	// Health check tanpa sesi.
	r.Get("/healthz", health.Handler)
	r.Get("/healthz/db", health.DBHandler(d.pool))
	// TODO(T12): pasang /static/ di sini, di luar sesi dan RequireLogin.

	r.Group(func(r chi.Router) {
		// CSRF dipasang sebelum sesi agar token tersedia saat render.
		r.Use(d.csrf)
		r.Use(d.sessions.LoadAndSave)

		// Publik, tapi butuh sesi.
		d.authHandler.Routes(r)

		// Wajib login.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireLogin(d.sessions))
			r.Get("/", homePlaceholder)
		})
	})

	return r
}

// homePlaceholder menampilkan identitas pengguna dan tombol logout.
//
// TODO(T11): ganti dengan home.html.
func homePlaceholder(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>AKRA</title>
<p>Masuk sebagai pengguna %s.</p>
<form method="post" action="/logout">%s<button type="submit">Keluar</button></form>`, html.EscapeString(id.UserID), csrf.Field(r))
}
