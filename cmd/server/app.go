package main

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"akra/internal/attendance"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/classes"
	"akra/internal/payments"
	"akra/internal/platform/csrf"
	"akra/internal/platform/database"
	"akra/internal/platform/health"
	"akra/internal/platform/jobs"
	"akra/internal/platform/mail"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/portal"
	"akra/internal/school"
	"akra/internal/students"
)

// appDeps adalah dependensi luar aplikasi. Uji end-to-end memakai fungsi
// yang sama dengan DB berupa transaksi yang di-rollback.
type appDeps struct {
	db       database.DB
	pinger   health.Pinger // biasanya pool yang sama dengan db
	sessions *scs.SessionManager
	render   *render.Renderer
	static   fs.FS
	mailer   mail.Mailer
	hasher   *auth.Hasher
	secure   bool   // cookie Secure dan origin https (di luar development)
	baseURL  string // URL publik untuk tautan di email
	logger   *slog.Logger
	jobs     *jobs.Client  // untuk memasukkan job latar belakang
	store    storage.Store // file privat (bukti transfer)
	dev      bool          // APP_ENV=development: pasang route /dev
}

// newApp menyusun semua service dan handler lalu mengembalikan router.
func newApp(d appDeps) (http.Handler, error) {
	authSvc, err := auth.NewService(auth.NewRepository(d.db), d.hasher)
	if err != nil {
		return nil, err
	}
	activation := auth.NewActivationService(d.db, d.hasher, d.mailer, d.baseURL, d.logger)

	return newRouter(routerDeps{
		pinger:      d.pinger,
		csrf:        csrf.Middleware(d.secure, d.logger),
		sessions:    d.sessions,
		render:      d.render,
		static:      d.static,
		authHandler: auth.NewHandler(authSvc, d.sessions, d.render, d.logger),
		resetHandler: auth.NewResetHandler(
			auth.NewResetService(auth.NewRepository(d.db), d.hasher, d.mailer,
				auth.NewSessionRevoker(d.sessions), d.baseURL, d.logger),
			d.sessions, d.render, d.logger),
		activation:   auth.NewActivationHandler(activation, d.sessions, d.render, d.logger),
		academicYear: school.NewAcademicYearHandler(school.NewAcademicYearService(d.db), d.sessions, d.render, d.logger),
		classes:      classes.NewHandler(classes.NewService(d.db), d.db, d.sessions, d.render, d.logger),
		students: students.NewHandler(
			students.NewService(d.db),
			students.NewGuardianService(d.db),
			students.NewGuardianAccountService(d.db, activation, d.logger),
			d.sessions, d.render, d.logger),
		portal:      portal.NewHandler(d.sessions, d.render),
		dev:         devRoutes(d),
		attendance:  attendance.NewHandler(attendance.NewService(d.db), d.db, d.sessions, d.render, d.logger),
		feeTypes:    billing.NewFeeTypeHandler(billing.NewFeeTypeService(d.db), d.sessions, d.render, d.logger),
		bulkInvoice: billing.NewBulkHandler(billing.NewBulkService(d.db, d.jobs), d.sessions, d.render, d.logger),
		payments:    payments.NewHandler(payments.NewService(d.db), payments.NewProofService(d.db, d.store), payments.NewVoidService(d.db), d.sessions, d.render, d.logger),
		storage:     localStorageHandler(d.store),
	}), nil
}

// localStorageHandler mengembalikan handler URL bertanda tangan driver
// local, atau nil untuk R2 (URL langsung ke Cloudflare).
func localStorageHandler(store storage.Store) http.Handler {
	if l, ok := store.(*storage.Local); ok {
		return l.Handler()
	}
	return nil
}
