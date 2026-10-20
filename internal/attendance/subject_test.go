package attendance

// Uji absensi per mapel (T68).

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"akra/internal/school"
)

func TestSubjectSessions_Constraints(t *testing.T) {
	tx, f := txFixture(t)
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	subject := `INSERT INTO subjects (school_id, name) VALUES ($1, $2) RETURNING id`
	mtk, ipa := one(subject, f.schoolA, "Matematika"), one(subject, f.schoolA, "IPA")
	subjectB := one(subject, f.schoolB, "Matematika")
	date := day("2026-09-15")

	daily, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, "", date, f.teacher)
	if err != nil {
		t.Fatal(err)
	}
	// Sesi harian dan sesi per mapel di tanggal yang sama berdampingan.
	math, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, mtk, date, f.teacher)
	if err != nil || math.ID == daily.ID || math.SubjectID != mtk || math.SubjectName != "Matematika" || daily.SubjectID != "" {
		t.Fatalf("subject session: %v %+v", err, math)
	}
	science, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, ipa, date, f.teacher)
	if err != nil || science.ID == math.ID {
		t.Fatalf("second subject: %v", err)
	}
	// Membuka lagi mengembalikan sesi yang sama.
	again, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, mtk, date, f.teacher)
	if err != nil || again.ID != math.ID {
		t.Errorf("reopen: %v %s", err, again.ID)
	}
	dailyAgain, err := GetOrCreateSession(ctx, tx, f.schoolA, f.class1A, "", date, f.teacher)
	if err != nil || dailyAgain.ID != daily.ID {
		t.Errorf("reopen daily: %v", err)
	}
	// Dua sesi untuk kelas, tanggal, dan mapel yang sama ditolak database.
	for _, sub := range []any{mtk, nil} {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := q.Exec(ctx, `INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, subject_id, session_date, created_by)
				VALUES ($1, $2, $3, $4, '2026-09-15', $5)`, f.schoolA, f.year, f.class1A, sub, f.teacher)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "attendance_sessions_class_date_subject_key") {
			t.Errorf("duplicate session (subject %v): error = %v", sub, err)
		}
	}
	// Mapel sekolah lain atau ID rusak.
	for _, sub := range []string{subjectB, "bukan-uuid", "00000000-0000-0000-0000-000000000001"} {
		err := sp(t, tx, func(q pgx.Tx) error {
			_, err := GetOrCreateSession(ctx, q, f.schoolA, f.class1A, sub, date, f.teacher)
			return err
		})
		if !errors.Is(err, ErrSubjectNotFound) {
			t.Errorf("subject %s: error = %v, want ErrSubjectNotFound", sub, err)
		}
	}
}

// TestSubjectRecap_DailyUnchanged: rekap harian tidak berubah setelah ada
// sesi per mapel, dan rekap mapel hanya menghitung sesi mapel itu.
func TestSubjectRecap_DailyUnchanged(t *testing.T) {
	tx, f := txFixture(t)
	ctx := context.Background()
	var mtk string
	if err := tx.QueryRow(ctx, `INSERT INTO subjects (school_id, name) VALUES ($1, 'Matematika') RETURNING id`, f.schoolA).Scan(&mtk); err != nil {
		t.Fatal(err)
	}
	svc := NewService(tx)
	actor := school.Actor{SchoolID: f.schoolA, UserID: f.teacher}
	mark := func(subjectID, date, status string) {
		t.Helper()
		s, records, err := svc.OpenSession(ctx, actor, f.class1A, subjectID, day(date))
		if err != nil {
			t.Fatal(err)
		}
		statuses := map[string]string{}
		for _, r := range records {
			statuses[r.EnrollmentID] = status
		}
		if _, _, err := svc.SaveStatuses(ctx, actor, s.ID, statuses); err != nil {
			t.Fatal(err)
		}
	}
	from, to := day("2026-09-01"), day("2026-09-30")
	mark("", "2026-09-14", StatusPresent)
	mark("", "2026-09-15", StatusSick)
	beforeStudents, err := SummaryByStudent(ctx, tx, f.schoolA, f.class1A, "", from, to)
	if err != nil {
		t.Fatal(err)
	}
	beforeClass, err := SummaryByClass(ctx, tx, f.schoolA, f.class1A, "", from, to)
	if err != nil {
		t.Fatal(err)
	}

	mark(mtk, "2026-09-15", StatusAbsent)
	mark(mtk, "2026-09-16", StatusExcused)

	afterStudents, _ := SummaryByStudent(ctx, tx, f.schoolA, f.class1A, "", from, to)
	afterClass, _ := SummaryByClass(ctx, tx, f.schoolA, f.class1A, "", from, to)
	if len(afterStudents) != len(beforeStudents) || afterClass != beforeClass {
		t.Fatalf("daily recap changed: %+v -> %+v", beforeClass, afterClass)
	}
	for i := range beforeStudents {
		if afterStudents[i] != beforeStudents[i] {
			t.Errorf("student %s: %+v -> %+v", beforeStudents[i].FullName, beforeStudents[i], afterStudents[i])
		}
	}
	if beforeClass.Sessions != 2 || beforeClass.Hadir != 2 || beforeClass.Sakit != 2 {
		t.Errorf("daily class summary: %+v", beforeClass)
	}
	math, err := SummaryByClass(ctx, tx, f.schoolA, f.class1A, mtk, from, to)
	if err != nil || math.Sessions != 2 || math.Alpa != 2 || math.Izin != 2 || math.Hadir != 0 {
		t.Errorf("subject class summary: %v %+v", err, math)
	}
	if has, _ := HasSubjectSessions(ctx, tx, f.schoolA, f.class1A); !has {
		t.Error("HasSubjectSessions = false")
	}
	if has, _ := HasSubjectSessions(ctx, tx, f.schoolA, f.class1B); has {
		t.Error("HasSubjectSessions(1B) = true")
	}
	if _, err := SummaryByStudent(ctx, tx, f.schoolA, f.class1A, "x", from, to); !errors.Is(err, ErrSubjectNotFound) {
		t.Errorf("bad subject id: %v", err)
	}
}

func TestSubjectAttendance_Handler(t *testing.T) {
	e := newHEnv(t)
	ctx := context.Background()
	var mtk, ipa string
	subject := `INSERT INTO subjects (school_id, name) VALUES ($1, $2) RETURNING id`
	if err := e.tx.QueryRow(ctx, subject, e.schoolA, "Matematika").Scan(&mtk); err != nil {
		t.Fatal(err)
	}
	if err := e.tx.QueryRow(ctx, subject, e.schoolA, "IPA").Scan(&ipa); err != nil {
		t.Fatal(err)
	}
	// guru2 (wali 1B) mengajar Matematika di 1A; IPA di 1A belum punya guru.
	if _, err := e.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
		SELECT school_id, academic_year_id, id, $2, $3 FROM classes WHERE id = $1`, e.class1A, mtk, e.guru2); err != nil {
		t.Fatal(err)
	}
	date := e.today.Format(dateLayout)
	subjectForm := "/absensi?kelas=" + e.class1A + "&tanggal=" + date + "&mapel="

	// Nonaktif: form per mapel ditolak, form harian tanpa pilihan mapel.
	if rec := e.do(http.MethodGet, subjectForm+mtk, e.guru2, "guru", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("subject form while disabled: status = %d", rec.Code)
	}
	if body := e.do(http.MethodGet, "/absensi?kelas="+e.class1A, e.guru1, "guru", nil).Body.String(); strings.Contains(body, "mode-tabs") {
		t.Error("mode tabs shown while disabled")
	}

	// Mengaktifkan: hanya admin/TU, isian harus aktif/nonaktif.
	for _, role := range []string{"guru", "kepala_sekolah", "bendahara"} {
		if rec := e.do(http.MethodPost, settingsPath, e.guru1, role, url.Values{"per_mapel": {"aktif"}}); rec.Code != http.StatusForbidden {
			t.Errorf("settings as %s: status = %d", role, rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, settingsPath, e.admin, "admin_tu", url.Values{"per_mapel": {"ya"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid setting: status = %d", rec.Code)
	}
	for range 2 { // yang kedua tidak berubah: tanpa audit
		if rec := e.do(http.MethodPost, settingsPath, e.admin, "admin_tu", url.Values{"per_mapel": {"aktif"}}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != subjectsPage {
			t.Fatalf("enable: status = %d", rec.Code)
		}
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = $2`, e.schoolA, ActionSettingsUpdate).Scan(&audits)
	if audits != 1 {
		t.Errorf("settings audits = %d, want 1", audits)
	}

	// Guru pengampu membuka dan menyimpan absensi mapel.
	rec := e.do(http.MethodGet, subjectForm+mtk, e.guru2, "guru", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Absensi 1A · Matematika") || !strings.Contains(body, `aria-current="page">Matematika</a>`) {
		t.Fatalf("subject form: status = %d", rec.Code)
	}
	sessionID := sessionAction.FindStringSubmatch(body)[1]
	ids := statusField.FindAllStringSubmatch(body, -1)
	if len(ids) != 30 {
		t.Fatalf("subject form rows = %d", len(ids))
	}
	save := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru2, "guru", url.Values{"s_" + ids[0][1]: {"alpa"}})
	if save.Code != http.StatusSeeOther || !strings.Contains(save.Header().Get("Location"), "mapel="+mtk) || e.counts(sessionID)["alpa"] != 1 {
		t.Fatalf("save subject session: status = %d, location %q", save.Code, save.Header().Get("Location"))
	}
	// Sesi harian kelas yang sama di tanggal yang sama tetap terpisah.
	dailyID, _ := e.open(e.class1A, date, e.guru1)
	if dailyID == sessionID || e.counts(dailyID)["alpa"] != 0 {
		t.Error("daily session shares records with the subject session")
	}

	// Guru tanpa penugasan mapel itu ditolak, termasuk wali kelasnya.
	for _, tc := range []struct{ user, subject string }{{e.guru1, mtk}, {e.guru1, ipa}, {e.guru2, ipa}} {
		if rec := e.do(http.MethodGet, subjectForm+tc.subject, tc.user, "guru", nil); rec.Code != http.StatusForbidden {
			t.Errorf("open subject %s as %s: status = %d, want 403", tc.subject, tc.user, rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, "/absensi/"+sessionID, e.guru1, "guru", url.Values{"s_" + ids[1][1]: {"alpa"}}); rec.Code != http.StatusForbidden {
		t.Errorf("save subject session as homeroom teacher: status = %d", rec.Code)
	}
	if e.counts(sessionID)["alpa"] != 1 {
		t.Error("forbidden save changed records")
	}
	var sessions int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM attendance_sessions WHERE class_id = $1 AND subject_id = $2`, e.class1A, ipa).Scan(&sessions)
	if sessions != 0 {
		t.Error("forbidden request created a session")
	}

	// Pilihan jenis absensi: wali kelas hanya harian, guru pengampu harian + mapelnya.
	if body := e.do(http.MethodGet, "/absensi?kelas="+e.class1A+"&tanggal="+date, e.guru1, "guru", nil).Body.String(); !strings.Contains(body, ">Harian</a>") || strings.Contains(body, ">Matematika</a>") {
		t.Error("homeroom teacher modes")
	}

	// Rekap: harian dan per mapel terpisah.
	daily := e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A, e.admin, "admin_tu", nil).Body.String()
	math := e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A+"&mapel="+mtk, e.admin, "admin_tu", nil).Body.String()
	if !strings.Contains(daily, "Absensi harian") || !strings.Contains(math, "Absensi Matematika") || !strings.Contains(math, `value="`+mtk+`" selected`) {
		t.Error("recap subject filter")
	}
	// Mapel yang bukan pilihan kelas diganti rekap harian.
	if body := e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1B+"&mapel="+mtk, e.admin, "admin_tu", nil).Body.String(); !strings.Contains(body, "Absensi harian") {
		t.Error("foreign subject in recap filter not reset to daily")
	}

	// Dinonaktifkan lagi: sesi mapel tetap ada, form mapel ditolak.
	if rec := e.do(http.MethodPost, settingsPath, e.admin, "admin_tu", url.Values{"per_mapel": {"nonaktif"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("disable: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, subjectForm+mtk, e.guru2, "guru", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("subject form after disable: status = %d", rec.Code)
	}
	if e.counts(sessionID)["alpa"] != 1 {
		t.Error("disabling removed subject records")
	}
	// Rekap tetap menawarkan mapel karena kelas sudah punya sesi mapel.
	if body := e.do(http.MethodGet, "/absensi/rekap?kelas="+e.class1A, e.admin, "admin_tu", nil).Body.String(); !strings.Contains(body, `id="mapel"`) {
		t.Error("recap subject filter hidden although subject sessions exist")
	}
}
