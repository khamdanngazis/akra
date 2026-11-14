package dapodik

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// ActionExportPD dicatat setiap ekspor format Daftar PD (memuat NIK).
const ActionExportPD = "dapodik.export_pd"

// maxExport membatasi jumlah siswa per ekspor agar dibuat langsung.
const maxExport = 3000

// exportPD mengunduh Excel format "Daftar Peserta Didik" Dapodik untuk
// siswa aktif (opsional satu kelas). Peran: admin_tu, kepala_sekolah.
func (h *Handler) exportPD(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	classID := r.URL.Query().Get("kelas")
	var classArg any
	if database.IsUUID(classID) {
		classArg = classID
	}
	rows, err := h.db.Query(ctx, `
		SELECT s.id, coalesce(c.name, '')
		FROM students s
		LEFT JOIN academic_years y ON y.school_id = s.school_id AND y.is_active
		LEFT JOIN enrollments e ON e.student_id = s.id AND e.academic_year_id = y.id AND e.end_date IS NULL
		LEFT JOIN classes c ON c.id = e.class_id
		WHERE s.school_id = $1 AND s.status = 'aktif' AND ($2::uuid IS NULL OR e.class_id = $2::uuid)
		ORDER BY c.name NULLS LAST, lower(s.full_name) LIMIT $3`, id.SchoolID, classArg, maxExport)
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	type pick struct{ id, class string }
	picks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pick, error) {
		var p pick
		return p, row.Scan(&p.id, &p.class)
	})
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	var out []ExportRow
	for _, p := range picks {
		in, err := load(ctx, h.db, h.keys, id.SchoolID, p.id)
		if err != nil {
			h.serverError(w, r, "load student", err)
			return
		}
		out = append(out, ExportRow{Input: in, ClassName: p.class})
	}
	if err := pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r),
			Action: ActionExportPD, EntityType: "school", EntityID: id.SchoolID,
			After: map[string]any{"rows": len(out), "with_nik": true, "class_id": classID}})
	}); err != nil {
		h.serverError(w, r, "audit export", err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="daftar-peserta-didik.xlsx"`)
	w.Header().Set("Cache-Control", "private, no-store")
	if err := WritePDExport(w, id.SchoolName, id.FullName, out, time.Now().In(clock.Jakarta)); err != nil {
		h.logger.Error("export pd", "error", err)
	}
}
