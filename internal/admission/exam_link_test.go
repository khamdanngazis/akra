package admission

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/exam"
	"akra/internal/platform/pii"
)

// seedAdmissionExam membuat ujian seleksi sekolah schoolID dengan satu
// paket dan mengembalikan id ujian dan paket.
func (e *env) seedAdmissionExam(t *testing.T, schoolID, userID, name string) (string, string) {
	t.Helper()
	ctx := context.Background()
	var examID, variantID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO exams (school_id, name, purpose, duration_minutes, created_by, owner_id, status)
		VALUES ($1, $2, 'seleksi', 60, $3, $3, 'terjadwal') RETURNING id`, schoolID, name, userID).Scan(&examID); err != nil {
		t.Fatal(err)
	}
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_variants (school_id, exam_id, label, seed) VALUES ($1, $2, 'A', 1) RETURNING id`,
		schoolID, examID).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	return examID, variantID
}

func TestOnlineAdmissionTest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p, tr := e.openPeriod(t)
	if _, err := e.tx.Exec(ctx, `UPDATE admission_tracks SET uses_test = true, method = 'tes' WHERE id = $1`, tr.ID); err != nil {
		t.Fatal(err)
	}
	apps := NewApplicantService(e.tx, pii.DevKeyring(), e.svc)
	present, absent, late := e.submitted(t, apps, p, tr), e.submitted(t, apps, p, tr), e.submitted(t, apps, p, tr)
	for _, a := range []Applicant{present, absent} {
		if _, err := apps.Transition(ctx, e.actor, a.ID, ApplicantVerified, ""); err != nil {
			t.Fatal(err)
		}
	}
	examID, variantID := e.seedAdmissionExam(t, e.schoolA, userA, "TPA Online")
	const userB = "88888888-8888-8888-8888-888888888888"
	if _, err := e.tx.Exec(ctx, `INSERT INTO users (id, school_id, email, password_hash, full_name) VALUES ($1, $2, 'tu@b.test', 'x', 'TU B')`, userB, e.schoolB); err != nil {
		t.Fatal(err)
	}
	otherExam, _ := e.seedAdmissionExam(t, e.schoolB, userB, "TPA Sekolah B")
	base := "/pendaftaran/periode/" + p.ID + "/tes"

	// Ujian sekolah lain tidak bisa dipilih (tidak tampil, dan ditolak).
	page := e.do(http.MethodGet, base, "admin_tu", nil).Body.String()
	if !strings.Contains(page, "TPA Online") || strings.Contains(page, "TPA Sekolah B") {
		t.Fatalf("exam options must list own school's selection exams only")
	}
	rec := e.do(http.MethodPost, base, "admin_tu", url.Values{"test_name": {"TPA"}, "weight": {"100"}, "method": {"online"}, "exam_id": {otherExam}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Ujian tidak ditemukan") {
		t.Fatalf("other school's exam = %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, base, "admin_tu", url.Values{"test_name": {"TPA"}, "weight": {"100"}, "method": {"online"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("online without exam = %d", rec.Code)
	}
	if rec := e.do(http.MethodPost, base, "admin_tu", url.Values{"test_name": {"TPA"}, "weight": {"100"}, "max_score": {"50"}, "method": {"online"}, "exam_id": {examID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add online test = %d %s", rec.Code, rec.Body.String())
	}
	tests, _ := NewSelectionService(e.tx).Tests(ctx, e.schoolA, p.ID)
	if len(tests) != 1 || tests[0].ExamID != examID || tests[0].ExamName != "TPA Online" {
		t.Fatalf("tests = %+v", tests)
	}
	test := tests[0]

	// Sesi gelombang (mulai 1 jam lagi), peserta: pendaftar terverifikasi.
	start := time.Now().Add(time.Hour)
	var sessionID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, period_id, location, starts_at, ends_at, created_by)
		VALUES ($1, $2, $3, 'jarak_jauh', $4, $5, $6) RETURNING id`, e.schoolA, examID, p.ID, start, start.Add(2*time.Hour), userA).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	// Belum ada peserta: tab tes menandai pendaftar yang belum masuk sesi.
	if body := e.do(http.MethodGet, base, "admin_tu", nil).Body.String(); !strings.Contains(body, "2 pendaftar terverifikasi belum masuk sesi") {
		t.Fatalf("missing applicants notice not shown")
	}
	if rec := e.do(http.MethodPost, base+"/"+test.ID+"/peserta", "admin_tu", url.Values{"session_id": {sessionID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add applicants = %d", rec.Code)
	}
	// Pendaftar yang diverifikasi belakangan: kartu langkah berikutnya
	// mengingatkan, lalu masuk sesi dari tab tes.
	if _, err := apps.Transition(ctx, e.actor, late.ID, ApplicantVerified, ""); err != nil {
		t.Fatal(err)
	}
	if body := e.do(http.MethodGet, "/pendaftaran/"+late.ID, "admin_tu", nil).Body.String(); !strings.Contains(body, "Belum masuk sesi tes online") {
		t.Errorf("late applicant next step missing")
	}
	if missing, _ := exam.ApplicantMissingSessions(ctx, e.tx, e.schoolA, present.ID); len(missing) != 0 {
		t.Errorf("present applicant flagged missing: %v", missing)
	}
	e.do(http.MethodPost, base+"/"+test.ID+"/peserta", "admin_tu", url.Values{"session_id": {sessionID}})
	var participants int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_session_participants WHERE session_id = $1`, sessionID).Scan(&participants)
	if participants != 3 {
		t.Fatalf("participants = %d, want 3", participants)
	}

	// Kartu tes di halaman status: kode akses, QR, cek perangkat; kode
	// bisa dipakai login.
	c := newPublicServer(t, e)
	status := "/s/test-adm-a/status/"
	if code, _, _ := c.post(status, url.Values{"number": {present.RegistrationNumber}, "birth_date": {"2019-05-17"}}); code != http.StatusSeeOther {
		t.Fatalf("status login = %d", code)
	}
	_, body := c.get(status)
	if !strings.Contains(body, "Kartu tes online") || !strings.Contains(body, "TPA Online") || !strings.Contains(body, "data:image/png;base64,") ||
		!strings.Contains(body, `href="/u/cek"`) {
		t.Fatalf("online card missing on status page")
	}
	i := strings.Index(body, `aria-label="Kode akses `)
	if i < 0 {
		t.Fatalf("access code missing")
	}
	code := body[i+len(`aria-label="Kode akses `) : i+len(`aria-label="Kode akses `)+exam.CodeLength]
	part, err := exam.Login(ctx, e.tx, exam.LoginRequest{SchoolID: e.schoolA, Identifier: present.RegistrationNumber, Code: code,
		DeviceID: "5f0c3d1e-2a4b-4c6d-8e9f-0a1b2c3d4e5f", Now: start})
	if err != nil || part.ApplicantID != present.ID {
		t.Fatalf("login with status card code: %+v, %v", part, err)
	}
	// Bukti pendaftaran memuat jadwal dan kode yang sama.
	d, err := proofData(ctx, &Handler{db: e.tx, periods: e.svc, documents: NewDocumentService(e.tx, nil, pii.DevKeyring()), exam: defaultExamLink()}, "A",
		mustLoad(t, apps, e.schoolA, present.ID))
	if err != nil || !strings.Contains(strings.Join(d.Tests, "\n"), code[:4]+" "+code[4:]) {
		t.Errorf("proof lines = %v, %v", d.Tests, err)
	}

	// Nilai masuk dari attempt yang dikumpulkan (sinkron dari penilaian,
	// diuji di paket exam); tidak hadir tetap tanpa nilai.
	var partID string
	e.tx.QueryRow(ctx, `SELECT id FROM exam_session_participants WHERE session_id = $1 AND applicant_id = $2`, sessionID, present.ID).Scan(&partID)
	var attemptID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_attempts (school_id, exam_id, session_id, participant_id, variant_id, seed, question_order, device_id,
			started_at, deadline_at, submitted_at, submit_reason, status, score_x100, score_status, graded_at)
		VALUES ($1, $2, $3, $4, $5, 1, '{}', gen_random_uuid(), $6, $7, $7, 'peserta', 'dikumpulkan', 8350, 'final', $7) RETURNING id`,
		e.schoolA, examID, sessionID, partID, variantID, start, start.Add(time.Hour)).Scan(&attemptID); err != nil {
		t.Fatal(err)
	}
	if _, err := exam.SyncAdmissionExam(ctx, e.tx, e.schoolA, examID); err != nil {
		t.Fatal(err)
	}
	ranking := func() map[string]Ranked {
		rs, err := apps.Ranking(ctx, e.schoolA, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]Ranked{}
		for _, tr := range rs {
			for _, r := range tr.Rows {
				out[r.ApplicantID] = r
			}
		}
		return out
	}
	rk := ranking()
	// 83,50 dari 100 → 41,75 dari nilai maksimal 50 → skor akhir 83,5.
	if r := rk[present.ID]; !r.Complete || r.Score != 83.5 {
		t.Errorf("present ranking = %+v", r)
	}
	if r := rk[absent.ID]; r.Complete {
		t.Errorf("absent applicant must stay incomplete: %+v", r)
	}
	// Nilai ulang mengubah nilai attempt → nilai tes dan peringkat ikut.
	e.tx.Exec(ctx, `UPDATE exam_attempts SET score_x100 = 9000 WHERE id = $1`, attemptID)
	exam.SyncAdmissionExam(ctx, e.tx, e.schoolA, examID)
	if r := ranking()[present.ID]; r.Score != 90 {
		t.Errorf("after regrade score = %v", r.Score)
	}

	// Grid nilai hanya baca, dengan tautan ke detail hasil; simpan ditolak.
	grid := e.do(http.MethodGet, base+"/"+test.ID+"/nilai", "admin_tu", nil).Body.String()
	if strings.Contains(grid, `name="score_`) || !strings.Contains(grid, "/hasil/peserta/"+attemptID) || !strings.Contains(grid, "45") {
		t.Errorf("online grid must be read-only with result links")
	}
	if rec := e.do(http.MethodPost, base+"/"+test.ID+"/nilai", "admin_tu", url.Values{"score_" + present.ID: {"10"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("typing online score = %d", rec.Code)
	}
	if r := ranking()[present.ID]; r.Score != 90 {
		t.Errorf("online score changed by typing: %v", r.Score)
	}
	// Detail pendaftar: sumber, durasi, integritas.
	if body := e.do(http.MethodGet, "/pendaftaran/"+present.ID, "admin_tu", nil).Body.String(); !strings.Contains(body, "Sumber: ujian online") ||
		!strings.Contains(body, "· 1 j ·") || !strings.Contains(body, "tanpa catatan integritas") {
		t.Errorf("applicant score card missing online source details")
	}
	// Kembali ke tes kertas: nilai online dihapus (belum dinilai lagi).
	if rec := e.do(http.MethodPost, base+"/"+test.ID, "admin_tu", url.Values{"test_name": {"TPA"}, "weight": {"100"}, "max_score": {"50"}, "method": {"kertas"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("switch to paper = %d", rec.Code)
	}
	if r := ranking()[present.ID]; r.Complete {
		t.Errorf("online score kept after switching to paper")
	}
}

func mustLoad(t *testing.T, apps *ApplicantService, schoolID, id string) Applicant {
	t.Helper()
	a, err := apps.Load(context.Background(), schoolID, id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
