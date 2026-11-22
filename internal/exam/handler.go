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

// section adalah satu bagian modul ujian di halaman depan.
type section struct {
	Title, Desc, Path string
	// Release adalah tahap rilis yang membuka bagian ini.
	Release Release
	Ready   bool // halaman sudah tersedia di rilis ini
}

type homeView struct {
	User     auth.Identity
	Module   Module
	Sections []section
}

var releaseLabels = map[Release]string{
	R1: "Tahap 1 — tes seleksi online",
	R2: "Tahap 2 — ulangan guru",
	R3: "Tahap 3 — PTS/PAS sekolah",
}

// ReleaseLabel mengembalikan nama tahap rilis untuk tampilan.
func (m Module) ReleaseLabel() string { return releaseLabels[m.Release] }

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.ensureSamples(r, id.SchoolID)
	a, ok := ActorFrom(r.Context())
	if !ok {
		var err error
		if a, err = LoadActor(r.Context(), h.db, id); err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "load exam actor", err)
			return
		}
	}
	m := ModuleFrom(r.Context())
	all := []struct {
		section
		act Action
	}{
		{section{Title: "Bank soal", Desc: "Soal per mapel, dipakai bersama guru mapel. Bisa diimpor dari Word.", Path: "/ujian/bank/", Release: R1, Ready: true}, ActBank},
		{section{Title: "Tes seleksi", Desc: "Tes online calon siswa; nilai langsung masuk ke peringkat pendaftaran.", Path: "/ujian/paket/", Release: R1, Ready: true}, ActAdmissionTest},
		{section{Title: "Ulangan kelas", Desc: "Ulangan dan ujian latihan untuk kelas yang Anda ampu, dari bank soal.", Path: "/ujian/paket/", Release: R2, Ready: true}, ActClassExam},
		{section{Title: "Ujian latihan", Desc: "Siswa mencoba halaman ujian sebelum ujian sungguhan. Buka untuk satu kelas dalam satu klik.", Path: "/ujian/latihan", Release: R1, Ready: true}, ActClassExam},
		{section{Title: "Agenda PTS/PAS", Desc: "Jadwal mapel per hari, ruang, sesi, pengawas, dan kartu peserta.", Path: "/ujian/agenda", Release: R3, Ready: true}, ActAgenda},
	}
	v := homeView{User: id, Module: m}
	for _, s := range all {
		if a.CanAny(s.act, Read) && m.Has(s.Release) {
			v.Sections = append(v.Sections, s.section)
		}
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
