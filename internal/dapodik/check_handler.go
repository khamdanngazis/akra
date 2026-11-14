package dapodik

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
)

type checkStudent struct {
	ID, NIS, FullName string
	Errors, Warnings  []Issue
}

type checkClass struct {
	Name     string
	Students []checkStudent
}

type checkView struct {
	User      auth.Identity
	CanEdit   bool
	Classes   []checkClass
	Checked   int
	WithError int
	Flash     string
	Error     string
}

// checkPage memeriksa semua siswa aktif dan menampilkan yang bermasalah,
// dikelompokkan per kelas. Peran: admin_tu, kepala_sekolah.
func (h *Handler) checkPage(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	rows, err := h.db.Query(ctx, `
		SELECT s.id, coalesce(c.name, 'Belum punya kelas'), coalesce(c.grade_level, 0)
		FROM students s
		LEFT JOIN academic_years y ON y.school_id = s.school_id AND y.is_active
		LEFT JOIN enrollments e ON e.student_id = s.id AND e.academic_year_id = y.id AND e.end_date IS NULL
		LEFT JOIN classes c ON c.id = e.class_id
		WHERE s.school_id = $1 AND s.status = 'aktif'
		ORDER BY c.name NULLS LAST, lower(s.full_name) LIMIT 3000`, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	type pick struct {
		id, class string
		grade     int
	}
	picks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pick, error) {
		var p pick
		var g int16
		err := row.Scan(&p.id, &p.class, &g)
		p.grade = int(g)
		return p, err
	})
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	view := checkView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Flash: auth.PopFlash(ctx, h.sessions)}
	today := clock.Today()
	byClass := map[string]int{}
	for _, p := range picks {
		in, err := load(ctx, h.db, h.keys, id.SchoolID, p.id)
		if err != nil {
			h.serverError(w, r, "load student", err)
			return
		}
		view.Checked++
		issues := Check(in, p.grade, today)
		if len(issues) == 0 {
			continue
		}
		cs := checkStudent{ID: in.Student.ID, NIS: in.Student.NIS, FullName: in.Student.FullName}
		for _, is := range issues {
			if is.Level == LevelError {
				cs.Errors = append(cs.Errors, is)
			} else {
				cs.Warnings = append(cs.Warnings, is)
			}
		}
		if len(cs.Errors) > 0 {
			view.WithError++
		}
		i, ok := byClass[p.class]
		if !ok {
			i = len(view.Classes)
			byClass[p.class] = i
			view.Classes = append(view.Classes, checkClass{Name: p.class})
		}
		view.Classes[i].Students = append(view.Classes[i].Students, cs)
	}
	h.render.Render(w, r, http.StatusOK, "dapodik/check", view)
}
