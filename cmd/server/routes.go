package main

import (
	"io/fs"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/classes"
	"akra/internal/home"
	"akra/internal/platform/health"
	"akra/internal/platform/httpserver"
	"akra/internal/portal"
	"akra/internal/school"
	"akra/internal/students"
)

type routerDeps struct {
	pinger       health.Pinger
	csrf         func(http.Handler) http.Handler
	sessions     *scs.SessionManager
	render       home.Renderer
	static       fs.FS
	authHandler  *auth.Handler
	resetHandler *auth.ResetHandler
	academicYear *school.AcademicYearHandler
	classes      *classes.Handler
	students     *students.Handler
	activation   *auth.ActivationHandler
	portal       *portal.Handler
	attendance   *attendance.Handler
	feeTypes     *billing.FeeTypeHandler
}

// newRouter menyusun semua route aplikasi.
//
// Route publik: /healthz, /healthz/db, /static/, /login, /logout,
// /lupa-password, /reset-password, dan /aktivasi. /portal hanya untuk orang
// tua; route lain menolak orang tua dengan 403.
// Semua route lain wajib berada di grup RequireLogin. Setiap route wajib
// menyebut peran yang diizinkan di komentar di atas handler-nya.
func newRouter(d routerDeps) http.Handler {
	r := chi.NewRouter()

	// Tanpa sesi dan tanpa CSRF.
	r.Get("/healthz", health.Handler)
	r.Get("/healthz/db", health.DBHandler(d.pinger))
	static := http.StripPrefix("/static/", httpserver.Static(d.static))
	r.Get("/static/*", static.ServeHTTP)
	r.Head("/static/*", static.ServeHTTP)

	r.Group(func(r chi.Router) {
		// CSRF dipasang sebelum sesi agar token tersedia saat render.
		r.Use(d.csrf)
		r.Use(d.sessions.LoadAndSave)

		// Publik, tapi butuh sesi.
		d.authHandler.Routes(r)
		d.resetHandler.Routes(r)
		d.activation.Routes(r)

		// Wajib login.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireLogin(d.sessions))
			// Peran: semua peran.
			r.Get("/", home.Handler(d.render, d.sessions))
			d.academicYear.Routes(r)
			d.classes.Routes(r)
			d.students.Routes(r)
			d.portal.Routes(r)
			d.attendance.Routes(r)
			d.feeTypes.Routes(r)
		})
	})

	return r
}
