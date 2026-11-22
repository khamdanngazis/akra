package exam

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/exam/mathrender"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/platform/storage"
)

// Handler melayani halaman staf modul ujian.
type Handler struct {
	db       database.DB
	store    storage.Store        // gambar soal
	math     *mathrender.Renderer // rumus dirender server saat simpan
	jobs     ImportJobInserter    // job baca impor; nil = langsung
	issuer   *Issuer              // kode kartu peserta (T135)
	baseURL  string               // URL publik untuk QR kartu
	support  Support              // kontak dukungan AKRA (T154)
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(db database.DB, store storage.Store, math *mathrender.Renderer, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{db: db, store: store, math: math, sessions: sessions, render: render, logger: logger,
		issuer: NewIssuer().WithCardCodes(DevTokenSigner())}
}

// WithCardCodes memakai kunci t untuk kode kartu pendaftar (T149); harus
// sama dengan kunci halaman status pendaftaran agar kode kartu cetak dan
// kartu di halaman status sama.
func (h *Handler) WithCardCodes(t *TokenSigner) *Handler {
	h.issuer = NewIssuer().WithCardCodes(t)
	return h
}

// Routes mendaftarkan route modul ujian. Semua route menjawab 404 bila
// modul nonaktif untuk sekolah (RequireModule).
func (h *Handler) Routes(r chi.Router) {
	r.Route("/ujian", func(r chi.Router) {
		r.Use(RequireModule(h.db, h.render, h.logger, R1))
		r.Use(h.withNav)
		// Peran: admin_tu, kepala_sekolah, guru.
		staff := r.With(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru))
		staff.Get("/", h.home)
		// Pusat bantuan dan panduan cetak (T154). Peran: admin_tu,
		// kepala_sekolah, guru.
		staff.Get("/bantuan", h.help)
		staff.Get("/panduan/pengawas.pdf", h.guidePDF(GuideProctor))
		staff.Get("/panduan/peserta.pdf", h.guidePDF(GuideParticipant))
		staff.Get("/panduan/orang-tua.pdf", h.guidePDF(GuideParent))
		h.bankRoutes(r)
		h.examRoutes(r)
		h.eventRoutes(r)
		h.practiceRoutes(r)
		h.settingsRoutes(r)
		h.proctorRoutes(r)
		h.essayRoutes(r)
	})
}

// quickLink adalah tombol "Mulai cepat" di beranda modul.
type quickLink struct {
	Label, Href string
	Primary     bool
}

type homeView struct {
	User   auth.Identity
	Module Module
	// Hari ini: sesi yang diawasi pengguna dan agenda yang berlangsung.
	Proctor []MySessionCard
	Events  []TodayEvent
	Control bool // boleh membuka pusat kendali agenda
	ToDo    []ToDo
	Quick   []quickLink
	// Fresh: sekolah belum punya ujian selain ujian latihan bawaan.
	Fresh bool
}

var releaseLabels = map[Release]string{
	R1: "Tahap 1 — tes seleksi online",
	R2: "Tahap 2 — ulangan guru",
	R3: "Tahap 3 — PTS/PAS sekolah",
}

// ReleaseLabel mengembalikan nama tahap rilis untuk tampilan.
func (m Module) ReleaseLabel() string { return releaseLabels[m.Release] }

// home adalah beranda modul ujian (T158): hari ini, perlu tindakan, dan
// mulai cepat. Peran: admin_tu, kepala_sekolah, guru.
func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	ctx := r.Context()
	h.ensureSamples(r, id.SchoolID)
	if _, ok := ActorFrom(ctx); !ok {
		a, err := LoadActor(ctx, h.db, id)
		if err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "load exam actor", err)
			return
		}
		sa.Actor = a
	}
	m := ModuleFrom(ctx)
	now := time.Now()
	v := homeView{User: id, Module: m}
	from, to := DayBounds(now, id.Location())
	ps, err := MyProctorSessions(ctx, h.db, id.SchoolID, id.UserID, from, to)
	if err != nil {
		h.fail(w, r, "home proctor sessions", err)
		return
	}
	v.Proctor = MySessionCards(ps, id.Location(), now)
	if m.Has(R3) && sa.CanAny(ActAgenda, Read) {
		v.Control = true
		if v.Events, err = TodayEvents(ctx, h.db, id.SchoolID, from); err != nil {
			h.fail(w, r, "home events", err)
			return
		}
	}
	review := false
	if sa.CanAny(ActBank, Read) {
		s, err := LoadExamSettings(ctx, h.db, id.SchoolID)
		if err != nil {
			h.fail(w, r, "home exam settings", err)
			return
		}
		review = s.Review
	}
	if v.ToDo, err = MyToDo(ctx, h.db, sa, review, now); err != nil {
		h.fail(w, r, "home to-do", err)
		return
	}
	if err := h.db.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM exams WHERE school_id = $1 AND NOT builtin AND archived_at IS NULL)`,
		id.SchoolID).Scan(&v.Fresh); err != nil {
		h.fail(w, r, "home fresh school", err)
		return
	}
	if sa.CanAny(ActClassExam, Write) || sa.CanAny(ActAdmissionTest, Write) {
		v.Quick = append(v.Quick, quickLink{Label: "Buat ujian", Href: "/ujian/paket/baru", Primary: true})
	}
	if sa.CanAny(ActBank, Write) {
		v.Quick = append(v.Quick, quickLink{Label: "Tambah atau impor soal", Href: "/ujian/bank/"})
	}
	if sa.CanAny(ActClassExam, Write) {
		v.Quick = append(v.Quick, quickLink{Label: "Coba halaman ujian", Href: "/ujian/latihan"})
	}
	h.render.Render(w, r, http.StatusOK, "exam/home", v)
}

// ensureSamples membuat konten contoh sekolah saat staf pertama membuka
// modul ujian (T154). Gagal tidak menghalangi halaman: dicoba lagi pada
// kunjungan berikutnya.
func (h *Handler) ensureSamples(r *http.Request, schoolID string) {
	if _, err := EnsureSamples(r.Context(), h.db, h.store, h.math, schoolID, time.Now()); err != nil {
		h.logger.ErrorContext(r.Context(), "ensure exam samples", "error", err)
	}
}
