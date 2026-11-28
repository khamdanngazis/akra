package main

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/alexedwards/scs/v2"

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
	"akra/internal/exam/mathrender"
	"akra/internal/grades"
	"akra/internal/home"
	"akra/internal/importer"
	"akra/internal/ops"
	"akra/internal/payments"
	"akra/internal/platform/csrf"
	"akra/internal/platform/database"
	"akra/internal/platform/health"
	"akra/internal/platform/jobs"
	"akra/internal/platform/mail"
	"akra/internal/platform/metrics"
	"akra/internal/platform/module"
	"akra/internal/platform/page"
	"akra/internal/platform/pii"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/portal"
	"akra/internal/reports"
	"akra/internal/reports/builder"
	"akra/internal/schedule"
	"akra/internal/school"
	"akra/internal/students"
	"akra/internal/wilayah"
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
	pii      *pii.Keyring  // enkripsi data pribadi (T85b)
	// examTokens menandatangani token attempt ujian (T139). Nil: kunci
	// development (uji).
	examTokens *exam.TokenSigner
	dev        bool // APP_ENV=development: pasang route /dev

	// Keandalan (T145). metrics nil: registry baru (uji).
	metrics   *metrics.Registry
	ops       opsDeps
	saveFault int          // EXAM_FAULT_SAVE_PERCENT (bukan production)
	support   exam.Support // kontak dukungan AKRA di pusat bantuan (T154)
	draining  *atomic.Bool // true setelah SIGTERM: /healthz 503
}

// opsDeps adalah setelan halaman status, /metrics, dan /internal.
type opsDeps struct {
	token    string
	env      string
	notifier ops.Notifier
}

// newApp menyusun semua service dan handler lalu mengembalikan router.
func newApp(d appDeps) (http.Handler, error) {
	if d.pii == nil {
		return nil, errors.New("app: PII keyring is required")
	}
	authSvc, err := auth.NewService(auth.NewRepository(d.db), d.hasher)
	if err != nil {
		return nil, err
	}
	periods := admission.NewService(d.db)
	applicants := admission.NewApplicantService(d.db, d.pii, periods)
	documents := admission.NewDocumentService(d.db, d.store, d.pii)
	activation := auth.NewActivationService(d.db, d.hasher, d.mailer, d.baseURL, d.logger)
	// Satu handler laporan: juga jalur ekspor laporan kustom (T79).
	examTokens := d.examTokens
	if examTokens == nil {
		examTokens = exam.DevTokenSigner()
	}
	participantSessions := auth.NewParticipantSessionManager(d.sessions.Store, d.secure, d.logger)
	math, err := mathrender.New(d.static)
	if err != nil {
		return nil, err
	}
	reg := d.metrics
	if reg == nil {
		reg = metrics.New()
	}
	draining := d.draining
	if draining == nil {
		draining = new(atomic.Bool)
	}
	reportsHandler := reports.NewHandler(d.db, reportJobs(d.jobs), d.store, d.render, d.logger)

	return newRouter(routerDeps{
		pinger:   d.pinger,
		draining: draining,
		hsts:     d.secure,
		ops: &ops.Handler{DB: d.db, Metrics: reg, Render: d.render, Logger: d.logger, Notifier: d.ops.notifier,
			Token: d.ops.token, Env: d.ops.env},
		csrf: csrf.Middleware(d.secure, d.logger),
		modules: module.Middleware(d.db, func(r *http.Request) string {
			id, _ := auth.CurrentUser(r.Context())
			return id.SchoolID
		}, func(w http.ResponseWriter, r *http.Request, err error) {
			id, _ := auth.CurrentUser(r.Context())
			page.ServerError(d.render, d.logger, w, r, id, "load school modules", err)
		}),
		sessions:    d.sessions,
		render:      d.render,
		static:      d.static,
		authHandler: auth.NewHandler(authSvc, d.sessions, d.render, d.logger),
		resetHandler: auth.NewResetHandler(
			auth.NewResetService(auth.NewRepository(d.db), d.hasher, d.mailer,
				auth.NewSessionRevoker(d.sessions), d.baseURL, d.logger),
			d.sessions, d.render, d.logger),
		activation: auth.NewActivationHandler(activation, d.sessions, d.render, d.logger),
		duties:     duty.NewHandler(d.db, d.sessions, d.render, d.logger),
		exam: exam.NewHandler(d.db, d.store, math, d.sessions, d.render, d.logger).WithJobs(examJobs(d.jobs)).WithBaseURL(d.baseURL).
			WithCardCodes(examTokens).WithSupport(d.support),
		participant: exam.NewParticipantHandler(d.db, participantSessions, d.render, d.logger, d.secure).WithMath(math).WithStorage(d.store).WithTokens(examTokens).
			WithMetrics(reg).WithSaveFault(d.saveFault).WithBaseURL(d.baseURL),
		participantSessions: participantSessions,
		schoolProfile:       school.NewProfileHandler(school.NewProfileService(d.db), d.sessions, d.render, d.logger),
		settingsHub:         school.NewSettingsHubHandler(d.render),
		academicYear:        school.NewAcademicYearHandler(school.NewAcademicYearService(d.db), d.sessions, d.render, d.logger),
		classes:             classes.NewHandler(classes.NewService(d.db), d.db, d.sessions, d.render, d.logger),
		subjects:            academic.NewSubjectHandler(academic.NewSubjectService(d.db), d.sessions, d.render, d.logger),
		assignments:         academic.NewAssignmentHandler(academic.NewAssignmentService(d.db), d.db, d.sessions, d.render, d.logger),
		schedule:            schedule.NewHandler(schedule.NewService(d.db), d.db, d.sessions, d.render, d.logger),
		components:          grades.NewComponentHandler(grades.NewComponentService(d.db), d.db, d.sessions, d.render, d.logger),
		gradeInput:          grades.NewInputHandler(grades.NewInputService(d.db), d.db, d.sessions, d.render, d.logger),
		gradeRecap:          grades.NewReportHandler(d.db, d.sessions, d.render, d.logger),
		reportConfig:        grades.NewReportConfigHandler(d.db, d.sessions, d.render, d.logger),
		reportSaved:         builder.NewConfigHandler(builder.NewConfigService(d.db, builder.Default), reportsHandler, d.sessions, d.render, d.logger),
		students: students.NewHandler(
			students.NewService(d.db),
			students.NewGuardianService(d.db),
			students.NewGuardianAccountService(d.db, activation, d.logger),
			students.NewProfileService(d.db, d.pii),
			students.NewGuardianDetailService(d.db, d.pii),
			d.sessions, d.render, d.logger).WithExamExport(exam.StudentExamExportAny),
		admission:       admission.NewHandler(d.db, periods, applicants, documents, d.sessions, d.render, d.logger).WithExam(examTokens, d.baseURL),
		dapodik:         dapodik.NewHandler(d.db, d.pii, d.store, d.sessions, d.render, d.logger),
		admissionPublic: admission.NewPublicHandler(d.db, periods, applicants, documents, d.sessions, d.render, d.logger).WithExam(examTokens, d.baseURL),
		wilayah:         wilayah.NewHandler(d.db, d.logger),
		portal:          portal.NewHandler(portal.NewService(d.db), d.sessions, d.render, d.logger),
		dev:             devRoutes(d),
		attendance:      attendance.NewHandler(attendance.NewService(d.db), d.db, d.sessions, d.render, d.logger),
		feeTypes:        billing.NewFeeTypeHandler(billing.NewFeeTypeService(d.db), d.sessions, d.render, d.logger),
		bulkInvoice:     billing.NewBulkHandler(billing.NewBulkService(d.db, d.jobs), d.sessions, d.render, d.logger),
		arrears:         billing.NewArrearsHandler(d.db, d.render, d.logger),
		payments:        payments.NewHandler(payments.NewService(d.db), payments.NewProofService(d.db, d.store), payments.NewVoidService(d.db), d.sessions, d.render, d.logger),
		storage:         localStorageHandler(d.store),
		auditLog:        auditview.NewHandler(d.db, d.render, d.logger),
		home:            home.NewHandler(d.db, d.render, d.sessions, d.logger),
		announce:        announcements.NewHandler(announcements.NewService(d.db), d.sessions, d.render, d.logger),
		reports:         reportsHandler,
		importer:        importer.NewHandler(importer.NewService(d.db, importJobs(d.jobs)), d.sessions, d.render, d.logger),
		mappings:        importer.NewMappingHandler(importer.NewMappingService(d.db), d.sessions, d.render, d.logger),
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

// importJobs mengembalikan inserter job impor, atau nil (interface nil,
// bukan pointer nil) jika antrean job tidak tersedia.
func importJobs(c *jobs.Client) importer.JobInserter {
	if c == nil {
		return nil
	}
	return c
}

// reportJobs mengembalikan inserter job laporan, atau nil (interface nil,
// bukan pointer nil) jika antrean job tidak tersedia.
func reportJobs(c *jobs.Client) reports.JobInserter {
	if c == nil {
		return nil
	}
	return c
}

// examJobs mengembalikan inserter job impor soal, atau nil (interface nil)
// jika antrean job tidak tersedia.
func examJobs(c *jobs.Client) exam.ImportJobInserter {
	if c == nil {
		return nil
	}
	return c
}
