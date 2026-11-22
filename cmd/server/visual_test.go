package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"

	"akra/internal/academic"
	"akra/internal/announcements"
	"akra/internal/auth"
	"akra/internal/billing"
	"akra/internal/exam"
	"akra/internal/importer"
	"akra/internal/payments"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/platform/render"
	"akra/internal/school"
)

// TestVisual menyimpan halaman utama dengan data demo sebagai HTML ke
// VISUAL_OUT untuk pemeriksaan tampilan (T162). scripts/visual.sh (make
// visual) lalu memotretnya di desktop 1280 px dan HP 360 px. Halaman staf
// modul ujian juga diperiksa dengan aturan tata letak (pageLint). Hanya
// berjalan bila VISUAL_OUT diisi, jadi tidak membebani CI.
func TestVisual(t *testing.T) {
	out := os.Getenv("VISUAL_OUT")
	dsn := os.Getenv("TEST_DATABASE_URL")
	if out == "" || dsn == "" {
		t.Skip("VISUAL_OUT not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hasher := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1})
	hash, _ := hasher.HashPassword("Rahasia-Visual-1")
	sid := one(`INSERT INTO schools (name, code, address) VALUES ('SD Islam Terpadu Al-Hikmah', 'test-visual', 'Jl. Melati No. 5, Bandung') RETURNING id`)
	user := func(email, name string, roles ...string) string {
		id := one(`INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, $3, $4, true) RETURNING id`, sid, email, hash, name)
		for _, r := range roles {
			exec(`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, r)
		}
		return id
	}
	admin := user("admin@v.id", "Rina Kusuma", "admin_tu")
	bend := user("bendahara@v.id", "Siti Aminah", "bendahara")
	guru := user("guru@v.id", "Budi Santoso", "guru")
	user("kepala@v.id", "Dr. Hasan Basri", "kepala_sekolah")
	today := clock.Today()
	start := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	year := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', $2, '2027-06-25', true) RETURNING id`, sid, start)
	c1A := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, '1A', 1, $3) RETURNING id`, sid, year, guru)
	c1B := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '1B', 1) RETURNING id`, sid, year)
	one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, '2A', 2) RETURNING id`, sid, year)
	names := []string{"Ahmad Fauzan", "Aisyah Putri", "Bagas Pratama", "Citra Lestari", "Dimas Saputra", "Fitri Handayani", "Gilang Ramadhan", "Hana Salsabila", "Ilham Maulana", "Jasmine Azzahra"}
	var students, enrolls []string
	for i, n := range names {
		c := c1A
		if i >= 6 {
			c = c1B
		}
		st := one(`INSERT INTO students (school_id, nis, full_name, gender, birth_date) VALUES ($1, $2, $3, $4, '2019-05-02') RETURNING id`, sid, fmt.Sprintf("2026%03d", i+1), n, []string{"L", "P"}[i%2])
		students = append(students, st)
		enrolls = append(enrolls, one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`, sid, st, c, year, start))
	}
	// Orang tua dengan dua anak.
	g := one(`INSERT INTO guardians (school_id, full_name, phone, email) VALUES ($1, 'Ibu Dewi Lestari', '+628123456789', 'dewi@v.id') RETURNING id`, sid)
	exec(`INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary) VALUES ($1, $2, $3, 'ibu', true), ($1, $4, $3, 'ibu', true)`, sid, students[1], g, students[7])
	parent := one(`INSERT INTO users (school_id, email, password_hash, full_name, guardian_id, is_active) VALUES ($1, 'dewi@v.id', $2, 'Ibu Dewi Lestari', $3, true) RETURNING id`, sid, hash, g)
	exec(`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = 'orang_tua'`, parent)

	actor := school.Actor{SchoolID: sid, UserID: bend}
	spp, _ := billing.CreateFeeType(ctx, tx, sid, billing.FeeTypeInput{Name: "SPP Bulanan", DefaultAmount: 350000, DefaultDueDay: 10})
	keg, _ := billing.CreateFeeType(ctx, tx, sid, billing.FeeTypeInput{Name: "Kegiatan", DefaultAmount: 150000})
	var firstInv billing.Invoice
	for i, e := range enrolls {
		inv, err := billing.CreateInvoice(ctx, tx, actor, billing.InvoiceRequest{EnrollmentID: e, Period: "2026-09", Items: []billing.ItemRequest{{FeeTypeID: spp.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			firstInv = inv
		}
		if i%3 == 0 {
			_, _ = payments.RecordPayment(ctx, tx, actor, payments.Request{InvoiceID: inv.ID, Amount: 350000, PaidOn: today, Method: payments.MethodTransfer, BankAccount: "BCA 1234567890"})
		} else if i%3 == 1 {
			_, _ = payments.RecordPayment(ctx, tx, actor, payments.Request{InvoiceID: inv.ID, Amount: 150000, PaidOn: today, Method: payments.MethodCash})
		}
		if i < 4 {
			_, _ = billing.CreateInvoice(ctx, tx, actor, billing.InvoiceRequest{EnrollmentID: e, Period: "2026-10", Items: []billing.ItemRequest{{FeeTypeID: spp.ID}, {FeeTypeID: keg.ID}}})
		}
	}
	// Absensi 1A beberapa hari.
	for d := 0; d < 4; d++ {
		day := today.AddDate(0, 0, -d)
		s := one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`, sid, year, c1A, day, guru)
		for i := 0; i < 6; i++ {
			st := []string{"hadir", "hadir", "hadir", "sakit", "hadir", "izin"}[(i+d)%6]
			exec(`INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by) VALUES ($1, $2, $3, $4, $5)`, sid, s, enrolls[i], st, guru)
		}
	}
	ann := announcements.NewService(tx)
	_, _ = ann.Create(ctx, school.Actor{SchoolID: sid, UserID: admin}, announcements.Form{Title: "Libur Maulid Nabi", Body: "Kegiatan belajar mengajar diliburkan pada hari Jumat.\nMasuk kembali hari Senin seperti biasa."})
	_, _ = ann.Create(ctx, school.Actor{SchoolID: sid, UserID: admin}, announcements.Form{Title: "Pentas seni kelas 1A", Body: "Orang tua diundang hadir pukul 09.00 di aula.", ClassID: c1A})

	var impRows importer.Rows
	for i, n := range []string{"Ahmad Rizki", "Bunga Citra", "Cahya Putra", "Dinda Ayu", "Eko Saputra", "Farah Nabila"} {
		var r importer.Row
		r.Line = i + 3
		if i >= 3 {
			r.Line++
		}
		r.Values = [importer.NumColumns]string{fmt.Sprintf("2026%03d", 101+i), n, "2019-05-02", "L", "1A", "Wali " + n, "081234567890", ""}
		impRows.Data = append(impRows.Data, r)
	}
	impRows.Data[2].Values[importer.ColClass] = "1X"
	impRows.Data[4].Values[importer.ColBirthDate] = "02/05/2019"
	impRows.Data[4].Values[importer.ColGuardianEmail] = "wali@"
	impRows.Data[5].Values[importer.ColNIS] = "2026101"
	impRows.Skipped = []importer.Skip{{Line: 2, Reason: importer.SkipExample}, {Line: 6, Reason: importer.SkipEmpty}}
	batchID, err := importer.NewService(tx, nil).Upload(ctx, school.Actor{SchoolID: sid, UserID: admin}, "Data siswa baru 2026.xlsx", impRows, today)
	if err != nil {
		t.Fatal(err)
	}
	var doneRows importer.Rows
	for i, n := range []string{"Gita Lestari", "Hadi Pranoto", "Indah Sari"} {
		doneRows.Data = append(doneRows.Data, importer.Row{Line: i + 2, Values: [importer.NumColumns]string{fmt.Sprintf("2026%03d", 201+i), n, "", "P", "1B", "Wali " + n, "", ""}})
	}
	doneRows.Data = append(doneRows.Data, importer.Row{Line: 5, Values: [importer.NumColumns]string{"2026001", "Ahmad Fauzan", "", "L", "1A"}})
	impSvc := importer.NewService(tx, nil)
	doneID, err := impSvc.Upload(ctx, school.Actor{SchoolID: sid, UserID: admin}, "Siswa pindahan.xlsx", doneRows, today)
	if err != nil {
		t.Fatal(err)
	}
	if err := impSvc.Run(ctx, school.Actor{SchoolID: sid, UserID: admin}, doneID, today); err != nil {
		t.Fatal(err)
	}
	for i, n := range [][2]string{{"Matematika", "MTK"}, {"Bahasa Indonesia", "BIN"}, {"Ilmu Pengetahuan Alam", "IPA"}, {"Pendidikan Agama Islam", ""}, {"Seni Budaya dan Prakarya", "SBdP"}} {
		sub, err := academic.CreateSubject(ctx, tx, sid, academic.SubjectInput{Name: n[0], Code: n[1]})
		if err != nil {
			t.Fatal(err)
		}
		if i == 4 {
			_, _ = academic.SetSubjectActive(ctx, tx, sid, sub.ID, false)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
		SELECT $1, $2, $3, s.id, $4 FROM subjects s WHERE s.school_id = $1 AND s.name IN ('Matematika', 'Bahasa Indonesia')`, sid, year, c1A, guru); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id)
		SELECT $1, $2, $3, s.id, $4 FROM subjects s WHERE s.school_id = $1 AND s.name = 'Matematika'`, sid, year, c1B, guru); err != nil {
		t.Fatal(err)
	}
	var mtkID string
	if err := tx.QueryRow(ctx, `SELECT id FROM subjects WHERE school_id = $1 AND name = 'Matematika'`, sid).Scan(&mtkID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent, sort_order)
		SELECT $1, $2, s.id, n, w, o FROM subjects s, (VALUES ('Tugas', 30, 1), ('UTS', 30, 2), ('UAS', 40, 3)) v(n, w, o)
		WHERE s.school_id = $1 AND s.name = 'Matematika'`, sid, year); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO grades (school_id, academic_year_id, enrollment_id, component_id, score, recorded_by)
		SELECT $1, $2, e.id, gc.id, 70 + (row_number() OVER ())::int * 3, $4 FROM enrollments e, grade_components gc
		WHERE e.school_id = $1 AND e.class_id = $3 AND gc.school_id = $1 AND gc.name = 'Tugas' AND e.end_date IS NULL
		LIMIT 4`, sid, year, c1A, guru); err != nil {
		t.Fatal(err)
	}
	gm, err := importer.NewMappingService(tx).Create(ctx, school.Actor{SchoolID: sid, UserID: admin}, "Rekap nilai lama", importer.TargetGrades,
		importer.ColumnMap{"NIS": "No Induk", "Kode mapel": "Mapel", "Nama komponen": "Jenis", "Nilai": "Skor"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var nis1A []string
	nr, _ := tx.Query(ctx, `SELECT st.nis FROM enrollments e JOIN students st ON st.id = e.student_id WHERE e.class_id = $1 AND e.end_date IS NULL ORDER BY st.nis`, c1A)
	for nr.Next() {
		var n string
		_ = nr.Scan(&n)
		nis1A = append(nis1A, n)
	}
	nr.Close()
	gRows := importer.MappedRows{Target: importer.TargetGrades}
	for i, n := range nis1A {
		gRows.Data = append(gRows.Data, importer.MappedRow{Line: i + 2, Values: []string{n, "MTK", "UTS", fmt.Sprint(78 + i)}})
	}
	l := len(gRows.Data) + 2
	gRows.Data = append(gRows.Data,
		importer.MappedRow{Line: l, Values: []string{nis1A[0], "IPA", "UTS", "80"}},
		importer.MappedRow{Line: l + 1, Values: []string{"9999999", "MTK", "UTS", "75"}},
		importer.MappedRow{Line: l + 2, Values: []string{nis1A[1], "MTK", "Remedial", "105"}})
	gradeBatchID, err := impSvc.UploadGrades(ctx, school.Actor{SchoolID: sid, UserID: admin}, "Nilai UTS 1A.xlsx", gRows, gm.ID, today)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO school_settings (school_id, key, value) VALUES ($1, 'report.portal_components', 'true')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO school_settings (school_id, key, value) VALUES ($1, 'attendance_per_subject', 'true')`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_slots (school_id, academic_year_id, class_id, subject_id, teacher_id, day_of_week, start_time, end_time)
		SELECT $1, $2, $3, s.id, $4, d, (t || ':00')::time, (t || ':40')::time
		FROM subjects s, unnest(ARRAY[1, 1, 2, 3, 3, 5]) WITH ORDINALITY AS x(d, n), LATERAL (SELECT (6 + n)::text AS t) tt
		WHERE s.school_id = $1 AND s.name = CASE WHEN n % 2 = 0 THEN 'Matematika' ELSE 'Bahasa Indonesia' END`, sid, year, c1A, guru); err != nil {
		t.Fatal(err)
	}
	var builderCfg string
	if err := tx.QueryRow(ctx, `INSERT INTO report_configs (school_id, owner_user_id, name, source, columns, filters, sort)
		SELECT $1, u.id, 'Tunggakan per kelas', 'arrears', '["class_name","student_name","fee_types","remaining","days_overdue"]',
		       '[{"column":"remaining","op":">=","value":"100000"},{"column":"class_name","op":"in","values":["1A","1B"]}]', '[{"column":"remaining","desc":true}]'
		FROM users u WHERE u.school_id = $1 AND u.email = 'bendahara@v.id' RETURNING id`, sid).Scan(&builderCfg); err != nil {
		t.Fatal(err)
	}
	// Modul ujian aktif (T125) agar halaman ujian ikut diperiksa.
	exec(`INSERT INTO school_settings (school_id, key, value) VALUES ($1, 'exam.enabled', 'true'), ($1, 'exam.release', 'r3')`, sid)
	rd, err := render.New(mustSub(t, "templates"), mustSub(t, "static"), logger)
	if err != nil {
		t.Fatal(err)
	}
	app, err := newApp(appDeps{db: tx, pinger: pool, sessions: auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger),
		render: rd, static: mustSub(t, "static"), mailer: &captureMailer{}, hasher: hasher, baseURL: "https://akra.test", logger: logger, pii: pii.DevKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	static, _ := filepath.Abs("../../web/static")
	save := func(name string, b *client, path string) {
		rec := b.get(path)
		body := strings.ReplaceAll(rec.Body.String(), `"/static/`, `"file://`+static+`/`)
		body = strings.ReplaceAll(body, `url("/static/`, `url("file://`+static+`/`)
		_ = os.WriteFile(filepath.Join(out, name+".html"), []byte(body), 0o600)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s: %d", name, path, rec.Code)
		}
		// Halaman staf modul ujian berkerangka aplikasi mengikuti aturan
		// tata letak (T162).
		if strings.HasPrefix(path, "/ujian") && strings.Contains(body, `class="app-chrome"`) {
			for _, v := range pageLint(rec.Body.String()) {
				t.Errorf("%s %s: %s", name, path, v)
			}
		}
	}
	anon := newClient(t, app)
	save("login", anon, "/login")
	pages := map[string][][2]string{
		"admin@v.id":     {{"admin-home", "/"}, {"settings-hub", "/pengaturan"}, {"attendance-settings", "/pengaturan/absensi"}, {"students", "/siswa"}, {"student", "/siswa/" + students[0]}, {"classes", "/kelas"}, {"rekap", "/absensi/rekap?kelas=" + c1A}, {"report", "/laporan/kehadiran"}, {"announce", "/pengumuman"}, {"announce-new", "/pengumuman/baru"}, {"audit", "/keuangan/audit"}, {"years", "/pengaturan/tahun-ajaran"}, {"subjects", "/pengaturan/mapel"}, {"class-recap", "/nilai/rekap?kelas=" + c1A}, {"report-config", "/pengaturan/rapor"}, {"assignments", "/pengaturan/penugasan?kelas=" + c1A}, {"schedule", "/jadwal?kelas=" + c1A}, {"import", "/impor/siswa"}, {"mapping", "/impor/mapping"}, {"import-batch", "/impor/batch/" + batchID}, {"import-done", "/impor/batch/" + doneID}, {"import-grades", "/impor/nilai"}, {"import-grade-batch", "/impor/batch/" + gradeBatchID}, {"school-profile", "/pengaturan/sekolah"}, {"duties", "/pengaturan/tugas"}, {"exam-home", "/ujian"}, {"exam-agenda", "/ujian/agenda"}},
		"bendahara@v.id": {{"bend-home", "/"}, {"pay-form", "/keuangan/pembayaran/baru?siswa=" + students[1]}, {"invoice", "/keuangan/tagihan/" + firstInv.ID}, {"arrears", "/keuangan/tunggakan"}, {"fee-types", "/keuangan/jenis-tagihan"}, {"bulk", "/keuangan/tagihan-massal"}, {"hub", "/laporan"}, {"pay-recap", "/keuangan/rekap-pembayaran?mode=harian&dari=2026-09-01&sampai=2026-10-31"}, {"pay-recap-month", "/keuangan/rekap-pembayaran?mode=bulanan"}, {"report-configs", "/laporan/konfigurasi"}, {"builder", "/laporan/bangun?konfigurasi=" + builderCfg}},
		"guru@v.id":      {{"guru-home", "/"}, {"guru-assign", "/pengaturan/penugasan"}, {"guru-schedule", "/jadwal/saya"}, {"guru-components", "/nilai/komponen"}, {"guru-grades", "/nilai/input"}, {"grade-recap", "/nilai/rekap?kelas=" + c1A + "&mapel=" + mtkID}, {"guru-recap", "/nilai/rekap"}, {"grade-input", "/nilai/input?kelas=" + c1A + "&mapel=" + mtkID}, {"guru-classes", "/absensi"}, {"attendance", "/absensi?kelas=" + c1A + "&tanggal=" + today.Format("2006-01-02")}},
		"kepala@v.id":    {{"kepala-home", "/"}},
		"dewi@v.id":      {{"portal", "/portal"}, {"portal-child", "/portal/siswa/" + students[1]}, {"portal-billing", "/portal/siswa/" + students[1] + "/tagihan"}, {"portal-ann", "/portal/pengumuman"}, {"portal-schedule", "/portal/siswa/" + students[1] + "/jadwal"}, {"portal-grades", "/portal/siswa/" + students[1] + "/nilai"}},
	}
	// Bank soal (T127): satu bank Matematika dengan stimulus dan soal Arab.
	vbank := one(`INSERT INTO exam_banks (school_id, name, subject_id, grade_level, created_by) VALUES ($1, 'Matematika Kelas 1', $2, 1, $3) RETURNING id`, sid, mtkID, admin)
	vstim := one(`INSERT INTO exam_stimuli (school_id, bank_id, title, body_src, body_html, created_by, updated_by) VALUES ($1, $2, 'Teks untuk soal 1–2', '<p>Bacaan</p>', '<p>Bacaan</p>', $3, $3) RETURNING id`, sid, vbank, admin)
	for i, body := range []string{"Hasil dari 3/4 + 1/2 adalah …", "Akar dari 144 adalah …", "مَا مَعْنَى الصَّلَاةُ؟", "Jelaskan proses fotosintesis secara singkat."} {
		kind, diff := "pg", "mudah"
		if i == 3 {
			kind, diff = "esai", "sulit"
		}
		var st any
		var ord any
		if i < 2 {
			st, ord = vstim, i+1
		}
		exec(`INSERT INTO exam_questions (school_id, bank_id, stimulus_id, stimulus_order, kind, body_src, body_html, status, difficulty, cognitive_level, tags, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, 'C2', '{pecahan}', $9, $9)`, sid, vbank, st, ord, kind, "<p>"+body+"</p>", map[bool]string{true: "siap", false: "draf"}[i != 2], diff, admin)
	}
	exec(`INSERT INTO learning_objectives (school_id, subject_id, grade_level, code, description, created_by) VALUES ($1, $2, 1, 'TP 1.1', 'Mengenal bilangan sampai 100', $3)`, sid, mtkID, admin)
	pages["admin@v.id"] = append(pages["admin@v.id"], [2]string{"exam-banks", "/ujian/bank/"}, [2]string{"exam-bank", "/ujian/bank/" + vbank}, [2]string{"exam-tp", "/ujian/tp/?mapel=" + mtkID})

	// Peserta ujian (T124): kode untuk siswa pertama, sesi sedang berjalan.
	var pNIS string
	if err := tx.QueryRow(ctx, `SELECT nis FROM students WHERE id = $1`, students[0]).Scan(&pNIS); err != nil {
		t.Fatal(err)
	}
	_, pCode, err := exam.NewIssuer().Issue(ctx, tx, sid, admin, exam.IssueRequest{Owner: exam.Owner{StudentID: students[0]}, Start: time.Now().Add(-5 * time.Minute), End: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	pc := newClient(t, app)
	save("participant-login", pc, "/u/test-visual?nis="+pNIS)
	if rec := pc.do(http.MethodPost, "/u/test-visual", url.Values{"identifier": {pNIS}, "code": {"SALAH123"}}); rec.Code == http.StatusUnprocessableEntity {
		body := strings.ReplaceAll(rec.Body.String(), `"/static/`, `"file://`+static+`/`)
		body = strings.ReplaceAll(body, `url("/static/`, `url("file://`+static+`/`)
		_ = os.WriteFile(filepath.Join(out, "participant-error.html"), []byte(body), 0o600)
	}
	if rec := pc.do(http.MethodPost, "/u/test-visual", url.Values{"identifier": {pNIS}, "code": {pCode}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("participant login: %d", rec.Code)
	}
	save("participant-home", pc, "/u/beranda")
	save("participant-index", newClient(t, app), "/u/")

	// Modul ujian (T154, T162): alur admin dari bank contoh sampai layar
	// pengawas dan hasil ujian latihan. Gambar panduan doc/panduan-ujian/img
	// diambil dari halaman ujian-* (scripts/visual.sh --panduan).
	{
		ab := newClient(t, app)
		if rec := ab.login("test-visual", "admin@v.id", "Rahasia-Visual-1"); rec.Code != http.StatusSeeOther {
			t.Fatalf("admin login: %d", rec.Code)
		}
		save("ujian-beranda", ab, "/ujian/")
		save("ujian-bank", ab, "/ujian/bank/")
		var sample string
		if err := tx.QueryRow(ctx, `SELECT id FROM exam_banks WHERE school_id = $1 AND sample ORDER BY grade_level LIMIT 1`, sid).Scan(&sample); err != nil {
			t.Fatal(err)
		}
		save("ujian-bank-contoh", ab, "/ujian/bank/"+sample)
		save("ujian-impor", ab, "/ujian/bank/"+sample+"/impor")
		save("ujian-tp", ab, "/ujian/tp/?mapel="+mtkID)
		save("ujian-daftar", ab, "/ujian/paket/")
		save("ujian-buat", ab, "/ujian/paket/baru?tujuan=ulangan")
		save("ujian-latihan", ab, "/ujian/latihan")
		if rec := ab.do(http.MethodPost, "/ujian/latihan", url.Values{"class_id": {c1A}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("open practice: %d", rec.Code)
		}
		var practice, sess string
		if err := tx.QueryRow(ctx, `SELECT e.id, x.id FROM exams e JOIN exam_sessions x ON x.exam_id = e.id WHERE e.school_id = $1 AND e.builtin LIMIT 1`, sid).Scan(&practice, &sess); err != nil {
			t.Fatal(err)
		}
		save("ujian-detail", ab, "/ujian/paket/"+practice)
		save("ujian-pengawas", ab, "/ujian/sesi/"+sess+"/awasi")
		save("ujian-sesi-saya", ab, "/ujian/sesi-saya")
		save("ujian-agenda", ab, "/ujian/agenda")
		save("ujian-ruang", ab, "/ujian/ruang")
		save("ujian-bantuan", ab, "/ujian/bantuan")
		save("ujian-hasil", ab, "/ujian/paket/"+practice+"/hasil")
		save("ujian-hasil-butir", ab, "/ujian/paket/"+practice+"/hasil/butir")
		save("ujian-stimulus", ab, "/ujian/stimulus/"+vstim)
		save("ujian-pengaturan", ab, "/ujian/pengaturan")
		save("ujian-bank-baru", ab, "/ujian/bank/baru")
		var vq string
		if err := tx.QueryRow(ctx, `SELECT id FROM exam_questions WHERE bank_id = $1 ORDER BY created_at LIMIT 1`, vbank).Scan(&vq); err != nil {
			t.Fatal(err)
		}
		save("ujian-soal", ab, "/ujian/soal/"+vq)
		save("ujian-soal-ubah", ab, "/ujian/soal/"+vq+"/ubah")
	}

	for email, list := range pages {
		b := newClient(t, app)
		if rec := b.login("test-visual", email, "Rahasia-Visual-1"); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", email, rec.Code)
		}
		for _, p := range list {
			save(p[0], b, p[1])
		}
	}
}
