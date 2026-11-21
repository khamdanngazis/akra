package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/platform/storage"
)

// Data uji yang dihasilkan seed dan dibaca run.
type seedData struct {
	Password string       `json:"password"`
	Schools  []seedSchool `json:"schools"`
}

type seedSchool struct {
	Code     string        `json:"code"`
	Admin    string        `json:"admin"`
	ExamID   string        `json:"exam_id"`
	Sessions []seedSession `json:"sessions"`
}

type seedSession struct {
	ID           string     `json:"id"`
	Class        string     `json:"class"`
	Participants []seedCard `json:"participants"`
}

type seedCard struct {
	NIS  string `json:"nis"`
	Code string `json:"code"`
}

// seed membuat sekolah uji beban "ujibeban-1".."ujibeban-N". Setiap sekolah:
// 500 siswa di 15 kelas, satu ujian ulangan 40 soal (10 bergambar) dengan
// satu sesi per kelas (pengawas: admin TU) yang mulai pada start, dan kartu
// peserta berkode. Hanya untuk database uji beban.
func seed(ctx context.Context, pool *pgxpool.Pool, store storage.Store, schools, perSchool int, start time.Time, durationMin int, out string) error {
	hasher := auth.NewHasher(auth.PasswordParams{MemoryKiB: 8192, Iterations: 1, Parallelism: 1})
	hash, err := hasher.HashPassword(seedPassword)
	if err != nil {
		return err
	}
	img, err := sampleImage()
	if err != nil {
		return err
	}
	data := seedData{Password: seedPassword}
	for n := 1; n <= schools; n++ {
		s, err := seedSchoolData(ctx, pool, store, hash, img, n, perSchool, start, durationMin)
		if err != nil {
			return fmt.Errorf("school %d: %w", n, err)
		}
		data.Schools = append(data.Schools, s)
		log.Printf("seeded %s: %d sessions", s.Code, len(s.Sessions))
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, b, 0o600)
}

const (
	seedPassword  = "Beban-Ujian-2026"
	classesPerSch = 15
	questionCount = 40
	imageCount    = 10
)

func seedSchoolData(ctx context.Context, pool *pgxpool.Pool, store storage.Store, hash string, img []byte, n, perSchool int, start time.Time, durationMin int) (seedSchool, error) {
	code := fmt.Sprintf("ujibeban-%d", n)
	s := seedSchool{Code: code, Admin: "tu@" + code + ".uji"}
	one := func(sql string, args ...any) (string, error) {
		var id string
		err := pool.QueryRow(ctx, sql, args...).Scan(&id)
		return id, err
	}
	schoolID, err := one(`INSERT INTO schools (name, code) VALUES ($1, $2) RETURNING id`, fmt.Sprintf("SMP Uji Beban %d", n), code)
	if err != nil {
		return s, err
	}
	steps := []string{
		`INSERT INTO school_settings (school_id, key, value) VALUES ($1, 'exam.enabled', 'true'), ($1, 'exam.release', 'r3')`,
		`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-13', '2027-06-25', true)`,
	}
	for _, q := range steps {
		if _, err := pool.Exec(ctx, q, schoolID); err != nil {
			return s, err
		}
	}
	admin, err := one(`INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, $3, 'TU Uji Beban', true) RETURNING id`,
		schoolID, s.Admin, hash)
	if err != nil {
		return s, err
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = 'admin_tu'`, admin); err != nil {
		return s, err
	}
	yearID, err := one(`SELECT id FROM academic_years WHERE school_id = $1`, schoolID)
	if err != nil {
		return s, err
	}
	subjectID, err := one(`INSERT INTO subjects (school_id, name, code) VALUES ($1, 'Matematika', 'MTK') RETURNING id`, schoolID)
	if err != nil {
		return s, err
	}
	// Kelas dan siswa.
	var classIDs []string
	for c := 0; c < classesPerSch; c++ {
		id, err := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level) VALUES ($1, $2, $3, 9) RETURNING id`,
			schoolID, yearID, fmt.Sprintf("9-%02d", c+1))
		if err != nil {
			return s, err
		}
		classIDs = append(classIDs, id)
	}
	if _, err := pool.Exec(ctx, `
		WITH st AS (INSERT INTO students (school_id, nis, full_name)
		            SELECT $1, $2 || lpad(g::text, 4, '0'), 'Peserta Beban ' || g FROM generate_series(1, $3) g RETURNING id, nis)
		INSERT INTO enrollments (school_id, academic_year_id, class_id, student_id, start_date)
		SELECT $1, $4, ($5::uuid[])[1 + (substr(st.nis, length(st.nis) - 3)::int - 1) % $6], st.id, '2026-07-13' FROM st`,
		schoolID, fmt.Sprintf("B%d", n), perSchool, yearID, classIDs, len(classIDs)); err != nil {
		return s, fmt.Errorf("students: %w", err)
	}
	sa := exam.StaffActor{Actor: exam.Actor{UserID: admin, Roles: []string{auth.RoleAdminTU}}, SchoolID: schoolID}
	now := time.Now()
	bank, err := exam.CreateBank(ctx, pool, sa, exam.BankForm{Name: "Bank Uji Beban", SubjectID: subjectID, GradeLevel: "9"}, now)
	if err != nil {
		return s, fmt.Errorf("bank: %w", err)
	}
	var qids []string
	for i := 0; i < questionCount; i++ {
		body := fmt.Sprintf("<p>Soal uji beban nomor %d: berapakah %d + %d?</p>", i+1, i, i+1)
		if i < imageCount {
			a, err := exam.SaveAsset(ctx, pool, store, sa, bank.ID, img)
			if err != nil {
				return s, fmt.Errorf("asset: %w", err)
			}
			// Bentuk HTML gambar keluaran editor (src diisi saat ditampilkan).
			body += fmt.Sprintf(`<p><img data-asset="%s" alt="Gambar soal %d"></p>`, a.ID, i+1)
		}
		c := func(id, t string) exam.Choice {
			return exam.Choice{ID: id, Src: "<p>" + t + "</p>", HTML: "<p>" + t + "</p>"}
		}
		q := exam.Question{Kind: exam.KindMC, BodySrc: body, BodyHTML: body, Points: 1, Status: exam.StatusReady,
			Options: exam.Options{Choices: []exam.Choice{c("a", fmt.Sprint(2*i)), c("b", fmt.Sprint(2*i+1)), c("c", fmt.Sprint(2*i+2)), c("d", fmt.Sprint(2*i+3))}},
			Key:     exam.AnswerKey{Correct: []string{"b"}}}
		created, err := exam.CreateQuestion(ctx, pool, sa, bank.ID, q, now)
		if err != nil {
			return s, fmt.Errorf("question: %w", err)
		}
		qids = append(qids, created.ID)
	}
	ex, err := exam.CreateExam(ctx, pool, sa, exam.BasicsForm{Name: "Ulangan Uji Beban", Purpose: string(exam.PurposeQuiz), SubjectID: subjectID,
		GradeLevel: "9", Duration: fmt.Sprint(durationMin)}, now)
	if err != nil {
		return s, fmt.Errorf("exam: %w", err)
	}
	s.ExamID = ex.ID
	var section string
	if err := pool.QueryRow(ctx, `SELECT id FROM exam_sections WHERE exam_id = $1 ORDER BY position LIMIT 1`, ex.ID).Scan(&section); err != nil {
		return s, fmt.Errorf("section: %w", err)
	}
	if _, err := exam.AddQuestions(ctx, pool, sa, ex.ID, section, qids, now); err != nil {
		return s, fmt.Errorf("add questions: %w", err)
	}
	var slots []exam.ClassSlot
	end := start.Add(time.Duration(durationMin+30) * time.Minute)
	for _, c := range classIDs {
		slots = append(slots, exam.ClassSlot{ClassID: c, Start: start, End: end, Location: "di_ruang"})
	}
	if _, err := exam.CreateClassSessions(ctx, pool, sa, ex.ID, slots, now); err != nil {
		return s, fmt.Errorf("sessions: %w", err)
	}
	// Pengawas tiap sesi: admin TU (bisa membuka semua layar pengawas).
	if _, err := pool.Exec(ctx, `INSERT INTO exam_session_proctors (school_id, session_id, user_id, assigned_by)
		SELECT school_id, id, $2, $2 FROM exam_sessions WHERE exam_id = $1 ON CONFLICT DO NOTHING`, ex.ID, admin); err != nil {
		return s, fmt.Errorf("proctors: %w", err)
	}
	if err := exam.ScheduleExam(ctx, pool, sa, ex.ID, now); err != nil {
		return s, fmt.Errorf("schedule: %w", err)
	}
	cards, err := exam.IssueCards(ctx, pool, sa, exam.NewIssuer(), exam.CardScope{ExamID: ex.ID}, ex.Name, "", false, now)
	if err != nil {
		return s, fmt.Errorf("cards: %w", err)
	}
	codes := map[string]string{}
	for _, c := range cards {
		codes[c.Identifier] = c.Code
	}
	rows, err := pool.Query(ctx, `SELECT x.id, c.name, st.nis FROM exam_sessions x JOIN classes c ON c.id = x.class_id
		JOIN exam_session_participants p ON p.session_id = x.id JOIN students st ON st.id = p.student_id
		WHERE x.exam_id = $1 ORDER BY c.name, st.nis`, ex.ID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	idx := map[string]int{}
	for rows.Next() {
		var sid, class, nis string
		if err := rows.Scan(&sid, &class, &nis); err != nil {
			return s, err
		}
		i, ok := idx[sid]
		if !ok {
			i = len(s.Sessions)
			idx[sid] = i
			s.Sessions = append(s.Sessions, seedSession{ID: sid, Class: class})
		}
		s.Sessions[i].Participants = append(s.Sessions[i].Participants, seedCard{NIS: nis, Code: codes[nis]})
	}
	return s, rows.Err()
}

// sampleImage membuat gambar JPEG 800×500 bergradasi dan berderau (±60 KB)
// agar ukuran gambar soal mendekati foto/diagram sungguhan.
func sampleImage() ([]byte, error) {
	m := image.NewRGBA(image.Rect(0, 0, 800, 500))
	r := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			n := uint8(r.IntN(40))
			m.Set(x, y, color.RGBA{uint8(x * 255 / 800), uint8(y * 255 / 500), 120 + n, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
