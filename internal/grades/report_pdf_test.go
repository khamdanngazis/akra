package grades

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"akra/internal/attendance"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

func TestSemesterRange(t *testing.T) {
	start, end := day("2026-07-13"), day("2027-06-25")
	if f, to := semesterRange(1, start, end); !f.Equal(start) || !to.Equal(day("2026-12-31")) {
		t.Errorf("semester 1: %s – %s", f, to)
	}
	if f, to := semesterRange(2, start, end); !f.Equal(day("2027-01-01")) || !to.Equal(end) {
		t.Errorf("semester 2: %s – %s", f, to)
	}
	for in, want := range map[string]int{"2026-07-01T09:00:00+07:00": 1, "2026-12-31T23:00:00+07:00": 1, "2027-01-01T00:30:00+07:00": 2, "2026-06-30T18:00:00Z": 1} {
		ts, _ := time.Parse(time.RFC3339, in)
		if got := DefaultSemester(ts); got != want {
			t.Errorf("DefaultSemester(%s) = %d, want %d", in, got, want)
		}
	}
}

func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

// cardFixture: siswa 0 lengkap di IPA (80/90/70 → 79) dan Matematika (UTS
// 88); siswa 1 belum punya UAS IPA dan nilai Matematika. Kehadiran harian
// 1A: sakit 15 Sep, alpa 1 Okt (semester 1), izin 1 Feb (semester 2).
func newCardFixture(t *testing.T) *recapFixture {
	t.Helper()
	f := newRecapFixture(t)
	ctx := context.Background()
	e0, e1 := f.enrollments[0], f.enrollments[1]
	f.grade(t, f.ipaComps[0], map[string]string{e0: "80", e1: "85"})
	f.grade(t, f.ipaComps[1], map[string]string{e0: "90", e1: "85"})
	f.grade(t, f.ipaComps[2], map[string]string{e0: "70"})
	f.grade(t, f.uts, map[string]string{e0: "88"})
	svc := attendance.NewService(f.tx)
	actor := school.Actor{SchoolID: f.schoolA, UserID: f.guru3}
	for date, status := range map[string]string{"2026-09-15": attendance.StatusSick, "2026-10-01": attendance.StatusAbsent, "2027-02-01": attendance.StatusExcused} {
		s, _, err := svc.OpenSession(ctx, actor, f.class1A, "", day(date))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.SaveStatuses(ctx, actor, s.ID, map[string]string{e0: status}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.tx.Exec(ctx, `UPDATE schools SET address = 'Jl. Melati 1, Bandung' WHERE id = $1`, f.schoolA); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(ctx, f.tx, f.schoolA, settings.ReportPrincipalName, "Hj. Siti Aminah, S.Pd."); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *recapFixture) card(t *testing.T, enrollmentID string, semester int) (ReportCard, error) {
	t.Helper()
	e, err := GetCardEnrollment(context.Background(), f.tx, f.schoolA, enrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	return BuildReportCard(context.Background(), f.tx, f.schoolA, e, semester, day("2026-12-19"))
}

func TestBuildReportCard_MatchesRecap(t *testing.T) {
	f := newCardFixture(t)
	ctx := context.Background()
	card, err := f.card(t, f.enrollments[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	if card.StudentName != "Siswa 01" || card.ClassName != "1A" || card.YearName != "2026/2027" || card.KKMLabel != "KKM" ||
		card.PrincipalName != "Hj. Siti Aminah, S.Pd." || card.SignerTitle != "Kepala Sekolah" || card.HomeroomName != "guru3@a.uji" {
		t.Errorf("identity: %+v", card)
	}
	// Angka sama dengan rekap T72 untuk setiap mapel.
	if len(card.Subjects) != 2 || card.Subjects[0].Name != "IPA" || card.Subjects[1].Name != "Matematika" {
		t.Fatalf("subjects: %+v", card.Subjects)
	}
	summary, err := ClassSummary(ctx, f.tx, f.schoolA, f.class(t))
	if err != nil {
		t.Fatal(err)
	}
	for i, sub := range summary.Subjects {
		var row StudentGrades
		for _, r := range sub.Rows {
			if r.EnrollmentID == f.enrollments[0] {
				row = r
			}
		}
		got := card.Subjects[i]
		if got.Score != row.Result.Score || got.Letter != row.Letter || got.KKM != sub.KKM {
			t.Errorf("%s: card %+v, recap %d %s KKM %d", sub.Subject.Name, got, row.Result.Score, row.Letter, sub.KKM)
		}
	}
	if card.Subjects[0].Score != 79 || card.Subjects[0].Letter != "C" || card.Subjects[1].Score != 88 || card.Subjects[1].Letter != "B" {
		t.Errorf("scores: %+v", card.Subjects)
	}
	// Kehadiran semester 1 vs semester 2 (rentang tanggal berbeda).
	if a := card.Attendance; a.Sakit != 1 || a.Alpa != 1 || a.Izin != 0 || !card.AttendanceTo.Equal(day("2026-12-31")) {
		t.Errorf("semester 1 attendance: %+v", a)
	}
	sem2, err := f.card(t, f.enrollments[0], 2)
	if err != nil || sem2.Attendance.Izin != 1 || sem2.Attendance.Sakit != 0 || sem2.Subjects[0].Score != 79 {
		t.Errorf("semester 2: %v %+v", err, sem2.Attendance)
	}
	if _, err := f.card(t, f.enrollments[0], 3); !errors.Is(err, ErrInvalidSemester) {
		t.Errorf("semester 3: %v", err)
	}
}

func TestBuildReportCard_Incomplete(t *testing.T) {
	f := newCardFixture(t)
	_, err := f.card(t, f.enrollments[1], 1)
	var ie *IncompleteError
	if !errors.As(err, &ie) || len(ie.Subjects) != 2 || ie.Subjects[0] != "IPA (1 komponen belum dinilai)" || ie.Subjects[1] != "Matematika (1 komponen belum dinilai)" {
		t.Fatalf("incomplete: %v %+v", err, ie)
	}
	// Mapel tanpa komponen nilai juga menahan rapor, bahkan untuk siswa yang lengkap.
	ctx := context.Background()
	var seni string
	if err := f.tx.QueryRow(ctx, `INSERT INTO subjects (school_id, name) VALUES ($1, 'Seni') RETURNING id`, f.schoolA).Scan(&seni); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
		f.schoolA, f.year, f.class1A, seni, f.guru1); err != nil {
		t.Fatal(err)
	}
	_, err = f.card(t, f.enrollments[0], 1)
	if !errors.As(err, &ie) || len(ie.Subjects) != 1 || !strings.Contains(ie.Subjects[0], "Seni (komponen nilai belum diatur)") {
		t.Errorf("unconfigured subject: %v %+v", err, ie)
	}
	// Siswa yang sudah pindah, sekolah lain, ID rusak: tidak ditemukan.
	if _, err := f.tx.Exec(ctx, `UPDATE enrollments SET end_date = '2026-09-30' WHERE id = $1`, f.enrollments[2]); err != nil {
		t.Fatal(err)
	}
	for _, tc := range [][2]string{{f.schoolA, f.enrollments[2]}, {f.schoolB, f.enrollments[0]}, {f.schoolA, "bukan"}} {
		if _, err := GetCardEnrollment(ctx, f.tx, tc[0], tc[1]); !errors.Is(err, ErrEnrollmentNotFound) {
			t.Errorf("GetCardEnrollment(%s): %v", tc[1], err)
		}
	}
}

func TestWriteReportCardPDF(t *testing.T) {
	f := newCardFixture(t)
	card, err := f.card(t, f.enrollments[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := WriteReportCardPDF(&a, card, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteReportCardPDF(&b, card, false); err != nil {
		t.Fatal(err)
	}
	pdf := a.String()
	if !strings.HasPrefix(pdf, "%PDF-") || !strings.Contains(pdf, "%%EOF") {
		t.Fatal("not a PDF")
	}
	for _, want := range []string{"LAPORAN HASIL BELAJAR SISWA", "Siswa 01", `1 \(Ganjil\)`, "IPA", "Matematika", "(79)", "(88)", "(KKM)",
		"Hj. Siti Aminah, S.Pd.", "Kepala Sekolah,", "guru3@a.uji", "Jl. Melati 1, Bandung", "Tanpa keterangan", "19 Desember 2026"} {
		if !strings.Contains(pdf, want) {
			t.Errorf("PDF misses %q", want)
		}
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("same card rendered differently")
	}
	if got := ReportCardFilename(card); got != "rapor-001-1A-smt1.pdf" {
		t.Errorf("filename %q", got)
	}
	// Format Merdeka: sebutan KKTP.
	card.KKMLabel = "KKTP"
	var m bytes.Buffer
	if err := WriteReportCardPDF(&m, card, false); err != nil || !strings.Contains(m.String(), "(KKTP)") {
		t.Errorf("merdeka label: %v", err)
	}
}

func TestReportCardHandler(t *testing.T) {
	f := newCardFixture(t)
	e := newEnvFor(t, f.fixture)
	ctx := context.Background()
	pdfPath := "/nilai/rapor/" + f.enrollments[0] + ".pdf?semester=1"

	for i, user := range [][2]string{{f.actor.UserID, "admin_tu"}, {f.guru3, "guru"}} {
		rec := e.do(http.MethodGet, pdfPath, user[0], user[1], nil)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) ||
			!strings.Contains(rec.Header().Get("Content-Disposition"), "rapor-001-1A-smt1.pdf") {
			t.Fatalf("%s: status = %d, type %q", user[1], rec.Code, rec.Header().Get("Content-Type"))
		}
		// Setiap cetak tercatat.
		if n := f.audits(ActionReportCardPrint); n != i+1 {
			t.Errorf("print audits = %d, want %d", n, i+1)
		}
	}
	var after string
	if err := f.tx.QueryRow(ctx, `SELECT after::text FROM audit_logs WHERE school_id = $1 AND action = $2 ORDER BY created_at DESC, id DESC LIMIT 1`,
		f.schoolA, ActionReportCardPrint).Scan(&after); err != nil || !strings.Contains(after, `"semester": 1`) || strings.Contains(after, "Siswa") {
		t.Errorf("audit: %v %s", err, after)
	}
	// Belum lengkap: 422 menyebut mapelnya, tanpa audit.
	rec := e.do(http.MethodGet, "/nilai/rapor/"+f.enrollments[1]+".pdf", f.actor.UserID, "admin_tu", nil)
	if body := rec.Body.String(); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "IPA (1 komponen belum dinilai)") || !strings.Contains(body, "Matematika") {
		t.Errorf("incomplete: status = %d", rec.Code)
	}
	if n := f.audits(ActionReportCardPrint); n != 2 {
		t.Errorf("incomplete card was audited: %d", n)
	}
	// Guru mapel (bukan wali), kepala sekolah, dan peran lain: 403.
	for _, user := range [][2]string{{f.guru1, "guru"}, {f.guru2, "guru"}, {f.actor.UserID, "kepala_sekolah"}, {f.actor.UserID, "bendahara"}, {f.actor.UserID, "orang_tua"}} {
		if rec := e.do(http.MethodGet, pdfPath, user[0], user[1], nil); rec.Code != http.StatusForbidden {
			t.Errorf("%v: status = %d, want 403", user, rec.Code)
		}
	}
	// Semester tidak valid 400; ID rusak atau tidak ada 404.
	if rec := e.do(http.MethodGet, "/nilai/rapor/"+f.enrollments[0]+".pdf?semester=3", f.actor.UserID, "admin_tu", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("semester 3: status = %d", rec.Code)
	}
	for _, id := range []string{"bukan", "00000000-0000-0000-0000-000000000000"} {
		if rec := e.do(http.MethodGet, "/nilai/rapor/"+id+".pdf", f.actor.UserID, "admin_tu", nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d", id, rec.Code)
		}
	}
	// Ringkasan kelas: daftar cetak untuk admin dan wali kelas, tidak untuk kepala sekolah.
	classPath := "/nilai/rekap?kelas=" + f.class1A
	body := e.do(http.MethodGet, classPath, f.actor.UserID, "admin_tu", nil).Body.String()
	if !strings.Contains(body, "/nilai/rapor/"+f.enrollments[0]+".pdf") || strings.Contains(body, "/nilai/rapor/"+f.enrollments[1]+".pdf") || !strings.Contains(body, "Belum siap") {
		t.Error("print list for admin")
	}
	if body := e.do(http.MethodGet, classPath, f.actor.UserID, "kepala_sekolah", nil).Body.String(); strings.Contains(body, "Cetak rapor") {
		t.Error("principal sees print list")
	}
}
