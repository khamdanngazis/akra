package portal

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/module"
)

// seedChildExam membuat ujian ulangan selesai yang hasilnya sudah dibuka,
// dengan nilai final untuk ani, dan ujian kedua yang tidak diikuti ani
// dengan susulan terjadwal.
func seedChildExam(t *testing.T, f *fixture) {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := f.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	teacher := one(`INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'guru@uji.id', 'x', 'Guru Uji') RETURNING id`, f.schoolA)
	subject := one(`INSERT INTO subjects (school_id, name, code) VALUES ($1, 'Matematika', 'MTK') RETURNING id`, f.schoolA)
	past := time.Now().Add(-48 * time.Hour)
	mkExam := func(name string, released bool) (string, string, string) {
		var rel, by any
		if released {
			rel, by = time.Now().Add(-time.Hour), teacher
		}
		e := one(`INSERT INTO exams (school_id, name, purpose, subject_id, duration_minutes, created_by, owner_id, status, results_mode, results_after_all,
				results_released_at, results_released_by)
			VALUES ($1, $2, 'ulangan', $3, 60, $4, $4, 'selesai', 'nilai_dan_pembahasan', true, $5, $6) RETURNING id`, f.schoolA, name, subject, teacher, rel, by)
		v := one(`INSERT INTO exam_variants (school_id, exam_id, label, seed) VALUES ($1, $2, 'A', 1) RETURNING id`, f.schoolA, e)
		s := one(`INSERT INTO exam_sessions (school_id, exam_id, starts_at, ends_at, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			f.schoolA, e, past, past.Add(time.Hour), teacher)
		return e, v, s
	}
	e1, v1, s1 := mkExam("UH Pecahan", true)
	p := one(`INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) VALUES ($1, $2, $3, '001') RETURNING id`, f.schoolA, s1, f.ani)
	if _, err := f.tx.Exec(ctx, `INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, variant_id, seed, question_order, device_id,
			started_at, deadline_at, submitted_at, submit_reason, status, score_x100, score_status, graded_at)
		VALUES ($1, $2, $3, $4, $5, 1, '{}', gen_random_uuid(), $6, $7, $7, 'peserta', 'dikumpulkan', 8750, 'final', $7)`,
		f.schoolA, e1, s1, p, v1, past, past.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e2, _, s2 := mkExam("UH Desimal", false)
	f.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) VALUES ($1, $2, $3, '001')`, f.schoolA, s2, f.ani)
	mk := one(`INSERT INTO exam_sessions (school_id, exam_id, kind, starts_at, ends_at, created_by) VALUES ($1, $2, 'susulan', $3, $4, $5) RETURNING id`,
		f.schoolA, e2, time.Now().Add(24*time.Hour), time.Now().Add(26*time.Hour), teacher)
	f.tx.Exec(ctx, `INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no) VALUES ($1, $2, $3, '001')`, f.schoolA, mk, f.ani)
}

func TestChildExams(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	seedChildExam(t, f)
	path := "/portal/siswa/" + f.ani + "/ujian"
	// Modul ujian nonaktif: halaman tidak ada.
	if rec := f.getPath(path, f.userSari, f.schoolA, "orang_tua"); rec.Code != http.StatusNotFound {
		t.Fatalf("module off = %d", rec.Code)
	}
	if _, err := f.tx.Exec(ctx, `INSERT INTO school_modules (school_id, module, enabled) VALUES ($1, 'exam', true)`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	module.Forget(f.schoolA) // diubah di luar module.Save
	rec := f.getPath(path, f.userSari, f.schoolA, "orang_tua")
	body := mainContent(rec.Body.String())
	if rec.Code != http.StatusOK || !strings.Contains(body, "UH Pecahan") || !strings.Contains(body, "87,50") {
		t.Fatalf("parent exams = %d\n%s", rec.Code, body)
	}
	// Tidak hadir dengan jadwal susulan; nilai ujian yang belum dibuka tidak tampil.
	if !strings.Contains(body, "UH Desimal") || !strings.Contains(body, "Ujian susulan") || !strings.Contains(body, "Susulan") {
		t.Errorf("absence with makeup missing")
	}
	// Tanpa soal, jawaban, kunci, atau tautan pembahasan.
	for _, s := range []string{"Kunci", "kunci jawaban:", "/u/hasil/", "Jawaban Anda", "pembahasan"} {
		if strings.Contains(body, s) {
			t.Errorf("parent page contains %q", s)
		}
	}
	// Wali kedua anak yang sama boleh; wali anak lain dan sekolah lain tidak.
	if rec := f.getPath(path, f.userAgus, f.schoolA, "orang_tua"); rec.Code != http.StatusOK {
		t.Errorf("second guardian = %d", rec.Code)
	}
	if rec := f.getPath(path, f.userRina, f.schoolA, "orang_tua"); rec.Code != http.StatusNotFound {
		t.Errorf("other family = %d", rec.Code)
	}
	if rec := f.getPath("/portal/siswa/"+f.dewi+"/ujian", f.userSari, f.schoolA, "orang_tua"); rec.Code != http.StatusNotFound {
		t.Errorf("other school child = %d", rec.Code)
	}
	if rec := f.getPath("/portal/siswa/"+f.ani, f.userSari, f.schoolA, "orang_tua"); !strings.Contains(rec.Body.String(), "/portal/siswa/"+f.ani+"/ujian") {
		t.Errorf("child detail missing exams link")
	}
}
