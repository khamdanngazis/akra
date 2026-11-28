// Package testutil berisi data dan daftar route untuk uji otorisasi lintas
// sekolah (T58). Hanya dipakai dari uji.
package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/announcements"
	"akra/internal/billing"
	"akra/internal/importer"
	"akra/internal/payments"
	"akra/internal/school"
)

// Password adalah password semua pengguna hasil SeedTwoSchools.
const Password = "Rahasia-Uji-Lintas-1"

// User adalah satu akun hasil seed.
type User struct {
	ID    string
	Email string
	Role  string
}

// School adalah satu sekolah hasil seed beserta ID semua entitasnya.
type School struct {
	ID   string
	Code string // kode sekolah untuk login
	// Marker ada di nama sekolah, pengguna, siswa, wali, jenis tagihan, mapel,
	// pengumuman, dan catatan pembayaran. Respons untuk pengguna sekolah lain
	// tidak boleh memuat teks ini.
	Marker string

	Admin, Treasurer, Teacher, Principal User
	// Parent adalah wali StudentIDs[0] (anak X); Parent2 wali StudentIDs[1]
	// (anak Y) di kelas lain (T59).
	Parent, Parent2 User

	YearID, ClassID string
	ClassName       string // tanpa Marker, karena form bisa menampilkan ulang isian
	// Class2ID adalah kelas StudentIDs[1]; StudentIDs[0] di ClassID.
	Class2ID    string
	Guardian2ID string
	Invoice2ID  string // tagihan StudentIDs[1]
	Payment2ID  string // pembayaran tunai Invoice2ID
	// Class2AnnouncementTitle adalah judul pengumuman khusus Class2ID.
	Class2AnnouncementTitle string
	StudentIDs              [2]string
	EnrollmentIDs           [2]string
	GuardianID              string
	FeeTypeID               string
	SubjectID               string
	SlotID                  string // slot jadwal ClassID untuk SubjectID (T66)
	GradeComponentID        string // komponen nilai SubjectID tahun YearID, bobot 100 (T69)
	ReportConfigID          string // konfigurasi laporan pribadi milik Admin (T77)
	InvoiceID               string
	PaymentID               string
	ProofID                 string
	VoidID                  string
	AnnouncementID          string
	AttendanceSessionID     string
	BulkRunID               string
	ReportJobID             string
	ImportBatchID           string
	// AdmissionPeriodID adalah periode pendaftaran (buka) dengan satu jalur
	// AdmissionTrackID (T89).
	AdmissionPeriodID string
	AdmissionTrackID  string
	// ApplicantID adalah pendaftar berstatus dikirim di periode itu, dengan
	// satu dokumen ApplicantDocID (T93).
	ApplicantID    string
	ApplicantDocID string
	// AdmissionTestID adalah tes seleksi di AdmissionPeriodID (T93a).
	AdmissionTestID string
	// ApplicantFeePaymentID adalah pembayaran biaya formulir ApplicantID (T94).
	ApplicantFeePaymentID string
	// DutyID adalah tugas tambahan tahun YearID (T125). Sengaja milik kepala
	// sekolah (bukan guru), sehingga tidak memberi hak koordinator dan uji
	// route tidak bergantung urutan.
	DutyID string
	// ExamBankID adalah bank soal mapel SubjectID milik Teacher (T127),
	// berisi ExamQuestionID (bertanda ExamObjectiveID).
	ExamBankID, ExamQuestionID, ExamObjectiveID string
	// ExamStimulusID adalah stimulus tanpa soal di ExamBankID, dan
	// ExamAssetID gambar di bank itu (T128).
	ExamStimulusID, ExamAssetID string
	// ExamImportID adalah impor Word milik Teacher berstatus siap dengan
	// satu butir soal (T129).
	ExamImportID string
	// ExamID adalah ulangan draf milik Teacher untuk SubjectID (T133)
	// berisi ExamQuestionID (ExamItemID) di bagian ExamSectionID, dengan
	// ExamSessionID untuk ClassID (pengawas Teacher, peserta StudentIDs[0]).
	ExamID, ExamSectionID, ExamItemID, ExamSessionID string
	// ExamRoomID adalah lab ujian; ExamEventID agenda PAS kelas 1 dengan
	// ruang itu, satu jam (ExamSlotID), dan sel mapel SubjectID
	// (ExamCellID) tanpa ujian (T134).
	ExamRoomID, ExamEventID, ExamSlotID, ExamCellID string
	// ExamPracticeSessionID adalah sesi paket latihan bawaan untuk ClassID
	// (T136).
	ExamPracticeSessionID string
}

// Users mengembalikan semua pengguna sekolah.
func (s *School) Users() []User {
	return []User{s.Admin, s.Treasurer, s.Teacher, s.Principal, s.Parent, s.Parent2}
}

// SeedTwoSchools membuat sekolah A dan B dengan isi yang setara: admin/TU,
// bendahara, guru (wali kelas), kepala sekolah, satu kelas di tahun ajaran
// aktif, dua siswa, satu wali dengan akun portal, satu tagihan, satu
// pembayaran transfer dengan bukti dan pengajuan pembatalan, satu
// pengumuman, satu sesi absensi, satu proses tagihan massal, satu job
// laporan, dan satu batch impor. passwordHash adalah hash dari Password.
func SeedTwoSchools(t testing.TB, tx pgx.Tx, passwordHash string) (a, b *School) {
	t.Helper()
	return seedSchool(t, tx, "a", passwordHash), seedSchool(t, tx, "b", passwordHash)
}

func seedSchool(t testing.TB, tx pgx.Tx, key, hash string) *School {
	t.Helper()
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("seed school %s: %v: %s", key, err, sql)
		}
		return id
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed school %s: %v: %s", key, err, sql)
		}
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed school %s: %v", key, err)
		}
	}

	marker := "RAHASIA-" + map[string]string{"a": "A", "b": "B"}[key]
	s := &School{Code: "test-lintas-" + key, Marker: marker}
	s.ID = one(`INSERT INTO schools (name, code) VALUES ($1, $2) RETURNING id`, "SD Uji "+marker, s.Code)

	user := func(role, name string, guardianID any) User {
		email := role + "@" + key + ".uji"
		if name == "Wali Kedua" {
			email = "orang_tua2@" + key + ".uji"
		}
		id := one(`INSERT INTO users (school_id, email, password_hash, full_name, guardian_id, is_active) VALUES ($1, $2, $3, $4, $5, true) RETURNING id`,
			s.ID, email, hash, name+" "+marker, guardianID)
		exec(`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, role)
		return User{ID: id, Email: email, Role: role}
	}
	s.Admin = user("admin_tu", "Admin", nil)
	s.Treasurer = user("bendahara", "Bendahara", nil)
	s.Teacher = user("guru", "Guru", nil)
	s.Principal = user("kepala_sekolah", "Kepala", nil)

	start := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	s.YearID = one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', $2, '2027-06-25', true) RETURNING id`, s.ID, start)
	s.ClassName = "Kelas " + strings.ToUpper(key)
	s.ClassID = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, $3, 1, $4) RETURNING id`,
		s.ID, s.YearID, s.ClassName, s.Teacher.ID)
	s.Class2ID = one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 2) RETURNING id`,
		s.ID, s.YearID, s.ClassName+"2")
	for i, class := range []string{s.ClassID, s.Class2ID} {
		s.StudentIDs[i] = one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, $2, $3) RETURNING id`,
			s.ID, fmt.Sprintf("%s-%03d", key, i+1), fmt.Sprintf("Siswa %s %d", marker, i+1))
		s.EnrollmentIDs[i] = one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			s.ID, s.StudentIDs[i], class, s.YearID, start)
	}
	s.GuardianID = one(`INSERT INTO guardians (school_id, full_name, phone, email) VALUES ($1, $2, '+6281200000001', $3) RETURNING id`,
		s.ID, "Wali "+marker, "orang_tua@"+key+".uji")
	exec(`INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary) VALUES ($1, $2, $3, 'ibu', true)`,
		s.ID, s.StudentIDs[0], s.GuardianID)
	s.Parent = user("orang_tua", "Wali", s.GuardianID)
	s.Guardian2ID = one(`INSERT INTO guardians (school_id, full_name, phone, email) VALUES ($1, $2, '+6281200000002', $3) RETURNING id`,
		s.ID, "Wali Kedua "+marker, "orang_tua2@"+key+".uji")
	exec(`INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary) VALUES ($1, $2, $3, 'ayah', true)`,
		s.ID, s.StudentIDs[1], s.Guardian2ID)
	s.Parent2 = user("orang_tua", "Wali Kedua", s.Guardian2ID)

	treasurer := school.Actor{SchoolID: s.ID, UserID: s.Treasurer.ID}
	ft, err := billing.CreateFeeType(ctx, tx, s.ID, billing.FeeTypeInput{Name: "SPP " + marker, DefaultAmount: 300000, DefaultDueDay: 10})
	check(err)
	s.FeeTypeID = ft.ID
	subject, err := academic.CreateSubject(ctx, tx, s.ID, academic.SubjectInput{Name: "Mapel " + marker, Code: "M" + strings.ToUpper(key)})
	check(err)
	s.SubjectID = subject.ID
	exec(`INSERT INTO teaching_assignments (school_id, academic_year_id, class_id, subject_id, teacher_id) VALUES ($1, $2, $3, $4, $5)`,
		s.ID, s.YearID, s.ClassID, s.SubjectID, s.Teacher.ID)
	s.SlotID = one(`INSERT INTO schedule_slots (school_id, academic_year_id, class_id, subject_id, teacher_id, day_of_week, start_time, end_time)
		VALUES ($1, $2, $3, $4, $5, 1, '07:00', '07:40') RETURNING id`, s.ID, s.YearID, s.ClassID, s.SubjectID, s.Teacher.ID)
	// Berstatus buka agar kontrol "hapus periode" tidak menghapusnya.
	s.AdmissionPeriodID = one(`INSERT INTO admission_periods (school_id, academic_year_id, name, target_grade, opens_on, closes_on, status, created_by)
		VALUES ($1, $2, $3, 1, '2026-12-01', '2027-02-28', 'buka', $4) RETURNING id`, s.ID, s.YearID, "Gelombang "+marker, s.Admin.ID)
	s.AdmissionTrackID = one(`INSERT INTO admission_tracks (school_id, period_id, name, quota) VALUES ($1, $2, $3, 30) RETURNING id`,
		s.ID, s.AdmissionPeriodID, "Reguler "+marker)
	s.AdmissionTestID = one(`INSERT INTO admission_tests (school_id, period_id, name, weight) VALUES ($1, $2, $3, 100) RETURNING id`,
		s.ID, s.AdmissionPeriodID, "Tes "+marker)
	s.ApplicantID = one(`INSERT INTO applicants (school_id, period_id, track_id, registration_number, status, full_name, gender, birth_date,
		contact_phone, consent_at, submitted_at) VALUES ($1, $2, $3, $4, 'dikirim', $5, 'L', '2019-05-17', '+6281200000009', now(), now()) RETURNING id`,
		s.ID, s.AdmissionPeriodID, s.AdmissionTrackID, "SPMB-"+marker, "Pendaftar "+marker)
	// Satu nilai agar kontrol "hapus tes" ditolak (409) dan tes tetap ada.
	exec(`INSERT INTO applicant_test_scores (school_id, applicant_id, test_id, score, recorded_by) VALUES ($1, $2, $3, 80, $4)`,
		s.ID, s.ApplicantID, s.AdmissionTestID, s.Admin.ID)
	exec(`UPDATE applicants SET fee_amount = 150000 WHERE id = $1`, s.ApplicantID)
	s.ApplicantFeePaymentID = one(`INSERT INTO applicant_fee_payments (school_id, applicant_id, amount, paid_on, method, receipt_number, recorded_by)
		VALUES ($1, $2, 150000, '2026-11-01', 'tunai', $3, $4) RETURNING id`, s.ID, s.ApplicantID, "KW/2026/F"+marker, s.Treasurer.ID)
	s.ApplicantDocID = one(`INSERT INTO applicant_documents (school_id, applicant_id, doc_type, storage_key, content_type, size_bytes, sha256)
		VALUES ($1, $2, 'kk', $3, 'image/png', 10, '\x00') RETURNING id`, s.ID, s.ApplicantID, "schools/"+s.ID+"/applicants/fixture.enc")
	s.GradeComponentID = one(`INSERT INTO grade_components (school_id, academic_year_id, subject_id, name, weight_percent) VALUES ($1, $2, $3, 'Ujian', 100) RETURNING id`,
		s.ID, s.YearID, s.SubjectID)
	exec(`INSERT INTO grades (school_id, academic_year_id, enrollment_id, component_id, score, recorded_by) VALUES ($1, $2, $3, $4, 75, $5)`,
		s.ID, s.YearID, s.EnrollmentIDs[0], s.GradeComponentID, s.Teacher.ID)
	inv, err := billing.CreateInvoice(ctx, tx, treasurer, billing.InvoiceRequest{EnrollmentID: s.EnrollmentIDs[0], Period: "2026-09", Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
	check(err)
	s.InvoiceID = inv.ID
	pay, err := payments.RecordPayment(ctx, tx, treasurer, payments.Request{InvoiceID: inv.ID, Amount: 100000,
		PaidOn: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), Method: payments.MethodTransfer, BankAccount: "BCA " + marker, Note: "Catatan " + marker})
	check(err)
	s.PaymentID = pay.ID
	inv2, err := billing.CreateInvoice(ctx, tx, treasurer, billing.InvoiceRequest{EnrollmentID: s.EnrollmentIDs[1], Period: "2026-09", Items: []billing.ItemRequest{{FeeTypeID: ft.ID}}})
	check(err)
	s.Invoice2ID = inv2.ID
	pay2, err := payments.RecordPayment(ctx, tx, treasurer, payments.Request{InvoiceID: inv2.ID, Amount: 50000,
		PaidOn: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), Method: payments.MethodCash})
	check(err)
	s.Payment2ID = pay2.ID
	sum := make([]byte, 32)
	_, _ = rand.Read(sum)
	s.ProofID = one(`INSERT INTO payment_proofs (school_id, payment_id, storage_key, sha256, size_bytes, content_type, uploaded_by)
		VALUES ($1, $2, $3, $4, 100, 'image/png', $5) RETURNING id`,
		s.ID, s.PaymentID, "schools/"+s.ID+"/proofs/uji-"+key+".png", sum, s.Treasurer.ID)
	s.VoidID = one(`INSERT INTO payment_voids (school_id, payment_id, reason, requested_by, requires_approval, status)
		VALUES ($1, $2, $3, $4, true, 'pending') RETURNING id`, s.ID, s.PaymentID, "Salah catat "+marker, s.Treasurer.ID)

	ann, err := announcements.NewService(tx).Create(ctx, school.Actor{SchoolID: s.ID, UserID: s.Admin.ID},
		announcements.Form{Title: "Pengumuman " + marker, Body: "Isi pengumuman " + marker})
	check(err)
	s.AnnouncementID = ann.ID
	s.Class2AnnouncementTitle = "Khusus kelas " + s.ClassName + "2 " + marker
	_, err = announcements.NewService(tx).Create(ctx, school.Actor{SchoolID: s.ID, UserID: s.Admin.ID},
		announcements.Form{Title: s.Class2AnnouncementTitle, Body: "Isi " + marker, ClassID: s.Class2ID})
	check(err)

	s.AttendanceSessionID = one(`INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by) VALUES ($1, $2, $3, '2026-10-16', $4) RETURNING id`,
		s.ID, s.YearID, s.ClassID, s.Teacher.ID)
	exec(`INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by) VALUES ($1, $2, $3, 'hadir', $4)`,
		s.ID, s.AttendanceSessionID, s.EnrollmentIDs[0], s.Teacher.ID)
	s.BulkRunID = one(`INSERT INTO bulk_invoice_runs (school_id, academic_year_id, fee_type_id, class_id, period, due_date, mode, status, requested_by)
		VALUES ($1, $2, $3, $4, '2026-10', '2026-10-10', 'sync', 'done', $5) RETURNING id`, s.ID, s.YearID, s.FeeTypeID, s.ClassID, s.Treasurer.ID)
	s.ReportConfigID = one(`INSERT INTO report_configs (school_id, owner_user_id, name, source, columns) VALUES ($1, $2, $3, 'attendance', '["nis","status"]') RETURNING id`,
		s.ID, s.Admin.ID, "Absensi "+marker)
	s.ReportJobID = one(`INSERT INTO report_jobs (school_id, requested_by, report_type, format, status, file_key, file_name, finished_at)
		VALUES ($1, $2, 'arrears', 'xlsx', 'selesai', $3, 'tunggakan.xlsx', now()) RETURNING id`, s.ID, s.Treasurer.ID, "schools/"+s.ID+"/reports/uji.xlsx")

	s.DutyID = one(`INSERT INTO staff_duties (school_id, academic_year_id, user_id, duty, created_by) VALUES ($1, $2, $3, 'koordinator_ujian', $4) RETURNING id`,
		s.ID, s.YearID, s.Principal.ID, s.Admin.ID)
	// Modul ujian aktif sampai tahap 3 agar semua route ujian bisa diuji.
	exec(`INSERT INTO school_modules (school_id, module, enabled, options) VALUES ($1, 'exam', true, '{"release": "r3"}')`, s.ID)

	s.ExamBankID = one(`INSERT INTO exam_banks (school_id, name, subject_id, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		s.ID, "Bank "+marker, s.SubjectID, s.Teacher.ID)
	s.ExamObjectiveID = one(`INSERT INTO learning_objectives (school_id, subject_id, grade_level, code, description, created_by) VALUES ($1, $2, 1, 'TP 1.1', $3, $4) RETURNING id`,
		s.ID, s.SubjectID, "TP "+marker, s.Teacher.ID)
	s.ExamQuestionID = one(`INSERT INTO exam_questions (school_id, bank_id, kind, body_src, body_html, options, answer_key, objective_id, status, search_text, created_by, updated_by)
		VALUES ($1, $2, 'pg', $3, $3, '{"choices":[{"id":"a","src":"1","html":"1"},{"id":"b","src":"2","html":"2"}]}', '{"correct":["a"]}', $4, 'siap', $3, $5, $5) RETURNING id`,
		s.ID, s.ExamBankID, "<p>Soal "+marker+"</p>", s.ExamObjectiveID, s.Teacher.ID)
	exec(`INSERT INTO exam_question_versions (school_id, question_id, version, snapshot, created_by) VALUES ($1, $2, 1, '{}', $3)`, s.ID, s.ExamQuestionID, s.Teacher.ID)
	s.ExamStimulusID = one(`INSERT INTO exam_stimuli (school_id, bank_id, title, body_src, body_html, created_by, updated_by) VALUES ($1, $2, $3, '<p>Bacaan</p>', '<p>Bacaan</p>', $4, $4) RETURNING id`,
		s.ID, s.ExamBankID, "Stimulus "+marker, s.Teacher.ID)
	exec(`INSERT INTO exam_stimulus_versions (school_id, stimulus_id, version, snapshot, created_by) VALUES ($1, $2, 1, '{}', $3)`, s.ID, s.ExamStimulusID, s.Teacher.ID)
	s.ExamImportID = one(`INSERT INTO exam_imports (school_id, bank_id, source, file_name, status, created_by) VALUES ($1, $2, 'word', 'uji.docx', 'siap', $3) RETURNING id`,
		s.ID, s.ExamBankID, s.Teacher.ID)
	exec(`INSERT INTO exam_import_items (import_id, school_id, seq, kind, status, data, number) VALUES ($1, $2, 0, 'soal', 'siap', '{"kind":["esai"],"body":["Soal impor"]}', '1')`,
		s.ExamImportID, s.ID)
	s.ExamAssetID = one(`INSERT INTO exam_assets (school_id, bank_id, storage_key, content_type, width, height, size_bytes, created_by) VALUES ($1, $2, $3, 'image/png', 10, 10, 100, $4) RETURNING id`,
		s.ID, s.ExamBankID, "schools/"+s.ID+"/exam/uji.png", s.Teacher.ID)

	s.ExamID = one(`INSERT INTO exams (school_id, academic_year_id, name, purpose, subject_id, grade_level, duration_minutes, owner_id, created_by)
		VALUES ($1, $2, $3, 'ulangan', $4, 1, 60, $5, $5) RETURNING id`, s.ID, s.YearID, "Ulangan "+marker, s.SubjectID, s.Teacher.ID)
	s.ExamSectionID = one(`INSERT INTO exam_sections (school_id, exam_id, position) VALUES ($1, $2, 1) RETURNING id`, s.ID, s.ExamID)
	s.ExamItemID = one(`INSERT INTO exam_items (school_id, exam_id, section_id, position, kind, question_id) VALUES ($1, $2, $3, 1, 'soal', $4) RETURNING id`,
		s.ID, s.ExamID, s.ExamSectionID, s.ExamQuestionID)
	variant := one(`INSERT INTO exam_variants (school_id, exam_id, label, seed) VALUES ($1, $2, 'A', 1) RETURNING id`, s.ID, s.ExamID)
	exec(`INSERT INTO exam_variant_items (school_id, variant_id, position, section_id, question_id) VALUES ($1, $2, 1, $3, $4)`,
		s.ID, variant, s.ExamSectionID, s.ExamQuestionID)
	s.ExamSessionID = one(`INSERT INTO exam_sessions (school_id, exam_id, class_id, starts_at, ends_at, created_by)
		VALUES ($1, $2, $3, '2030-01-07 01:00+00', '2030-01-07 03:00+00', $4) RETURNING id`, s.ID, s.ExamID, s.ClassID, s.Teacher.ID)
	exec(`INSERT INTO exam_session_participants (school_id, session_id, student_id, participant_no, variant_id) VALUES ($1, $2, $3, 'P-1', $4)`,
		s.ID, s.ExamSessionID, s.StudentIDs[0], variant)
	exec(`INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by) VALUES ($1, $2, $3, $3)`, s.ID, s.ExamSessionID, s.Teacher.ID)

	s.ExamRoomID = one(`INSERT INTO exam_rooms (school_id, name, kind, capacity) VALUES ($1, $2, 'lab', 30) RETURNING id`, s.ID, "Lab "+marker)
	s.ExamEventID = one(`INSERT INTO exam_events (school_id, academic_year_id, name, purpose, starts_on, ends_on, grades, created_by)
		VALUES ($1, $2, $3, 'pas', '2030-01-07', '2030-01-11', '{1}', $4) RETURNING id`, s.ID, s.YearID, "PAS "+marker, s.Admin.ID)
	exec(`INSERT INTO exam_event_rooms (school_id, event_id, room_id) VALUES ($1, $2, $3)`, s.ID, s.ExamEventID, s.ExamRoomID)
	s.ExamSlotID = one(`INSERT INTO exam_event_slots (school_id, event_id, day, start_time, end_time) VALUES ($1, $2, '2030-01-07', '07:30', '09:00') RETURNING id`,
		s.ID, s.ExamEventID)
	s.ExamCellID = one(`INSERT INTO exam_event_cells (school_id, event_id, slot_id, grade_level, subject_id) VALUES ($1, $2, $3, 1, $4) RETURNING id`,
		s.ID, s.ExamEventID, s.ExamSlotID, s.SubjectID)

	practice := one(`INSERT INTO exams (school_id, name, purpose, duration_minutes, owner_id, created_by, builtin, status, results_mode,
		results_after_all, max_attempts, shuffle_questions, shuffle_options)
		VALUES ($1, 'Latihan Antarmuka Ujian', 'latihan', 30, $2, $2, true, 'terjadwal', 'nilai_dan_pembahasan', false, 5, false, false) RETURNING id`, s.ID, s.Admin.ID)
	exec(`INSERT INTO exam_sections (school_id, exam_id, position) VALUES ($1, $2, 1)`, s.ID, practice)
	s.ExamPracticeSessionID = one(`INSERT INTO exam_sessions (school_id, exam_id, class_id, label, location, starts_at, ends_at, created_by)
		VALUES ($1, $2, $3, 'Latihan', 'jarak_jauh', '2030-01-06 01:00+00', '2030-01-09 01:00+00', $4) RETURNING id`, s.ID, practice, s.ClassID, s.Admin.ID)

	var rows importer.Rows
	rows.Data = []importer.Row{{Line: 2, Values: [importer.NumColumns]string{key + "-900", "Impor " + marker, "", "", s.ClassName}}}
	s.ImportBatchID, err = importer.NewService(tx, nil).Upload(ctx, school.Actor{SchoolID: s.ID, UserID: s.Admin.ID}, "impor-"+key+".xlsx", rows, time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC))
	check(err)
	return s
}
