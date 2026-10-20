package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/page"
	"akra/internal/schedule"
)

const pageSchedule = "portal/schedule"

// ChildSchedule adalah jadwal kelas anak di tahun ajaran aktif.
type ChildSchedule struct {
	Child    ChildDetail // hanya ID, NIS, FullName, ClassName, YearName
	HasClass bool
	Days     []schedule.DayView
}

// GetChildSchedule mengembalikan jadwal kelas aktif anak studentID milik akun
// userID (kepemilikan diperiksa dengan checkChild, T44). Anak yang belum
// punya kelas di tahun ajaran aktif mendapat HasClass false.
func (s *Service) GetChildSchedule(ctx context.Context, schoolID, userID, studentID string) (ChildSchedule, error) {
	if err := s.checkChild(ctx, schoolID, userID, studentID); err != nil {
		return ChildSchedule{}, err
	}
	var cs ChildSchedule
	err := s.db.QueryRow(ctx, `SELECT id, nis, full_name FROM students WHERE school_id = $1 AND id = $2`, schoolID, studentID).
		Scan(&cs.Child.ID, &cs.Child.NIS, &cs.Child.FullName)
	if err != nil {
		return ChildSchedule{}, fmt.Errorf("portal: get child: %w", err)
	}
	var classID string
	err = s.db.QueryRow(ctx, `
		SELECT c.id, c.name, y.name
		FROM enrollments e
		JOIN academic_years y ON y.school_id = e.school_id AND y.id = e.academic_year_id AND y.is_active
		JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
		WHERE e.school_id = $1 AND e.student_id = $2 AND e.end_date IS NULL`,
		schoolID, studentID).Scan(&classID, &cs.Child.ClassName, &cs.Child.YearName)
	if errors.Is(err, pgx.ErrNoRows) {
		return cs, nil
	}
	if err != nil {
		return ChildSchedule{}, fmt.Errorf("portal: current class: %w", err)
	}
	cs.HasClass = true
	if cs.Days, err = schedule.ClassWeek(ctx, s.db, schoolID, classID); err != nil {
		return ChildSchedule{}, err
	}
	return cs, nil
}

type scheduleView struct {
	User  auth.Identity
	Child ChildDetail
	Has   bool
	Week  schedule.WeekView
	Flash string
	Error string
}

func (h *Handler) childSchedule(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	cs, err := h.svc.GetChildSchedule(r.Context(), id.SchoolID, id.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Data siswa tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "child schedule", err)
		return
	}
	v := scheduleView{User: id, Child: cs.Child, Has: cs.HasClass,
		Week: schedule.WeekView{Days: cs.Days, Today: schedule.TodayTab(time.Now()), ShowTeacher: true}}
	h.render.Render(w, r, http.StatusOK, pageSchedule, v)
}
