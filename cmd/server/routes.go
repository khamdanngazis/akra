package main

import (
	"io/fs"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"akra/internal/academic"
	"akra/internal/admission"
	"akra/internal/announcements"
	"akra/internal/attendance"
	"akra/internal/audit/view"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/classes"
	"akra/internal/dapodik"
	"akra/internal/duty"
	"akra/internal/exam"
	"akra/internal/grades"
	"akra/internal/home"
	"akra/internal/importer"
	"akra/internal/payments"
	"akra/internal/platform/health"
	"akra/internal/platform/httpserver"
	"akra/internal/platform/page"
	"akra/internal/platform/storage"
	"akra/internal/portal"
	"akra/internal/reports"
	"akra/internal/reports/builder"
	"akra/internal/schedule"
	"akra/internal/school"
	"akra/internal/students"
	"akra/internal/wilayah"
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
	// schoolProfile adalah Pengaturan → Profil sekolah (T126).
	schoolProfile *school.ProfileHandler
	// duties adalah Pengaturan → Tugas tambahan (T125).
	duties *duty.Handler
	// exam adalah halaman staf modul ujian (MVP 4), aktif per sekolah.
	exam *exam.Handler
	// participant adalah login dan beranda peserta ujian (/u/..., T124),
	// dengan sesi peserta sendiri (participantSessions).
	participant         *exam.ParticipantHandler
	participantSessions *scs.SessionManager
	classes             *classes.Handler
	subjects            *academic.SubjectHandler
	assignments         *academic.AssignmentHandler
	schedule            *schedule.Handler
	components          *grades.ComponentHandler
	gradeInput          *grades.InputHandler
	gradeRecap          *grades.ReportHandler
	reportConfig        *grades.ReportConfigHandler
	reportSaved         *builder.ConfigHandler
	students            *students.Handler
	admission           *admission.Handler
	dapodik             *dapodik.Handler
	// admissionPublic adalah formulir pendaftaran tanpa login (T90).
	admissionPublic *admission.PublicHandler
	// wilayah adalah daftar wilayah untuk isian alamat (T110), publik.
	wilayah     *wilayah.Handler
	activation  *auth.ActivationHandler
	portal      *portal.Handler
	attendance  *attendance.Handler
	feeTypes    *billing.FeeTypeHandler
	bulkInvoice *billing.BulkHandler
	arrears     *billing.ArrearsHandler
	payments    *payments.Handler
	auditLog    *auditview.Handler
	reports     *reports.Handler
	announce    *announcements.Handler
	home        *home.Handler
	importer    *importer.Handler
	mappings    *importer.MappingHandler
	// storage menyajikan URL bertanda tangan driver storage local (nil untuk R2).
	storage http.Handler
	// dev berisi route khusus development (nil di staging/production).
	dev func(chi.Router)
}

// newRouter menyusun semua route aplikasi.
//
// Route publik: /healthz, /healthz/db, /static/, /login, /logout,
// /lupa-password, /reset-password, /aktivasi, dan formulir pendaftaran
// /s/{code}/daftar (T90). /portal hanya untuk orang
// tua; route lain menolak orang tua dengan 403.
// Semua route lain wajib berada di grup RequireLogin. Setiap route wajib
// menyebut peran yang diizinkan di komentar di atas handler-nya.
func newRouter(d routerDeps) http.Handler {
	r := chi.NewRouter()

	// Tanpa sesi dan tanpa CSRF.
	r.Get("/healthz", health.Handler)
	r.Get("/healthz/db", health.DBHandler(d.pinger))
	// Aset teks dan halaman peserta dikompresi: halaman ujian di HP dengan
	// kuota terbatas (T138: transfer awal < 150 KB di luar gambar).
	compress := middleware.Compress(5, "text/html", "text/css", "text/javascript", "application/javascript", "application/json", "image/svg+xml")
	static := compress(http.StripPrefix("/static/", httpserver.Static(d.static)))
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

	// Peserta ujian (T124): sesi peserta terpisah dari sesi staf. Route
	// staf di bawah tidak pernah membaca sesi ini, dan cookie peserta hanya
	// berlaku di path /u.
	if d.participant != nil {
		r.Group(func(r chi.Router) {
			r.Use(httpserver.LimitBody(httpserver.MaxRequestBody))
			r.Use(compress)
			r.Use(d.csrf)
			r.Use(d.participantSessions.LoadAndSave)
			d.participant.Routes(r)
		})
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
		d.admissionPublic.Routes(r)
		d.wilayah.Routes(r)

		// Wajib login.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireLogin(d.sessions))
			// Peran: semua peran.
			r.Method(http.MethodGet, "/", d.home)
			d.academicYear.Routes(r)
			d.schoolProfile.Routes(r)
			d.duties.Routes(r)
			d.exam.Routes(r)
			d.classes.Routes(r)
			d.subjects.Routes(r)
			d.assignments.Routes(r)
			d.schedule.Routes(r)
			d.components.Routes(r)
			d.gradeInput.Routes(r)
			d.gradeRecap.Routes(r)
			d.reportConfig.Routes(r)
			d.reportSaved.Routes(r)
			d.students.Routes(r)
			d.admission.Routes(r)
			d.dapodik.Routes(r)
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
			d.mappings.Routes(r)
		})
	})

	return r
}
