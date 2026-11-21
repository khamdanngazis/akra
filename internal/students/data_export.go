package students

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
)

// Ekspor data pribadi siswa (hak akses subjek data, UU PDP; T153): semua
// data tentang satu siswa dalam satu berkas JSON, untuk diserahkan kepada
// siswa/orang tua yang memintanya. Hanya admin_tu; setiap unduhan tercatat
// di audit karena memuat data rinci yang tidak di-mask.

// ActionDataExport adalah aksi audit ekspor data pribadi siswa.
const ActionDataExport = "student.data_export"

// DataExport adalah isi berkas ekspor.
type DataExport struct {
	GeneratedAt time.Time         `json:"dibuat"`
	Student     exportStudent     `json:"siswa"`
	Profile     map[string]string `json:"data_rinci"`
	Guardians   []exportGuardian  `json:"wali"`
	Enrollments []exportRow       `json:"riwayat_kelas"`
	Attendance  []exportRow       `json:"absensi"`
	Grades      []exportRow       `json:"nilai"`
	Invoices    []exportRow       `json:"tagihan"`
	Payments    []exportRow       `json:"pembayaran"`
	Exams       any               `json:"ujian"`
}

type exportStudent struct {
	NIS       string     `json:"nis"`
	FullName  string     `json:"nama"`
	BirthDate *time.Time `json:"tanggal_lahir,omitempty"`
	Gender    string     `json:"jenis_kelamin,omitempty"`
	Status    string     `json:"status"`
}

type exportGuardian struct {
	FullName     string `json:"nama"`
	Relationship string `json:"hubungan"`
	Phone        string `json:"telepon,omitempty"`
	Email        string `json:"email,omitempty"`
	Address      string `json:"alamat,omitempty"`
}

// exportRow adalah satu baris data dengan kolom bernama.
type exportRow map[string]any

// queryRows menjalankan query dan mengembalikan baris sebagai map kolom.
func queryRows(ctx context.Context, q database.DBTX, sql string, args ...any) ([]exportRow, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []exportRow{}
	fields := rows.FieldDescriptions()
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := exportRow{}
		for i, f := range fields {
			row[f.Name] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ExamExporter mengembalikan data ujian siswa untuk ekspor (dipasang dari
// modul ujian lewat WithExamExport, agar paket ini tidak bergantung pada
// modul ujian).
type ExamExporter func(ctx context.Context, q database.DBTX, schoolID, studentID string) (any, error)

// WithExamExport memasang pengekspor data ujian (T153).
func (h *Handler) WithExamExport(f ExamExporter) *Handler {
	h.examExport = f
	return h
}

// BuildDataExport menyusun semua data siswa studentID milik schoolID.
func BuildDataExport(ctx context.Context, q database.DBTX, profiles *ProfileService, exams ExamExporter, schoolID, studentID string, now time.Time) (DataExport, error) {
	st, err := GetByID(ctx, q, schoolID, studentID)
	if err != nil {
		return DataExport{}, err
	}
	d := DataExport{GeneratedAt: now, Student: exportStudent{NIS: st.NIS, FullName: st.FullName, BirthDate: st.BirthDate, Gender: st.Gender, Status: st.Status},
		Profile: map[string]string{}}
	if profiles != nil {
		p, err := profiles.Get(ctx, schoolID, studentID)
		if err != nil {
			return d, err
		}
		for _, f := range ProfileFields {
			if v := p.Secret(f.Key).Reveal(); v != "" {
				d.Profile[f.Key] = v
			}
		}
	}
	gs, err := ListByStudent(ctx, q, schoolID, studentID)
	if err != nil {
		return d, err
	}
	d.Guardians = []exportGuardian{}
	for _, g := range gs {
		d.Guardians = append(d.Guardians, exportGuardian{FullName: g.FullName, Relationship: g.Relationship, Phone: g.Phone, Email: g.Email, Address: g.Address})
	}
	sections := []struct {
		dst *[]exportRow
		sql string
	}{
		{&d.Enrollments, `SELECT y.name AS tahun_ajaran, c.name AS kelas, e.start_date AS mulai, e.end_date AS selesai
			FROM enrollments e JOIN classes c ON c.id = e.class_id JOIN academic_years y ON y.id = e.academic_year_id
			WHERE e.school_id = $1 AND e.student_id = $2 ORDER BY e.start_date`},
		{&d.Attendance, `SELECT s.session_date AS tanggal, coalesce(sub.name, '') AS mapel, r.status, coalesce(r.note, '') AS catatan
			FROM attendance_records r JOIN attendance_sessions s ON s.id = r.session_id JOIN enrollments e ON e.id = r.enrollment_id
			LEFT JOIN subjects sub ON sub.id = s.subject_id
			WHERE r.school_id = $1 AND e.student_id = $2 ORDER BY s.session_date`},
		{&d.Grades, `SELECT y.name AS tahun_ajaran, sub.name AS mapel, gc.name AS komponen, g.score::text AS nilai
			FROM grades g JOIN enrollments e ON e.id = g.enrollment_id JOIN grade_components gc ON gc.id = g.component_id
			JOIN subjects sub ON sub.id = gc.subject_id JOIN academic_years y ON y.id = g.academic_year_id
			WHERE g.school_id = $1 AND e.student_id = $2 ORDER BY y.start_date, sub.name, gc.sort_order`},
		{&d.Invoices, `SELECT i.period AS periode, i.due_date AS jatuh_tempo, i.total_amount AS jumlah, i.status, i.voided_at AS dibatalkan
			FROM invoices i JOIN enrollments e ON e.id = i.enrollment_id
			WHERE i.school_id = $1 AND e.student_id = $2 ORDER BY i.period`},
		{&d.Payments, `SELECT p.paid_on AS tanggal, p.amount AS jumlah, p.method AS cara, p.receipt_number AS kuitansi
			FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN enrollments e ON e.id = i.enrollment_id
			WHERE p.school_id = $1 AND e.student_id = $2 ORDER BY p.paid_on`},
	}
	for _, s := range sections {
		if *s.dst, err = queryRows(ctx, q, s.sql, schoolID, studentID); err != nil {
			return d, fmt.Errorf("students: data export: %w", err)
		}
	}
	d.Exams = []any{}
	if exams != nil {
		if d.Exams, err = exams(ctx, q, schoolID, studentID); err != nil {
			return d, err
		}
	}
	return d, nil
}

// dataExport mengunduh data pribadi siswa sebagai JSON. Peran: admin_tu.
func (h *Handler) dataExport(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	studentID := chi.URLParam(r, "id")
	d, err := BuildDataExport(r.Context(), h.svc.db, h.profiles, h.examExport, id.SchoolID, studentID, time.Now())
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentMissing)
		return
	}
	if err != nil {
		h.serverError(w, r, "data export", err)
		return
	}
	if err := pgx.BeginFunc(r.Context(), h.svc.db, func(tx pgx.Tx) error {
		return audit.Record(r.Context(), tx, audit.Entry{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r), Action: ActionDataExport,
			EntityType: "student", EntityID: studentID})
	}); err != nil {
		h.serverError(w, r, "data export audit", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="data-pribadi-`+d.Student.NIS+`.json"`)
	w.Header().Set("Cache-Control", "private, no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(d)
}
