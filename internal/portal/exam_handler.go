package portal

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

const pageExams = "portal/exams"

// Ujian anak di portal orang tua (T151): jadwal ujian yang sudah
// diumumkan, nilai yang sudah dibuka guru, dan ketidakhadiran beserta
// jadwal susulan. Tidak pernah memuat soal, jawaban, maupun kunci.

type examItemView struct {
	Name, Subject, When string
	Makeup              bool
	Room, Number        string
}

type examAbsenceView struct {
	ExamName, Subject, Makeup string
}

type examsView struct {
	User     auth.Identity
	Child    ChildDetail
	Upcoming []examItemView
	Results  []exam.StudentResult
	Absent   []examAbsenceView
	Flash    string
	Error    string
}

// childExams menampilkan ujian anak. 404 untuk anak orang lain atau bila
// modul ujian tidak aktif.
func (h *Handler) childExams(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	studentID := chi.URLParam(r, "id")
	if err := h.svc.checkChild(ctx, id.SchoolID, id.UserID, studentID); errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	} else if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "check child", err)
		return
	}
	m, err := exam.LoadModule(ctx, h.svc.db, id.SchoolID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "exam module", err)
		return
	}
	if !m.Enabled {
		page.NotFound(h.render, w, r, id, "Halaman tidak ditemukan.")
		return
	}
	v := examsView{User: id}
	if err := h.svc.db.QueryRow(ctx, `SELECT id, nis, full_name FROM students WHERE school_id = $1 AND id = $2`, id.SchoolID, studentID).
		Scan(&v.Child.ID, &v.Child.NIS, &v.Child.FullName); err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child", fmt.Errorf("portal: child: %w", err))
		return
	}
	c, err := exam.LoadChildExams(ctx, h.svc.db, id.SchoolID, studentID, time.Now())
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child exams", err)
		return
	}
	loc := id.Location()
	for _, it := range c.Upcoming {
		a, b := it.Start.In(loc), it.End.In(loc)
		when := clock.FormatDateID(a) + ", " + a.Format("15.04") + "–" + b.Format("15.04") + " " + clock.ZoneLabel(loc)
		if !clock.DateIn(it.Start, loc).Equal(clock.DateIn(it.End, loc)) {
			when = clock.FormatDateTimeID(a, loc) + " s.d. " + clock.FormatDateTimeID(b, loc)
		}
		v.Upcoming = append(v.Upcoming, examItemView{Name: it.Name, Subject: it.Subject, When: when, Makeup: it.Makeup, Room: it.Room, Number: it.Number})
	}
	v.Results = c.Results
	for _, a := range c.Absent {
		av := examAbsenceView{ExamName: a.ExamName, Subject: a.Subject}
		if a.Makeup != nil {
			av.Makeup = clock.FormatDateTimeID(*a.Makeup, loc)
		}
		v.Absent = append(v.Absent, av)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	h.render.Render(w, r, http.StatusOK, pageExams, v)
}
