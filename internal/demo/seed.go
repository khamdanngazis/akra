package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/billing"
	"akra/internal/payments"
	"akra/internal/platform/settings"
	"akra/internal/school"
)

// Options mengatur sekolah demo.
type Options struct {
	Code    string // kode sekolah untuk login, mis. "demo"
	Name    string
	Address string
	// PasswordHash adalah hash argon2id dari password semua akun demo.
	PasswordHash string
	// Today adalah tanggal hari ini (Asia/Jakarta, tengah malam UTC). Data
	// dibuat relatif terhadap tanggal ini agar selalu tampak terkini.
	Today time.Time
	// Seed membuat isi acak bisa diulang.
	Seed uint64
}

// Account adalah satu akun demo.
type Account struct {
	Role  string
	Name  string
	Email string
	Note  string
}

// Result adalah ringkasan data yang dibuat.
type Result struct {
	SchoolID string
	Accounts []Account
	Counts   map[string]int
}

const (
	classesPerGrade  = 2
	studentsPerClass = 20
	sppAmount        = 250000
	activityAmount   = 150000
	voidThreshold    = 100000
	bankAccount      = "BCA 123-456-7890 a.n. SD Harapan Bangsa"
)

var (
	boyNames    = []string{"Ahmad", "Budi", "Dimas", "Fajar", "Gilang", "Hafiz", "Ilham", "Raka", "Rizky", "Yusuf", "Bagas", "Arif", "Daffa", "Farhan", "Naufal", "Rafi", "Satria", "Zaki"}
	girlNames   = []string{"Aisyah", "Bunga", "Citra", "Dewi", "Fitri", "Hana", "Intan", "Kirana", "Nabila", "Putri", "Salsabila", "Zahra", "Alya", "Keisha", "Nayla", "Rania", "Syifa", "Tiara"}
	familyNames = []string{"Pratama", "Saputra", "Lestari", "Wijaya", "Hidayat", "Nugroho", "Santoso", "Kusuma", "Ramadhan", "Maharani", "Permata", "Firmansyah", "Setiawan", "Utami", "Rahmawati", "Siregar", "Nasution", "Hakim"}
	fatherNames = []string{"Hendra", "Agus", "Bambang", "Joko", "Dedi", "Eko", "Rudi", "Andi", "Heri", "Slamet", "Wahyu", "Yudi"}
	motherNames = []string{"Siti", "Nur", "Sri", "Rina", "Yuni", "Wulan", "Dian", "Ratna", "Endang", "Lilis", "Ani", "Maya"}
	teachers    = []string{"Dra. Sri Mulyani", "Ahmad Fauzi, S.Pd.", "Nurul Hidayah, S.Pd.", "Bambang Wijaya, S.Pd.", "Rahmawati, S.Pd.", "Dedi Kurniawan, S.Pd.",
		"Siti Aminah, S.Pd.", "Yusuf Hakim, S.Pd.", "Fitriani, S.Pd.", "Agus Salim, S.Pd.", "Lestari Handayani, S.Pd.", "Hendra Gunawan, S.Pd."}
)

// payPolicy menentukan pola pembayaran siswa tertentu (akun orang tua demo).
type payPolicy int

const (
	payRandom  payPolicy = iota
	payAll               // semua tagihan lunas
	payArrears           // dua bulan terakhir belum dibayar
)

type student struct {
	id, enrollment, name, class string
	policy                      payPolicy
}

// Seed membuat sekolah demo lengkap di dalam tx: tahun ajaran, 12 kelas
// dengan wali kelas, 240 siswa dan walinya, tagihan SPP sejak awal tahun
// ajaran dan uang kegiatan, pembayaran tunai/transfer (termasuk pembatalan
// yang langsung berlaku dan yang menunggu persetujuan), absensi harian sampai
// kemarin, pengumuman, serta akun aktif untuk semua peran termasuk orang tua.
// Pembayaran dan pembatalan dibuat lewat service aplikasi, sehingga nomor
// kuitansi dan jejak audit sama seperti penggunaan sungguhan.
func Seed(ctx context.Context, tx pgx.Tx, o Options) (Result, error) {
	rng := rand.New(rand.NewPCG(o.Seed, 2026))
	res := Result{Counts: map[string]int{}}
	one := func(sql string, args ...any) (string, error) {
		var id string
		err := tx.QueryRow(ctx, sql, args...).Scan(&id)
		return id, err
	}
	domain := "@" + o.Code + ".akra.test"
	var err error
	if res.SchoolID, err = one(`INSERT INTO schools (name, code, address) VALUES ($1, $2, $3) RETURNING id`, o.Name, o.Code, o.Address); err != nil {
		return res, fmt.Errorf("demo: school %s (already exists? use -reset): %w", o.Code, err)
	}
	sid := res.SchoolID
	user := func(local, name, role, note string, guardianID any) (string, error) {
		email := local + domain
		id, err := one(`INSERT INTO users (school_id, email, password_hash, full_name, guardian_id, is_active) VALUES ($1, $2, $3, $4, $5, true) RETURNING id`,
			sid, email, o.PasswordHash, name, guardianID)
		if err != nil {
			return "", fmt.Errorf("demo: user %s: %w", email, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2`, id, role); err != nil {
			return "", err
		}
		res.Accounts = append(res.Accounts, Account{Role: role, Name: name, Email: email, Note: note})
		return id, nil
	}
	admin, err := user("tu", "Rina Marlina", "admin_tu", "Admin/TU: siswa, kelas, impor, pengumuman", nil)
	if err != nil {
		return res, err
	}
	treasurerID, err := user("bendahara", "Sri Wahyuni", "bendahara", "Bendahara: tagihan, pembayaran, kuitansi", nil)
	if err != nil {
		return res, err
	}
	const principalName = "Hasan Basri, S.Pd., M.Pd."
	principalID, err := user("kepala", principalName, "kepala_sekolah", "Kepala sekolah: ringkasan, laporan, persetujuan pembatalan", nil)
	if err != nil {
		return res, err
	}
	treasurer := school.Actor{SchoolID: sid, UserID: treasurerID}

	// Tahun ajaran: tahun lalu (selesai) dan tahun berjalan (aktif).
	startYear := o.Today.Year()
	if o.Today.Month() < time.July {
		startYear--
	}
	start := time.Date(startYear, time.July, 13, 0, 0, 0, 0, time.UTC)
	end := time.Date(startYear+1, time.June, 25, 0, 0, 0, 0, time.UTC)
	if _, err := tx.Exec(ctx, `INSERT INTO academic_years (school_id, name, start_date, end_date) VALUES ($1, $2, $3, $4)`,
		sid, fmt.Sprintf("%d/%d", startYear-1, startYear), start.AddDate(-1, 0, 0), end.AddDate(-1, 0, 0)); err != nil {
		return res, err
	}
	year, err := one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, $2, $3, $4, true) RETURNING id`,
		sid, fmt.Sprintf("%d/%d", startYear, startYear+1), start, end)
	if err != nil {
		return res, err
	}

	// Kelas 1A–6B dengan wali kelas; akun guru untuk 1A dan 4B ditandai di ringkasan.
	var classIDs, classNames, homerooms []string
	for i, t := range teachers {
		grade, letter := i/classesPerGrade+1, string(rune('A'+i%classesPerGrade))
		name := fmt.Sprintf("%d%s", grade, letter)
		note := "Wali kelas " + name
		if name == "1A" {
			note += "; UAS belum diisi (coba input nilai)"
		}
		tid, err := user("guru."+strings.ToLower(name), t, "guru", note, nil)
		if err != nil {
			return res, err
		}
		cid, err := one(`INSERT INTO classes (school_id, academic_year_id, name, grade_level, homeroom_teacher_id) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			sid, year, name, grade, tid)
		if err != nil {
			return res, err
		}
		classIDs, classNames, homerooms = append(classIDs, cid), append(classNames, name), append(homerooms, tid)
	}

	// Siswa dan wali. Beberapa siswa sengaja diberi nama tetap untuk akun
	// orang tua demo.
	fixed := map[string]struct {
		name, gender string
		policy       payPolicy
	}{
		"1B-0": {"Alya Lestari", "P", payAll},
		"2A-0": {"Kirana Wijaya", "P", payRandom},
		"3A-0": {"Naufal Saputra", "L", payArrears},
		"5B-0": {"Raka Wijaya", "L", payRandom},
	}
	var students []student
	byKey := map[string]student{}
	// Nama siswa unik di sekolah demo, termasuk terhadap nama tetap, agar
	// tidak ada dua "Raka Wijaya" saat demo.
	used := map[string]bool{}
	for _, f := range fixed {
		used[f.name] = true
	}
	nis := 0
	for ci, cname := range classNames {
		for k := range studentsPerClass {
			nis++
			key := fmt.Sprintf("%s-%d", cname, k)
			gender := []string{"L", "P"}[rng.IntN(2)]
			var first, family string
			for {
				first, family = pick(rng, boyNames), pick(rng, familyNames)
				if gender == "P" {
					first = pick(rng, girlNames)
				}
				if !used[first+" "+family] {
					break
				}
			}
			name, policy := first+" "+family, payRandom
			used[name] = true
			if f, ok := fixed[key]; ok {
				name, gender, policy = f.name, f.gender, f.policy
				family = name[strings.LastIndex(name, " ")+1:]
			}
			grade := ci/classesPerGrade + 1
			birth := time.Date(startYear-6-grade, time.Month(1+rng.IntN(12)), 1+rng.IntN(28), 0, 0, 0, 0, time.UTC)
			stID, err := one(`INSERT INTO students (school_id, nis, full_name, gender, birth_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				sid, fmt.Sprintf("%d%04d", startYear, nis), name, gender, birth)
			if err != nil {
				return res, err
			}
			enr, err := one(`INSERT INTO enrollments (school_id, student_id, class_id, academic_year_id, start_date) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				sid, stID, classIDs[ci], year, start)
			if err != nil {
				return res, err
			}
			// Kakak-adik Wijaya berbagi satu wali (dibuat di bawah).
			if key != "5B-0" {
				rel, gname := "ibu", pick(rng, motherNames)+" "+family
				if rng.IntN(3) == 0 {
					rel, gname = "ayah", pick(rng, fatherNames)+" "+family
				}
				switch key {
				case "1B-0":
					rel, gname = "ibu", "Dian Lestari"
				case "2A-0":
					rel, gname = "ibu", "Wulan Wijaya"
				case "3A-0":
					rel, gname = "ayah", "Hendra Saputra"
				}
				gid, err := one(`INSERT INTO guardians (school_id, full_name, phone) VALUES ($1, $2, $3) RETURNING id`,
					sid, gname, fmt.Sprintf("+62812%08d", 10000000+nis*37))
				if err != nil {
					return res, err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary) VALUES ($1, $2, $3, $4, true)`,
					sid, stID, gid, rel); err != nil {
					return res, err
				}
			}
			s := student{id: stID, enrollment: enr, name: name, class: cname, policy: policy}
			students = append(students, s)
			byKey[key] = s
		}
	}
	// Akun orang tua demo (aktif, tanpa email aktivasi).
	parent := func(local, studentKey, note string) error {
		var gid, gname string
		if err := tx.QueryRow(ctx, `SELECT g.id, g.full_name FROM guardians g JOIN student_guardians sg ON sg.guardian_id = g.id
			WHERE sg.student_id = $1`, byKey[studentKey].id).Scan(&gid, &gname); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE guardians SET email = $2 WHERE id = $1`, gid, local+domain); err != nil {
			return err
		}
		_, err := user(local, gname, "orang_tua", note, gid)
		return err
	}
	// Raka (5B) adik Kirana (2A): ditautkan ke wali yang sama.
	if _, err := tx.Exec(ctx, `INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship, is_primary)
		SELECT $1, $2, guardian_id, 'ibu', true FROM student_guardians WHERE student_id = $3`, sid, byKey["5B-0"].id, byKey["2A-0"].id); err != nil {
		return res, err
	}
	if err := parent("ortu.wijaya", "2A-0", "Orang tua 2 anak: Kirana (2A) dan Raka (5B)"); err != nil {
		return res, err
	}
	if err := parent("ortu.saputra", "3A-0", "Orang tua Naufal (3A), ada tunggakan"); err != nil {
		return res, err
	}
	if err := parent("ortu.lestari", "1B-0", "Orang tua Alya (1B), semua lunas"); err != nil {
		return res, err
	}
	res.Counts["siswa"] = len(students)

	// Jenis tagihan dan pengaturan keuangan.
	spp, err := billing.CreateFeeType(ctx, tx, sid, billing.FeeTypeInput{Name: "SPP", DefaultAmount: sppAmount, DefaultDueDay: 10})
	if err != nil {
		return res, err
	}
	activity, err := billing.CreateFeeType(ctx, tx, sid, billing.FeeTypeInput{Name: "Uang Kegiatan Semester Ganjil", DefaultAmount: activityAmount, DefaultDueDay: 15})
	if err != nil {
		return res, err
	}
	if err := settings.Set(ctx, tx, sid, settings.VoidApprovalThreshold, fmt.Sprint(voidThreshold)); err != nil {
		return res, err
	}

	// Tagihan: SPP setiap bulan sejak Juli sampai bulan ini; uang kegiatan sekali (Juli).
	type plannedPay struct {
		invoice string
		amount  int64
		paidOn  time.Time
		method  payments.Method
	}
	var plans []plannedPay
	months := monthsBetween(start, o.Today)
	for _, s := range students {
		for mi, m := range months {
			inv, err := billing.CreateInvoice(ctx, tx, treasurer, billing.InvoiceRequest{EnrollmentID: s.enrollment, Period: m.Format("2006-01"),
				Items: []billing.ItemRequest{{FeeTypeID: spp.ID}}})
			if err != nil {
				return res, fmt.Errorf("demo: invoice %s %s: %w", s.name, m.Format("2006-01"), err)
			}
			res.Counts["tagihan"]++
			current := mi == len(months)-1
			full, partial := 90, 4
			if current {
				full, partial = 55, 10
			}
			switch {
			case s.policy == payArrears && mi >= len(months)-2:
				continue
			case s.policy == payAll:
			default:
				roll := rng.IntN(100)
				if roll >= full+partial {
					continue
				}
				if roll >= full {
					plans = append(plans, plannedPay{inv.ID, 100000, payDate(rng, inv.DueDate, m, o.Today), method(rng)})
					continue
				}
			}
			plans = append(plans, plannedPay{inv.ID, sppAmount, payDate(rng, inv.DueDate, m, o.Today), method(rng)})
		}
		inv, err := billing.CreateInvoice(ctx, tx, treasurer, billing.InvoiceRequest{EnrollmentID: s.enrollment, Period: months[0].Format("2006-01"),
			Items: []billing.ItemRequest{{FeeTypeID: activity.ID}}})
		if err != nil {
			return res, err
		}
		res.Counts["tagihan"]++
		if roll := rng.IntN(100); s.policy == payAll || roll < 75 {
			plans = append(plans, plannedPay{inv.ID, activityAmount, payDate(rng, inv.DueDate, months[0], o.Today), method(rng)})
		} else if roll < 85 && s.policy != payArrears {
			plans = append(plans, plannedPay{inv.ID, 75000, payDate(rng, inv.DueDate, months[0], o.Today), method(rng)})
		}
	}
	// Dicatat urut tanggal bayar, seperti bendahara mencatatnya: nomor
	// kuitansi berurutan sesuai waktu.
	slices.SortStableFunc(plans, func(a, b plannedPay) int { return a.paidOn.Compare(b.paidOn) })
	var thisMonth []payments.Payment
	for i, p := range plans {
		req := payments.Request{InvoiceID: p.invoice, Amount: p.amount, PaidOn: p.paidOn, Method: p.method}
		if p.method == payments.MethodTransfer {
			req.BankAccount, req.Reference = bankAccount, fmt.Sprintf("TRF%s%04d", p.paidOn.Format("060102"), i%10000)
		}
		pay, err := payments.RecordPayment(ctx, tx, treasurer, req)
		if err != nil {
			return res, fmt.Errorf("demo: payment %d: %w", i, err)
		}
		res.Counts["pembayaran"]++
		if p.paidOn.Year() == o.Today.Year() && p.paidOn.Month() == o.Today.Month() {
			thisMonth = append(thisMonth, pay)
		}
	}

	// Waktu pencatatan pembayaran (dan audit-nya) dimundurkan ke tanggal
	// bayar, agar beranda ("pembayaran hari ini", "pembayaran terbaru") dan
	// audit tampak seperti pemakaian sehari-hari, bukan semuanya dicatat saat
	// seed berjalan.
	if res.Counts["dimundurkan"], err = backdatePayments(ctx, tx, sid); err != nil {
		return res, err
	}

	// Pembatalan: satu kecil (langsung berlaku) dan satu besar (menunggu kepala sekolah).
	voids := payments.NewVoidService(tx)
	var small, large *payments.Payment
	for i := range thisMonth {
		p := &thisMonth[i]
		if small == nil && p.Amount <= voidThreshold {
			small = p
		} else if large == nil && p.Amount > voidThreshold && p.Method == payments.MethodTransfer {
			large = p
		}
	}
	if small != nil {
		if _, err := voids.Request(ctx, treasurer, small.ID, "Salah input: pembayaran tercatat di siswa yang keliru"); err != nil {
			return res, fmt.Errorf("demo: small void: %w", err)
		}
		res.Counts["pembatalan_berlaku"]++
	}
	if large != nil {
		if _, err := voids.Request(ctx, treasurer, large.ID, "Transfer ganda, dana dikembalikan ke orang tua"); err != nil {
			return res, fmt.Errorf("demo: large void: %w", err)
		}
		res.Counts["pembatalan_menunggu"]++
	}

	// Absensi setiap hari sekolah sejak awal tahun ajaran sampai kemarin;
	// hari ini sengaja kosong agar wali kelas bisa mencoba mengisinya.
	tag, err := tx.Exec(ctx, `
		WITH s AS (
			INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by)
			SELECT c.school_id, c.academic_year_id, c.id, d::date, c.homeroom_teacher_id
			FROM classes c CROSS JOIN generate_series($2::date, $3::date - 1, interval '1 day') d
			WHERE c.school_id = $1 AND extract(isodow FROM d) < 6
			RETURNING id, school_id, class_id, created_by)
		INSERT INTO attendance_records (school_id, session_id, enrollment_id, status, updated_by)
		SELECT s.school_id, s.id, e.id,
		       CASE WHEN h < 930 THEN 'hadir' WHEN h < 965 THEN 'sakit' WHEN h < 990 THEN 'izin' ELSE 'alpa' END,
		       s.created_by
		FROM s JOIN enrollments e ON e.school_id = s.school_id AND e.class_id = s.class_id,
		     LATERAL (SELECT abs(hashtext(s.id::text || e.id::text)) % 1000 AS h) x`, sid, start, o.Today)
	if err != nil {
		return res, fmt.Errorf("demo: attendance: %w", err)
	}
	res.Counts["absensi"] = int(tag.RowsAffected())

	// MVP 2: mapel, jadwal, nilai, laporan tersimpan, mapping impor. Rata-rata
	// nilai per siswa acak; Naufal sengaja rendah (ada nilai di bawah KKM di
	// portal), Alya tinggi.
	ability := map[string]int{"Naufal Saputra": 62, "Alya Lestari": 90}
	for _, s := range students {
		if _, ok := ability[s.name]; !ok {
			ability[s.name] = 66 + rng.IntN(26)
		}
	}
	if err := seedMVP2(ctx, tx, o, rng, mvp2Input{sid: sid, year: year, admin: admin, principal: principalID, principalName: principalName,
		treasurer: treasurerID, start: start, classIDs: classIDs, classNames: classNames, homerooms: homerooms, students: students,
		ability: ability, user: user}, &res); err != nil {
		return res, err
	}

	// Pengumuman, sebagian untuk satu kelas.
	for i, a := range []struct{ title, body, class string }{
		{"Libur Maulid Nabi Muhammad SAW", "Kegiatan belajar mengajar diliburkan. Siswa masuk kembali keesokan harinya seperti biasa.", ""},
		{"Jadwal Penilaian Tengah Semester", "Penilaian Tengah Semester dilaksanakan minggu depan, Senin–Jumat. Mohon orang tua mendampingi anak belajar di rumah.", ""},
		{"Kegiatan Jumat Bersih", "Setiap Jumat pukul 07.00 siswa dan guru membersihkan lingkungan sekolah. Siswa membawa lap dan kantong sampah.", ""},
		{"Kunjungan edukasi ke museum", "Kelas 4A berkunjung ke Museum Geologi pada hari Kamis. Biaya sudah termasuk uang kegiatan.", "4A"},
		{"Pengambilan rapor tengah semester", "Rapor tengah semester dapat diambil orang tua/wali di kelas masing-masing pada hari Sabtu pukul 08.00–11.00.", ""},
	} {
		var class any
		if a.class != "" {
			class = classIDs[slices.Index(classNames, a.class)]
		}
		if _, err := tx.Exec(ctx, `INSERT INTO announcements (school_id, academic_year_id, class_id, title, body, published_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, sid, year, class, a.title, a.body, o.Today.Add(-time.Duration(4*i+1)*24*time.Hour+9*time.Hour), admin); err != nil {
			return res, err
		}
	}
	return res, nil
}

func pick(rng *rand.Rand, list []string) string { return list[rng.IntN(len(list))] }

func method(rng *rand.Rand) payments.Method {
	if rng.IntN(10) < 6 {
		return payments.MethodCash
	}
	return payments.MethodTransfer
}

// monthsBetween mengembalikan awal setiap bulan dari bulan start sampai bulan today.
func monthsBetween(start, today time.Time) []time.Time {
	var out []time.Time
	for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(today); m = m.AddDate(0, 1, 0) {
		out = append(out, m)
	}
	return out
}

// payDate memilih tanggal bayar di sekitar jatuh tempo, di dalam bulan
// tagihan dan tidak setelah hari ini.
func payDate(rng *rand.Rand, due, month, today time.Time) time.Time {
	d := due.AddDate(0, 0, rng.IntN(12)-8)
	if d.Before(month) {
		d = month
	}
	if d.After(today) {
		d = today
	}
	return d
}

// backdatePayments mengubah payments.created_at dan audit payment.create ke
// tanggal bayar pukul 08.00–14.00 WIB. Kedua tabel append-only, jadi trigger
// dimatikan sementara (session_replication_role, butuh superuser) di
// savepoint; jika tidak diizinkan, langkah ini dilewati (mengembalikan 0).
func backdatePayments(ctx context.Context, tx pgx.Tx, schoolID string) (int, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = sp.Rollback(ctx) }()
	if _, err := sp.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		return 0, nil
	}
	tag, err := sp.Exec(ctx, `
		WITH t AS (
			SELECT id, (paid_on + time '08:00' + (abs(hashtext(id::text)) % 360) * interval '1 minute') AT TIME ZONE 'Asia/Jakarta' AS at
			FROM payments WHERE school_id = $1),
		p AS (UPDATE payments SET created_at = t.at FROM t WHERE payments.id = t.id RETURNING payments.id, t.at)
		UPDATE audit_logs a SET created_at = p.at FROM p
		WHERE a.school_id = $1 AND a.action = 'payment.create' AND a.entity_id = p.id::text`, schoolID)
	if err != nil {
		return 0, fmt.Errorf("demo: backdate payments: %w", err)
	}
	if _, err := sp.Exec(ctx, `SET LOCAL session_replication_role = origin`); err != nil {
		return 0, err
	}
	if err := sp.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
