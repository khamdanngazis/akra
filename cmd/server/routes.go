package main

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/auth"
	"akra/internal/home"
	"akra/internal/platform/health"
)

type routerDeps struct {
	pool        *pgxpool.Pool
	csrf        func(http.Handler) http.Handler
	sessions    *scs.SessionManager
	render      home.Renderer
	static      fs.FS
	authHandler *auth.Handler
}

// newRouter menyusun semua route aplikasi.
//
// Route publik: /healthz, /healthz/db, /static/, /login, dan /logout.
// Semua route lain wajib berada di grup RequireLogin. Setiap route wajib
// menyebut peran yang diizinkan di komentar di atas handler-nya.
func newRouter(d routerDeps) http.Handler {
	r := chi.NewRouter()

	// Tanpa sesi dan tanpa CSRF.
	r.Get("/healthz", health.Handler)
	r.Get("/healthz/db", health.DBHandler(d.pool))
	static := http.StripPrefix("/static/", staticHandler(d.static))
	r.Get("/static/*", static.ServeHTTP)
	r.Head("/static/*", static.ServeHTTP)

	r.Group(func(r chi.Router) {
		// CSRF dipasang sebelum sesi agar token tersedia saat render.
		r.Use(d.csrf)
		r.Use(d.sessions.LoadAndSave)

		// Publik, tapi butuh sesi.
		d.authHandler.Routes(r)

		// Wajib login.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireLogin(d.sessions))
			// Peran: semua peran.
			r.Get("/", home.Handler(d.render, d.sessions))
		})
	})

	return r
}

// staticHandler melayani file dari fsys tanpa daftar isi folder.
//
// TODO(T12): ambil dari embed.FS dan tambahkan header cache.
func staticHandler(fsys fs.FS) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
