package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/auth"
	"akra/internal/grades"
	"akra/internal/importer"
	"akra/internal/payments"
	"akra/internal/platform/settings"
	"akra/internal/reports/builder"
	"akra/internal/schedule"
	"akra/internal/school"
)

// Data demo MVP 2 (T64–T82): mapel, penugasan guru, jadwal, komponen dan
// nilai, pengaturan rapor, absensi per mapel, laporan tersimpan, dan
// mapping impor. Semuanya dibuat lewat service aplikasi (validasi dan audit
// asli), kecuali absensi per mapel yang disalin dari absensi harian.

// demoSubject adalah satu mapel demo. Specialist: diajar guru mapel di semua
// kelas; selain itu diajar wali kelas.
type demoSubject struct {
	name, code string
	kkm        int
	blocks     int // jam blok per minggu (70 menit)
	specialist string
}

var demoSubjects = []demoSubject{
	{"Pendidikan Agama Islam", "PAI", 75, 1, "pai"},
	{"Pendidikan Pancasila", "PPKN", 70, 2, ""},
	{"Bahasa Indonesia", "BIN", 70, 4, ""},
	{"Matematika", "MTK", 65, 4, ""},
	{"Ilmu Pengetahuan Alam", "IPA", 70, 2, ""},
	{"Ilmu Pengetahuan Sosial", "IPS", 70, 2, ""},
	{"Seni Budaya dan Prakarya", "SBDP", 70, 1, ""},
	{"Pendidikan Jasmani", "PJOK", 70, 1, "pjok"},
	{"Bahasa Inggris", "BING", 0, 1, "inggris"},
}

// Guru mapel (bukan wali kelas).
var specialists = []struct{ key, name, note string }{
	{"pai", "Abdul Rahman, S.Pd.I.", "Guru PAI di semua kelas"},
	{"pjok", "Dimas Prasetyo, S.Pd.", "Guru PJOK di semua kelas"},
	{"inggris", "Maria Ulfa, S.Pd.", "Guru Bahasa Inggris di semua kelas"},
}

// Jam blok: Senin–Kamis empat blok, Jumat dua blok (18 blok per kelas,
// sama dengan jumlah demoSubjects.blocks).
var scheduleBlocks = func() [][3]string {
	times := [][2]string{{"07:00", "08:10"}, {"08:10", "09:20"}, {"09:45", "10:55"}, {"10:55", "12:05"}}
	var out [][3]string
	for day := 1; day <= 5; day++ {
		n := 4
		if day == 5 {
			n = 2
		}
		for _, t := range times[:n] {
			out = append(out, [3]string{strconv.Itoa(day), t[0], t[1]})
		}
	}
	return out
}()

// componentsFor mengembalikan komponen penilaian mapel (total bobot 100).
func componentsFor(code string) []grades.RowInput {
	if code == "PJOK" {
		return []grades.RowInput{{Name: "Praktik", Weight: "60", Active: true}, {Name: "Teori", Weight: "40", Active: true}}
	}
	return []grades.RowInput{{Name: "Tugas", Weight: "30", Active: true}, {Name: "UTS", Weight: "30", Active: true}, {Name: "UAS", Weight: "40", Active: true}}
}

type mvp2Input struct {
	sid, year, admin, principal, principalName, treasurer string
	start                                                 time.Time
	classIDs, classNames, homerooms                       []string
	students                                              []student
	ability                                               map[string]int // nama siswa -> rata-rata nilai
	user                                                  func(local, name, role, note string, guardianID any) (string, error)
}

func seedMVP2(ctx context.Context, tx pgx.Tx, o Options, rng *rand.Rand, in mvp2Input, res *Result) error {
	actor := school.Actor{SchoolID: in.sid, UserID: in.admin}

	// Guru mapel.
	teacherOf := map[string]string{}
	for _, s := range specialists {
		id, err := in.user("guru."+s.key, s.name, auth.RoleGuru, s.note, nil)
		if err != nil {
			return err
		}
		teacherOf[s.key] = id
	}

	// Mapel (T64, T73: KKM; 0 = memakai KKM default sekolah). Satu mapel
	// lama dinonaktifkan untuk contoh.
	subjectIDs := make([]string, len(demoSubjects))
	for i, d := range demoSubjects {
		sin := academic.SubjectInput{Name: d.name, Code: d.code}
		if d.kkm > 0 {
			sin.KKM = &d.kkm
		}
		sub, err := academic.CreateSubject(ctx, tx, in.sid, sin)
		if err != nil {
			return fmt.Errorf("demo: subject %s: %w", d.name, err)
		}
		subjectIDs[i] = sub.ID
	}
	old, err := academic.CreateSubject(ctx, tx, in.sid, academic.SubjectInput{Name: "Bahasa Daerah", Code: "BD"})
	if err != nil {
		return err
	}
	if _, err := academic.SetSubjectActive(ctx, tx, in.sid, old.ID, false); err != nil {
		return err
	}
	res.Counts["mapel"] = len(demoSubjects) + 1

	// Penugasan (T65): wali kelas untuk mapel umum, guru mapel untuk sisanya.
	assignments := academic.NewAssignmentService(tx)
	teacherFor := func(ci, si int) string {
		if k := demoSubjects[si].specialist; k != "" {
			return teacherOf[k]
		}
		return in.homerooms[ci]
	}
	for ci, cid := range in.classIDs {
		m := map[string]string{}
		for si := range demoSubjects {
			m[subjectIDs[si]] = teacherFor(ci, si)
		}
		if _, err := assignments.Save(ctx, actor, cid, m); err != nil {
			return fmt.Errorf("demo: assignments %s: %w", in.classNames[ci], err)
		}
		res.Counts["penugasan"] += len(m)
	}

	// Jadwal (T66). Guru mapel mengajar satu blok per kelas; blok kelas ci
	// digeser (ci, ci+6, ci+12) sehingga tidak ada guru yang bentrok. Blok
	// lain diisi mapel wali kelas berurutan.
	slots := schedule.NewService(tx)
	for ci, cid := range in.classIDs {
		plan := make([]int, len(scheduleBlocks))
		for b := range plan {
			plan[b] = -1
		}
		var general []int
		for si, d := range demoSubjects {
			if d.specialist == "" {
				for range d.blocks {
					general = append(general, si)
				}
				continue
			}
			for j, s := range specialists {
				if s.key == d.specialist {
					plan[(ci+6*j)%len(scheduleBlocks)] = si
				}
			}
		}
		for b := range plan {
			if plan[b] < 0 {
				plan[b], general = general[0], general[1:]
			}
		}
		for b, si := range plan {
			blk := scheduleBlocks[b]
			if _, err := slots.Create(ctx, actor, cid, schedule.Form{SubjectID: subjectIDs[si], Day: blk[0], Start: blk[1], End: blk[2]}); err != nil {
				return fmt.Errorf("demo: schedule %s %v: %w", in.classNames[ci], blk, err)
			}
			res.Counts["jadwal"]++
		}
	}

	// Komponen (T69) dan nilai (T70). Nilai diisi guru pengampu lewat
	// InputService.Save. UAS kelas 1A sengaja kosong: rekap 1A "belum
	// lengkap" dan guru 1A bisa mencoba mengisinya.
	comps := grades.NewComponentService(tx)
	input := grades.NewInputService(tx)
	for si, sid := range subjectIDs {
		if _, err := comps.Save(ctx, actor, sid, componentsFor(demoSubjects[si].code)); err != nil {
			return fmt.Errorf("demo: components %s: %w", demoSubjects[si].name, err)
		}
		list, err := grades.ListComponents(ctx, tx, in.sid, in.year, sid)
		if err != nil {
			return err
		}
		for ci, cid := range in.classIDs {
			teacher := school.Actor{SchoolID: in.sid, UserID: teacherFor(ci, si)}
			for _, c := range list {
				if in.classNames[ci] == "1A" && c.Name == "UAS" {
					continue
				}
				tg, err := grades.ResolveComponent(ctx, tx, in.sid, cid, c.ID)
				if err != nil {
					return err
				}
				scores := map[string]string{}
				for _, s := range in.students {
					if s.class == in.classNames[ci] {
						scores[s.enrollment] = demoScore(rng, in.ability[s.name], demoSubjects[si].code)
					}
				}
				if _, err := input.Save(ctx, teacher, tg, scores); err != nil {
					return fmt.Errorf("demo: grades %s %s %s: %w", in.classNames[ci], demoSubjects[si].code, c.Name, err)
				}
				res.Counts["nilai"] += len(scores)
			}
		}
	}

	// Pengaturan rapor (T73, T75) dan absensi per mapel (T68).
	for k, v := range map[string]string{
		settings.ReportPrincipalName:    in.principalName,
		settings.ReportKKMDefault:       "70",
		settings.ReportPortalComponents: "true",
		settings.AttendancePerSubject:   "true",
	} {
		if err := settings.Set(ctx, tx, in.sid, k, v); err != nil {
			return err
		}
	}
	// Absensi Matematika dua minggu terakhir sesuai jadwal, statusnya
	// disalin dari absensi harian hari itu.
	tag, err := tx.Exec(ctx, `
		WITH s AS (
			INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, subject_id, session_date, created_by)
			SELECT DISTINCT ss.school_id, ss.academic_year_id, ss.class_id, ss.subject_id, d::date, ss.teacher_id
			FROM schedule_slots ss CROSS JOIN generate_series(greatest($3::date - 14, $4::date), $3::date - 1, interval '1 day') d
			WHERE ss.school_id = $1 AND ss.subject_id = $2 AND extract(isodow FROM d) = ss.day_of_week
			RETURNING id, school_id, class_id, session_date, created_by)
		INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by)
		SELECT s.school_id, s.id, ar.enrollment_id, ar.status, s.created_by
		FROM s JOIN attendance_sessions daily ON daily.school_id = s.school_id AND daily.class_id = s.class_id
			AND daily.session_date = s.session_date AND daily.subject_id IS NULL
		JOIN attendance_records ar ON ar.school_id = daily.school_id AND ar.session_id = daily.id`,
		in.sid, subjectIDs[3], o.Today, in.start)
	if err != nil {
		return fmt.Errorf("demo: subject attendance: %w", err)
	}
	res.Counts["absensi_mapel"] = int(tag.RowsAffected())

	// Laporan tersimpan (T77): bersama dan pribadi.
	reports := builder.NewConfigService(tx, builder.Default)
	monthStart := time.Date(o.Today.Year(), o.Today.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	for _, r := range []struct {
		owner, role, name string
		shared            bool
		cfg               builder.Config
	}{
		{in.principal, auth.RoleKepalaSekolah, "Tunggakan lebih dari 30 hari", true, builder.Config{Source: "arrears",
			Columns: []string{"class_name", "student_name", "nis", "fee_types", "period", "remaining", "days_overdue"},
			Filters: []builder.Filter{{Column: "days_overdue", Op: builder.OpGte, Value: "30"}},
			Order:   []builder.Order{{Column: "class_name"}, {Column: "days_overdue", Desc: true}}}},
		{in.principal, auth.RoleKepalaSekolah, "Nilai UTS Matematika", false, builder.Config{Source: "grades",
			Columns: []string{"class_name", "student_name", "subject", "component", "score_x100"},
			Filters: []builder.Filter{{Column: "subject", Op: builder.OpEq, Value: "Matematika"}, {Column: "component", Op: builder.OpEq, Value: "UTS"}},
			Order:   []builder.Order{{Column: "score_x100", Desc: true}}}},
		{in.treasurer, auth.RoleBendahara, "Transfer bulan ini", false, builder.Config{Source: "payments",
			Columns: []string{"payment_date", "receipt_number", "student_name", "class_name", "amount", "status"},
			Filters: []builder.Filter{{Column: "method", Op: builder.OpEq, Value: string(payments.MethodTransfer)}, {Column: "payment_date", Op: builder.OpGte, Value: monthStart}},
			Order:   []builder.Order{{Column: "payment_date", Desc: true}}}},
		{in.admin, auth.RoleAdminTU, "Siswa alpa", true, builder.Config{Source: "attendance",
			Columns: []string{"session_date", "class_name", "student_name", "subject", "status"},
			Filters: []builder.Filter{{Column: "status", Op: builder.OpEq, Value: "alpa"}},
			Order:   []builder.Order{{Column: "session_date", Desc: true}, {Column: "class_name"}}}},
	} {
		id := auth.Identity{UserID: r.owner, SchoolID: in.sid, Roles: []string{r.role}}
		if _, err := reports.Create(ctx, school.Actor{SchoolID: in.sid, UserID: r.owner}, id, builder.ConfigInput{Name: r.name, Config: r.cfg, Shared: r.shared}); err != nil {
			return fmt.Errorf("demo: report config %s: %w", r.name, err)
		}
		res.Counts["laporan_tersimpan"]++
	}

	// Mapping impor (T81) untuk format ekspor sekolah lain.
	maps := importer.NewMappingService(tx)
	for _, m := range []struct {
		name, target string
		cols         importer.ColumnMap
	}{
		{"Ekspor Dapodik", importer.TargetStudents, importer.ColumnMap{"NIS": "NIPD", "Nama lengkap": "Nama", "Tanggal lahir": "Tanggal Lahir",
			"Jenis kelamin": "JK", "Kelas": "Rombel Saat Ini", "Nama wali": "Nama Ibu Kandung", "Telepon wali": "HP"}},
		{"Rekap nilai guru", importer.TargetGrades, importer.ColumnMap{"NIS": "No Induk", "Kode mapel": "Mapel", "Nama komponen": "Jenis Penilaian", "Nilai": "Skor"}},
	} {
		if _, err := maps.Create(ctx, actor, m.name, m.target, m.cols, nil); err != nil {
			return fmt.Errorf("demo: mapping %s: %w", m.name, err)
		}
		res.Counts["mapping"]++
	}
	return nil
}

// demoScore mengembalikan nilai sekitar kemampuan siswa (±10), kadang
// berkoma, sebagai isian form ("85" atau "87,5").
func demoScore(rng *rand.Rand, ability int, code string) string {
	n := ability + rng.IntN(21) - 10
	if code == "PJOK" {
		n += 5
	}
	n = max(40, min(100, n))
	if n < 100 && rng.IntN(5) == 0 {
		return strconv.Itoa(n) + ",5"
	}
	return strconv.Itoa(n)
}
