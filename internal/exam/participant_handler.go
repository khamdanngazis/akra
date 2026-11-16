package exam

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/platform/ratelimit"
)

// Percobaan login peserta dibatasi per IP dan per nomor peserta. Batas IP
// longgar karena satu lab atau satu WiFi sekolah memakai satu IP publik.
const attemptParticipantLogin = "exam_login"

var participantLimits = map[string]ratelimit.Limit{
	attemptParticipantLogin: {Window: 15 * time.Minute, PerIP: 1000, PerKey: 10},
}

// DeviceCookieName adalah cookie penanda perangkat peserta. Kode ujian
// terikat ke perangkat pertama yang memakainya.
const DeviceCookieName = "akra_perangkat"

// ParticipantHandler melayani login dan beranda peserta ujian (/u/...).
type ParticipantHandler struct {
	db       database.DB
	sessions *scs.SessionManager // sesi peserta, bukan sesi staf
	render   page.Renderer
	logger   *slog.Logger
	limiter  *ratelimit.Limiter
	secure   bool
	now      func() time.Time
}

// NewParticipantHandler mengembalikan ParticipantHandler. sessions harus
// dibuat dengan auth.NewParticipantSessionManager. Route-nya dipasang di
// luar grup sesi staf.
func NewParticipantHandler(db database.DB, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger, secure bool) *ParticipantHandler {
	return &ParticipantHandler{db: db, sessions: sessions, render: render, logger: logger,
		limiter: ratelimit.New(db, participantLimits), secure: secure, now: time.Now}
}

// Routes mendaftarkan route peserta.
func (h *ParticipantHandler) Routes(r chi.Router) {
	r.Route("/u", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireParticipant(h.sessions, participantLoginPath))
			// Peserta: siswa dan pendaftar.
			r.Get("/beranda", h.home)
		})
		// Peserta (atau tanpa sesi): keluar.
		r.Post("/keluar", h.logout)
		// Publik: petunjuk bila peserta belum/tidak lagi masuk.
		r.Get("/", h.index)
		// Publik: login peserta per sekolah.
		r.Get("/{code}", h.loginForm)
		// Publik: login peserta per sekolah.
		r.Post("/{code}", h.login)
	})
}

func participantLoginPath(_ *http.Request, code string) string {
	if code == "" {
		return "/u/"
	}
	return "/u/" + url.PathEscape(code)
}

func (h *ParticipantHandler) index(w http.ResponseWriter, r *http.Request) {
	h.render.Render(w, r, http.StatusOK, "exam/participant_index", nil)
}

type participantSchool struct {
	ID, Code, Name, Timezone string
}

// loadSchool membaca sekolah dari kode di URL; ok false bila sekolah tidak
// ada atau modul ujian nonaktif (keduanya 404).
func (h *ParticipantHandler) loadSchool(r *http.Request) (participantSchool, bool, error) {
	code := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "code")))
	var s participantSchool
	err := h.db.QueryRow(r.Context(), `SELECT id, code, name, timezone FROM schools WHERE code = $1`, code).
		Scan(&s.ID, &s.Code, &s.Name, &s.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	if err != nil {
		return s, false, fmt.Errorf("exam: load school: %w", err)
	}
	m, err := LoadModule(r.Context(), h.db, s.ID)
	if err != nil {
		return s, false, err
	}
	return s, m.Has(R1), nil
}

type loginView struct {
	School     participantSchool
	Identifier string
	Error      string
	Flash      string
}

func (h *ParticipantHandler) loginForm(w http.ResponseWriter, r *http.Request) {
	s, ok, err := h.loadSchool(r)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant login form", err)
		return
	}
	if !ok {
		page.NotFound(h.render, w, r, nil, "Halaman ujian sekolah ini tidak ditemukan. Periksa alamat di kartu ujian.")
		return
	}
	h.ensureDevice(w, r)
	// QR di kartu berisi ?nis=..., sehingga peserta cukup mengetik kode.
	v := loginView{School: s, Identifier: strings.TrimSpace(r.URL.Query().Get("nis")),
		Flash: auth.PopFlash(r.Context(), h.sessions)}
	h.render.Render(w, r, http.StatusOK, "exam/participant_login", v)
}

// ensureDevice memastikan cookie perangkat ada dan mengembalikan nilainya.
func (h *ParticipantHandler) ensureDevice(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(DeviceCookieName); err == nil && database.IsUUID(c.Value) {
		return c.Value
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h1 := hex.EncodeToString(b[:])
	id := h1[0:8] + "-" + h1[8:12] + "-" + h1[12:16] + "-" + h1[16:20] + "-" + h1[20:]
	http.SetCookie(w, &http.Cookie{Name: DeviceCookieName, Value: id, Path: auth.ParticipantCookiePath,
		MaxAge: 365 * 24 * 3600, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
	return id
}

const maxLoginForm = 2 << 10

func (h *ParticipantHandler) login(w http.ResponseWriter, r *http.Request) {
	s, ok, err := h.loadSchool(r)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant login", err)
		return
	}
	if !ok {
		page.NotFound(h.render, w, r, nil, "Halaman ujian sekolah ini tidak ditemukan. Periksa alamat di kartu ujian.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginForm)
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, nil, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	// Kode ujian tidak pernah ditulis ke log maupun ke tampilan ulang form.
	identifier := strings.TrimSpace(r.PostForm.Get("identifier"))
	code := r.PostForm.Get("code")
	device := h.ensureDevice(w, r)
	ip := auth.ClientIP(r)
	key := s.ID + "|" + strings.ToUpper(identifier)
	v := loginView{School: s, Identifier: identifier}

	allowed, err := h.limiter.Allow(r.Context(), attemptParticipantLogin, key, ip)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant rate limit", err)
		return
	}
	if !allowed {
		v.Error = "Terlalu banyak percobaan. Tunggu beberapa menit, lalu coba lagi atau minta bantuan pengawas."
		h.render.Render(w, r, http.StatusTooManyRequests, "exam/participant_login", v)
		return
	}
	p, err := Login(r.Context(), h.db, LoginRequest{SchoolID: s.ID, Identifier: identifier, Code: code, DeviceID: device, Now: h.now()})
	_ = h.limiter.Record(r.Context(), s.ID, attemptParticipantLogin, key, ip, err == nil)
	if err != nil {
		msg, status := loginMessage(err, clock.Location(s.Timezone), h.now())
		if status == http.StatusInternalServerError {
			page.ServerError(h.render, h.logger, w, r, nil, "participant login", err)
			return
		}
		h.logger.InfoContext(r.Context(), "participant login rejected", "school_id", s.ID, "reason", err.Error())
		v.Error = msg
		h.render.Render(w, r, status, "exam/participant_login", v)
		return
	}
	p.SchoolCode, p.SchoolName, p.Timezone = s.Code, s.Name, s.Timezone
	if err := auth.StartParticipantSession(r.Context(), h.sessions, p); err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "start participant session", err)
		return
	}
	h.logger.InfoContext(r.Context(), "participant login", "school_id", s.ID, "credential_id", p.CredentialID, "read_only", p.ReadOnly)
	http.Redirect(w, r, "/u/beranda", http.StatusSeeOther)
}

// loginMessage menerjemahkan error login menjadi pesan untuk peserta.
func loginMessage(err error, loc *time.Location, now time.Time) (string, int) {
	hhmm := func(t time.Time) string {
		l := t.In(loc)
		s := l.Format("15.04") + " " + clock.ZoneLabel(loc)
		if !clock.DateIn(t, loc).Equal(clock.DateIn(now, loc)) {
			s += ", " + clock.FormatDateID(l)
		}
		return s
	}
	var nyv *NotYetValidError
	var exp *ExpiredError
	switch {
	case errors.As(err, &nyv):
		return "Kode belum berlaku, sesi mulai pukul " + hhmm(nyv.SessionStart) + ". Kode bisa dipakai mulai " + hhmm(nyv.ValidFrom) + ".", http.StatusUnprocessableEntity
	case errors.As(err, &exp):
		return "Kode sudah tidak berlaku. Sesi berakhir pukul " + hhmm(exp.Ended) + ".", http.StatusUnprocessableEntity
	case errors.Is(err, ErrOtherDevice):
		return "Kode sudah dipakai di perangkat lain, minta pengawas.", http.StatusConflict
	case errors.Is(err, ErrLocked):
		return "Akses terkunci setelah 5 kali kode salah. Minta pengawas membuka kunci.", http.StatusLocked
	case errors.Is(err, ErrBadLogin):
		return "NIS/nomor pendaftaran atau kode salah. Periksa kembali kartu ujian Anda.", http.StatusUnprocessableEntity
	}
	return "", http.StatusInternalServerError
}

type participantExam struct {
	Date   string // "17 November 2026"
	Time   string // "08.00–09.30 WIB"
	Status string // "Belum dimulai", "Bisa dikerjakan", "Selesai"
	Tone   string
}

type participantHomeView struct {
	Participant auth.Participant
	Exams       []participantExam
}

func (h *ParticipantHandler) home(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.CurrentParticipant(r.Context())
	now := h.now()
	rows, err := h.db.Query(r.Context(), `
		SELECT valid_from, valid_until FROM student_credentials
		WHERE `+ownerWhere+` AND coalesce(results_until, valid_until) > $4
		ORDER BY valid_from`, p.SchoolID, nilIfEmpty(p.StudentID), nilIfEmpty(p.ApplicantID), now.Add(-recentWindow))
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant home", err)
		return
	}
	v := participantHomeView{Participant: p}
	loc := p.Location()
	for rows.Next() {
		var from, until time.Time
		if err := rows.Scan(&from, &until); err != nil {
			rows.Close()
			page.ServerError(h.render, h.logger, w, r, nil, "participant home", err)
			return
		}
		begin := from.Add(CodeEarly).In(loc)
		e := participantExam{Date: clock.FormatDateID(begin),
			Time: begin.Format("15.04") + "–" + until.In(loc).Format("15.04") + " " + clock.ZoneLabel(loc)}
		switch {
		case now.Before(from):
			e.Status, e.Tone = "Belum dimulai", "info"
		case !now.After(until):
			e.Status, e.Tone = "Bisa dikerjakan", "success"
		default:
			e.Status, e.Tone = "Selesai", ""
		}
		v.Exams = append(v.Exams, e)
	}
	if err := rows.Err(); err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant home", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/participant_home", v)
}

func (h *ParticipantHandler) logout(w http.ResponseWriter, r *http.Request) {
	code := auth.ParticipantSchoolCode(r.Context(), h.sessions)
	if err := h.sessions.Destroy(r.Context()); err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant logout", err)
		return
	}
	http.Redirect(w, r, participantLoginPath(r, code), http.StatusSeeOther)
}
