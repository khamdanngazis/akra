package main

import (
	"io/fs"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/academic"
	"akra/internal/announcements"
	"akra/internal/attendance"
	"akra/internal/audit/view"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/classes"
	"akra/internal/home"
	"akra/internal/importer"
	"akra/internal/payments"
	"akra/internal/platform/health"
	"akra/internal/platform/httpserver"
	"akra/internal/platform/page"
	"akra/internal/platform/storage"
	"akra/internal/portal"
	"akra/internal/reports"
	"akra/internal/school"
	"akra/internal/students"
)

type routerDeps struct {
	pinger       health.Pinger
	csrf         func(http.Handler) http.Handler
	sessions     *scs.SessionManager
	render       page.Renderer
	static       fs.FS
	authHandler  *auth.Handler
	resetHandler *auth.ResetHandler
	academicYear *school.AcademicYearHandler
	classes      *classes.Handler
	subjects     *academic.SubjectHandler
	assignments  *academic.AssignmentHandler
	students     *students.Handler
	activation   *auth.ActivationHandler
	portal       *portal.Handler
	attendance   *attendance.Handler
	feeTypes     *billing.FeeTypeHandler
	bulkInvoice  *billing.BulkHandler
	arrears      *billing.ArrearsHandler
	payments     *payments.Handler
	auditLog     *auditview.Handler
	reports      *reports.Handler
	announce     *announcements.Handler
	home         *home.Handler
	importer     *importer.Handler
	// storage menyajikan URL bertanda tangan driver storage local (nil untuk R2).
	storage http.Handler
	// dev berisi route khusus development (nil di staging/production).
	dev func(chi.Router)
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

	// Driver storage local: URL bertanda tangan dan kedaluwarsa (pengganti
	// presigned URL R2), tanpa sesi.
	if d.storage != nil {
		r.Get(storage.LocalRoute+"/*", d.storage.ServeHTTP)
	}

	// Hanya development: alat bantu tanpa sesi dan tanpa CSRF.
	if d.dev != nil {
		r.Route("/dev", d.dev)
	}

	r.Group(func(r chi.Router) {
		// Batas body dipasang sebelum CSRF: nosurf mem-parse form multipart
		// (unggahan) seluruhnya untuk mencari token.
		r.Use(httpserver.LimitBody(httpserver.MaxRequestBody))
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
			r.Method(http.MethodGet, "/", d.home)
			d.academicYear.Routes(r)
			d.classes.Routes(r)
			d.subjects.Routes(r)
			d.assignments.Routes(r)
			d.students.Routes(r)
			d.portal.Routes(r)
			d.attendance.Routes(r)
			d.feeTypes.Routes(r)
			d.bulkInvoice.Routes(r)
			d.arrears.Routes(r)
			d.payments.Routes(r)
			d.auditLog.Routes(r)
			d.reports.Routes(r)
			d.announce.Routes(r)
			d.importer.Routes(r)
		})
	})

	return r
}
