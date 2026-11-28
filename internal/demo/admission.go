package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/admission"
	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/exam/mathrender"
	"akra/internal/platform/module"
	"akra/internal/platform/pii"
	"akra/internal/school"
)

// Pendaftaran demo: SPMB tahun ajaran berikutnya dengan dua jalur (Reguler
// bertes dan Afirmasi urutan daftar), tes wawancara di kertas, dan tes
// tertulis online (ujian seleksi) dengan sesi dari rumah yang sedang dibuka,
// sehingga calon siswa demo bisa langsung mencoba ujian.

// Applicant adalah pendaftar demo untuk dicoba: cek status di
// /s/{kode}/status dengan nomor pendaftaran dan tanggal lahir, lalu masuk
// ujian di /u/{kode} dengan nomor pendaftaran dan kode dari kartu tes.
type Applicant struct {
	Name, Number, BirthDate, Status string
}

type admissionInput struct {
	sid, admin, treasurer string
	year                  time.Time // awal tahun ajaran berjalan
	math                  *mathrender.Renderer
	keys                  *pii.Keyring
}

var applicantNames = []struct {
	name, gender string
}{
	{"Alif Rahman Hakim", "L"}, {"Aqila Zahra Putri", "P"}, {"Bima Satria Nugraha", "L"}, {"Cinta Ramadhani", "P"},
	{"Daffa Alfarizi", "L"}, {"Elvina Maharani", "P"}, {"Fathan Al Ghifari", "L"}, {"Gendis Ayu Lestari", "P"},
	{"Hafidz Maulana", "L"}, {"Inayah Khairunnisa", "P"}, {"Juna Pratama", "L"}, {"Kayla Azzahra", "P"},
	{"Lutfi Hidayat", "L"}, {"Nabila Syifa", "P"},
}

// Soal tes tertulis calon siswa kelas 1: membaca, berhitung, dan logika
// sederhana.
var admissionQuestions = []url.Values{
	{"kind": {"pg"}, "body": {"Ada 3 apel dan 4 jeruk. Berapa jumlah semua buah?"}, "choice_0": {"6"}, "choice_1": {"7"}, "choice_2": {"8"}, "choice_3": {"5"}, "correct": {"1"}, "difficulty": {"mudah"}},
	{"kind": {"pg"}, "body": {"Angka yang lebih besar dari 8 adalah …"}, "choice_0": {"5"}, "choice_1": {"7"}, "choice_2": {"9"}, "choice_3": {"8"}, "correct": {"2"}, "difficulty": {"mudah"}},
	{"kind": {"pg"}, "body": {"Lanjutkan pola: 2, 4, 6, …"}, "choice_0": {"7"}, "choice_1": {"8"}, "choice_2": {"10"}, "choice_3": {"12"}, "correct": {"1"}, "difficulty": {"sedang"}},
	{"kind": {"pg"}, "body": {"Hewan yang bisa terbang adalah …"}, "choice_0": {"Kucing"}, "choice_1": {"Ikan"}, "choice_2": {"Burung"}, "choice_3": {"Sapi"}, "correct": {"2"}, "difficulty": {"mudah"}},
	{"kind": {"pg"}, "body": {"Huruf pertama kata BOLA adalah …"}, "choice_0": {"A"}, "choice_1": {"B"}, "choice_2": {"L"}, "choice_3": {"O"}, "correct": {"1"}, "difficulty": {"mudah"}},
	{"kind": {"pg"}, "body": {"Sebelum hari Rabu adalah hari …"}, "choice_0": {"Senin"}, "choice_1": {"Selasa"}, "choice_2": {"Kamis"}, "choice_3": {"Jumat"}, "correct": {"1"}, "difficulty": {"sedang"}},
	{"kind": {"benar_salah"}, "body": {"Tentukan benar atau salah."}, "statement_0": {"Matahari terbit di pagi hari."}, "truth_0": {"B"},
		"statement_1": {"Ikan hidup di pohon."}, "truth_1": {"S"}, "difficulty": {"mudah"}},
	{"kind": {"pg_kompleks"}, "body": {"Pilih semua warna pada bendera Indonesia."}, "choice_0": {"Merah"}, "choice_1": {"Biru"},
		"choice_2": {"Putih"}, "choice_3": {"Hijau"}, "correct": {"0", "2"}, "scoring": {"proporsional"}, "difficulty": {"mudah"}},
	{"kind": {"isian"}, "body": {"5 + 5 = …"}, "accepted": {"10 | sepuluh"}, "difficulty": {"mudah"}},
	{"kind": {"menjodohkan"}, "body": {"Jodohkan benda dengan kegunaannya."}, "left_0": {"Sikat gigi"}, "right_0": {"Membersihkan gigi"},
		"left_1": {"Payung"}, "right_1": {"Melindungi dari hujan"}, "distractor_0": {"Menulis"}, "difficulty": {"sedang"}},
}

// seedAdmission membuat pendaftaran demo. Tanpa modul pendaftaran tidak ada
// yang dibuat; tanpa modul ujian hanya tes wawancara di kertas.
func seedAdmission(ctx context.Context, tx pgx.Tx, o Options, rng *rand.Rand, in admissionInput, res *Result) error {
	set, err := module.Load(ctx, tx, in.sid)
	if err != nil {
		return err
	}
	if !set.Has(module.Admission) {
		return nil
	}
	// Layanan pendaftaran membaca modul aktif dari context request.
	ctx = module.With(ctx, set)
	online := set.Has(module.Exam) && in.math != nil
	actor := school.Actor{SchoolID: in.sid, UserID: in.admin}
	now := time.Now()

	// Tahun ajaran tujuan (calon siswa kelas 1 tahun depan).
	nextStart := in.year.AddDate(1, 0, 0)
	var nextYear string
	if err := tx.QueryRow(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, $2, $3, $4) RETURNING id`,
		in.sid, fmt.Sprintf("%d/%d", nextStart.Year(), nextStart.Year()+1), nextStart, nextStart.AddDate(0, 11, 12)).Scan(&nextYear); err != nil {
		return fmt.Errorf("demo: next year: %w", err)
	}
	periods := admission.NewService(tx)
	p, err := periods.Create(ctx, actor, admission.PeriodForm{AcademicYearID: nextYear,
		Name:        fmt.Sprintf("SPMB %d/%d Gelombang 1", nextStart.Year(), nextStart.Year()+1),
		TargetGrade: "1", OpensOn: o.Today.AddDate(0, 0, -14).Format(time.DateOnly), ClosesOn: o.Today.AddDate(0, 0, 45).Format(time.DateOnly),
		FeeAmount: "Rp150.000"})
	if err != nil {
		return fmt.Errorf("demo: admission period: %w", err)
	}
	regular, err := periods.AddTrack(ctx, actor, p.ID, admission.TrackForm{Name: "Reguler", Quota: "40", Method: admission.MethodTest})
	if err != nil {
		return fmt.Errorf("demo: track: %w", err)
	}
	affirm, err := periods.AddTrack(ctx, actor, p.ID, admission.TrackForm{Name: "Afirmasi", Quota: "8", Method: admission.MethodOrder})
	if err != nil {
		return fmt.Errorf("demo: track: %w", err)
	}
	if _, err := periods.SetStatus(ctx, actor, p.ID, admission.StatusOpen); err != nil {
		return fmt.Errorf("demo: open period: %w", err)
	}

	// Pendaftar: 10 jalur Reguler, 4 jalur Afirmasi. Sebagian diverifikasi
	// (peserta tes), sebagian baru dikirim, satu perlu perbaikan.
	apps := admission.NewApplicantService(tx, in.keys, periods)
	fees := admission.NewFeeService(tx)
	var verified []admission.Applicant
	for i, n := range applicantNames {
		tr := regular
		if i >= 10 {
			tr = affirm
		}
		birth := time.Date(nextStart.Year()-7, time.Month(1+rng.IntN(12)), 1+rng.IntN(28), 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		a, _, err := apps.CreateDraft(ctx, in.sid, admission.DraftForm{PeriodID: p.ID, TrackID: tr.ID, FullName: n.name, Gender: n.gender,
			BirthDate: birth, Phone: fmt.Sprintf("08123%07d", 4000000+i), Consent: true}, "")
		if err != nil {
			return fmt.Errorf("demo: applicant draft: %w", err)
		}
		for _, s := range []struct {
			step string
			v    map[string]string
		}{
			{admission.StepChild, map[string]string{"full_name": n.name, "gender": n.gender, "birth_date": birth, "nik": fmt.Sprintf("3273%012d", 100000000+i),
				"kk_number": fmt.Sprintf("3273%012d", 200000000+i), "birth_place": "Bandung", "religion": "01"}},
			{admission.StepAddress, map[string]string{"street": fmt.Sprintf("Jl. Cempaka No. %d", 3+i), "village": "Sukajadi", "district": "Sukasari", "residence_type": "01", "transport_mode": "02"}},
			{admission.StepParents, map[string]string{"ibu.full_name": "Ibu " + n.name, "ibu.nik": fmt.Sprintf("3273%012d", 300000000+i), "ibu.phone": fmt.Sprintf("08129%07d", 5000000+i), "ibu.occupation": "07"}},
			{admission.StepSchool, map[string]string{"previous_school": "TK Pelita Hati"}},
		} {
			if _, err := apps.SaveStep(ctx, in.sid, a.ID, s.step, s.v); err != nil {
				return fmt.Errorf("demo: applicant %s step %s: %w", n.name, s.step, err)
			}
		}
		if a, err = apps.Submit(ctx, in.sid, a.ID); err != nil {
			return fmt.Errorf("demo: submit applicant: %w", err)
		}
		status := admission.ApplicantSubmitted
		switch {
		case i == 8:
			if _, err := apps.Transition(ctx, actor, a.ID, admission.ApplicantFix, "Foto akta kelahiran kurang jelas, mohon unggah ulang."); err != nil {
				return err
			}
			status = admission.ApplicantFix
		case i < 7 || i == 10 || i == 11:
			if i < 5 || i == 10 {
				if _, err := fees.Record(ctx, school.Actor{SchoolID: in.sid, UserID: in.treasurer}, a.ID,
					admission.FeePaymentForm{PaidOn: o.Today.AddDate(0, 0, -7+i%5).Format(time.DateOnly), Method: "tunai"}); err != nil {
					return fmt.Errorf("demo: form fee: %w", err)
				}
			}
			if _, err := apps.Transition(ctx, actor, a.ID, admission.ApplicantVerified, ""); err != nil {
				return fmt.Errorf("demo: verify applicant: %w", err)
			}
			status = admission.ApplicantVerified
			if tr.ID == regular.ID {
				verified = append(verified, a)
			}
		}
		res.Applicants = append(res.Applicants, Applicant{Name: n.name, Number: a.RegistrationNumber, BirthDate: birth, Status: status})
	}
	res.Counts["pendaftar"] = len(applicantNames)

	// Tes: wawancara di kertas (nilai sebagian sudah diketik TU) dan tes
	// tertulis online bila modul ujian aktif.
	sel := admission.NewSelectionService(tx)
	interview, err := sel.AddTest(ctx, actor, p.ID, admission.TestForm{Name: "Wawancara", Weight: "40", MaxScore: "100",
		Rubric: "Kemandirian, komunikasi, dan kesiapan belajar.", Method: admission.TestMethodPaper})
	if err != nil {
		return fmt.Errorf("demo: interview test: %w", err)
	}
	scores := map[string]string{}
	for i, a := range verified {
		if i < 4 {
			scores[a.ID] = fmt.Sprint(70 + rng.IntN(26))
		}
	}
	if err := sel.SaveScores(ctx, actor, p.ID, interview.ID, scores); err != nil {
		return fmt.Errorf("demo: interview scores: %w", err)
	}
	if !online {
		return nil
	}
	examID, err := seedAdmissionExam(ctx, tx, in, p.ID, regular.ID, now)
	if err != nil {
		return err
	}
	if _, err := sel.AddTest(ctx, actor, p.ID, admission.TestForm{Name: "Tes Tertulis Online", Weight: "60", MaxScore: "100",
		Method: admission.TestMethodOnline, ExamID: examID}); err != nil {
		return fmt.Errorf("demo: online test: %w", err)
	}
	res.Counts["tes_online"] = 1
	return nil
}

// seedAdmissionExam membuat bank dan ujian seleksi beserta sesi dari rumah
// yang sedang dibuka (7 hari) untuk pendaftar terverifikasi jalur Reguler.
func seedAdmissionExam(ctx context.Context, tx pgx.Tx, in admissionInput, periodID, trackID string, now time.Time) (string, error) {
	a, err := exam.LoadActor(ctx, tx, auth.Identity{UserID: in.admin, SchoolID: in.sid, Roles: []string{auth.RoleAdminTU}})
	if err != nil {
		return "", err
	}
	sa := exam.StaffActor{Actor: a, SchoolID: in.sid}
	bank, err := exam.CreateBank(ctx, tx, sa, exam.BankForm{Name: "Tes Seleksi Calon Siswa Kelas 1"}, now)
	if err != nil {
		return "", fmt.Errorf("demo: admission bank: %w", err)
	}
	var ids []string
	for _, v := range admissionQuestions {
		q, _, err := exam.BuildQuestion(exam.ParseQuestionForm(v), in.math)
		if err != nil {
			return "", fmt.Errorf("demo: admission question: %w", err)
		}
		c, err := exam.CreateQuestion(ctx, tx, sa, bank.ID, q, now)
		if err != nil {
			return "", fmt.Errorf("demo: admission question: %w", err)
		}
		ids = append(ids, c.ID)
	}
	e, err := exam.CreateExam(ctx, tx, sa, exam.BasicsForm{Name: "Tes Tertulis SPMB", Purpose: string(exam.PurposeAdmission), Duration: "30"}, now)
	if err != nil {
		return "", fmt.Errorf("demo: admission exam: %w", err)
	}
	sections, err := exam.LoadContent(ctx, tx, e)
	if err != nil || len(sections) == 0 {
		return "", fmt.Errorf("demo: admission exam sections: %v", err)
	}
	if _, err := exam.AddQuestions(ctx, tx, sa, e.ID, sections[0].ID, ids, now); err != nil {
		return "", fmt.Errorf("demo: admission exam items: %w", err)
	}
	if _, err := exam.CreateAdmissionSession(ctx, tx, sa, e.ID, exam.AdmissionSlot{PeriodID: periodID, TrackID: trackID,
		Start: now.Add(-time.Hour).Truncate(time.Minute), End: now.Add(7 * 24 * time.Hour).Truncate(time.Minute), Location: "jarak_jauh"}, now); err != nil {
		return "", fmt.Errorf("demo: admission session: %w", err)
	}
	if err := exam.ScheduleExam(ctx, tx, sa, e.ID, now); err != nil {
		return "", fmt.Errorf("demo: schedule admission exam: %w", err)
	}
	return e.ID, nil
}
