package exam

import (
	"bytes"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// practiceRoutes mendaftarkan halaman ujian latihan (T136): membuka paket
// latihan bawaan untuk satu kelas dalam ≤ 3 klik.
func (h *Handler) practiceRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		// Peran: admin_tu, guru (kelas yang diajar), kepala_sekolah (baca).
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru))
		r.Use(RequireAccess(h.db, h.render, h.logger, ActClassExam, Read))
		r.Get("/latihan", h.practice)
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleGuru))
			r.Post("/latihan", h.practiceOpen)
			r.Post("/latihan/{sesi}/kartu", h.practiceCards)
		})
	})
}

type practiceSessionRow struct {
	Session
	When    string
	Active  bool
	CanCard bool
	Issued  int
}

type practiceView struct {
	User      auth.Identity
	Exam      Exam
	Comp      Composition
	Classes   []ClassOption
	Sessions  []practiceSessionRow
	CanOpen   bool
	Flash     string
	Error     string
	Window    string
	ExamLink  string
	Questions []string
}

func (h *Handler) practice(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	ctx := r.Context()
	now := time.Now()
	ex, err := EnsurePractice(ctx, h.db, sa, h.math, now)
	if err != nil {
		h.fail(w, r, "ensure practice", err)
		return
	}
	loc := id.Location()
	v := practiceView{User: id, Exam: ex, Flash: auth.PopFlash(ctx, h.sessions), CanOpen: sa.HasAnyRole(roleAdmin, roleTeacher),
		Window: "3 hari", ExamLink: examPath(ex.ID) + "?tab=soal"}
	plan, err := BuildPlan(ctx, h.db, ex)
	if err != nil {
		h.fail(w, r, "practice plan", err)
		return
	}
	v.Comp = plan.Composition()
	for _, k := range []Kind{KindMC, KindMulti, KindTF, KindShort, KindMatch, KindEssay} {
		if v.Comp.ByKind[k] > 0 {
			v.Questions = append(v.Questions, k.Label())
		}
	}
	// Kelas yang boleh dibuka.
	all, err := ClassesFor(ctx, h.db, Actor{Roles: []string{roleAdmin}}, Exam{SchoolID: sa.SchoolID}, now)
	if err != nil {
		h.fail(w, r, "list classes", err)
		return
	}
	for _, c := range all {
		if canOpenPractice(sa.Actor, c.ID, now) {
			v.Classes = append(v.Classes, c)
		}
	}
	sessions, err := ListSessions(ctx, h.db, sa.SchoolID, ex.ID)
	if err != nil {
		h.fail(w, r, "list practice sessions", err)
		return
	}
	for _, s := range sessions {
		if s.EndsAt.Before(now.Add(-7 * 24 * time.Hour)) {
			continue
		}
		row := practiceSessionRow{Session: s, When: sessionWhen(s, loc), Active: !now.Before(s.StartsAt) && now.Before(s.EndsAt),
			CanCard: s.ClassID != "" && canOpenPractice(sa.Actor, s.ClassID, now) && now.Before(s.EndsAt)}
		_ = h.db.QueryRow(ctx, `SELECT count(DISTINCT student_id) FROM student_credentials WHERE exam_session_id = $1 AND revoked_at IS NULL`, s.ID).Scan(&row.Issued)
		v.Sessions = append(v.Sessions, row)
	}
	sort.SliceStable(v.Sessions, func(i, j int) bool { return v.Sessions[i].StartsAt.After(v.Sessions[j].StartsAt) })
	h.render.Render(w, r, http.StatusOK, "exam/practice", v)
}

func (h *Handler) practiceOpen(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 8<<10) {
		return
	}
	id, sa := h.staff(r)
	now := time.Now()
	ex, err := EnsurePractice(r.Context(), h.db, sa, h.math, now)
	if err != nil {
		h.fail(w, r, "ensure practice", err)
		return
	}
	s, err := OpenPractice(r.Context(), h.db, sa, ex, r.PostForm.Get("class_id"), now)
	switch {
	case errors.Is(err, ErrNotYourClass):
		page.Forbidden(h.render, w, r, id, "Anda hanya bisa membuka latihan untuk kelas yang Anda ajar.")
		return
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	case err != nil:
		h.fail(w, r, "open practice", err)
		return
	default:
		h.flash(r, "Latihan dibuka untuk "+s.Title()+" sampai "+clock.FormatDateTimeID(s.EndsAt, id.Location())+". Cetak kartu agar siswa bisa masuk.")
	}
	http.Redirect(w, r, "/ujian/latihan", http.StatusSeeOther)
}

func (h *Handler) practiceCards(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r, 8<<10) {
		return
	}
	id, sa := h.staff(r)
	now := time.Now()
	ex, ok, err := GetPractice(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "get practice", err)
		return
	}
	s, err := GetSession(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "sesi"))
	if errors.Is(err, ErrNotFound) || (err == nil && (!ok || s.ExamID != ex.ID)) {
		page.NotFound(h.render, w, r, id, "Sesi latihan tidak ditemukan.")
		return
	}
	if err != nil {
		h.fail(w, r, "get practice session", err)
		return
	}
	if s.ClassID == "" || !canOpenPractice(sa.Actor, s.ClassID, now) {
		page.Forbidden(h.render, w, r, id, "Anda hanya bisa mencetak kartu latihan untuk kelas yang Anda ajar.")
		return
	}
	// Kartu latihan: kode lama diganti tanpa konfirmasi (latihan tidak dinilai).
	cards, err := IssueCards(r.Context(), h.db, sa, h.issuer, CardScope{ExamID: ex.ID, SessionID: s.ID}, ex.Name, h.baseURL, true, now)
	if err != nil {
		h.docsFail(w, r, "/ujian/latihan", "practice cards", err)
		return
	}
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return
	}
	h.writePDF(w, r, "kartu-latihan-"+strings.TrimPrefix(s.Title(), "Kelas "), func(b *bytes.Buffer) error { return WriteCardsPDFWith(b, lh, cards, now, true, cardOptions(r)) })
}
