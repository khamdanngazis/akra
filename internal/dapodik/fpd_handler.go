package dapodik

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
	"akra/internal/students"
)

// maxFPDClass membatasi jumlah siswa per unduhan F-PD kelas agar PDF dibuat
// langsung tanpa job latar belakang.
const maxFPDClass = 100

func (h *Handler) writeFPD(w http.ResponseWriter, r *http.Request, schoolName, filename string, inputs []Input) {
	var buf bytes.Buffer
	if err := WriteFPD(&buf, schoolName, inputs, time.Now().In(clock.Jakarta), true); err != nil {
		h.serverError(w, r, "write fpd", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(buf.Bytes())
}

// studentFPD mengunduh F-PD terisi satu siswa. Peran: admin_tu,
// kepala_sekolah (memuat NIK utuh).
func (h *Handler) studentFPD(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	in, err := load(r.Context(), h.db, h.keys, id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, students.ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "load student", err)
		return
	}
	h.writeFPD(w, r, id.SchoolName, "formulir-pd-"+in.Student.NIS+".pdf", []Input{in})
}

// classFPD mengunduh F-PD terisi semua siswa aktif satu kelas, satu siswa
// per halaman. Peran: admin_tu, kepala_sekolah.
func (h *Handler) classFPD(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	classID := chi.URLParam(r, "classID")
	if !database.IsUUID(classID) {
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	}
	var className string
	err := h.db.QueryRow(r.Context(), `SELECT name FROM classes WHERE school_id = $1 AND id = $2`, id.SchoolID, classID).Scan(&className)
	if errors.Is(err, pgx.ErrNoRows) {
		page.NotFound(h.render, w, r, id, "Kelas tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "get class", err)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT s.id FROM enrollments e JOIN students s ON s.id = e.student_id
		WHERE e.school_id = $1 AND e.class_id = $2 AND e.end_date IS NULL ORDER BY lower(s.full_name) LIMIT $3`, id.SchoolID, classID, maxFPDClass+1)
	if err != nil {
		h.serverError(w, r, "list class students", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		h.serverError(w, r, "list class students", err)
		return
	}
	if len(ids) > maxFPDClass {
		page.NotFound(h.render, w, r, id, "Kelas ini terlalu besar untuk diunduh sekaligus. Unduh per siswa.")
		return
	}
	var inputs []Input
	for _, sid := range ids {
		in, err := load(r.Context(), h.db, h.keys, id.SchoolID, sid)
		if err != nil {
			h.serverError(w, r, "load student", err)
			return
		}
		inputs = append(inputs, in)
	}
	if len(inputs) == 0 {
		inputs = []Input{{}}
	}
	h.writeFPD(w, r, id.SchoolName, "formulir-pd-kelas-"+className+".pdf", inputs)
}

// blankFPD mengunduh F-PD kosong untuk diisi tangan. Peran: admin_tu,
// kepala_sekolah.
func (h *Handler) blankFPD(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.writeFPD(w, r, id.SchoolName, "formulir-pd-kosong.pdf", []Input{{}})
}
