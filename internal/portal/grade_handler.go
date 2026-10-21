package portal

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/grades"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const pageGrades = "portal/grades"

type gradesView struct {
	User     auth.Identity
	Grades   ChildGrades
	Semester int
	Flash    string
	Error    string
}

func (h *Handler) childGrades(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	g, err := h.svc.GetChildGrades(r.Context(), id.SchoolID, id.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child grades", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, pageGrades, gradesView{User: id, Grades: g, Semester: grades.DefaultSemester(time.Now())})
}

func (h *Handler) childReportCard(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	semester := grades.DefaultSemester(time.Now())
	if raw := r.URL.Query().Get("semester"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || (n != 1 && n != 2) {
			page.BadRequest(h.render, w, r, id, "Semester tidak valid", "Pilih semester 1 (Ganjil) atau 2 (Genap).")
			return
		}
		semester = n
	}
	actor := school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
	card, err := h.svc.ChildReportCard(r.Context(), actor, chi.URLParam(r, "id"), semester, clock.Today())
	switch {
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	case errors.Is(err, ErrReportCardUnavailable):
		page.BadRequest(h.render, w, r, id, "Rapor belum tersedia", "Rapor bisa diunduh setelah nilai semua mata pelajaran lengkap. Lihat halaman nilai untuk mapel yang belum lengkap.")
		return
	case err != nil:
		page.ServerError(h.render, h.logger, w, r, id, "child report card", err)
		return
	}
	var buf bytes.Buffer
	if err := grades.WriteReportCardPDF(&buf, card, true); err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "render report card", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/pdf")
	hdr.Set("Content-Disposition", `attachment; filename="`+grades.ReportCardFilename(card)+`"`)
	hdr.Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}
